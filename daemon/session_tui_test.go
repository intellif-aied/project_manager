package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

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

func TestSessionPickerShowsWorkingDirectoryAndLastActivity(t *testing.T) {
	session := &SessionInfo{
		SessionRef: "session-directory",
		AgentType:  "codex",
		Cwd:        "/home/user/projects/aida-console",
		Summary:    "修复上传交互",
		EndedAt:    time.Date(2026, 7, 16, 13, 45, 0, 0, time.Local),
	}
	model := newSessionPickerModel([]*SessionInfo{session})
	model.width = 120

	view := model.View()
	if !strings.Contains(view, "/home/user/pr../aida-console") ||
		!strings.Contains(view, "07-16 13:45:00") ||
		!strings.Contains(view, "修复上传交互") {
		t.Fatalf("view=%q", view)
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

func TestSessionPickerUsesSubAgentLabel(t *testing.T) {
	root := testGroupedSession("root", "", 1)
	root.SelectionChildren = []*SessionInfo{
		testGroupedSession("child-1", "root", 2),
		testGroupedSession("child-2", "root", 3),
	}
	model := newSessionPickerModel([]*SessionInfo{root})
	model.width = 120

	view := model.View()
	if !strings.Contains(view, "sub-agent(2)") || strings.Contains(view, "子会话") {
		t.Fatalf("view=%q", view)
	}
}
