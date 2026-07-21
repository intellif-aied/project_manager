package main

import (
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

var autoSyncChooseEnable = chooseAutoSyncEnable
var autoSyncChooseTime = chooseAutoSyncTime
var autoSyncChoosePastTime = chooseAutoSyncPastTime

type autoSyncEnableModel struct {
	cursor    int
	enabled   bool
	done      bool
	cancelled bool
}

func (m autoSyncEnableModel) Init() tea.Cmd { return nil }

func (m autoSyncEnableModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
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
		m.enabled = m.cursor == 0
		m.done = true
		return m, tea.Quit
	case "q", "esc", "ctrl+c":
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

func (m autoSyncEnableModel) View() string {
	if m.done || m.cancelled {
		return ""
	}
	options := []string{"开启自动同步", "暂不开启"}
	var builder strings.Builder
	builder.WriteString("让 Aida 自动同步 Session\n\n")
	builder.WriteString("每天在你选择的时间自动上传全部 Session，日报和使用分析可以及时看到最新数据。\n")
	builder.WriteString("如果当时电脑未运行，Aida 会在恢复运行后第一时间补传。\n\n")
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

func chooseAutoSyncEnable(input io.Reader, output io.Writer) (bool, bool, error) {
	program := tea.NewProgram(
		autoSyncEnableModel{},
		tea.WithInput(input),
		tea.WithOutput(output),
	)
	finalModel, err := program.Run()
	if err != nil {
		return false, false, err
	}
	model := finalModel.(autoSyncEnableModel)
	return model.enabled, model.cancelled, nil
}

type autoSyncTimeModel struct {
	hour      int
	minute    int
	field     int
	done      bool
	cancelled bool
}

func newAutoSyncTimeModel(value string) autoSyncTimeModel {
	var hour, minute int
	if _, err := fmt.Sscanf(value, "%d:%d", &hour, &minute); err != nil ||
		hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		hour, minute = 18, 0
	}
	return autoSyncTimeModel{hour: hour, minute: minute}
}

func (m autoSyncTimeModel) Init() tea.Cmd { return nil }

func (m autoSyncTimeModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := message.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "left", "h", "shift+tab":
		m.field = 0
	case "right", "l", "tab":
		m.field = 1
	case "up", "k":
		if m.field == 0 {
			m.hour = (m.hour + 1) % 24
		} else {
			m.minute = (m.minute + 1) % 60
		}
	case "down", "j":
		if m.field == 0 {
			m.hour = (m.hour + 23) % 24
		} else {
			m.minute = (m.minute + 59) % 60
		}
	case "enter":
		m.done = true
		return m, tea.Quit
	case "q", "esc", "ctrl+c":
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

func (m autoSyncTimeModel) View() string {
	if m.done || m.cancelled {
		return ""
	}
	hour, minute := fmt.Sprintf(" %02d ", m.hour), fmt.Sprintf(" %02d ", m.minute)
	if m.field == 0 {
		hour = "[" + hour + "]"
		minute = " " + minute + " "
	} else {
		hour = " " + hour + " "
		minute = "[" + minute + "]"
	}
	return fmt.Sprintf(
		"选择每天自动同步的时间\n\nAida 将按北京时间同步 Session。\n\n小时 %s : 分钟 %s\n\n←/→ 切换 · ↑/↓ 调整 · Enter 确认 · q 取消\n",
		hour,
		minute,
	)
}

func chooseAutoSyncTime(value string, input io.Reader, output io.Writer) (string, bool, error) {
	program := tea.NewProgram(
		newAutoSyncTimeModel(value),
		tea.WithInput(input),
		tea.WithOutput(output),
	)
	finalModel, err := program.Run()
	if err != nil {
		return "", false, err
	}
	model := finalModel.(autoSyncTimeModel)
	return fmt.Sprintf("%02d:%02d", model.hour, model.minute), model.cancelled, nil
}

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
