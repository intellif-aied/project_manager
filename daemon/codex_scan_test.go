package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseCodexJSONLForkUsesRootMetadataAndExcludesInheritedCounters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-fork.jsonl")
	lines := []string{
		`{"timestamp":"2026-07-16T02:00:00Z","type":"session_meta","payload":{"id":"child-session","timestamp":"2026-07-16T02:00:00Z","cwd":"/child","source":{"subagent":{"thread_spawn":{"parent_thread_id":"parent-session"}}}}}`,
		tokenCountLine("2026-07-16T01:00:00Z", 100),
		`{"timestamp":"2026-07-16T01:00:01Z","type":"session_meta","payload":{"id":"parent-session","timestamp":"2026-07-16T01:00:00Z","cwd":"/parent"}}`,
		tokenCountLine("2026-07-16T02:01:00Z", 120),
		tokenCountLine("2026-07-16T02:02:00Z", 130),
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	session := parseCodexJSONL(path)
	if session.SessionRef != "child-session" || session.ParentSessionRef != "parent-session" ||
		session.Cwd != "/child" || session.ForkSource != "source.subagent.thread_spawn.parent_thread_id" {
		t.Fatalf("unexpected fork metadata: %+v", session)
	}
	if session.StartedAt.Format(time.RFC3339) != "2026-07-16T02:00:00Z" || session.TotalTok != 30 {
		t.Fatalf("fork start/token total: start=%s total=%d", session.StartedAt, session.TotalTok)
	}
}

func TestParseCodexJSONLCounterResetFallsBackToLastActivity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-test.jsonl")
	lines := []string{
		`{"timestamp":"2026-07-01T01:00:00Z","type":"session_meta","payload":{"id":"session-reset","timestamp":"2026-07-01T01:00:00Z","cwd":"/tmp/project"}}`,
		tokenCountLine("2026-07-01T01:01:00Z", 100),
		tokenCountLine("2026-07-02T01:00:00Z", 10),
		tokenCountLine("2026-07-02T01:01:00Z", 120),
		tokenCountLine("2026-07-02T01:02:00Z", 130),
	}
	data := []byte(lines[0] + "\n" + lines[1] + "\n" + lines[2] + "\n" + lines[3] + "\n" + lines[4] + "\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	session := parseCodexJSONL(path)
	if session == nil {
		t.Fatal("parseCodexJSONL returned nil")
	}
	if session.TotalTok != 130 {
		t.Fatalf("TotalTok = %d, want 130", session.TotalTok)
	}
	var total int64
	for _, slice := range session.ActivitySlices {
		total += slice.TotalTokens
		if !slice.IsEstimated || slice.TokenSliceStrategy != "session_total_last_activity" {
			t.Fatalf("unexpected fallback metadata: %+v", slice)
		}
	}
	if total != session.TotalTok {
		t.Fatalf("slice total = %d, session total = %d", total, session.TotalTok)
	}
	last := session.ActivitySlices[len(session.ActivitySlices)-1]
	if last.ActivityDate != "2026-07-02" || last.TotalTokens != 130 {
		t.Fatalf("last slice = %+v", last)
	}
}

func tokenCountLine(timestamp string, total int64) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"cached_input_tokens":0,"output_tokens":0,"reasoning_output_tokens":0,"total_tokens":%d}}}}`, timestamp, total, total)
}
