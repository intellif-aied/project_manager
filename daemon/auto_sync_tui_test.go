package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEnableChoiceDefaultsToEnableAndContinues(t *testing.T) {
	model := autoSyncEnableModel{}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(autoSyncEnableModel)

	if !model.done || !model.enabled || model.cancelled {
		t.Fatalf("enable choice = %+v, want enabled and done", model)
	}
	view := (autoSyncEnableModel{}).View()
	if !strings.Contains(view, "日报和使用分析可以及时看到最新数据") ||
		!strings.Contains(view, "> 开启自动同步") {
		t.Fatalf("enable view = %q", view)
	}
}

func TestTimeChoiceUsesArrowKeysWithoutTextInput(t *testing.T) {
	model := newAutoSyncTimeModel("18:00")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(autoSyncTimeModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(autoSyncTimeModel)
	for range 5 {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
		model = updated.(autoSyncTimeModel)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(autoSyncTimeModel)

	if !model.done || model.hour != 19 || model.minute != 5 {
		t.Fatalf("time choice = %+v, want 19:05 and done", model)
	}
}

func TestPastTimeChoiceMovesToTomorrowAndContinues(t *testing.T) {
	model := newAutoSyncPastTimeModel("18:00")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(autoSyncPastTimeModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(autoSyncPastTimeModel)

	if !model.done || model.choice != autoSyncStartTomorrow {
		t.Fatalf("past-time choice = %+v, want tomorrow and done", model)
	}
}
