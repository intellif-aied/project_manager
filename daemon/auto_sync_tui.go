package main

import (
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

var autoSyncChoosePastTime = chooseAutoSyncPastTime

type autoSyncStartChoice int

const (
	autoSyncStartImmediate autoSyncStartChoice = iota
	autoSyncStartTomorrow
)

type autoSyncPastTimeModel struct {
	dailyTime string
	cursor    int
	choice    autoSyncStartChoice
	done      bool
	cancelled bool
}

func newAutoSyncPastTimeModel(dailyTime string) autoSyncPastTimeModel {
	return autoSyncPastTimeModel{dailyTime: dailyTime}
}

func (m autoSyncPastTimeModel) Init() tea.Cmd { return nil }

func (m autoSyncPastTimeModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := message.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch key.String() {
	case "up", "k":
		m.cursor = 0
	case "down", "j":
		m.cursor = 1
	case "enter":
		m.choice = autoSyncStartChoice(m.cursor)
		m.done = true
		return m, tea.Quit
	case "q", "esc", "ctrl+c":
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

func (m autoSyncPastTimeModel) View() string {
	if m.done || m.cancelled {
		return ""
	}

	options := []string{
		"今天立即同步一次",
		fmt.Sprintf("从明天 %s 开始", m.dailyTime),
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "你选择的 %s 今天已经过去\n", m.dailyTime)
	builder.WriteString("请选择首次同步时间\n\n")
	for index, option := range options {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		fmt.Fprintf(&builder, "%s%s\n", cursor, option)
	}
	builder.WriteString("\n↑/↓ 选择 · Enter 下一步 · q 取消\n")
	return builder.String()
}

func chooseAutoSyncPastTime(dailyTime string, input io.Reader, output io.Writer) (autoSyncStartChoice, bool, error) {
	program := tea.NewProgram(
		newAutoSyncPastTimeModel(dailyTime),
		tea.WithInput(input),
		tea.WithOutput(output),
	)
	finalModel, err := program.Run()
	if err != nil {
		return autoSyncStartImmediate, false, err
	}
	model := finalModel.(autoSyncPastTimeModel)
	return model.choice, model.cancelled, nil
}
