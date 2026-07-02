package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
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
	ProjectDir     string
	Cwd            string
	GitBranch      string
	StartedAt      time.Time
	EndedAt        time.Time
	Model          string
	Models         []string // distinct models seen, in insertion order
	Summary        string
	ToolCalls      map[string]int
	InputTok       int64
	OutputTok      int64
	CacheCreateTok int64
	CacheReadTok   int64
	TotalTok       int64
	NumLines       int
	SubFiles       []string // subagent JSONL file paths
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
	fmt.Printf("  %-4s  %-6s  %-19s  %-9s  %-9s  %-10s  %-22s  %-36s  %s\n",
		"#", "Agent", "Started", "Tokens", "Duration", "Model", "Project/CWD", "Session", "Summary")
	fmt.Println("  " + strings.Repeat("-", 156))
}

func formatSessionListRow(index int, s *SessionInfo) string {
	if s == nil {
		return ""
	}
	started := "-"
	if !s.StartedAt.IsZero() {
		started = s.StartedAt.Format("2006-01-02 15:04")
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
		started,
		s.FormatTokens(),
		dur,
		truncateMiddle(firstNonEmpty(s.Model, "-"), 10),
		truncateMiddle(sessionProjectDisplay(s), 22),
		firstNonEmpty(s.SessionRef, "-"),
		truncate(firstNonEmpty(s.Summary, "-"), 48),
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
	projectFilter := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--all", "-a":
			showAll = true
		case "--project", "-p":
			if i+1 < len(args) {
				projectFilter = args[i+1]
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
		fmt.Println("No sessions found.")
		fmt.Println()
		fmt.Println("Claude Code session logs are stored at:")
		fmt.Printf("  %s/\n", claudeDir)
		fmt.Println()
		fmt.Println("Each .jsonl file = one session. Sub-agent sessions are excluded.")
		return
	}

	printSessionListHeader()

	for i, s := range sessions {
		fmt.Println(formatSessionListRow(i+1, s))
		if len(s.SubFiles) > 0 {
			fmt.Printf("        %-38s %d sub-agent(s)\n", "", len(s.SubFiles))
		}
	}

	fmt.Printf("\n  Total: %d sessions\n", len(sessions))
	fmt.Printf("  Claude logs: %s/\n", claudeDir)
	fmt.Printf("  Codex logs:  %s/\n\n", codexDir)
}

// ---- upload ----

func cmdUpload(args []string) {
	cfg := loadConfig()
	requireAuth(cfg)

	uploadAll := false
	var selectedIdx []int

	for _, a := range args {
		if a == "--all" || a == "-a" {
			uploadAll = true
		} else if n, err := strconv.Atoi(a); err == nil {
			selectedIdx = append(selectedIdx, n)
		}
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
		// Interactive picker
		fmt.Println("\nSelect sessions to upload:")
		fmt.Println()
		printSessionListHeader()
		for i, s := range sessions {
			fmt.Println(formatSessionListRow(i+1, s))
		}
		fmt.Println()
		fmt.Print("Enter session numbers (e.g. 1,3,5 or 'all'): ")

		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "all" || input == "a" {
			toUpload = sessions
		} else {
			for _, part := range strings.Split(input, ",") {
				part = strings.TrimSpace(part)
				if n, err := strconv.Atoi(part); err == nil && n >= 1 && n <= len(sessions) {
					toUpload = append(toUpload, sessions[n-1])
				}
			}
		}
	}

	if uploadAll {
		fmt.Println("\nSessions selected by --all:")
		printSessionListHeader()
		for i, s := range toUpload {
			fmt.Println(formatSessionListRow(i+1, s))
		}
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
			fmt.Printf("  [FAIL]  %-14s  %s  %v\n", s.SessionRef[:12], s.StartedAt.Format("15:04"), err)
			continue
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+cfg.Token)

		respBody, err := doRequest(req)
		if err != nil {
			fmt.Printf("  [FAIL]  %-14s  %s  %v\n", s.SessionRef[:12], s.StartedAt.Format("15:04"), err)
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
			fmt.Printf("  [FAIL]  %-14s  %s  invalid response: %v\n", s.SessionRef[:12], s.StartedAt.Format("15:04"), err)
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
				s.SessionRef[:12], s.StartedAt.Format("15:04"), s.FormatTokens(), trunc(s.Summary, 40))
			totalUploaded++
		case "updated":
			fmt.Printf("  [OK]    %-14s  %s  updated existing session\n",
				s.SessionRef[:12], s.StartedAt.Format("15:04"))
			totalUploaded++
		case "duplicate":
			fmt.Printf("  [SKIP]  %-14s  %s  (already uploaded)\n",
				s.SessionRef[:12], s.StartedAt.Format("15:04"))
		default:
			fmt.Printf("  [%s]  %-14s  %s\n", mainStatus, s.SessionRef[:12], s.StartedAt.Format("15:04"))
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

func cmdConsume(args []string) {
	cfg := loadConfig()

	once := false
	for _, a := range args {
		if a == "--once" {
			once = true
		}
	}

	consumerCfg := loadConsumerConfig()
	if consumerCfg.DatabaseURL == "" {
		requireAuth(cfg)
	}
	if once || consumerCfg.RunOnStart {
		if err := runConsumerOnce(cfg, consumerCfg); err != nil {
			fmt.Printf("[consumer] failed: %v\n", err)
			if once {
				os.Exit(1)
			}
		}
	}
	if once {
		return
	}

	mode := "server-db"
	if consumerCfg.DatabaseURL == "" {
		mode = "local-files"
	}
	fmt.Printf("[consumer] started. mode=%s daily_at=%s tz=%s\n",
		mode, consumerCfg.DailyAt, consumerCfg.TimeZone)
	for {
		next := nextDailyRun(time.Now(), consumerCfg.DailyAt)
		fmt.Printf("[consumer] next run at %s\n", next.Format(time.RFC3339))
		time.Sleep(time.Until(next))
		if err := runConsumerOnce(cfg, consumerCfg); err != nil {
			fmt.Printf("[consumer] failed: %v\n", err)
		}
	}
}

func runConsumerOnce(cfg *Config, consumerCfg ConsumerConfig) error {
	targetDate := time.Now().AddDate(0, 0, consumerCfg.ReportOffset).Format("2006-01-02")
	if targetDate != time.Now().Format("2006-01-02") {
		return fmt.Errorf("AIDA_REPORT_DATE_OFFSET is not supported by the current API; use 0 for today's report")
	}
	if consumerCfg.DatabaseURL != "" {
		return runServerConsumerOnce(consumerCfg, targetDate)
	}
	fmt.Printf("[consumer] processing report_date=%s\n", targetDate)

	sessions := scanSessions(consumerCfg.ClaudeDir, true)
	home, _ := os.UserHomeDir()
	sessions = append(sessions, scanCodexSessions(filepath.Join(home, ".codex", "sessions"), true)...)
	sortSessionsNewestFirst(sessions)
	sessions = filterSessionsForReport(sessions, targetDate, consumerCfg.ProjectFilter)
	if len(sessions) == 0 {
		fmt.Printf("[consumer] no sessions found for %s\n", targetDate)
	} else {
		fmt.Printf("[consumer] uploading %d session(s)\n", len(sessions))
		for _, s := range sessions {
			if err := uploadOneSession(cfg, s); err != nil {
				fmt.Printf("[consumer] upload failed %s: %v\n", shortRef(s.SessionRef), err)
			}
		}
	}

	report, err := getTodayReport(cfg)
	if err != nil {
		return err
	}
	prompt := buildDailyReportPrompt(targetDate, sessions)
	content, err := generateDailyReportWithClaude(consumerCfg, prompt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("claude returned empty report")
	}
	if err := updateReportContent(cfg, report.ID, content); err != nil {
		return err
	}
	fmt.Printf("[consumer] report updated: %s (%d chars)\n", report.ID, len(content))
	return nil
}

func filterSessionsForReport(sessions []*SessionInfo, reportDate, projectFilter string) []*SessionInfo {
	var filtered []*SessionInfo
	for _, s := range sessions {
		if s.StartedAt.Format("2006-01-02") != reportDate {
			continue
		}
		if projectFilter != "" && !strings.Contains(s.ProjectDir, projectFilter) && !strings.Contains(s.Cwd, projectFilter) {
			continue
		}
		filtered = append(filtered, s)
	}
	return filtered
}

func uploadOneSession(cfg *Config, s *SessionInfo) error {
	allSessions := collectSessionsWithFiles(s)
	return uploadBatchMultipart(cfg, allSessions)
}

type sessionWithFile struct {
	info     *SessionInfo
	filePath string
}

func collectSessionsWithFiles(s *SessionInfo) []sessionWithFile {
	var items []sessionWithFile
	items = append(items, sessionWithFile{info: s, filePath: s.FilePath})
	for _, subFile := range s.SubFiles {
		sub := parseJSONL(subFile)
		if sub != nil && sub.SessionRef != "" {
			items = append(items, sessionWithFile{info: sub, filePath: subFile})
		}
	}
	return items
}

func uploadBatchMultipart(cfg *Config, items []sessionWithFile) error {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	metadata := make([]map[string]any, 0, len(items))
	for _, item := range items {
		metadata = append(metadata, buildUploadPayload(item.info))
	}
	metadataJSON, _ := json.Marshal(map[string]any{"sessions": metadata})
	writer.WriteField("metadata", string(metadataJSON))

	for _, item := range items {
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
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	respBody, err := doRequest(req)
	if err != nil {
		return err
	}

	var result struct {
		Results []struct {
			SessionRef string `json:"session_ref"`
			Status     string `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return err
	}
	for _, r := range result.Results {
		if strings.HasPrefix(r.Status, "error:") {
			return fmt.Errorf("%s: %s", shortRef(r.SessionRef), r.Status)
		}
	}
	return nil
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

func buildDailyReportPrompt(reportDate string, sessions []*SessionInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "请根据下面的 Claude Code session log 摘要生成 %s 的个人工作日报。\n", reportDate)
	b.WriteString("要求：只输出 Markdown；使用中文；结构包含“今日完成”“问题与风险”“明日计划”“Session 明细”；内容要具体，避免夸大；没有信息时写“暂无”。\n\n")
	if len(sessions) == 0 {
		b.WriteString("当天没有扫描到 session log。\n")
		return b.String()
	}
	for i, s := range sessions {
		fmt.Fprintf(&b, "Session %d\n", i+1)
		fmt.Fprintf(&b, "- ID: %s\n", s.SessionRef)
		fmt.Fprintf(&b, "- Project: %s\n", firstNonEmpty(s.ProjectDir, s.Cwd))
		fmt.Fprintf(&b, "- Time: %s - %s\n", s.StartedAt.Format(time.RFC3339), s.EndedAt.Format(time.RFC3339))
		fmt.Fprintf(&b, "- Duration: %s\n", s.Duration())
		fmt.Fprintf(&b, "- Model: %s\n", firstNonEmpty(s.Model, "unknown"))
		fmt.Fprintf(&b, "- Tokens: input=%d output=%d total=%d\n", s.InputTok, s.OutputTok, s.TotalTok)
		fmt.Fprintf(&b, "- Tools: %s\n", formatToolCalls(s.ToolCalls))
		fmt.Fprintf(&b, "- First user request: %s\n\n", s.Summary)
	}
	return b.String()
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
			if sessions[j].StartedAt.After(sessions[i].StartedAt) {
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

		if event.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, event.Timestamp); err == nil {
				lastTS = t
				if s.StartedAt.IsZero() {
					s.StartedAt = t
				}
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
			if firstUserMsg && s.Summary == "" {
				var msg UserMsg
				if json.Unmarshal(event.Message, &msg) == nil {
					for _, c := range msg.Content {
						if c.Type == "text" && c.Text != "" {
							s.Summary = c.Text
							if len(s.Summary) > 200 {
								s.Summary = s.Summary[:197] + "..."
							}
							break
						}
					}
				}
				firstUserMsg = false
			}

		case "assistant":
			var msg AssistantMsg
			if json.Unmarshal(event.Message, &msg) == nil {
				if msg.Model != "" && msg.Model != "<synthetic>" {
					s.Model = msg.Model
					s.Models = appendDistinct(s.Models, msg.Model)
				}
				if msg.Usage != nil {
					s.InputTok += msg.Usage.InputTokens
					s.OutputTok += msg.Usage.OutputTokens
					s.CacheCreateTok += msg.Usage.CacheCreationInputTokens
					s.CacheReadTok += msg.Usage.CacheReadInputTokens
				}
				for _, c := range msg.Content {
					if c.Type == "tool_use" && c.Name != "" {
						s.ToolCalls[c.Name]++
					}
				}
			}
		}
	}

	s.TotalTok = s.InputTok + s.OutputTok + s.CacheCreateTok + s.CacheReadTok
	if !lastTS.IsZero() {
		s.EndedAt = lastTS
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

func doRequest(req *http.Request) (json.RawMessage, error) {
	client := &http.Client{Timeout: 60 * time.Second}
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
