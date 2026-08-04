package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	term "github.com/charmbracelet/x/term"
)

type Event struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	Timestamp string          `json:"timestamp"`
	Cwd       string          `json:"cwd,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	GitBranch string          `json:"gitBranch,omitempty"`
}

type AssistantMsg struct {
	Model   string         `json:"model"`
	Usage   *UsageInfo     `json:"usage"`
	Content []ContentBlock `json:"content"`
}

type UsageInfo struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Name string `json:"name"`
	Text string `json:"text"`
}

type UserMsg struct {
	Content []ContentBlock `json:"content"`
}

type SessionInfo struct {
	SessionRef           string
	ParentSessionRef     string
	ForkedAt             time.Time
	ForkSource           string
	ForkAgentPath        string
	AgentType            string // "" (== claude_code, default) or "codex"
	FilePath             string
	FileModifiedAt       time.Time
	ProjectDir           string
	Cwd                  string
	GitBranch            string
	StartedAt            time.Time
	EndedAt              time.Time
	Model                string
	Models               []string // distinct models seen, in insertion order
	Summary              string
	RecentSummary        string
	SummaryAt            time.Time
	RecentSummaryAt      time.Time
	SummaryStatus        string
	SummarySource        string
	ToolCalls            map[string]int
	InputTok             int64
	OutputTok            int64
	CacheCreateTok       int64
	CacheReadTok         int64
	TotalTok             int64
	NumLines             int
	SubFiles             []string // subagent JSONL file paths
	ActivitySlices       []ActivitySlice
	SelectionChildren    []*SessionInfo `json:"-"`
	SelectionActiveAt    time.Time      `json:"-"`
	SelectionMissingRoot bool           `json:"-"`
	SelectionIssue       string         `json:"-"`
	ParseWarningCount    int            `json:"-"`
}

type ActivitySlice struct {
	ActivityDate        string         `json:"activity_date"`
	ActivityStartAt     time.Time      `json:"activity_start_at"`
	ActivityEndAt       time.Time      `json:"activity_end_at"`
	Timezone            string         `json:"timezone,omitempty"`
	AgentType           string         `json:"agent_type,omitempty"`
	Model               string         `json:"model,omitempty"`
	Models              []string       `json:"models,omitempty"`
	Summary             string         `json:"summary,omitempty"`
	Excerpt             string         `json:"excerpt,omitempty"`
	MessageCount        int            `json:"message_count,omitempty"`
	SourceEventCount    int            `json:"source_event_count,omitempty"`
	ToolCalls           map[string]int `json:"tool_calls,omitempty"`
	GitCommits          []string       `json:"git_commits,omitempty"`
	InputTokens         int64          `json:"input_tokens,omitempty"`
	OutputTokens        int64          `json:"output_tokens,omitempty"`
	CacheCreationTokens int64          `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int64          `json:"cache_read_tokens,omitempty"`
	TotalTokens         int64          `json:"total_tokens,omitempty"`
	SourceHasRawLog     bool           `json:"source_has_raw_log"`
	TokenSliceStrategy  string         `json:"token_slice_strategy,omitempty"`
	SummaryStrategy     string         `json:"summary_strategy,omitempty"`
	ParserVersion       string         `json:"parser_version,omitempty"`
	SliceVersion        int            `json:"slice_version,omitempty"`
	IsEstimated         bool           `json:"is_estimated"`
}

func (s *SessionInfo) Duration() time.Duration {
	if s.EndedAt.IsZero() || s.StartedAt.IsZero() {
		return 0
	}
	d := s.EndedAt.Sub(s.StartedAt)
	if d < 0 {
		return 0
	}
	return d.Round(time.Second)
}

func (s *SessionInfo) FormatTokens() string {
	return formatTokens(s.TotalTok)
}

func (s *SessionInfo) LastActiveAt() time.Time {
	if s == nil {
		return time.Time{}
	}
	if !s.EndedAt.IsZero() {
		return s.EndedAt
	}
	if !s.StartedAt.IsZero() {
		return s.StartedAt
	}
	return s.FileModifiedAt
}

func formatLastActiveTime(s *SessionInfo, layout string) string {
	if activeAt := sessionSelectionLastActiveAt(s); !activeAt.IsZero() {
		return activeAt.Format(layout)
	}
	return "-"
}

func activityLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*3600)
	}
	return loc
}

func activityDate(t time.Time) string {
	return t.In(activityLocation()).Format("2006-01-02")
}

