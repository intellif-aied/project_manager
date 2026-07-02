package main

import (
	"strings"
	"testing"
	"time"
)

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
		"2026-07-02 14:21",
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
