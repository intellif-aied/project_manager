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
	SessionRef     string
	AgentType      string // "" (== claude_code, default) or "codex"
	FilePath       string
	FileModifiedAt time.Time
	ProjectDir     string
	Cwd            string
	GitBranch      string
	StartedAt      time.Time
	EndedAt        time.Time
	Model          string
	Models         []string // distinct models seen, in insertion order
	Summary        string
	SummaryStatus  string
	SummarySource  string
	ToolCalls      map[string]int
	InputTok       int64
	OutputTok      int64
	CacheCreateTok int64
	CacheReadTok   int64
	TotalTok       int64
	NumLines       int
	SubFiles       []string // subagent JSONL file paths
	ActivitySlices []ActivitySlice
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
	if activeAt := s.LastActiveAt(); !activeAt.IsZero() {
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
		"#", "Agent", "最近活动", "Tokens", "Duration", "Model", "Project/CWD", "Session", "Summary")
	fmt.Fprintln(output, "  "+strings.Repeat("-", 156))
}

func formatSessionListRow(index int, s *SessionInfo) string {
	if s == nil {
		return ""
	}
	lastActive := "-"
	if activeAt := s.LastActiveAt(); !activeAt.IsZero() {
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
		truncate(firstNonEmpty(s.Summary, "暂无摘要"), 48),
	)
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

func cmdLogin(args []string) {
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
		fmt.Print("Server URL [http://localhost:8080/api/v1]: ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		server = strings.TrimSpace(input)
		if server == "" {
			server = "http://localhost:8080/api/v1"
		}
	}
	if server != "" {
		cfg.APIURL = strings.TrimRight(server, "/")
	}

	if token == "" {
		fmt.Print("Enter API token: ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		token = strings.TrimSpace(input)
	}

	if token == "" {
		fmt.Println("Error: token is required")
		os.Exit(1)
	}
	cfg.Token = token

	// Verify
	resp, err := apiGet(cfg, "/auth/me")
	if err != nil {
		fmt.Printf("Login failed: %v\n", err)
		fmt.Println("Check your token and server URL")
		os.Exit(1)
	}

	var user struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	json.Unmarshal(resp, &user)

	cfg.ServerInfo = fmt.Sprintf("%s (%s)", user.Name, user.Role)
	saveConfig(cfg)

	fmt.Printf("Logged in as %s (%s) at %s\n", user.Name, user.Role, cfg.APIURL)
	fmt.Printf("Config saved to %s\n", configPath())
}

// ---- sessions ----

func cmdSessions(args []string) {
	showAll := false
	jsonOutput := false
	projectFilter := ""
	pageNumber := 1
	pageSize := defaultSessionPageSize
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--all", "-a":
			showAll = true
		case "--json":
			jsonOutput = true
		case "--project", "-p":
			if i+1 < len(args) {
				projectFilter = args[i+1]
				i++
			}
		case "--page":
			if i+1 < len(args) {
				pageNumber, _ = strconv.Atoi(args[i+1])
				i++
			}
		case "--page-size":
			if i+1 < len(args) {
				pageSize, _ = strconv.Atoi(args[i+1])
				i++
			}
		}
	}

	home, _ := os.UserHomeDir()
	claudeDir := filepath.Join(home, ".claude", "projects")
	codexDir := filepath.Join(home, ".codex", "sessions")

	sessions := scanSessions(claudeDir, showAll)
	sessions = append(sessions, scanCodexSessions(codexDir, showAll)...)
	sortSessionsNewestFirst(sessions)
	if projectFilter != "" {
		var filtered []*SessionInfo
		for _, s := range sessions {
			if strings.Contains(s.ProjectDir, projectFilter) || strings.Contains(s.Cwd, projectFilter) {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered
	}

	if len(sessions) == 0 {
		if jsonOutput {
			writeSessionsJSON(os.Stdout, sessions, 1, pageSize)
			return
		}
		fmt.Println("No sessions found.")
		fmt.Println()
		fmt.Println("Claude Code session logs are stored at:")
		fmt.Printf("  %s/\n", claudeDir)
		fmt.Println()
		fmt.Println("Each .jsonl file = one session. Sub-agent sessions are excluded.")
		return
	}

	page, err := paginateSessions(sessions, pageNumber, pageSize)
	if err != nil {
		fmt.Printf("Invalid pagination: %v\n", err)
		return
	}
	if jsonOutput {
		writeSessionsJSON(os.Stdout, sessions, pageNumber, pageSize)
		return
	}
	writeSessionPage(os.Stdout, "Session 列表", page, nil)

	fmt.Printf("  Claude logs: %s/\n", claudeDir)
	fmt.Printf("  Codex logs:  %s/\n\n", codexDir)
}

// ---- upload ----

func cmdUpload(args []string) {
	cfg := loadConfig()
	requireAuth(cfg)

	uploadAll := false
	pageSize := defaultSessionPageSize
	var selectedIdx []int

	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--all" || a == "-a" {
			uploadAll = true
		} else if a == "--page-size" && i+1 < len(args) {
			pageSize, _ = strconv.Atoi(args[i+1])
			i++
		} else if n, err := strconv.Atoi(a); err == nil {
			selectedIdx = append(selectedIdx, n)
		}
	}
	if !allowedSessionPageSizes[pageSize] {
		fmt.Println("Invalid page size: use 10, 20, 50, or 100")
		return
	}

	home, _ := os.UserHomeDir()
	sessions := scanSessions(filepath.Join(home, ".claude", "projects"), true)
	sessions = append(sessions, scanCodexSessions(filepath.Join(home, ".codex", "sessions"), true)...)
	sortSessionsNewestFirst(sessions)

	if len(sessions) == 0 {
		fmt.Println("No sessions found to upload.")
		return
	}

	var toUpload []*SessionInfo

	if uploadAll {
		toUpload = sessions
	} else if len(selectedIdx) > 0 {
		for _, idx := range selectedIdx {
			if idx < 1 || idx > len(sessions) {
				fmt.Printf("Invalid session number: %d (range 1-%d)\n", idx, len(sessions))
				os.Exit(1)
			}
			toUpload = append(toUpload, sessions[idx-1])
		}
	} else {
		reader := bufio.NewReader(os.Stdin)
		var err error
		toUpload, err = selectSessionsInteractively(sessions, pageSize, reader, os.Stdout)
		if err != nil {
			fmt.Printf("Session selection failed: %v\n", err)
			return
		}
	}

	if uploadAll {
		fmt.Printf("\n--all selected all %d locally discoverable sessions; unchanged sessions will be skipped.\n", len(toUpload))
	}

	if len(toUpload) == 0 {
		fmt.Println("No sessions selected.")
		return
	}

	fmt.Printf("\nUploading %d session(s) to %s ...\n\n", len(toUpload), cfg.APIURL)

	totalUploaded := 0
	totalSubs := 0

	for _, s := range toUpload {
		allSessions := collectSessionsWithFiles(s)
		incrementalResults, incrementalErr := uploadSessionGroupIncremental(cfg, allSessions, s.SessionRef)
		if !errors.Is(incrementalErr, errSessionSyncNotEnabled) {
			for index, result := range incrementalResults {
				label := "OK"
				switch result.Status {
				case "unchanged":
					label = "SKIP"
				case "content_cleared":
					label = "BLOCKED"
				}
				fmt.Printf("  [%-7s] %-14s  incremental=%s chunks=%d",
					label, shortRef(result.SessionRef), result.Status, result.UploadedChunks)
				if result.PendingTail {
					fmt.Print(" pending-half-line")
				}
				fmt.Println()
				if result.Status == "uploaded" {
					if index == 0 {
						totalUploaded++
					} else {
						totalSubs++
					}
				}
			}
			if incrementalErr != nil {
				fmt.Printf("  [FAIL]  %-14s  %v\n", shortRef(s.SessionRef), incrementalErr)
			}
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

		req, err := http.NewRequest("POST", cfg.APIURL+"/sessions/batch", &buf)
		if err != nil {
			fmt.Printf("  [FAIL]  %-14s  %s  %v\n", s.SessionRef[:12], formatLastActiveTime(s, "01-02 15:04"), err)
			continue
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+cfg.Token)

		respBody, err := doRequestWithTimeout(req, sessionUploadRequestTimeout)
		if err != nil {
			fmt.Printf("  [FAIL]  %-14s  %s  %v\n", s.SessionRef[:12], formatLastActiveTime(s, "01-02 15:04"), err)
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
			fmt.Printf("  [FAIL]  %-14s  %s  invalid response: %v\n", s.SessionRef[:12], formatLastActiveTime(s, "01-02 15:04"), err)
			continue
		}

		mainStatus := "unknown"
		subSuccess := 0
		hadError := false
		for _, r := range result.Results {
			if r.SessionRef == s.SessionRef {
				mainStatus = r.Status
			} else if r.Status == "created" || r.Status == "updated" || r.Status == "duplicate" {
				subSuccess++
			}
			if strings.HasPrefix(r.Status, "error:") {
				hadError = true
				ref := r.SessionRef
				if len(ref) > 12 {
					ref = ref[:12]
				}
				fmt.Printf("  [FAIL]  %-14s  %s\n", ref, r.Status)
			}
		}

		switch mainStatus {
		case "created":
			fmt.Printf("  [OK]    %-14s  %s  %8s  %s\n",
				s.SessionRef[:12], formatLastActiveTime(s, "01-02 15:04"), s.FormatTokens(), trunc(s.Summary, 40))
			totalUploaded++
		case "updated":
			fmt.Printf("  [OK]    %-14s  %s  updated existing session\n",
				s.SessionRef[:12], formatLastActiveTime(s, "01-02 15:04"))
			totalUploaded++
		case "duplicate":
			fmt.Printf("  [SKIP]  %-14s  %s  (already uploaded)\n",
				s.SessionRef[:12], formatLastActiveTime(s, "01-02 15:04"))
		default:
			fmt.Printf("  [%s]  %-14s  %s\n", mainStatus, s.SessionRef[:12], formatLastActiveTime(s, "01-02 15:04"))
		}

		if subSuccess > 0 {
			fmt.Printf("          └─ %d sub-agent(s) processed\n", subSuccess)
			totalSubs += subSuccess
		}
		if hadError {
			fmt.Println("          └─ one or more batch items failed; see errors above")
		}
	}

	fmt.Printf("\nDone. %d main + %d sub-agent(s) processed.\n", totalUploaded, totalSubs)
	if totalUploaded > 0 || totalSubs > 0 {
		fmt.Printf("Dashboard: %s\n", strings.Replace(cfg.APIURL, "/api/v1", "", 1))
	}
}

// ---- consume ----

type sessionWithFile struct {
	info     *SessionInfo
	filePath string
}

func collectSessionsWithFiles(s *SessionInfo) []sessionWithFile {
	var items []sessionWithFile
	items = append(items, sessionWithFile{info: s, filePath: s.FilePath})
	seenRefs := map[string]bool{}
	if s.SessionRef != "" {
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
func cmdStatus() {
	cfg := loadConfig()

	if cfg.Token == "" {
		fmt.Println("Not logged in.")
		fmt.Println("\nRun: aida login --server <url> --token <token>")
		return
	}

	fmt.Printf("Server:  %s\n", cfg.APIURL)
	fmt.Printf("Config:  %s\n", configPath())

	if cfg.ServerInfo != "" {
		fmt.Printf("User:    %s\n", cfg.ServerInfo)
	}

	resp, err := apiGet(cfg, "/auth/me")
	if err != nil {
		fmt.Printf("Status:  disconnected (%v)\n", err)
		return
	}
	var user struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	json.Unmarshal(resp, &user)
	fmt.Printf("Status:  logged in as %s (%s)\n", user.Name, user.Role)
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

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	firstUserMsg := true
	var lastTS time.Time
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
		if event.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, event.Timestamp); err == nil {
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
			if firstUserMsg && s.Summary == "" {
				var msg UserMsg
				if json.Unmarshal(event.Message, &msg) == nil {
					for _, c := range msg.Content {
						if c.Type == "text" && c.Text != "" {
							appendSliceText(currentSlice, c.Text)
							s.Summary = c.Text
							s.SummaryStatus = "ok"
							s.SummarySource = "user.message"
							if len(s.Summary) > 200 {
								s.Summary = s.Summary[:197] + "..."
							}
							break
						}
					}
				}
				firstUserMsg = false
			} else {
				var msg UserMsg
				if json.Unmarshal(event.Message, &msg) == nil {
					for _, c := range msg.Content {
						if c.Type == "text" && c.Text != "" {
							appendSliceText(currentSlice, c.Text)
							break
						}
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
					}
				}
			}
		}
	}

	s.TotalTok = s.InputTok + s.OutputTok + s.CacheCreateTok + s.CacheReadTok
	if !lastTS.IsZero() {
		s.EndedAt = lastTS
	}
	finalizeActivitySlices(s, activitySlices)
	if s.Summary == "" {
		if scanner.Err() != nil {
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
	req, err := http.NewRequest("GET", cfg.APIURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	return doRequest(req)
}

func apiPost(cfg *Config, path string, body []byte) (json.RawMessage, error) {
	req, err := http.NewRequest("POST", cfg.APIURL+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	return doRequest(req)
}

func apiPut(cfg *Config, path string, body []byte) (json.RawMessage, error) {
	req, err := http.NewRequest("PUT", cfg.APIURL+path, bytes.NewReader(body))
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