func ensureActivitySlice(slices map[string]*ActivitySlice, t time.Time, agentType string) *ActivitySlice {
	if t.IsZero() {
		return nil
	}
	date := activityDate(t)
	slice := slices[date]
	if slice == nil {
		slice = &ActivitySlice{
			ActivityDate:       date,
			ActivityStartAt:    t,
			ActivityEndAt:      t,
			Timezone:           "Asia/Shanghai",
			AgentType:          agentType,
			ToolCalls:          map[string]int{},
			SourceHasRawLog:    true,
			TokenSliceStrategy: "exact",
			SummaryStrategy:    "rule",
			ParserVersion:      "daemon-v1",
			SliceVersion:       1,
		}
		slices[date] = slice
	}
	if t.Before(slice.ActivityStartAt) {
		slice.ActivityStartAt = t
	}
	if t.After(slice.ActivityEndAt) {
		slice.ActivityEndAt = t
	}
	slice.SourceEventCount++
	return slice
}

func appendSliceText(slice *ActivitySlice, text string) {
	if slice == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if slice.Summary == "" {
		slice.Summary = truncate(text, 200)
	}
	if slice.Excerpt == "" {
		slice.Excerpt = truncate(text, 1200)
		return
	}
	if len([]rune(slice.Excerpt)) < 1200 {
		slice.Excerpt = truncate(slice.Excerpt+"\n"+text, 1200)
	}
}

func finalizeActivitySlices(s *SessionInfo, slices map[string]*ActivitySlice) {
	if len(slices) == 0 {
		return
	}
	dates := make([]string, 0, len(slices))
	for date := range slices {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	s.ActivitySlices = make([]ActivitySlice, 0, len(dates))
	for _, date := range dates {
		slice := *slices[date]
		if slice.AgentType == "" {
			slice.AgentType = firstNonEmpty(s.AgentType, "claude_code")
		}
		if slice.Model == "" {
			slice.Model = s.Model
		}
		if len(slice.Models) == 0 {
			slice.Models = s.Models
		}
		if slice.TotalTokens == 0 {
			slice.TotalTokens = slice.InputTokens + slice.OutputTokens + slice.CacheCreationTokens + slice.CacheReadTokens
		}
		if slice.MessageCount == 0 && slice.SourceEventCount > 0 {
			slice.MessageCount = slice.SourceEventCount
		}
		if slice.Summary == "" {
			slice.Summary = "当天有活动，但暂无可读摘要"
			slice.SummaryStrategy = "empty"
		}
		if slice.ToolCalls == nil {
			slice.ToolCalls = map[string]int{}
		}
		s.ActivitySlices = append(s.ActivitySlices, slice)
	}
}

func formatTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return strconv.FormatInt(n, 10)
}

func printSessionListHeader() {
	writeSessionListHeader(os.Stdout)
}

func writeSessionListHeader(output io.Writer) {
	fmt.Fprintf(output, "  %-4s  %-6s  %-19s  %-9s  %-9s  %-10s  %-22s  %-36s  %s\n",
		"#", "Agent", "最近活动", "总Tokens", "Duration", "Model", "Project/CWD", "Session", "Summary")
	fmt.Fprintln(output, "  "+strings.Repeat("-", 156))
}

func formatSessionListRow(index int, s *SessionInfo) string {
	if s == nil {
		return ""
	}
	lastActive := "-"
	if activeAt := sessionSelectionLastActiveAt(s); !activeAt.IsZero() {
		lastActive = activeAt.Format("2006-01-02 15:04")
	}
	dur := "-"
	if d := s.Duration(); d > 0 {
		dur = fmt.Sprintf("%dm", int(d.Minutes()))
	}
	agent := s.AgentType
	if agent == "" {
		agent = "claude"
	}
	return fmt.Sprintf("  %-4d  %-6s  %-19s  %-9s  %-9s  %-10s  %-22s  %-36s  %s",
		index,
		truncate(agent, 6),
		lastActive,
		s.FormatTokens(),
		dur,
		truncateMiddle(firstNonEmpty(s.Model, "-"), 10),
		truncateMiddle(sessionProjectDisplay(s), 22),
		firstNonEmpty(s.SessionRef, "-"),
		truncate(compactSessionText(firstNonEmpty(s.Summary, "暂无摘要")), 48),
	)
}

func compactSessionText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func sessionProjectDisplay(s *SessionInfo) string {
	if s == nil {
		return "-"
	}
	if s.ProjectDir != "" {
		return s.ProjectDir
	}
	if s.Cwd != "" {
		base := filepath.Base(s.Cwd)
		if base != "." && base != string(filepath.Separator) {
			return base
		}
		return s.Cwd
	}
	return "-"
}

