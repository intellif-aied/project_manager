package usage

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestClaudeMonotonicSnapshotsFoldToFinalObservation(t *testing.T) {
	result := parseUsageFixture(t, "claude_monotonic.jsonl", "claude_code", ParseState{})
	if len(result.Records) != 4 || result.MalformedCount != 0 || result.UnknownUsageCount != 0 {
		t.Fatalf("result=%+v", result)
	}
	first := result.Records[0]
	advance := result.Records[1]
	if fold := FoldClaudeObservation(first, advance); fold.Action != FoldAdvance {
		t.Fatalf("advance fold=%+v", fold)
	}
	if fold := FoldClaudeObservation(result.Records[2], result.Records[3]); fold.Action != FoldDuplicate {
		t.Fatalf("duplicate fold=%+v", fold)
	}
	normalized, err := NormalizeWithOptions(advance, NormalizerOptions{ClaudeCacheWriteVariant: "5m"})
	if err != nil {
		t.Fatal(err)
	}
	if normalized.UncachedInputTokens != 120 || normalized.CacheReadTokens != 35 || normalized.CacheWrite5mTokens != 10 || normalized.OutputTokens != 25 || normalized.TotalTokens != 190 || !normalized.IsEstimated {
		t.Fatalf("normalized=%+v", normalized)
	}
	if _, err := Normalize(advance); !errors.Is(err, ErrClaudeCacheWriteVariantRequired) {
		t.Fatalf("missing variant err=%v", err)
	}
}

func TestClaudeNonMonotonicSnapshotConflicts(t *testing.T) {
	result := parseUsageFixture(t, "claude_conflict.jsonl", "claude_code", ParseState{})
	if len(result.Records) != 2 {
		t.Fatalf("records=%d", len(result.Records))
	}
	if fold := FoldClaudeObservation(result.Records[0], result.Records[1]); fold.Action != FoldConflict {
		t.Fatalf("fold=%+v", fold)
	}
}

