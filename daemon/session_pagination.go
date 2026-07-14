package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultSessionPageSize = 20

var allowedSessionPageSizes = map[int]bool{10: true, 20: true, 50: true, 100: true}

type sessionListPage struct {
	Items      []*SessionInfo
	Page       int
	PageSize   int
	Total      int
	TotalPages int
	Offset     int
}

func paginateSessions(sessions []*SessionInfo, page, pageSize int) (sessionListPage, error) {
	if !allowedSessionPageSizes[pageSize] {
		return sessionListPage{}, errors.New("page size must be one of 10, 20, 50, or 100")
	}
	totalPages := (len(sessions) + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}
	if page < 1 || page > totalPages {
		return sessionListPage{}, fmt.Errorf("page must be between 1 and %d", totalPages)
	}
	offset := (page - 1) * pageSize
	end := min(offset+pageSize, len(sessions))
	return sessionListPage{
		Items: sessions[offset:end], Page: page, PageSize: pageSize,
		Total: len(sessions), TotalPages: totalPages, Offset: offset,
	}, nil
}

func writeSessionPage(output io.Writer, title string, page sessionListPage, selected map[int]bool) {
	fmt.Fprintf(output, "\n%s  第 %d / %d 页，共 %d 条", title, page.Page, page.TotalPages, page.Total)
	if selected != nil {
		fmt.Fprintf(output, "，已选择 %d 条", len(selected))
	}
	fmt.Fprintln(output)
	narrow := sessionTerminalWidth() < 120
	if !narrow {
		writeSessionListHeader(output)
	}
	for index, session := range page.Items {
		globalIndex := page.Offset + index + 1
		marker := "   "
		if selected != nil {
			marker = "[ ]"
			if selected[globalIndex] {
				marker = "[x]"
			}
		}
		if narrow {
			writeNarrowSessionRow(output, marker, globalIndex, session)
		} else {
			fmt.Fprintf(output, "%s%s\n", marker, formatSessionListRow(globalIndex, session))
		}
		if !narrow && len(session.SubFiles) > 0 {
			fmt.Fprintf(output, "        %-38s %d sub-agent(s)\n", "", len(session.SubFiles))
		}
	}
}

func sessionTerminalWidth() int {
	if width, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && width > 0 {
		return width
	}
	return 160
}

func writeNarrowSessionRow(output io.Writer, marker string, index int, session *SessionInfo) {
	activeAt := "-"
	if value := session.LastActiveAt(); !value.IsZero() {
		activeAt = value.Format("01-02 15:04")
	}
	duration := "-"
	if value := session.Duration(); value > 0 {
		duration = fmt.Sprintf("%dm", int(value.Minutes()))
	}
	agent := firstNonEmpty(session.AgentType, "claude")
	fmt.Fprintf(output, "%s %-4d %-6s %-11s %s\n", marker, index, truncate(agent, 6), activeAt, firstNonEmpty(session.SessionRef, "-"))
	fmt.Fprintf(output, "    %s\n", truncate(firstNonEmpty(session.Summary, "暂无摘要"), max(20, sessionTerminalWidth()-6)))
	fmt.Fprintf(output, "    %s · %s · %s · %s Token · %d sub-agent\n",
		truncateMiddle(sessionProjectDisplay(session), 20), truncateMiddle(firstNonEmpty(session.Model, "-"), 14),
		duration, session.FormatTokens(), len(session.SubFiles))
}

type sessionJSONItem struct {
	Index          int      `json:"index"`
	SessionRef     string   `json:"session_ref"`
	AgentType      string   `json:"agent_type"`
	LastActivityAt string   `json:"last_activity_at,omitempty"`
	Summary        string   `json:"summary"`
	SummaryStatus  string   `json:"summary_status"`
	SummarySource  string   `json:"summary_source,omitempty"`
	Project        string   `json:"project"`
	CWD            string   `json:"cwd,omitempty"`
	Model          string   `json:"model,omitempty"`
	Models         []string `json:"models,omitempty"`
	DurationSecs   int64    `json:"duration_secs"`
	LocalTokens    int64    `json:"local_token_preview"`
	SubagentCount  int      `json:"subagent_count"`
}