func truncateMiddle(s string, n int) string {
	runes := []rune(s)
	if n <= 3 || len(runes) <= n {
		return s
	}
	keep := n - 2
	left := keep / 2
	right := keep - left
	return string(runes[:left]) + ".." + string(runes[len(runes)-right:])
}

func cmdLogin(args []string) int {
	cfg := loadConfig()

	server := ""
	token := ""
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "--server", "-s":
			server = args[i+1]
		case "--token", "-t":
			token = args[i+1]
		}
	}

	if server == "" && cfg.APIURL == "" {
		fmt.Println("Aida 尚未完成连接配置，请重新运行安装程序")
		return 1
	}
	if server != "" {
		cfg.APIURL = strings.TrimRight(server, "/")
		cfg.AutoRoute = false
	}

	if token == "" {
		fmt.Print("请输入 Aida 个人令牌（粘贴后按 Enter，输入不会显示）：")
		if inputInfo, err := os.Stdin.Stat(); err == nil && inputInfo.Mode()&os.ModeCharDevice != 0 {
			input, readErr := term.ReadPassword(os.Stdin.Fd())
			fmt.Println()
			if readErr != nil {
				fmt.Println("无法读取个人令牌，请重试")
				return 1
			}
			token = strings.TrimSpace(string(input))
		} else {
			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			token = strings.TrimSpace(input)
		}
	}

	if token == "" {
		fmt.Println("个人令牌不能为空")
		return 1
	}
	fmt.Println("正在登录...")
	cfg.Token = token

	// Verify
	resp, err := apiGet(cfg, "/auth/me")
	if err != nil {
		fmt.Println("登录失败，请检查个人令牌后重试")
		return 1
	}

	var user struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	json.Unmarshal(resp, &user)

	cfg.ServerInfo = fmt.Sprintf("%s (%s)", user.Name, user.Role)
	if err := saveConfig(cfg); err != nil {
		fmt.Println("登录信息保存失败，请运行 aida status 检查")
		return 1
	}

	writeLoginSuccess(os.Stdout, user.Name)
	return 0
}

func writeLoginSuccess(output io.Writer, userName string) {
	fmt.Fprintf(output, "登录成功，%s\n", userName)
}

// ---- upload ----

