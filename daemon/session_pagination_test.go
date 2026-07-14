package main

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"testing"
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
