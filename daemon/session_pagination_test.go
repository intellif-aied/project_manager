package main

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSessionPaginationUsesStableGlobalIndices(t *testing.T) {
	sessions := fakeSessionList(45)
	page, err := paginateSessions(sessions, 2, 20)
	if err != nil {
		t.Fatal(err)
	}
	if page.Offset != 20 || len(page.Items) != 20 || page.TotalPages != 3 || page.Items[0].SessionRef != "session-021" {
		t.Fatalf("page=%+v first=%q", page, page.Items[0].SessionRef)
	}
}

func TestInteractiveSessionSelectionPersistsAcrossPages(t *testing.T) {
	sessions := fakeSessionList(25)
	input := bufio.NewReader(strings.NewReader("1\nn\n21\np\ndone\n"))
	var output bytes.Buffer
	selected, err := selectSessionsInteractively(sessions, 20, input, &output)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 || selected[0].SessionRef != "session-001" || selected[1].SessionRef != "session-021" {
		t.Fatalf("selected=%+v", selected)
	}
	if !strings.Contains(output.String(), "第 2 / 2 页") || !strings.Contains(output.String(), "已选择 2 条") {
		t.Fatalf("output does not show page/selection state:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "d 完成") {
		t.Fatalf("interactive prompt should use the single-letter completion command: %q", output.String())
	}
	if strings.Count(output.String(), "\x1b[H\x1b[2J") < 1 {
		t.Fatalf("pagination should clear the previous page before rendering, output=%q", output.String())
	}
}

func TestInteractiveSessionSelectionUsesTenRowsByDefaultAtCallSite(t *testing.T) {
	if defaultSessionPageSize != 10 {
		t.Fatalf("default session page size = %d, want 10", defaultSessionPageSize)
	}
}

func TestSessionPageRowsStaySingleLineWhenSummaryContainsMarkdown(t *testing.T) {
	session := fakeSessionList(1)[0]
	session.Summary = "第一行\n\n第二行  - 明细\n第三行"
	page, err := paginateSessions([]*SessionInfo{session}, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writeSessionPage(&output, "Session 列表", page, nil)
	if strings.Contains(output.String(), "第一行\n") {
		t.Fatalf("summary must be compacted to one row: %q", output.String())
	}
	if !strings.Contains(output.String(), "第一行 第二行 - 明细 第三行") {
		t.Fatalf("compacted summary missing: %q", output.String())
	}
}

func TestInteractiveSelectAllCoversEveryPage(t *testing.T) {
	sessions := fakeSessionList(37)
	input := bufio.NewReader(strings.NewReader("all\ndone\n"))
	selected, err := selectSessionsInteractively(sessions, 20, input, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != len(sessions) || selected[len(selected)-1].SessionRef != "session-037" {
		t.Fatalf("selected=%d last=%q", len(selected), selected[len(selected)-1].SessionRef)
	}
}

func TestSES008FiveHundredSessionsSupportPagingJumpAndCrossPageSelection(t *testing.T) {
	sessions := fakeSessionList(500)
	input := bufio.NewReader(strings.NewReader("1\ng 25\n250\ns 100\ng 5\n500\nd\n"))
	var output bytes.Buffer
	selected, err := selectSessionsInteractively(sessions, 10, input, &output)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 3 || selected[0].SessionRef != "session-001" ||
		selected[1].SessionRef != "session-250" || selected[2].SessionRef != "session-500" {
		t.Fatalf("selected=%+v", selected)
	}
	for _, expected := range []string{"第 25 / 50 页", "第 5 / 5 页", "已选择 3 条"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("500-session output missing %q", expected)
		}
	}
}

func TestSessionPaginationRejectsUnsupportedPageSize(t *testing.T) {
	if _, err := paginateSessions(fakeSessionList(1), 1, 25); err == nil {
		t.Fatal("expected unsupported page size to fail")
	}
}

func TestNarrowSessionListKeepsSummaryAndDetailFields(t *testing.T) {
	t.Setenv("COLUMNS", "80")
	session := fakeSessionList(1)[0]
	session.ProjectDir = "project-manager"
	session.Model = "gpt-test"
	session.TotalTok = 1234
	session.SubFiles = []string{"subagent.jsonl"}
	page, err := paginateSessions([]*SessionInfo{session}, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writeSessionPage(&output, "Session 列表", page, nil)
	for _, expected := range []string{"session-001", "summary 1", "project-manager", "gpt-test", "1.2K Token", "1 sub-agent"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("narrow output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestSessionsJSONIncludesSummaryDiagnosticsAndPagination(t *testing.T) {
	sessions := fakeSessionList(2)
	sessions[0].SummaryStatus = "ok"
	sessions[0].SummarySource = "event_msg.user_message"
	sessions[1].Summary = ""
	var output bytes.Buffer
	if err := writeSessionsJSON(&output, sessions, 1, 20); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"summary_status":"ok"`, `"summary_source":"event_msg.user_message"`,
		`"summary_status":"empty"`, `"summary":"暂无摘要"`, `"total":2`,
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("JSON output missing %s: %s", expected, output.String())
		}
	}
}

func TestSessionsJSONIncludesGroupMetadataAndLatestActivity(t *testing.T) {
	root := testGroupedSession("root", "", 1)
	child := testGroupedSession("child", "root", 2)
	root.SelectionChildren = []*SessionInfo{child}
	root.SelectionActiveAt = time.Date(2026, 7, 16, 12, 34, 0, 0, time.UTC)

	var output bytes.Buffer
	if err := writeSessionsJSON(&output, []*SessionInfo{root}, 1, 20); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"last_activity_at":"2026-07-16T12:34:00Z"`,
		`"subagent_count":1`,
		`"member_session_refs":["child"]`,
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("JSON output missing %s: %s", expected, output.String())
		}
	}
}

func fakeSessionList(count int) []*SessionInfo {
	sessions := make([]*SessionInfo, 0, count)
	for index := 1; index <= count; index++ {
		sessions = append(sessions, &SessionInfo{
			SessionRef: fmt.Sprintf("session-%03d", index),
			AgentType:  "codex",
			Summary:    fmt.Sprintf("summary %d", index),
		})
	}
	return sessions
}