func TestCodexCumulativeCountersProduceDisjointDeltas(t *testing.T) {
	content := usageFixture(t, "codex_cumulative.jsonl")
	lines := bytes.SplitAfter(content, []byte{'\n'})
	firstChunk := bytes.Join(lines[:3], nil)
	secondChunk := bytes.Join(lines[3:], nil)
	first, err := ParseProviderChunk("codex", bytes.NewReader(firstChunk), 0, ParseState{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ParseProviderChunk("codex", bytes.NewReader(secondChunk), int64(len(firstChunk)), first.State)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Records) != 1 || len(second.Records) != 1 || second.EndCursor != int64(len(content)) {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	firstUsage, err := Normalize(first.Records[0])
	if err != nil {
		t.Fatal(err)
	}
	secondUsage, err := Normalize(second.Records[0])
	if err != nil {
		t.Fatal(err)
	}
	if firstUsage.UncachedInputTokens != 80 || firstUsage.CacheReadTokens != 20 || firstUsage.OutputTokens != 20 || firstUsage.TotalTokens != 120 {
		t.Fatalf("first normalized=%+v", firstUsage)
	}
	if secondUsage.UncachedInputTokens != 50 || secondUsage.CacheReadTokens != 30 || secondUsage.OutputTokens != 20 || secondUsage.TotalTokens != 100 {
		t.Fatalf("second normalized=%+v", secondUsage)
	}
	if firstUsage.TotalTokens+secondUsage.TotalTokens != 220 {
		t.Fatalf("combined total=%d", firstUsage.TotalTokens+secondUsage.TotalTokens)
	}
}

func TestCodexKnownContextWindowIsExcludedFromBillableTotal(t *testing.T) {
	for _, test := range []struct {
		model         string
		contextWindow int64
	}{
		{model: "gpt-5.5", contextWindow: CodexGPT55ContextWindow},
		{model: "gpt-5.3-codex-spark", contextWindow: CodexGPT53CodexSparkContextWindow},
	} {
		t.Run(test.model, func(t *testing.T) {
			content := []byte(fmt.Sprintf(
				`{"timestamp":"2026-07-10T00:00:00Z","type":"turn_context","payload":{"model":%q}}`+"\n"+
					`{"timestamp":"2026-07-10T00:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":30,"total_tokens":%d}}}}`+"\n"+
					`{"timestamp":"2026-07-10T00:02:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":140,"cached_input_tokens":25,"output_tokens":40,"total_tokens":%d}}}}`+"\n",
				test.model, 100+30+test.contextWindow, 140+40+test.contextWindow,
			))
			parsed, err := ParseProviderChunk("codex", bytes.NewReader(content), 0, ParseState{})
			if err != nil {
				t.Fatal(err)
			}
			if len(parsed.Records) != 2 {
				t.Fatalf("records=%d", len(parsed.Records))
			}
			if parsed.Records[0].Quality == QualityConflict || parsed.Records[1].Quality == QualityConflict {
				t.Fatalf("unexpected conflict: %+v", parsed.Records)
			}
			if parsed.Records[0].Counters.TotalTokens != 130 || parsed.Records[1].Delta.TotalTokens != 50 {
				t.Fatalf("records=%+v", parsed.Records)
			}
			if normalized, err := Normalize(parsed.Records[1]); err != nil || normalized.TotalTokens != 50 {
				t.Fatalf("normalized=%+v err=%v", normalized, err)
			}
		})
	}
}

func TestCodexLastUsagePreservesLongContextRequestBoundary(t *testing.T) {
	content := []byte(
		`{"timestamp":"2026-07-10T00:00:00Z","type":"turn_context","payload":{"model":"gpt-5.6-sol"}}` + "\n" +
			`{"timestamp":"2026-07-10T00:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":272000,"cached_input_tokens":270000,"output_tokens":100,"total_tokens":272100},"last_token_usage":{"input_tokens":272000,"cached_input_tokens":270000,"output_tokens":100,"total_tokens":272100}}}}` + "\n" +
			`{"timestamp":"2026-07-10T00:02:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":544001,"cached_input_tokens":540000,"output_tokens":200,"total_tokens":544201},"last_token_usage":{"input_tokens":272001,"cached_input_tokens":270000,"output_tokens":100,"total_tokens":272101}}}}` + "\n",
	)
	parsed, err := ParseProviderChunk("codex", bytes.NewReader(content), 0, ParseState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Records) != 2 {
		t.Fatalf("records=%d", len(parsed.Records))
	}
	if parsed.Records[0].Delta.RequestInputTokens != CodexLongContextInputThreshold ||
		parsed.Records[1].Delta.RequestInputTokens != CodexLongContextInputThreshold+1 {
		t.Fatalf("request inputs=%d,%d", parsed.Records[0].Delta.RequestInputTokens, parsed.Records[1].Delta.RequestInputTokens)
	}
	if parsed.Records[1].Delta.TotalTokens != 272101 {
		t.Fatalf("second delta=%+v", parsed.Records[1].Delta)
	}
}

func TestCodexUnchangedCumulativeCountersAdvanceCursorWithoutUsageRecord(t *testing.T) {
	content := []byte(
		`{"timestamp":"2026-07-10T00:00:00Z","type":"turn_context","payload":{"model":"gpt-5.6-sol"}}` + "\n" +
			`{"timestamp":"2026-07-10T00:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":80,"output_tokens":20,"total_tokens":120}}}}` + "\n" +
			`{"timestamp":"2026-07-10T00:02:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":80,"output_tokens":20,"total_tokens":120}}}}` + "\n",
	)
	parsed, err := ParseProviderChunk("codex", bytes.NewReader(content), 0, ParseState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Records) != 1 || parsed.EndCursor != int64(len(content)) {
		t.Fatalf("records=%d end=%d want=%d", len(parsed.Records), parsed.EndCursor, len(content))
	}
	if parsed.State.PreviousCodexCounters == nil || parsed.State.PreviousCodexCounters.TotalTokens != 120 {
		t.Fatalf("state=%+v", parsed.State)
	}
}

func TestCodexNullTokenInfoIsInitializationMarker(t *testing.T) {
	content := []byte(
		`{"timestamp":"2026-07-10T00:00:00Z","type":"turn_context","payload":{"model":"gpt-5.6-sol"}}` + "\n" +
			`{"timestamp":"2026-07-10T00:00:01Z","type":"event_msg","payload":{"type":"token_count","info":null}}` + "\n" +
			`{"timestamp":"2026-07-10T00:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":80,"output_tokens":20,"total_tokens":120}}}}` + "\n",
	)
	parsed, err := ParseProviderChunk("codex", bytes.NewReader(content), 0, ParseState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Records) != 1 || parsed.UnknownUsageCount != 0 || parsed.MalformedCount != 0 ||
		parsed.EndCursor != int64(len(content)) {
		t.Fatalf("parsed=%+v", parsed)
	}
	if parsed.Records[0].Counters.TotalTokens != 120 {
		t.Fatalf("record=%+v", parsed.Records[0])
	}
}

func TestCodexForkInheritedCountersAreBaselineOnly(t *testing.T) {
	content := strings.Join([]string{
		`{"timestamp":"2026-07-16T02:00:00Z","type":"session_meta","payload":{"id":"child","timestamp":"2026-07-16T02:00:00Z","source":{"subagent":{"thread_spawn":{"parent_thread_id":"parent"}}}}}`,
		`{"timestamp":"2026-07-16T01:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":20,"total_tokens":120}}}}`,
		`{"timestamp":"2026-07-16T02:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":130,"cached_input_tokens":25,"output_tokens":30,"total_tokens":160}}}}`,
		`{"timestamp":"2026-07-16T02:02:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":140,"cached_input_tokens":27,"output_tokens":35,"total_tokens":175}}}}`,
	}, "\n") + "\n"
	parsed, err := ParseProviderChunk("codex", strings.NewReader(content), 0, ParseState{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.State.ForkParentSessionRef != "parent" || !parsed.State.ForkBaselineReady ||
		parsed.State.ForkBaselineMissing || len(parsed.Records) != 2 {
		t.Fatalf("unexpected fork state/records: state=%+v records=%d", parsed.State, len(parsed.Records))
	}
	if parsed.Records[0].Delta.TotalTokens != 40 || parsed.Records[1].Delta.TotalTokens != 15 {
		t.Fatalf("unexpected child deltas: %+v", parsed.Records)
	}
}

func TestCodexSubagentRewrittenHistoryStopsAtCommunicationBoundary(t *testing.T) {
	lines := []string{
		`{"timestamp":"2026-07-16T02:00:00Z","type":"session_meta","payload":{"id":"child","timestamp":"2026-07-16T02:00:00Z","source":{"subagent":{"thread_spawn":{"parent_thread_id":"parent"}}}}}`,
		`{"timestamp":"2026-07-16T02:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":20,"total_tokens":120}}}}`,
		`{"timestamp":"2026-07-16T02:00:02Z","type":"inter_agent_communication_metadata","payload":{"type":"inter_agent_communication_metadata","payload":{"trigger_turn":false}}}`,
		`{"timestamp":"2026-07-16T02:00:02Z","type":"session_meta","payload":{"id":"copied-parent"}}`,
		`{"timestamp":"2026-07-16T02:00:03Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":130,"cached_input_tokens":25,"output_tokens":30,"total_tokens":160}}}}`,
		`{"timestamp":"2026-07-16T02:00:04Z","type":"inter_agent_communication_metadata","payload":{"type":"inter_agent_communication_metadata","payload":{"trigger_turn":true}}}`,
		`{"timestamp":"2026-07-16T02:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":140,"cached_input_tokens":27,"output_tokens":35,"total_tokens":175}}}}`,
		`{"timestamp":"2026-07-16T02:00:06Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"cached_input_tokens":29,"output_tokens":40,"total_tokens":190}}}}`,
	}
	firstContent := strings.Join(lines[:5], "\n") + "\n"
	first, err := ParseProviderChunk("codex", strings.NewReader(firstContent), 0, ParseState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Records) != 0 || first.State.ForkBaselineReady || !first.State.ForkBaselineMissing {
		t.Fatalf("copied history was not held as baseline: state=%+v records=%d", first.State, len(first.Records))
	}
	secondContent := strings.Join(lines[5:], "\n") + "\n"
	second, err := ParseProviderChunk("codex", strings.NewReader(secondContent), int64(len(firstContent)), first.State)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Records) != 2 || !second.State.ForkBaselineReady || second.State.ForkBaselineMissing {
		t.Fatalf("unexpected boundary state/records: state=%+v records=%d", second.State, len(second.Records))
	}
	if second.Records[0].Delta.TotalTokens != 15 || second.Records[1].Delta.TotalTokens != 15 {
		t.Fatalf("copied history leaked into child usage: %+v", second.Records)
	}
}

func TestUsageParserRejectsIncompleteAndOversizedLines(t *testing.T) {
	if _, err := ParseProviderChunk("codex", bytes.NewReader([]byte(`{"type":"event_msg"}`)), 0, ParseState{}); !errors.Is(err, ErrIncompleteLine) {
		t.Fatalf("incomplete err=%v", err)
	}
	oversized := append(bytes.Repeat([]byte{'x'}, MaxUsageLineBytes+1), '\n')
	if _, err := ParseProviderChunk("codex", bytes.NewReader(oversized), 0, ParseState{}); !errors.Is(err, ErrUsageLineTooLarge) {
		t.Fatalf("oversized err=%v", err)
	}
}

func TestCodexCounterDecreaseAndModelTransitionRemainExplicit(t *testing.T) {
	content := []byte(
		`{"timestamp":"2026-07-10T00:00:00Z","type":"turn_context","payload":{"model":"gpt-first"}}` + "\n" +
			`{"timestamp":"2026-07-10T00:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":20,"total_tokens":120}}}}` + "\n" +
			`{"timestamp":"2026-07-10T00:02:00Z","type":"turn_context","payload":{"model":"gpt-second"}}` + "\n" +
			`{"timestamp":"2026-07-10T00:03:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":90,"cached_input_tokens":10,"output_tokens":25,"total_tokens":115}}}}` + "\n",
	)
	parsed, err := ParseProviderChunk("codex", bytes.NewReader(content), 0, ParseState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Records) != 2 || parsed.Records[0].RawModel != "gpt-first" || parsed.Records[1].RawModel != "gpt-second" {
		t.Fatalf("records=%+v", parsed.Records)
	}
	if parsed.Records[1].Quality != QualityConflict {
		t.Fatalf("decrease quality=%s reason=%s", parsed.Records[1].Quality, parsed.Records[1].QualityReason)
	}
}

func TestProviderParserIsInvariantAcrossLineAlignedChunkBoundaries(t *testing.T) {
	for _, test := range []struct {
		name     string
		provider string
	}{
		{name: "claude_monotonic.jsonl", provider: "claude_code"},
		{name: "codex_cumulative.jsonl", provider: "codex"},
	} {
		t.Run(test.provider, func(t *testing.T) {
			content := usageFixture(t, test.name)
			lines := bytes.SplitAfter(content, []byte{'\n'})
			if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
				lines = lines[:len(lines)-1]
			}
			whole, err := ParseProviderChunk(test.provider, bytes.NewReader(content), 0, ParseState{})
			if err != nil {
				t.Fatal(err)
			}
			for mask := 0; mask < 1<<(len(lines)-1); mask++ {
				var got ParseResult
				var state ParseState
				cursor := int64(0)
				chunkStart := 0
				for line := 0; line < len(lines); line++ {
					last := line == len(lines)-1
					if !last && mask&(1<<line) == 0 {
						continue
					}
					chunk := bytes.Join(lines[chunkStart:line+1], nil)
					parsed, parseErr := ParseProviderChunk(test.provider, bytes.NewReader(chunk), cursor, state)
					if parseErr != nil {
						t.Fatalf("mask=%b: %v", mask, parseErr)
					}
					got.Records = append(got.Records, parsed.Records...)
					got.ScannedLineCount += parsed.ScannedLineCount
					got.MalformedCount += parsed.MalformedCount
					got.UnknownUsageCount += parsed.UnknownUsageCount
					got.EndCursor = parsed.EndCursor
					state = parsed.State
					cursor = parsed.EndCursor
					chunkStart = line + 1
				}
				got.State = state
				if !reflect.DeepEqual(got, whole) {
					t.Fatalf("mask=%b chunked result differs\n got=%+v\nwant=%+v", mask, got, whole)
				}
			}
		})
	}
}

func parseUsageFixture(t *testing.T, name, provider string, state ParseState) ParseResult {
	t.Helper()
	content := usageFixture(t, name)
	result, err := ParseProviderChunk(provider, bytes.NewReader(content), 0, state)
	if err != nil {
		t.Fatal(err)
	}
	if result.EndCursor != int64(len(content)) {
		t.Fatalf("end cursor=%d want=%d", result.EndCursor, len(content))
	}
	return result
}

func usageFixture(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "v2_usage", name))
	if err != nil {
		t.Fatal(err)
	}
	return content
}
