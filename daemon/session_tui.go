package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

type sessionPickerModel struct {
	sessions         []*SessionInfo
	filtered         []int
	selected         map[int]bool
	cursor           int
	width            int
	height           int
	query            string
	searching        bool
	cancelled        bool
	done             bool
	selectionOptions sessionSelectionOptions
}

func newSessionPickerModel(sessions []*SessionInfo) *sessionPickerModel {
	return newSessionPickerModelWithOptions(sessions, defaultSessionSelectionOptions())
}

func newSessionPickerModelWithOptions(sessions []*SessionInfo, options sessionSelectionOptions) *sessionPickerModel {
	model := &sessionPickerModel{
		sessions: sessions, selected: map[int]bool{}, width: 120, height: 28, selectionOptions: options,
	}
	model.applyFilter()
	return model
}

func (m *sessionPickerModel) Init() tea.Cmd {
	return nil
}

func (m *sessionPickerModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = max(60, msg.Width)
		m.height = max(14, msg.Height)
	case tea.KeyMsg:
		if m.searching {
			switch msg.String() {
			case "esc":
				m.searching = false
			case "enter":
				m.searching = false
			case "backspace", "ctrl+h":
				runes := []rune(m.query)
				if len(runes) > 0 {
					m.query = string(runes[:len(runes)-1])
					m.applyFilter()
				}
			case "ctrl+c":
				m.cancelled = true
				return m, tea.Quit
			default:
				if msg.Type == tea.KeyRunes {
					m.query += string(msg.Runes)
					m.applyFilter()
				}
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c", "q":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			m.done = true
			return m, tea.Quit
		case "/":
			m.searching = true
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "pgup":
			m.move(-m.visibleRows())
		case "pgdown":
			m.move(m.visibleRows())
		case "home":
			m.cursor = 0
		case "end":
			if len(m.filtered) > 0 {
				m.cursor = len(m.filtered) - 1
			}
		case " ":
			if len(m.filtered) > 0 {
				index := m.filtered[m.cursor]
				m.selected[index] = !m.selected[index]
				if !m.selected[index] {
					delete(m.selected, index)
				}
			}
		case "a":
			if !m.selectionOptions.AllowSelectAll {
				break
			}
			allSelected := len(m.filtered) > 0
			for _, index := range m.filtered {
				if !m.selected[index] {
					allSelected = false
					break
				}
			}
			for _, index := range m.filtered {
				if allSelected {
					delete(m.selected, index)
				} else {
					m.selected[index] = true
				}
			}
		}
	}
	return m, nil
}

func (m *sessionPickerModel) View() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Aida Session 上传  共 %d 条 · 已选择 %d 条\n", len(m.filtered), len(m.selected))
	if m.searching {
		fmt.Fprintf(&builder, "搜索: %s_\n", m.query)
	} else if m.query != "" {
		fmt.Fprintf(&builder, "搜索: %s  (/ 修改)\n", m.query)
	} else {
		builder.WriteString("搜索: / 输入关键词\n")
	}
	builder.WriteString(strings.Repeat("-", sessionPickerLineWidth(m.width)) + "\n")

	if len(m.filtered) == 0 {
		builder.WriteString("没有匹配的 Session。\n")
	} else {
		start, end := m.visibleRange()
		for position := start; position < end; position++ {
			index := m.filtered[position]
			session := m.sessions[index]
			cursor := " "
			if position == m.cursor {
				cursor = ">"
			}
			check := "[ ]"
			if m.selected[index] {
				check = "[x]"
			}
			activeAt := "-"
			if value := sessionSelectionLastActiveAt(session); !value.IsZero() {
				activeAt = value.Format("01-02 15:04:05")
			}
			lineWidth := sessionPickerLineWidth(m.width)
			pathWidth := max(16, min(36, lineWidth/4))
			firstPrefix := fmt.Sprintf("%s %s %s  %-*s  ", cursor, check, activeAt,
				pathWidth, truncateMiddle(sessionPathDisplay(session), pathWidth))
			agentLabel := truncateToDisplayWidth(firstNonEmpty(session.AgentType, "claude"), 12)
			agentWidth := max(7, displayWidth(agentLabel))
			lastPrefix := fmt.Sprintf("      %s %s  最新消息：",
				padToDisplayWidth(agentLabel, agentWidth), firstNonEmpty(session.SessionRef, "-"))
			contentColumn := max(displayWidth(firstPrefix), displayWidth(lastPrefix))
			firstPrefix = padToDisplayWidth(firstPrefix, contentColumn)
			lastPrefix = padToDisplayWidth(lastPrefix, contentColumn)
			contentWidth := max(8, lineWidth-contentColumn)
			fmt.Fprintf(&builder, "%s%s\n", firstPrefix,
				truncateToDisplayWidth(compactSessionText(firstNonEmpty(session.Summary, "暂无摘要")), contentWidth))
			fmt.Fprintf(&builder, "%s%s\n", lastPrefix,
				truncateToDisplayWidth(firstNonEmpty(displayRecentSummary(session), "暂无消息"), contentWidth))
			if position+1 < end {
				builder.WriteByte('\n')
			}
		}
	}
	builder.WriteString(strings.Repeat("-", sessionPickerLineWidth(m.width)) + "\n")
	if m.selectionOptions.AllowSelectAll {
		builder.WriteString("↑↓/j k 移动  Space 选择  / 搜索  a 全选结果  Enter 上传  q 取消\n")
	} else {
		builder.WriteString("↑↓/j k 移动  Space 选择  / 搜索  Enter 上传  q 取消\n")
		fmt.Fprintf(&builder, "%s\n", firstNonEmpty(m.selectionOptions.SelectAllDisabledNotice, "当前客户端不支持全选，请逐项选择 Session"))
	}
	return builder.String()
}