func writeSessionsJSON(output io.Writer, sessions []*SessionInfo, pageNumber, pageSize int) error {
	page, err := paginateSessions(sessions, pageNumber, pageSize)
	if err != nil {
		return err
	}
	items := make([]sessionJSONItem, 0, len(page.Items))
	for offset, session := range page.Items {
		status := session.SummaryStatus
		if status == "" {
			if strings.TrimSpace(session.Summary) == "" {
				status = "empty"
			} else {
				status = "ok"
			}
		}
		lastActivity := ""
		if value := session.LastActiveAt(); !value.IsZero() {
			lastActivity = value.UTC().Format(time.RFC3339Nano)
		}
		items = append(items, sessionJSONItem{
			Index: page.Offset + offset + 1, SessionRef: session.SessionRef,
			AgentType: firstNonEmpty(session.AgentType, "claude_code"), LastActivityAt: lastActivity,
			Summary: firstNonEmpty(session.Summary, "暂无摘要"), SummaryStatus: status, SummarySource: session.SummarySource,
			Project: sessionProjectDisplay(session), CWD: session.Cwd, Model: session.Model, Models: session.Models,
			DurationSecs: int64(session.Duration().Seconds()), LocalTokens: session.TotalTok, SubagentCount: len(session.SubFiles),
		})
	}
	return json.NewEncoder(output).Encode(map[string]any{
		"sessions": items,
		"pagination": map[string]int{
			"page": page.Page, "page_size": page.PageSize, "total": page.Total, "total_pages": page.TotalPages,
		},
	})
}

func selectSessionsInteractively(
	sessions []*SessionInfo,
	pageSize int,
	reader *bufio.Reader,
	output io.Writer,
) ([]*SessionInfo, error) {
	if reader == nil || output == nil {
		return nil, errors.New("interactive input and output are required")
	}
	pageNumber := 1
	selected := map[int]bool{}
	for {
		page, err := paginateSessions(sessions, pageNumber, pageSize)
		if err != nil {
			return nil, err
		}
		writeSessionPage(output, "选择要上传的 Session", page, selected)
		fmt.Fprintf(output, "命令：编号切换选择，n 下一页，p 上一页，g <页码> 跳转，s <10|20|50|100> 每页条数，all 选择全部 %d 条，done 确认，q 取消\n> ", len(sessions))
		input, readErr := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if readErr != nil && input == "" {
			return nil, readErr
		}
		lower := strings.ToLower(input)
		switch {
		case lower == "q" || lower == "quit":
			return nil, nil
		case lower == "done" || lower == "d":
			indices := make([]int, 0, len(selected))
			for index := range selected {
				indices = append(indices, index)
			}
			sort.Ints(indices)
			result := make([]*SessionInfo, 0, len(indices))
			for _, index := range indices {
				result = append(result, sessions[index-1])
			}
			return result, nil
		case lower == "all" || lower == "a":
			for index := range sessions {
				selected[index+1] = true
			}
		case lower == "n" || lower == "next":
			if pageNumber < page.TotalPages {
				pageNumber++
			}
		case lower == "p" || lower == "prev":
			if pageNumber > 1 {
				pageNumber--
			}
		case strings.HasPrefix(lower, "g "):
			target, parseErr := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(lower, "g ")))
			if parseErr != nil || target < 1 || target > page.TotalPages {
				fmt.Fprintf(output, "页码无效，可选范围 1-%d。\n", page.TotalPages)
				continue
			}
			pageNumber = target
		case strings.HasPrefix(lower, "s "):
			target, parseErr := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(lower, "s ")))
			if parseErr != nil || !allowedSessionPageSizes[target] {
				fmt.Fprintln(output, "每页条数仅支持 10、20、50、100。")
				continue
			}
			pageSize = target
			pageNumber = 1
		default:
			changed := false
			for _, value := range strings.Split(input, ",") {
				index, parseErr := strconv.Atoi(strings.TrimSpace(value))
				if parseErr != nil || index < 1 || index > len(sessions) {
					continue
				}
				if selected[index] {
					delete(selected, index)
				} else {
					selected[index] = true
				}
				changed = true
			}
			if !changed {
				fmt.Fprintln(output, "未识别命令。")
			}
		}
	}
}
