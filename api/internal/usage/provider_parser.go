package usage

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const MaxUsageLineBytes = 500 << 20

const CodexLongContextInputThreshold int64 = 272000

// Codex token_count.total_tokens includes the model context window for these
// two production models. The context window is provider metadata, not a
// billable usage component, so it must be removed before cumulative deltas
// are built. Unknown model/window combinations remain a quality conflict.
const (
	CodexGPT55ContextWindow           int64 = 258400
	CodexGPT53CodexSparkContextWindow int64 = 950000
)

var (
	ErrUsageLineTooLarge = errors.New("usage parser line exceeds limit")
	ErrIncompleteLine    = errors.New("usage parser chunk ends with an incomplete JSONL line")
)

type QualityStatus string

const (
	QualityExact      QualityStatus = "exact"
	QualityEstimated  QualityStatus = "estimated"
	QualityIncomplete QualityStatus = "incomplete"
	QualityConflict   QualityStatus = "conflict"
)

type TokenCounters struct {
	InputTokens         int64 `json:"input_tokens"`
	CachedInputTokens   int64 `json:"cached_input_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_output_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
	RequestInputTokens  int64 `json:"request_input_tokens,omitempty"`
}

type ParseState struct {
	PreviousCodexCounters *TokenCounters `json:"previous_codex_counters,omitempty"`
	ActiveModel           string         `json:"active_model,omitempty"`
	CounterSegment        int64          `json:"counter_segment"`
	RootMetadataSeen      bool           `json:"root_metadata_seen,omitempty"`
	ForkParentSessionRef  string         `json:"fork_parent_session_ref,omitempty"`
	ForkSource            string         `json:"fork_source,omitempty"`
	ForkedAt              *time.Time     `json:"forked_at,omitempty"`
	ForkBaselineReady     bool           `json:"fork_baseline_ready,omitempty"`
	ForkBaselineMissing   bool           `json:"fork_baseline_missing,omitempty"`
	ForkMetadataConflict  bool           `json:"fork_metadata_conflict,omitempty"`
}

type UsageRecord struct {
	Provider            string
	EventKey            string
	IdentityStrategy    string
	ProviderFingerprint string
	SourceStartCursor   int64
	SourceEndCursor     int64
	OccurredAt          time.Time
	RawModel            string
	RawUsage            json.RawMessage
	RawUsageHash        string
	Counters            TokenCounters
	Delta               TokenCounters
	Quality             QualityStatus
	QualityReason       string
}

type ParseResult struct {
	Records           []UsageRecord
	EndCursor         int64
	ScannedLineCount  int64
	MalformedCount    int64
	UnknownUsageCount int64
	State             ParseState
}

func ParseProviderChunk(
	provider string,
	reader io.Reader,
	startCursor int64,
	state ParseState,
) (ParseResult, error) {
	provider = normalizeProvider(provider)
	if provider == "" || reader == nil || startCursor < 0 {
		return ParseResult{}, errors.New("supported provider, reader, and non-negative cursor are required")
	}
	buffered := bufio.NewReaderSize(reader, 64<<10)
	result := ParseResult{EndCursor: startCursor, State: state}
	for {
		line, err := readUsageLine(buffered)
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return ParseResult{}, err
		}
		start := result.EndCursor
		result.EndCursor += int64(len(line))
		result.ScannedLineCount++
		record, kind := parseProviderLine(provider, line, start, result.EndCursor, &result.State)
		switch kind {
		case lineMalformed:
			result.MalformedCount++
		case lineUnknownUsage:
			result.UnknownUsageCount++
		case lineUsage:
			result.Records = append(result.Records, record)
		}
	}
}

type parsedLineKind int

const (
	lineIgnored parsedLineKind = iota
	lineMalformed
	lineUnknownUsage
	lineUsage
)

type providerEnvelope struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
	Payload   json.RawMessage `json:"payload"`
}

func parseProviderLine(
	provider string,
	line []byte,
	startCursor, endCursor int64,
	state *ParseState,
) (UsageRecord, parsedLineKind) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return UsageRecord{}, lineIgnored
	}
	var envelope providerEnvelope
	if err := json.Unmarshal(trimmed, &envelope); err != nil || envelope.Type == "" {
		return UsageRecord{}, lineMalformed
	}
	switch provider {
	case "claude_code":
		return parseClaudeLine(envelope, line, startCursor, endCursor, state)
	case "codex":
		return parseCodexLine(envelope, line, startCursor, endCursor, state)
	default:
		return UsageRecord{}, lineIgnored
	}
}

type claudeUsage struct {
	InputTokens         *int64 `json:"input_tokens"`
	OutputTokens        *int64 `json:"output_tokens"`
	CacheCreationTokens *int64 `json:"cache_creation_input_tokens"`
	CacheReadTokens     *int64 `json:"cache_read_input_tokens"`
}

type claudeMessage struct {
	ID    string          `json:"id"`
	Model string          `json:"model"`
	Usage json.RawMessage `json:"usage"`
}

func parseClaudeLine(
	envelope providerEnvelope,
	rawLine []byte,
	startCursor, endCursor int64,
	state *ParseState,
) (UsageRecord, parsedLineKind) {
	if envelope.Type != "assistant" {
		return UsageRecord{}, lineIgnored
	}
	var message claudeMessage
	if len(envelope.Message) == 0 || json.Unmarshal(envelope.Message, &message) != nil {
		return UsageRecord{}, lineUnknownUsage
	}
	if message.Model != "" && message.Model != "<synthetic>" {
		state.ActiveModel = message.Model
	}
	if len(message.Usage) == 0 || bytes.Equal(bytes.TrimSpace(message.Usage), []byte("null")) {
		return UsageRecord{}, lineIgnored
	}
	var usage claudeUsage
	if json.Unmarshal(message.Usage, &usage) != nil {
		return UsageRecord{}, lineUnknownUsage
	}
	if usage.InputTokens == nil || usage.OutputTokens == nil {
		return UsageRecord{}, lineUnknownUsage
	}
	counters := TokenCounters{
		InputTokens: *usage.InputTokens, OutputTokens: *usage.OutputTokens,
		CacheCreationTokens: optionalInt64(usage.CacheCreationTokens),
		CachedInputTokens:   optionalInt64(usage.CacheReadTokens),
	}
	if countersHaveNegative(counters) {
		return UsageRecord{}, lineUnknownUsage
	}
	counters.TotalTokens = counters.InputTokens + counters.CachedInputTokens + counters.CacheCreationTokens + counters.OutputTokens
	rawUsage := compactJSON(message.Usage)
	lineHash := hashBytes(rawLine)
	eventKey := "claude:message:" + strings.TrimSpace(message.ID)
	identity := "message.id"
	fingerprint := eventKey
	quality := QualityExact
	reason := ""
	if strings.TrimSpace(message.ID) == "" {
		eventKey = fmt.Sprintf("claude:cursor:%d:%d:%s", startCursor, endCursor, lineHash)
		fingerprint = eventKey
		identity = "cursor_hash_fallback"
		quality = QualityEstimated
		reason = "assistant usage has no message.id"
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, envelope.Timestamp)
	if err != nil {
		return UsageRecord{}, lineUnknownUsage
	}
	model := message.Model
	if model == "" || model == "<synthetic>" {
		model = state.ActiveModel
	}
	return UsageRecord{
		Provider: "claude_code", EventKey: eventKey, IdentityStrategy: identity,
		ProviderFingerprint: fingerprint, SourceStartCursor: startCursor, SourceEndCursor: endCursor,
		OccurredAt: occurredAt.UTC(), RawModel: model, RawUsage: rawUsage, RawUsageHash: hashBytes(rawUsage),
		Counters: counters, Delta: counters, Quality: quality, QualityReason: reason,
	}, lineUsage
}

type codexTokenInfo struct {
	Total json.RawMessage `json:"total_token_usage"`
	Last  json.RawMessage `json:"last_token_usage"`
}

type codexTokenCounters struct {
	InputTokens       *int64 `json:"input_tokens"`
	CachedInputTokens *int64 `json:"cached_input_tokens"`
	OutputTokens      *int64 `json:"output_tokens"`
	ReasoningTokens   *int64 `json:"reasoning_output_tokens"`
	TotalTokens       *int64 `json:"total_tokens"`
}

func parseCodexLine(
	envelope providerEnvelope,
	rawLine []byte,
	startCursor, endCursor int64,
	state *ParseState,
) (UsageRecord, parsedLineKind) {
	if envelope.Type == "session_meta" {
		if !state.RootMetadataSeen {
			state.RootMetadataSeen = true
			var meta codexRootSessionMeta
			if json.Unmarshal(envelope.Payload, &meta) == nil {
				state.ForkParentSessionRef, state.ForkSource = codexForkParent(meta)
				if state.ForkParentSessionRef != "" {
					if state.ForkSource == "source.subagent.thread_spawn.parent_thread_id" {
						state.ForkBaselineMissing = true
					}
					timestamp := strings.TrimSpace(meta.Timestamp)
					if timestamp == "" {
						timestamp = strings.TrimSpace(envelope.Timestamp)
					}
					if forkedAt, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
						forkedAt = forkedAt.UTC()
						state.ForkedAt = &forkedAt
					}
				}
			}
		}
		return UsageRecord{}, lineIgnored
	}
	if envelope.Type == "inter_agent_communication_metadata" {
		if state.ForkParentSessionRef != "" &&
			state.ForkSource == "source.subagent.thread_spawn.parent_thread_id" &&
			codexInterAgentTriggerTurn(envelope.Payload) &&
			!state.ForkBaselineReady {
			state.ForkBaselineReady = true
			state.ForkBaselineMissing = false
		}
		return UsageRecord{}, lineIgnored
	}
	if envelope.Type == "turn_context" {
		var context struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(envelope.Payload, &context) == nil && context.Model != "" {
			state.ActiveModel = context.Model
		}
		return UsageRecord{}, lineIgnored
	}
	if envelope.Type != "event_msg" {
		return UsageRecord{}, lineIgnored
	}
	var event struct {
		Type string          `json:"type"`
		Info json.RawMessage `json:"info"`
	}
	if json.Unmarshal(envelope.Payload, &event) != nil {
		return UsageRecord{}, lineMalformed
	}
	if event.Type != "token_count" {
		return UsageRecord{}, lineIgnored
	}
	var info codexTokenInfo
	if bytes.Equal(bytes.TrimSpace(event.Info), []byte("null")) {
		return UsageRecord{}, lineIgnored
	}
	if len(event.Info) == 0 || json.Unmarshal(event.Info, &info) != nil || len(info.Total) == 0 {
		return UsageRecord{}, lineUnknownUsage
	}
	var total codexTokenCounters
	if json.Unmarshal(info.Total, &total) != nil ||
		total.InputTokens == nil || total.OutputTokens == nil || total.TotalTokens == nil {
		return UsageRecord{}, lineUnknownUsage
	}
	counters := TokenCounters{
		InputTokens: *total.InputTokens, CachedInputTokens: optionalInt64(total.CachedInputTokens),
		OutputTokens: *total.OutputTokens, ReasoningTokens: optionalInt64(total.ReasoningTokens),
		TotalTokens: *total.TotalTokens,
	}
	if len(info.Last) > 0 {
		var last codexTokenCounters
		if json.Unmarshal(info.Last, &last) == nil && last.InputTokens != nil && *last.InputTokens >= 0 {
			counters.RequestInputTokens = *last.InputTokens
		}
	}
	if countersHaveNegative(counters) || counters.CachedInputTokens > counters.InputTokens {
		return UsageRecord{}, lineUnknownUsage
	}
	if contextWindow := codexContextWindowTokens(state.ActiveModel); contextWindow > 0 &&
		counters.TotalTokens-counters.InputTokens-counters.OutputTokens == contextWindow {
		// Keep the provider payload in raw_usage_json, but normalize the parsed
		// cumulative counter so deltas and billing use actual usage components.
		counters.TotalTokens = counters.InputTokens + counters.OutputTokens
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, envelope.Timestamp)
	if err != nil {
		return UsageRecord{}, lineUnknownUsage
	}
	occurredAt = occurredAt.UTC()
	if state.ForkParentSessionRef != "" && state.ForkedAt != nil {
		if occurredAt.Before(*state.ForkedAt) {
			state.PreviousCodexCounters = &counters
			state.ForkBaselineReady = true
			state.ForkBaselineMissing = false
			return UsageRecord{}, lineIgnored
		}
		if state.ForkSource == "source.subagent.thread_spawn.parent_thread_id" &&
			state.ForkBaselineMissing && !state.ForkBaselineReady {
			state.PreviousCodexCounters = &counters
			return UsageRecord{}, lineIgnored
		}
		if !state.ForkBaselineReady {
			state.PreviousCodexCounters = &counters
			state.ForkBaselineReady = true
			state.ForkBaselineMissing = true
			return UsageRecord{}, lineIgnored
		}
	}
	delta := counters
	quality := QualityEstimated
	reason := "Codex token_count has no stable cross-source provider event id"
	if state.PreviousCodexCounters != nil {
		delta = subtractCounters(counters, *state.PreviousCodexCounters)
		if countersHaveNegative(delta) {
			quality = QualityConflict
			reason = "cumulative token counters decreased without a verified reset boundary"
		}
	}
	if counters.TotalTokens != counters.InputTokens+counters.OutputTokens {
		quality = QualityConflict
		reason = "provider total_tokens does not equal input_tokens + output_tokens"
	}
	if state.ForkMetadataConflict {
		quality = QualityConflict
		reason = "Codex fork parent metadata conflicts with the stored session"
	} else if state.ForkBaselineMissing && quality != QualityConflict {
		quality = QualityEstimated
		reason = "Codex fork inherited baseline was unavailable; the first observed cumulative counter was not billed"
	}
	state.PreviousCodexCounters = &counters
	if delta.InputTokens == 0 && delta.CachedInputTokens == 0 && delta.OutputTokens == 0 && delta.TotalTokens == 0 {
		return UsageRecord{}, lineIgnored
	}
	rawUsage := compactJSON(info.Total)
	lineHash := hashBytes(rawLine)
	eventKey := fmt.Sprintf("codex:segment:%d:cursor:%d:%d", state.CounterSegment, startCursor, endCursor)
	return UsageRecord{
		Provider: "codex", EventKey: eventKey, IdentityStrategy: "generation_cursor",
		ProviderFingerprint: eventKey + ":" + lineHash,
		SourceStartCursor:   startCursor, SourceEndCursor: endCursor,
		OccurredAt: occurredAt, RawModel: state.ActiveModel,
		RawUsage: rawUsage, RawUsageHash: hashBytes(rawUsage), Counters: counters, Delta: delta,
		Quality: quality, QualityReason: reason,
	}, lineUsage
}

func codexContextWindowTokens(model string) int64 {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "gpt-5.5":
		return CodexGPT55ContextWindow
	case "gpt-5.3-codex-spark":
		return CodexGPT53CodexSparkContextWindow
	default:
		return 0
	}
}

func codexInterAgentTriggerTurn(payload json.RawMessage) bool {
	var marker struct {
		TriggerTurn bool `json:"trigger_turn"`
		Payload     struct {
			TriggerTurn bool `json:"trigger_turn"`
		} `json:"payload"`
	}
	return json.Unmarshal(payload, &marker) == nil && (marker.TriggerTurn || marker.Payload.TriggerTurn)
}

type codexRootSessionMeta struct {
	ID             string          `json:"id"`
	Timestamp      string          `json:"timestamp"`
	ForkedFromID   string          `json:"forked_from_id"`
	ParentThreadID string          `json:"parent_thread_id"`
	SessionID      string          `json:"session_id"`
	ThreadSource   json.RawMessage `json:"thread_source"`
	Source         json.RawMessage `json:"source"`
}

func codexForkParent(meta codexRootSessionMeta) (string, string) {
	if parent := nestedRawString(meta.Source, "subagent", "thread_spawn", "parent_thread_id"); parent != "" {
		return parent, "source.subagent.thread_spawn.parent_thread_id"
	}
	if parent := strings.TrimSpace(meta.ForkedFromID); parent != "" {
		return parent, "forked_from_id"
	}
	if parent := strings.TrimSpace(meta.ParentThreadID); parent != "" {
		return parent, "parent_thread_id"
	}
	if rawIndicatesSubagent(meta.ThreadSource) {
		if parent := strings.TrimSpace(meta.SessionID); parent != "" && parent != strings.TrimSpace(meta.ID) {
			return parent, "thread_source.subagent.session_id"
		}
	}
	return "", ""
}

func nestedRawString(raw json.RawMessage, path ...string) string {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return ""
	}
	for _, key := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		value = object[key]
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func rawIndicatesSubagent(raw json.RawMessage) bool {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.EqualFold(strings.TrimSpace(text), "subagent")
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) == nil {
		_, ok := object["subagent"]
		return ok
	}
	return false
}

func normalizeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude", "claude_code", "claude-code":
		return "claude_code"
	case "codex":
		return "codex"
	default:
		return ""
	}
}

func subtractCounters(current, previous TokenCounters) TokenCounters {
	return TokenCounters{
		InputTokens:         current.InputTokens - previous.InputTokens,
		CachedInputTokens:   current.CachedInputTokens - previous.CachedInputTokens,
		CacheCreationTokens: current.CacheCreationTokens - previous.CacheCreationTokens,
		OutputTokens:        current.OutputTokens - previous.OutputTokens,
		ReasoningTokens:     current.ReasoningTokens - previous.ReasoningTokens,
		TotalTokens:         current.TotalTokens - previous.TotalTokens,
		RequestInputTokens:  current.RequestInputTokens,
	}
}

func countersHaveNegative(counters TokenCounters) bool {
	return counters.InputTokens < 0 || counters.CachedInputTokens < 0 || counters.CacheCreationTokens < 0 ||
		counters.OutputTokens < 0 || counters.ReasoningTokens < 0 || counters.TotalTokens < 0 || counters.RequestInputTokens < 0
}

func optionalInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func compactJSON(raw json.RawMessage) json.RawMessage {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return append(json.RawMessage(nil), raw...)
	}
	return append(json.RawMessage(nil), compact.Bytes()...)
}

func readUsageLine(reader *bufio.Reader) ([]byte, error) {
	var line bytes.Buffer
	for {
		fragment, err := reader.ReadSlice('\n')
		if line.Len()+len(fragment) > MaxUsageLineBytes {
			return nil, ErrUsageLineTooLarge
		}
		line.Write(fragment)
		switch {
		case err == nil:
			return line.Bytes(), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && line.Len() == 0:
			return nil, io.EOF
		case errors.Is(err, io.EOF):
			return nil, ErrIncompleteLine
		default:
			return nil, err
		}
	}
}

func hashBytes(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}