func sessionPickerLineWidth(width int) int {
	return max(20, min(width, 168))
}

func padToDisplayWidth(value string, width int) string {
	return value + strings.Repeat(" ", max(0, width-displayWidth(value)))
}

func truncateToDisplayWidth(value string, width int) string {
	return runewidth.Truncate(value, width, "...")
}

func displayWidth(value string) int {
	return runewidth.StringWidth(value)
}

func sessionPathDisplay(session *SessionInfo) string {
	if session == nil {
		return "-"
	}
	return firstNonEmpty(session.Cwd, session.ProjectDir, "-")
}

func (m *sessionPickerModel) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(m.query))
	m.filtered = m.filtered[:0]
	for index, session := range m.sessions {
		searchable := strings.ToLower(sessionSelectionSearchText(session))
		if query == "" || strings.Contains(searchable, query) {
			m.filtered = append(m.filtered, index)
		}
	}
	if len(m.filtered) == 0 {
		m.cursor = 0
	} else if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
}

func (m *sessionPickerModel) move(delta int) {
	if len(m.filtered) == 0 {
		return
	}
	m.cursor = max(0, min(m.cursor+delta, len(m.filtered)-1))
}

func (m *sessionPickerModel) visibleRows() int {
	return max(2, (m.height-7)/3)
}

func (m *sessionPickerModel) visibleRange() (int, int) {
	rows := m.visibleRows()
	start := max(0, m.cursor-rows+1)
	if m.cursor < rows {
		start = 0
	}
	end := min(len(m.filtered), start+rows)
	return start, end
}

func (m *sessionPickerModel) selectedSessions() []*SessionInfo {
	indices := make([]int, 0, len(m.selected))
	for index := range m.selected {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	result := make([]*SessionInfo, 0, len(indices))
	for _, index := range indices {
		result = append(result, m.sessions[index])
	}
	return result
}

func terminalSupportsTUI(input, output *os.File) bool {
	if input == nil || output == nil {
		return false
	}
	inputInfo, inputErr := input.Stat()
	outputInfo, outputErr := output.Stat()
	return inputErr == nil && outputErr == nil &&
		inputInfo.Mode()&os.ModeCharDevice != 0 &&
		outputInfo.Mode()&os.ModeCharDevice != 0
}

func selectSessionsWithTUI(sessions []*SessionInfo) ([]*SessionInfo, error) {
	return selectSessionsWithTUIOptions(sessions, defaultSessionSelectionOptions())
}

func selectSessionsWithTUIOptions(sessions []*SessionInfo, options sessionSelectionOptions) ([]*SessionInfo, error) {
	model := newSessionPickerModelWithOptions(sessions, options)
	finalModel, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		return nil, err
	}
	final, ok := finalModel.(*sessionPickerModel)
	if !ok || final.cancelled || !final.done {
		return nil, nil
	}
	return final.selectedSessions(), nil
}
