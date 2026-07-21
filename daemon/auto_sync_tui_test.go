package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

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
