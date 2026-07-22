package main

import (
	"strings"
	"testing"
	"time"

	"github.com/aidashboard/daemon/internal/sessionadapter"
	tea "github.com/charmbracelet/bubbletea"
)

func TestOpenClawUsesSharedTUIPickerWithoutSelectAll(t *testing.T) {
	descriptors := []sessionadapter.Descriptor{
		{ClientType: "openclaw", NativeSessionRef: "openclaw-session-1", Summary: "first"},
		{ClientType: "openclaw", NativeSessionRef: "openclaw-session-2", Summary: "second"},
	}
	sessions, _ := additionalSessionsForPicker("openclaw", descriptors)
	model := newSessionPickerModelWithOptions(sessions, additionalSessionSelectionOptions("openclaw"))
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	if len(model.selected) != 0 {
		t.Fatalf("OpenClaw select-all selected=%d", len(model.selected))
	}
	view := model.View()
	if !strings.Contains(view, "Aida Session 上传") ||
		!strings.Contains(view, "openclaw openclaw-session-1") ||
		!strings.Contains(view, "不支持全选，请逐项选择") {
		t.Fatalf("view=%q", view)
	}
}

func TestSessionPickerSelectsAcrossFilteredResults(t *testing.T) {
	model := newSessionPickerModel(fakeSessionList(20))
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, value := range "session-01" {
		model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{value}})
	}
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model.Update(tea.KeyMsg{Type: tea.KeySpace})

	selected := model.selectedSessions()
	if len(selected) != 2 || selected[0].SessionRef != "session-010" || selected[1].SessionRef != "session-011" {
		t.Fatalf("selected=%+v", selected)
	}
}

func TestSessionPickerSelectAllAppliesToSearchResults(t *testing.T) {
	model := newSessionPickerModel(fakeSessionList(30))
	model.query = "session-02"
	model.applyFilter()
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	if len(model.selected) != 10 {
		t.Fatalf("selected=%d want=10", len(model.selected))
	}
	if view := model.View(); !strings.Contains(view, "已选择 10 条") {
		t.Fatalf("view=%q", view)
	}
}

func TestSessionPickerShowsTwoLineSummaryAndFullSessionID(t *testing.T) {
	session := &SessionInfo{
		SessionRef:    "c5030a93-271a-44d4-9afe-125d9b32a850",
		AgentType:     "codex",
		Cwd:           "/home/user/projects/aida-console",
		Summary:       "修复上传交互",
		RecentSummary: "重新上传并复查统计",
		EndedAt:       time.Date(2026, 7, 16, 13, 45, 0, 0, time.Local),
	}
	model := newSessionPickerModel([]*SessionInfo{session})
	model.width = 120

	view := model.View()
	if !strings.Contains(view, "07-16 13:45:00") ||
		!strings.Contains(view, "/home/user/pro") || !strings.Contains(view, "aida-console") ||
		!strings.Contains(view, "修复上传交互") ||
		!strings.Contains(view, "codex   c5030a93-271a-44d4-9afe-125d9b32a850  最新消息：重新上传并复查统计") {
		t.Fatalf("view=%q", view)
	}
	lines := strings.Split(view, "\n")
	var summaryColumn, latestColumn int
	for _, line := range lines {
		if index := strings.Index(line, "修复上传交互"); index >= 0 {
			summaryColumn = displayWidth(line[:index])
		}
		if index := strings.Index(line, "重新上传并复查统计"); index >= 0 {
			latestColumn = displayWidth(line[:index])
		}
	}
	if summaryColumn == 0 || summaryColumn != latestColumn {
		t.Fatalf("summary columns differ: first=%d latest=%d view=%q", summaryColumn, latestColumn, view)
	}
}

func TestSessionPickerSeparatesSessionsWithBlankLine(t *testing.T) {
	sessions := fakeSessionList(2)
	sessions[0].RecentSummary = "first latest message"
	model := newSessionPickerModel(sessions)
	model.width = 120

	view := model.View()
	firstLatest := "first latest message"
	secondSummary := compactSessionText(model.sessions[1].Summary)
	if !strings.Contains(view, firstLatest+"\n\n") {
		t.Fatalf("first Session is not followed by a blank line:\n%s", view)
	}
	if !strings.Contains(view, secondSummary) {
		t.Fatalf("second Session missing from view:\n%s", view)
	}
	if got := model.visibleRows(); got != 7 {
		t.Fatalf("visible rows=%d, want 7 for three-line Session entries", got)
	}
}

func TestSessionPickerSearchesChildAgentPath(t *testing.T) {
	root := testGroupedSession("root", "", 1)
	child := testGroupedSession("child", "root", 2)
	child.ForkAgentPath = "/root/final_architecture_audit"
	root.SelectionChildren = []*SessionInfo{child}

	model := newSessionPickerModel([]*SessionInfo{root})
	model.query = "final_architecture_audit"
	model.applyFilter()

	if len(model.filtered) != 1 || model.filtered[0] != 0 {
		t.Fatalf("filtered=%+v", model.filtered)
	}
}

func TestSessionPickerHidesSubAgentLabel(t *testing.T) {
	root := testGroupedSession("root", "", 1)
	root.SelectionChildren = []*SessionInfo{
		testGroupedSession("child-1", "root", 2),
		testGroupedSession("child-2", "root", 3),
	}
	model := newSessionPickerModel([]*SessionInfo{root})
	model.width = 120

	view := model.View()
	if strings.Contains(view, "sub-agent") || strings.Contains(view, "子会话") {
		t.Fatalf("view=%q", view)
	}
}
