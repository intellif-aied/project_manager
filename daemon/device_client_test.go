package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseClaudeJSONLContinuesAfterElevenMiBEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large-event.jsonl")
	largeText := strings.Repeat("x", 11<<20)
	content := strings.Join([]string{
		`{"type":"user","sessionId":"large-session","timestamp":"2026-07-17T01:00:00Z","message":{"content":[{"type":"text","text":"first request"}]}}`,
		fmt.Sprintf(`{"type":"assistant","sessionId":"large-session","timestamp":"2026-07-17T01:01:00Z","message":{"model":"test-model","usage":{"input_tokens":7,"output_tokens":3},"content":[{"type":"text","text":"%s"}]}}`, largeText),
		`{"type":"user","sessionId":"large-session","timestamp":"2026-07-17T01:02:00Z","message":{"content":[{"type":"text","text":"final request"}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	session := parseJSONL(path)
	if session == nil {
		t.Fatal("parseJSONL returned nil")
	}
	if session.NumLines != 3 || session.TotalTok != 10 || session.ParseWarningCount != 0 {
		t.Fatalf("lines=%d tokens=%d warnings=%d", session.NumLines, session.TotalTok, session.ParseWarningCount)
	}
	if session.RecentSummary != "final request" || session.EndedAt.Format(time.RFC3339) != "2026-07-17T01:02:00Z" {
		t.Fatalf("recent=%q ended=%s", session.RecentSummary, session.EndedAt)
	}
}

func TestRemovedSessionsCommandReturnsUsageError(t *testing.T) {
	if code := run([]string{"sessions"}); code != 2 {
		t.Fatalf("exit code=%d, want 2", code)
	}
}

func TestSessionUploadRequestTimeoutIsLongerThanDefault(t *testing.T) {
	if sessionUploadRequestTimeout <= defaultRequestTimeout {
		t.Fatalf("session upload timeout = %s, want longer than %s", sessionUploadRequestTimeout, defaultRequestTimeout)
	}
}

func TestDoRequestWithTimeoutUsesRequestedLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(80 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	shortReq, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doRequestWithTimeout(shortReq, 20*time.Millisecond); err == nil {
		t.Fatal("short request unexpectedly succeeded")
	}

	longReq, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doRequestWithTimeout(longReq, time.Second); err != nil {
		t.Fatalf("long request failed: %v", err)
	}
}

func TestFormatSessionListRowIncludesIdentifyingFields(t *testing.T) {
	start := time.Date(2026, 7, 2, 14, 21, 0, 0, time.UTC)
	s := &SessionInfo{
		SessionRef: "019f21f0-faff-7ed2-bed5-0e794805c733",
		AgentType:  "codex",
		ProjectDir: "project-manager",
		StartedAt:  start,
		EndedAt:    start.Add(18 * time.Minute),
		Model:      "gpt-5.4-mini",
		Summary:    "排查 AIDA upload 待上传列表信息不足",
		TotalTok:   42300,
	}

	row := formatSessionListRow(1, s)
	for _, want := range []string{
		"codex",
		"2026-07-02 14:39",
		"42.3K",
		"18m",
		"project-manager",
		"019f21f0-faff-7ed2-bed5-0e794805c733",
		"排查 AIDA upload",
	} {
		if !strings.Contains(row, want) {
			t.Fatalf("formatSessionListRow() missing %q in %q", want, row)
		}
	}
}

func TestSortSessionsNewestFirstUsesLastActivity(t *testing.T) {
	now := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	longRunning := &SessionInfo{SessionRef: "old-start-recent-activity", StartedAt: now.Add(-48 * time.Hour), EndedAt: now}
	newerStart := &SessionInfo{SessionRef: "new-start-old-activity", StartedAt: now.Add(-time.Hour), EndedAt: now.Add(-30 * time.Minute)}
	sessions := []*SessionInfo{newerStart, longRunning}

	sortSessionsNewestFirst(sessions)
	if sessions[0] != longRunning {
		t.Fatalf("first session = %s, want %s", sessions[0].SessionRef, longRunning.SessionRef)
	}
}

func TestTruncateKeepsUTF8Valid(t *testing.T) {
	got := truncate("当前aida 客户端的mac版本遇到问题了", 12)
	if !strings.Contains(got, "...") {
		t.Fatalf("truncate() = %q, want ellipsis", got)
	}
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("truncate() produced replacement rune: %q", got)
	}
}

func TestFormatSessionListRowDefaultsClaudeAndCwd(t *testing.T) {
	s := &SessionInfo{
		SessionRef: "session-1",
		Cwd:        "/Users/alice/dev/project-manager",
		Summary:    "本地 Claude 会话",
	}

	row := formatSessionListRow(2, s)
	for _, want := range []string{"claude", "session-1", "project-manager", "本地 Claude 会话"} {
		if !strings.Contains(row, want) {
			t.Fatalf("formatSessionListRow() missing %q in %q", want, row)
		}
	}
}

func TestCollectSessionsWithFilesSkipsDuplicateSubagentRefs(t *testing.T) {
	dir := t.TempDir()
	parentPath := filepath.Join(dir, "parent.jsonl")
	sameSubPath := filepath.Join(dir, "same-sub.jsonl")
	distinctSubPath := filepath.Join(dir, "distinct-sub.jsonl")

	writeClaudeSession := func(path, ref string) {
		t.Helper()
		line := `{"type":"user","sessionId":"` + ref + `","timestamp":"2026-07-02T09:00:00Z","message":{"content":[{"type":"text","text":"hello"}]}}` + "\n"
		if err := os.WriteFile(path, []byte(line), 0600); err != nil {
			t.Fatalf("write test session: %v", err)
		}
	}
	writeClaudeSession(parentPath, "same-session")
	writeClaudeSession(sameSubPath, "same-session")
	writeClaudeSession(distinctSubPath, "distinct-session")

	items := collectSessionsWithFiles(&SessionInfo{
		SessionRef: "same-session",
		FilePath:   parentPath,
		SubFiles:   []string{sameSubPath, distinctSubPath},
	})

	if len(items) != 2 {
		t.Fatalf("items len = %d, want parent + one distinct subagent", len(items))
	}
	if items[0].info.SessionRef != "same-session" || items[1].info.SessionRef != "distinct-session" {
		t.Fatalf("unexpected refs: %#v", []string{items[0].info.SessionRef, items[1].info.SessionRef})
	}
}