func cmdUpload(args []string) int {
	cfg := loadConfig()
	if err := requireAuth(cfg); err != nil {
		fmt.Println("请先运行 aida login 登录")
		return 1
	}
	resolveAPIEndpoint(cfg)

	uploadAll := false
	uploadMode := uploadModePersonal
	pageSize := defaultSessionPageSize
	var selectedIdx []int

	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--all" || a == "-a" {
			uploadAll = true
		} else if a == "--team" {
			uploadMode = uploadModeTeam
			uploadAll = true
		} else if a == "--page-size" && i+1 < len(args) {
			pageSize, _ = strconv.Atoi(args[i+1])
			i++
		} else if n, err := strconv.Atoi(a); err == nil {
			selectedIdx = append(selectedIdx, n)
		}
	}
	if uploadMode == uploadModeTeam && len(selectedIdx) > 0 {
		fmt.Println("团队模式会自动扫描全部 Session，不能与 Session 编号同时使用")
		return 2
	}
	if !allowedSessionPageSizes[pageSize] {
		fmt.Println("每页条数仅支持 10、20、50 或 100")
		return 2
	}

	home, _ := os.UserHomeDir()
	sessions := scanSessionsForCommand(filepath.Join(home, ".claude", "projects"), filepath.Join(home, ".codex", "sessions"), true, true)
	ignoreConfig, err := loadSessionIgnoreConfig()
	if err != nil {
		fmt.Println("无法读取忽略配置，为保护隐私已停止上传，请检查 ~/.aida/ignore.json")
		return 1
	}
	sessions = filterIgnoredSessionGroups(sessions, ignoreConfig)

	if len(sessions) == 0 {
		if uploadMode == uploadModeTeam {
			if err := updateTeamSyncUnresolved(map[string]int{}, true); err != nil {
				fmt.Printf("团队同步日志保存失败：%v\n", err)
				return 1
			}
		}
		fmt.Println("没有找到可上传的 Session")
		return 0
	}

	var toUpload []*SessionInfo

	if uploadAll {
		toUpload = sessions
	} else if len(selectedIdx) > 0 {
		for _, idx := range selectedIdx {
			if idx < 1 || idx > len(sessions) {
				fmt.Printf("Session 编号无效，可选范围为 1-%d\n", len(sessions))
				return 2
			}
			toUpload = append(toUpload, sessions[idx-1])
		}
	} else {
		var err error
		if terminalSupportsTUI(os.Stdin, os.Stdout) {
			toUpload, err = selectSessionsWithTUI(sessions)
		} else {
			reader := bufio.NewReader(os.Stdin)
			toUpload, err = selectSessionsInteractively(sessions, pageSize, reader, os.Stdout)
		}
		if err != nil {
			fmt.Println("无法完成 Session 选择，请重试")
			return 1
		}
	}

	teamUploadGroups := make([][]sessionWithFile, len(toUpload))
	teamSessionCount := 0
	if uploadMode == uploadModeTeam {
		for index, session := range toUpload {
			teamUploadGroups[index] = collectSessionsWithFiles(session)
			teamSessionCount += len(teamUploadGroups[index])
		}
		fmt.Printf("\n团队模式扫描 %d 个本地 Session，归并为 %d 个上传组，按工作目录同步到团队成员。\n", teamSessionCount, len(toUpload))
	} else if uploadAll {
		fmt.Printf("\n已选择全部 %d 个 Session，未变化的内容会自动跳过。\n", len(toUpload))
	}

	if len(toUpload) == 0 {
		fmt.Println("未选择 Session")
		return 0
	}

	releaseUpload, lockCode := beginSessionUpload(os.Stdout)
	if lockCode != 0 {
		return lockCode
	}
	defer releaseUpload()

	fmt.Printf("\n正在上传 %d 个 Session...\n\n", len(toUpload))

	totalFailed := 0
	succeededSessions := 0
	failedSessions := 0
	pendingSessions := 0
	blockedSessions := 0
	succeededSessionItems := 0
	failedSessionItems := 0
	pendingSessionItems := 0
	blockedSessionItems := 0
	unresolvedDirectories := map[string]int{}

	for sessionIndex, s := range toUpload {
		if sessionIndex > 0 {
			fmt.Println()
		}
		allSessions := collectSessionsWithFiles(s)
		if uploadMode == uploadModeTeam {
			allSessions = teamUploadGroups[sessionIndex]
		}
		incrementalResults, incrementalErr := uploadSessionGroupIncrementalWithMode(cfg, allSessions, s.SessionRef, uploadMode)
		if !errors.Is(incrementalErr, errSessionSyncNotEnabled) {
			sessionFailed := false
			sessionPending := false
			sessionBlocked := false
			sessionErrors := []string{}
			for _, result := range incrementalResults {
				if uploadMode == uploadModeTeam && result.ErrorCode == "TEAM_DIRECTORY_UNMAPPED" {
					directory := strings.TrimSpace(result.CWD)
					if directory == "" {
						directory = "(unknown)"
					} else {
						directory = filepath.Clean(directory)
					}
					unresolvedDirectories[directory]++
					sessionPending = true
					pendingSessionItems++
					continue
				}
				if uploadMode == uploadModeTeam && result.Status == "blocked" && isNonRetryableTeamPrepareError(result.ErrorCode) {
					sessionBlocked = true
					blockedSessionItems++
					sessionErrors = append(sessionErrors, result.SessionRef+": "+result.ErrorCode+"（归属冲突，已跳过）")
					continue
				}
				if result.Status == "content_cleared" || result.ErrorCode != "" || result.Status == "failed" {
					totalFailed++
					sessionFailed = true
					failedSessionItems++
					if result.ErrorCode != "" {
						sessionErrors = append(sessionErrors, result.SessionRef+": "+result.ErrorCode)
					}
				} else {
					succeededSessionItems++
				}
			}
			if incrementalErr != nil {
				totalFailed++
				sessionFailed = true
				failedSessionItems += len(allSessions) - len(incrementalResults)
				sessionErrors = append(sessionErrors, incrementalErr.Error())
			}
			if sessionFailed {
				printSessionUploadResult(os.Stdout, s, true)
			} else if sessionBlocked {
				printSessionUploadBlocked(os.Stdout, s)
				blockedSessions++
			} else if sessionPending {
				printSessionUploadPending(os.Stdout, s)
				pendingSessions++
			} else {
				printSessionUploadResult(os.Stdout, s, false)
			}
			writeSessionUploadErrors(os.Stdout, sessionErrors)
			if sessionFailed {
				failedSessions++
			} else if !sessionPending && !sessionBlocked {
				succeededSessions++
			}
			continue
		}
		if uploadMode == uploadModeTeam {
			totalFailed++
			failedSessions++
			failedSessionItems += len(allSessions)
			printSessionUploadResult(os.Stdout, s, true)
			continue
		}

		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		metadata := make([]map[string]any, 0, len(allSessions))
		for _, item := range allSessions {
			metadata = append(metadata, buildUploadPayload(item.info))
		}
		metadataJSON, _ := json.Marshal(map[string]any{"sessions": metadata})
		writer.WriteField("metadata", string(metadataJSON))

		for _, item := range allSessions {
			f, err := os.Open(item.filePath)
			if err != nil {
				continue
			}
			part, err := writer.CreateFormFile("file_"+item.info.SessionRef, filepath.Base(item.filePath))
			if err != nil {
				f.Close()
				continue
			}
			io.Copy(part, f)
			f.Close()
		}
		writer.Close()

		req, err := http.NewRequest("POST", apiBaseURL(cfg)+"/sessions/batch", &buf)
		if err != nil {
			totalFailed++
			printSessionUploadResult(os.Stdout, s, true)
			failedSessions++
			continue
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+cfg.Token)

		respBody, err := doRequestWithTimeout(req, sessionUploadRequestTimeout)
		if err != nil {
			totalFailed++
			printSessionUploadResult(os.Stdout, s, true)
			failedSessions++
			continue
		}

		var result struct {
			Total   int `json:"total"`
			Results []struct {
				SessionRef string `json:"session_ref"`
				ID         string `json:"id"`
				Status     string `json:"status"`
			} `json:"results"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			totalFailed++
			printSessionUploadResult(os.Stdout, s, true)
			failedSessions++
			continue
		}

		mainStatus := "unknown"
		hadError := false
		for _, r := range result.Results {
			if r.SessionRef == s.SessionRef {
				mainStatus = r.Status
			}
			if strings.HasPrefix(r.Status, "error:") {
				hadError = true
			}
		}

		if mainStatus != "created" && mainStatus != "updated" && mainStatus != "duplicate" {
			totalFailed++
			hadError = true
		}
		if hadError {
			totalFailed++
		}
		printSessionUploadResult(os.Stdout, s, hadError)
		if hadError {
			failedSessions++
		} else {
			succeededSessions++
		}
	}
	if uploadMode == uploadModeTeam {
		if err := completeTeamSyncScan(unresolvedDirectories); err != nil {
			fmt.Printf("\n团队同步日志保存失败：%v\n", err)
			totalFailed++
		}
	}

	switch {
	case totalFailed > 0:
		if uploadMode == uploadModeTeam {
			fmt.Printf("\n%s\n", formatTeamUploadSummary(succeededSessions, succeededSessionItems, pendingSessions, pendingSessionItems, blockedSessions, blockedSessionItems, failedSessions, failedSessionItems, true))
		} else {
			fmt.Printf("\n上传完成：成功 %d 个，失败 %d 个\n", succeededSessions, failedSessions)
		}
	case pendingSessions > 0 || blockedSessions > 0:
		fmt.Printf("\n%s\n", formatTeamUploadSummary(succeededSessions, succeededSessionItems, pendingSessions, pendingSessionItems, blockedSessions, blockedSessionItems, failedSessions, failedSessionItems, false))
	default:
		if uploadMode == uploadModeTeam {
			fmt.Printf("\n%s\n", formatTeamUploadSummary(succeededSessions, succeededSessionItems, pendingSessions, pendingSessionItems, blockedSessions, blockedSessionItems, failedSessions, failedSessionItems, false))
		} else {
			fmt.Printf("\n上传完成：成功 %d 个\n", succeededSessions)
		}
	}
	if totalFailed > 0 {
		return 1
	}
	return 0
}

func formatTeamUploadSummary(succeededGroups, succeededSessions, pendingGroups, pendingSessions, blockedGroups, blockedSessions, failedGroups, failedSessions int, includeFailed bool) string {
	if includeFailed {
		if blockedGroups > 0 {
			return fmt.Sprintf("上传完成：成功 %d 组（%d 个 Session），待配置 %d 组（%d 个 Session），归属冲突 %d 组（%d 个 Session，已跳过），失败 %d 组（%d 个 Session）", succeededGroups, succeededSessions, pendingGroups, pendingSessions, blockedGroups, blockedSessions, failedGroups, failedSessions)
		}
		return fmt.Sprintf("上传完成：成功 %d 组（%d 个 Session），待配置 %d 组（%d 个 Session），失败 %d 组（%d 个 Session）", succeededGroups, succeededSessions, pendingGroups, pendingSessions, failedGroups, failedSessions)
	}
	if blockedGroups > 0 && pendingGroups > 0 {
		return fmt.Sprintf("上传完成：成功 %d 组（%d 个 Session），待配置 %d 组（%d 个 Session），归属冲突 %d 组（%d 个 Session，已跳过）；运行 aida log 查看目录。", succeededGroups, succeededSessions, pendingGroups, pendingSessions, blockedGroups, blockedSessions)
	}
	if blockedGroups > 0 {
		return fmt.Sprintf("上传完成：成功 %d 组（%d 个 Session），归属冲突 %d 组（%d 个 Session，已跳过）", succeededGroups, succeededSessions, blockedGroups, blockedSessions)
	}
	if pendingGroups > 0 {
		return fmt.Sprintf("上传完成：成功 %d 组（%d 个 Session），待配置 %d 组（%d 个 Session）；运行 aida log 查看目录。", succeededGroups, succeededSessions, pendingGroups, pendingSessions)
	}
	return fmt.Sprintf("上传完成：成功 %d 组（%d 个 Session）", succeededGroups, succeededSessions)
}

func printSessionUploadBlocked(output io.Writer, session *SessionInfo) {
	summary := strings.TrimSpace(session.Summary)
	if summary == "" {
		summary = "Session"
	}
	fmt.Fprintf(output, "[归属冲突] %s  %s\n", formatLastActiveTime(session, "01-02 15:04"), trunc(summary, 60))
}

func printSessionUploadPending(output io.Writer, session *SessionInfo) {
	summary := strings.TrimSpace(session.Summary)
	if summary == "" {
		summary = "Session"
	}
	fmt.Fprintf(output, "[待配置] %s  %s\n", formatLastActiveTime(session, "01-02 15:04"), trunc(summary, 60))
}

func printSessionUploadResult(output io.Writer, session *SessionInfo, failed bool) {
	status := "完成"
	if failed {
		status = "失败"
	}
	summary := strings.TrimSpace(session.Summary)
	if summary == "" {
		summary = "Session"
	}
	fmt.Fprintf(output, "[%s] %s  %s\n", status, formatLastActiveTime(session, "01-02 15:04"), trunc(summary, 60))
}

func writeSessionUploadErrors(output io.Writer, reasons []string) {
	for _, reason := range reasons {
		reason = strings.TrimSpace(reason)
		if reason != "" {
			fmt.Fprintf(output, "  原因：%s\n", reason)
		}
	}
}

func completeTeamSyncScan(unresolvedDirectories map[string]int) error {
	return updateTeamSyncUnresolved(unresolvedDirectories, true)
}

// ---- consume ----

type sessionWithFile struct {
	info     *SessionInfo
	filePath string
}

func collectSessionsWithFiles(s *SessionInfo) []sessionWithFile {
	var items []sessionWithFile
	if s == nil {
		return items
	}
	seenRefs := map[string]bool{}
	if s.SessionRef != "" && s.FilePath != "" {
		items = append(items, sessionWithFile{info: s, filePath: s.FilePath})
		seenRefs[s.SessionRef] = true
	}
	for _, subFile := range s.SubFiles {
		sub := parseJSONL(subFile)
		if sub == nil || sub.SessionRef == "" || seenRefs[sub.SessionRef] {
			continue
		}
		seenRefs[sub.SessionRef] = true
		items = append(items, sessionWithFile{info: sub, filePath: subFile})
	}
	for _, child := range s.SelectionChildren {
		if child == nil || child.SessionRef == "" || child.FilePath == "" || seenRefs[child.SessionRef] {
			continue
		}
		seenRefs[child.SessionRef] = true
		items = append(items, sessionWithFile{info: child, filePath: child.FilePath})
	}
	return items
}

type reportResponse struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	ReportDate string `json:"report_date"`
}

func getTodayReport(cfg *Config) (*reportResponse, error) {
	resp, err := apiGet(cfg, "/reports/today")
	if err != nil {
		return nil, err
	}
	var report reportResponse
	if err := json.Unmarshal(resp, &report); err != nil {
		return nil, err
	}
	if report.ID == "" {
		return nil, fmt.Errorf("report response missing id")
	}
	return &report, nil
}

func updateReportContent(cfg *Config, reportID, content string) error {
	body, _ := json.Marshal(map[string]string{"content": content})
	_, err := apiPut(cfg, "/reports/"+reportID, body)
	return err
}

func formatToolCalls(toolCalls map[string]int) string {
	if len(toolCalls) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(toolCalls))
	for name, count := range toolCalls {
		parts = append(parts, fmt.Sprintf("%s=%d", name, count))
	}
	return strings.Join(parts, ", ")
}

func shortRef(ref string) string {
	if len(ref) <= 12 {
		return ref
	}
	return ref[:12]
}
func cmdStatus() int {
	cfg := loadConfig()

	if cfg.Token == "" {
		fmt.Println("Not logged in.")
		fmt.Println("\nRun: aida login")
		return 1
	}

	resolveAPIEndpoint(cfg)
	fmt.Printf("Server:  %s (%s)\n", apiBaseURL(cfg), cfg.ActiveRoute)
	fmt.Printf("Public:  %s\n", cfg.APIURL)
	fmt.Printf("Config:  %s\n", configPath())

	if cfg.ServerInfo != "" {
		fmt.Printf("User:    %s\n", cfg.ServerInfo)
	}

	resp, err := apiGet(cfg, "/auth/me")
	if err != nil {
		fmt.Printf("Status:  disconnected (%v)\n", err)
		return 1
	}
	var user struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	json.Unmarshal(resp, &user)
	fmt.Printf("Status:  logged in as %s (%s)\n", user.Name, user.Role)
	return 0
}

// ---- scanning ----

func scanSessions(claudeDir string, showAll bool) []*SessionInfo {
	var sessions []*SessionInfo

	filepath.WalkDir(claudeDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+"subagents"+string(filepath.Separator)) {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		if !showAll && time.Since(info.ModTime()) > 48*time.Hour {
			return nil
		}

		session := parseJSONL(path)
		if session == nil || session.SessionRef == "" {
			return nil
		}

		rel, _ := filepath.Rel(claudeDir, path)
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		session.ProjectDir = decodeProjectDir(parts[0])
		session.FilePath = path
		session.FileModifiedAt = info.ModTime()

		// Collect subagent sessions from <session-id>/subagents/*.jsonl
		sessionDir := strings.TrimSuffix(path, ".jsonl")
		subDir := filepath.Join(sessionDir, "subagents")
		if entries, err := os.ReadDir(subDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
					session.SubFiles = append(session.SubFiles, filepath.Join(subDir, e.Name()))
				}
			}
		}

		sessions = append(sessions, session)
		return nil
	})

	// Sort newest first
	sortSessionsNewestFirst(sessions)

	return sessions
}

func sortSessionsNewestFirst(sessions []*SessionInfo) {
	for i := 0; i < len(sessions); i++ {
		for j := i + 1; j < len(sessions); j++ {
			if sessions[j].LastActiveAt().After(sessions[i].LastActiveAt()) {
				sessions[i], sessions[j] = sessions[j], sessions[i]
			}
		}
	}
}

func decodeProjectDir(dir string) string {
	// -home-gh-project-manager -> /home/gh/project-manager
	dir = strings.TrimPrefix(dir, "-")
	parts := strings.Split(dir, "-")
	return "/" + strings.Join(parts, "/")
}

func parseJSONL(path string) *SessionInfo {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	s := &SessionInfo{
		ToolCalls: make(map[string]int),
	}

	scanner := newJSONLScanner(f, defaultParseMaxLineBytes)

	var lastTS time.Time
	summaryCollector := sessionSummaryCollector{}
	activitySlices := map[string]*ActivitySlice{}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		s.NumLines++

		var event Event
		if json.Unmarshal(line, &event) != nil {
			continue
		}

		if event.SessionID != "" && s.SessionRef == "" {
			s.SessionRef = event.SessionID
		}

		var currentSlice *ActivitySlice
		var eventAt time.Time
		if event.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, event.Timestamp); err == nil {
				eventAt = t
				lastTS = t
				if s.StartedAt.IsZero() {
					s.StartedAt = t
				}
				currentSlice = ensureActivitySlice(activitySlices, t, "claude_code")
			}
		}

		if event.Cwd != "" && s.Cwd == "" {
			s.Cwd = event.Cwd
		}
		if event.GitBranch != "" && s.GitBranch == "" {
			s.GitBranch = event.GitBranch
		}

		switch event.Type {
		case "user":
			if currentSlice != nil {
				currentSlice.MessageCount++
			}
			var msg UserMsg
			if json.Unmarshal(event.Message, &msg) == nil {
				for _, c := range msg.Content {
					if c.Type == "text" && c.Text != "" {
						appendSliceText(currentSlice, c.Text)
						summaryCollector.Add(c.Text, eventAt, "user.message")
					}
				}
			}

		case "assistant":
			if currentSlice != nil {
				currentSlice.MessageCount++
			}
			var msg AssistantMsg
			if json.Unmarshal(event.Message, &msg) == nil {
				if msg.Model != "" && msg.Model != "<synthetic>" {
					s.Model = msg.Model
					s.Models = appendDistinct(s.Models, msg.Model)
					if currentSlice != nil {
						currentSlice.Model = msg.Model
						currentSlice.Models = appendDistinct(currentSlice.Models, msg.Model)
					}
				}
				if msg.Usage != nil {
					s.InputTok += msg.Usage.InputTokens
					s.OutputTok += msg.Usage.OutputTokens
					s.CacheCreateTok += msg.Usage.CacheCreationInputTokens
					s.CacheReadTok += msg.Usage.CacheReadInputTokens
					if currentSlice != nil {
						currentSlice.InputTokens += msg.Usage.InputTokens
						currentSlice.OutputTokens += msg.Usage.OutputTokens
						currentSlice.CacheCreationTokens += msg.Usage.CacheCreationInputTokens
						currentSlice.CacheReadTokens += msg.Usage.CacheReadInputTokens
					}
				}
				for _, c := range msg.Content {
					if c.Type == "tool_use" && c.Name != "" {
						s.ToolCalls[c.Name]++
						if currentSlice != nil {
							currentSlice.ToolCalls[c.Name]++
						}
					}
					if c.Type == "text" && c.Text != "" {
						appendSliceText(currentSlice, c.Text)
						summaryCollector.AddReply(c.Text, eventAt, "assistant.message")
					}
				}
			}
		}
	}
	s.ParseWarningCount = scanner.Skipped()

	s.TotalTok = s.InputTok + s.OutputTok + s.CacheCreateTok + s.CacheReadTok
	if !lastTS.IsZero() {
		s.EndedAt = lastTS
	}
	summaryCollector.Apply(s)
	finalizeActivitySlices(s, activitySlices)
	if s.Summary == "" {
		if scanner.Err() != nil || s.ParseWarningCount > 0 {
			s.SummaryStatus = "parse_error"
		} else {
			s.SummaryStatus = "empty"
		}
	}

	if s.SessionRef == "" {
		return nil
	}
	return s
}

// ---- API helpers ----

func apiGet(cfg *Config, path string) (json.RawMessage, error) {
	req, err := http.NewRequest("GET", apiBaseURL(cfg)+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	return doRequest(req)
}

func apiPost(cfg *Config, path string, body []byte) (json.RawMessage, error) {
	req, err := http.NewRequest("POST", apiBaseURL(cfg)+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	return doRequest(req)
}

func apiPut(cfg *Config, path string, body []byte) (json.RawMessage, error) {
	req, err := http.NewRequest("PUT", apiBaseURL(cfg)+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	return doRequest(req)
}

const (
	defaultRequestTimeout       = 60 * time.Second
	sessionUploadRequestTimeout = 30 * time.Minute
)

func doRequest(req *http.Request) (json.RawMessage, error) {
	return doRequestWithTimeout(req, defaultRequestTimeout)
}

func doRequestWithTimeout(req *http.Request, timeout time.Duration) (json.RawMessage, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 401 {
		return nil, fmt.Errorf("unauthorized - token may be expired or invalid")
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(data), 200))
	}

	return json.RawMessage(data), nil
}

func buildUploadPayload(s *SessionInfo) map[string]any {
	p := map[string]any{
		"session_ref": s.SessionRef,
		"started_at":  s.StartedAt.Format(time.RFC3339),
		"model":       s.Model,
	}
	if s.AgentType != "" {
		p["agent_type"] = s.AgentType
	}
	if !s.EndedAt.IsZero() && !s.StartedAt.IsZero() {
		p["ended_at"] = s.EndedAt.Format(time.RFC3339)
		d := int(s.EndedAt.Sub(s.StartedAt).Seconds())
		if d > 0 {
			p["duration_secs"] = d
		}
	}
	if s.Summary != "" {
		p["summary"] = s.Summary
	}
	if len(s.ToolCalls) > 0 {
		p["tool_calls"] = s.ToolCalls
	}
	if s.TotalTok > 0 {
		p["token_usage"] = map[string]any{
			"input_tokens":          s.InputTok,
			"output_tokens":         s.OutputTok,
			"cache_creation_tokens": s.CacheCreateTok,
			"cache_read_tokens":     s.CacheReadTok,
			"total_tokens":          s.TotalTok,
		}
	}
	if len(s.Models) > 0 {
		p["models"] = s.Models
	}
	if len(s.ActivitySlices) > 0 {
		p["activity_slices"] = s.ActivitySlices
	}
	return p
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 3 {
		return string(runes[:n])
	}
	return string(runes[:n-3]) + "..."
}

func appendDistinct(list []string, v string) []string {
	for _, item := range list {
		if item == v {
			return list
		}
	}
	return append(list, v)
}

func trunc(s string, n int) string {
	return truncate(s, n)
}
