package main

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lib/pq"
)

//go:embed report_skills/default_daily.md
var defaultDailyReportSkill string

type reportUser struct {
	ID   string
	Name string
}

type reportGenerateRequest struct {
	UserID     string `json:"user_id"`
	ReportDate string `json:"report_date"`
}

type reportDraftGenerateRequest struct {
	UserID              string                     `json:"user_id"`
	UserName            string                     `json:"user_name"`
	ReportDate          string                     `json:"report_date"`
	Sessions            []reportDraftSession       `json:"sessions"`
	TaskCandidates      []reportDraftTaskCandidate `json:"task_candidates"`
	SkillID             string                     `json:"skill_id"`
	SkillContent        string                     `json:"skill_content,omitempty"`
	IncludeTaskProgress bool                       `json:"include_task_progress"`
}

type personalWeeklyReportGenerateRequest struct {
	UserID               string   `json:"user_id"`
	WeekStart            string   `json:"week_start"`
	SourceDailyReportIDs []string `json:"source_daily_report_ids"`
}

type reportDraftSession struct {
	ID               string         `json:"id"`
	SessionRef       string         `json:"session_ref"`
	AgentType        string         `json:"agent_type"`
	StartedAt        time.Time      `json:"started_at"`
	EndedAt          *time.Time     `json:"ended_at,omitempty"`
	DurationSecs     *int           `json:"duration_secs,omitempty"`
	Model            string         `json:"model"`
	Summary          string         `json:"summary,omitempty"`
	ToolCallsJSON    map[string]int `json:"tool_calls_json,omitempty"`
	TaskID           *string        `json:"task_id,omitempty"`
	TaskTitle        string         `json:"task_title,omitempty"`
	RequirementID    *string        `json:"requirement_id,omitempty"`
	RequirementTitle string         `json:"requirement_title,omitempty"`
	InputTokens      int64          `json:"input_tokens"`
	OutputTokens     int64          `json:"output_tokens"`
	TotalTokens      int64          `json:"total_tokens"`
}

type reportDraftTaskCandidate struct {
	TaskID           string `json:"task_id"`
	TaskTitle        string `json:"task_title"`
	RequirementID    string `json:"requirement_id"`
	RequirementTitle string `json:"requirement_title"`
	CurrentStatus    string `json:"current_status"`
	CurrentProgress  int    `json:"current_progress"`
	Owner            string `json:"owner"`
}

type reportDraftResponse struct {
	ReportMarkdown          string                        `json:"report_markdown"`
	SelectedSessionIDs      []string                      `json:"selected_session_ids"`
	SkillName               string                        `json:"skill_name"`
	TaskProgressSuggestions []reportDraftTaskProgressItem `json:"task_progress_suggestions"`
}

type reportDraftTaskProgressItem struct {
	TaskID                string   `json:"task_id"`
	TaskTitle             string   `json:"task_title"`
	RequirementID         string   `json:"requirement_id,omitempty"`
	RequirementTitle      string   `json:"requirement_title,omitempty"`
	SuggestedStatus       string   `json:"suggested_status"`
	SuggestedProgress     int      `json:"suggested_progress"`
	EvidenceSessionIDs    []string `json:"evidence_session_ids"`
	EvidenceSessionTitles []string `json:"evidence_session_titles"`
	Reason                string   `json:"reason"`
}

type teamReportGenerateRequest struct {
	TeamID     string `json:"team_id"`
	LeaderID   string `json:"leader_id"`
	ReportDate string `json:"report_date"`
}

type teamWeeklyReportGenerateRequest struct {
	TeamID                        string   `json:"team_id"`
	LeaderID                      string   `json:"leader_id"`
	WeekStart                     string   `json:"week_start"`
	SourcePersonalWeeklyReportIDs []string `json:"source_personal_weekly_report_ids"`
}

type departmentReportGenerateRequest struct {
	ReportDate string `json:"report_date"`
}

type departmentWeeklyReportGenerateRequest struct {
	WeekStart string `json:"week_start"`
}

type teamMemberDailyReport struct {
	ID       string
	UserName string
	Content  string
}

type departmentTeamReport struct {
	ID         string
	TeamName   string
	LeaderName string
	Content    string
}

type weeklyPersonalReport struct {
	ID        string
	UserName  string
	Role      string
	WeekStart string
	WeekEnd   string
	Content   string
}

type weeklyDailyReport struct {
	ID         string
	UserName   string
	ReportDate string
	Content    string
}

type weeklyTeamDailyReport struct {
	ID         string
	ReportDate string
	Content    string
}

type weeklyTaskSummary struct {
	ID               string
	Title            string
	RequirementTitle string
	AssigneeName     string
	Status           string
	Priority         string
}

type reportSession struct {
	ID               string
	SessionRef       string
	StartedAt        time.Time
	EndedAt          sql.NullTime
	DurationSecs     sql.NullInt64
	Model            sql.NullString
	Summary          sql.NullString
	ToolCallsJSON    sql.NullString
	TaskTitle        sql.NullString
	RequirementTitle sql.NullString
	InputTokens      int64
	OutputTokens     int64
	TotalTokens      int64
}

func cmdServeReports(args []string) {
	cfg := loadConsumerConfig()
	if cfg.DatabaseURL == "" {
		fmt.Println("DATABASE_URL is required for report generator service")
		os.Exit(1)
	}

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		fmt.Printf("Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		fmt.Printf("Failed to connect database: %v\n", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/reports/generate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req reportGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writePlainJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.UserID == "" {
			writePlainJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id is required"})
			return
		}
		if req.ReportDate == "" {
			req.ReportDate = activityDate(time.Now())
		}
		reportID, sessionCount, err := generateServerReportForUser(db, cfg, req.UserID, req.ReportDate)
		if err != nil {
			writePlainJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writePlainJSON(w, http.StatusOK, map[string]any{
			"report_id":     reportID,
			"session_count": sessionCount,
		})
	})

	mux.HandleFunc("/reports/draft", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req reportDraftGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writePlainJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.UserID == "" {
			writePlainJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id is required"})
			return
		}
		if len(req.Sessions) == 0 {
			writePlainJSON(w, http.StatusBadRequest, map[string]string{"error": "sessions is required"})
			return
		}
		if req.SkillID != "" && req.SkillID != "default_daily" {
			writePlainJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported skill_id: " + req.SkillID})
			return
		}
		if req.ReportDate == "" {
			req.ReportDate = activityDate(time.Now())
		}
		draft, err := generateServerDraftReport(cfg, req)
		if err != nil {
			writePlainJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writePlainJSON(w, http.StatusOK, draft)
	})

	mux.HandleFunc("/reports/weekly/generate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req personalWeeklyReportGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writePlainJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.UserID == "" {
			writePlainJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id is required"})
			return
		}
		if req.WeekStart == "" {
			req.WeekStart = currentWeekStart().Format("2006-01-02")
		}
		content, err := generateServerPersonalWeeklyReportPreview(db, cfg, req)
		if err != nil {
			writePlainJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writePlainJSON(w, http.StatusOK, map[string]any{
			"report_markdown": content,
		})
	})

	mux.HandleFunc("/reports/team/generate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req teamReportGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writePlainJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.TeamID == "" || req.LeaderID == "" {
			writePlainJSON(w, http.StatusBadRequest, map[string]string{"error": "team_id and leader_id are required"})
			return
		}
		if req.ReportDate == "" {
			req.ReportDate = activityDate(time.Now())
		}
		reportID, err := generateServerTeamReport(db, cfg, req.TeamID, req.LeaderID, req.ReportDate)
		if err != nil {
			writePlainJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writePlainJSON(w, http.StatusOK, map[string]any{
			"report_id": reportID,
		})
	})
	mux.HandleFunc("/reports/department/generate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req departmentReportGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writePlainJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.ReportDate == "" {
			req.ReportDate = activityDate(time.Now())
		}
		reportID, err := generateServerDepartmentReport(db, cfg, req.ReportDate)
		if err != nil {
			writePlainJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writePlainJSON(w, http.StatusOK, map[string]any{
			"report_id": reportID,
		})
	})
	mux.HandleFunc("/reports/team/weekly/generate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req teamWeeklyReportGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writePlainJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.TeamID == "" || req.LeaderID == "" {
			writePlainJSON(w, http.StatusBadRequest, map[string]string{"error": "team_id and leader_id are required"})
			return
		}
		if req.WeekStart == "" {
			req.WeekStart = currentWeekStart().Format("2006-01-02")
		}
		content, err := generateServerTeamWeeklyReport(db, cfg, req)
		if err != nil {
			writePlainJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writePlainJSON(w, http.StatusOK, map[string]any{
			"report_markdown": content,
		})
	})
	mux.HandleFunc("/reports/department/weekly/generate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req departmentWeeklyReportGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writePlainJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.WeekStart == "" {
			req.WeekStart = currentWeekStart().Format("2006-01-02")
		}
		reportID, err := generateServerDepartmentWeeklyReport(db, cfg, req.WeekStart)
		if err != nil {
			writePlainJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writePlainJSON(w, http.StatusOK, map[string]any{
			"report_id": reportID,
		})
	})
	fmt.Printf("[report-generator] listening on :%s tz=%s\n", cfg.Port, cfg.TimeZone)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		fmt.Printf("Report generator failed: %v\n", err)
		os.Exit(1)
	}
}

func runServerConsumerOnce(cfg ConsumerConfig, targetDate string) error {
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return err
	}

	users, err := listReportUsers(db)
	if err != nil {
		return err
	}
	fmt.Printf("[consumer] server mode: report_date=%s users=%d\n", targetDate, len(users))

	for _, u := range users {
		if _, sessionCount, err := generateServerReportForUser(db, cfg, u.ID, targetDate); err != nil {
			fmt.Printf("[consumer] user %s report generation failed: %v\n", u.Name, err)
			continue
		} else {
			fmt.Printf("[consumer] report updated for %s (%d sessions)\n", u.Name, sessionCount)
		}
	}
	return nil
}

func generateServerReportForUser(db *sql.DB, cfg ConsumerConfig, userID, targetDate string) (string, int, error) {
	u, err := getReportUser(db, userID)
	if err != nil {
		return "", 0, err
	}
	sessions, err := listUserReportSessions(db, userID, targetDate, cfg.TimeZone)
	if err != nil {
		return "", 0, err
	}
	prompt := buildServerDailyReportPrompt(targetDate, *u, sessions)
	content, err := generateDailyReportWithClaude(cfg, prompt)
	if err != nil {
		return "", len(sessions), err
	}
	if strings.TrimSpace(content) == "" {
		return "", len(sessions), fmt.Errorf("claude returned empty report")
	}
	reportID, err := upsertDailyReport(db, userID, targetDate, content, sessions)
	if err != nil {
		return "", len(sessions), err
	}
	return reportID, len(sessions), nil
}

func generateServerDraftReport(cfg ConsumerConfig, req reportDraftGenerateRequest) (*reportDraftResponse, error) {
	prompt, err := buildServerDraftReportPrompt(req)
	if err != nil {
		return nil, err
	}
	content, err := generateDailyReportWithClaude(cfg, prompt)
	if err != nil {
		return nil, err
	}
	draft, err := parseReportDraftOutput(content, req)
	if err != nil {
		return nil, err
	}
	return draft, nil
}

func buildServerDraftReportPrompt(req reportDraftGenerateRequest) (string, error) {
	if len(req.Sessions) == 0 {
		return "", fmt.Errorf("sessions is required")
	}

	sessionIDs := make([]string, 0, len(req.Sessions))
	for _, session := range req.Sessions {
		sessionIDs = append(sessionIDs, session.ID)
	}
	sessionsJSON, _ := json.MarshalIndent(req.Sessions, "", "  ")
	tasksJSON, _ := json.MarshalIndent(req.TaskCandidates, "", "  ")

	var b strings.Builder
	b.WriteString(defaultDailyReportSkill)
	b.WriteString("\n\n")
	if strings.TrimSpace(req.SkillContent) != "" {
		b.WriteString("## 本次上传 Skill 补充约束\n\n")
		b.WriteString(req.SkillContent)
		b.WriteString("\n\n")
	}
	fmt.Fprintf(&b, "请为用户“%s”生成 %s 的个人日报草稿。\n", req.UserName, req.ReportDate)
	b.WriteString("你只能使用下面 JSON 数据中的 session 和任务候选。\n")
	b.WriteString("输出必须是一个 JSON object，不要 markdown code fence，不要解释文本。\n")
	b.WriteString("如果 include_task_progress=false 或没有明确任务证据，task_progress_suggestions 必须是空数组。\n")
	b.WriteString("任务建议只能引用 task_candidates 中的 task_id。\n")
	b.WriteString("证据 session 只能引用 selected_session_ids 中的 id。\n\n")
	fmt.Fprintf(&b, "selected_session_ids: %s\n", mustJSON(sessionIDs))
	fmt.Fprintf(&b, "include_task_progress: %t\n\n", req.IncludeTaskProgress)
	b.WriteString("sessions:\n")
	b.WriteString(string(sessionsJSON))
	b.WriteString("\n\n")
	b.WriteString("task_candidates:\n")
	b.WriteString(string(tasksJSON))
	b.WriteString("\n\n")
	b.WriteString(`只输出如下结构:
{
  "report_markdown": "# M 月 D 日日报\n\n## 今日完成\n...\n\n## 风险与阻塞\n...\n\n## 明日计划\n...",
  "task_progress_suggestions": []
}
`)
	return b.String(), nil
}

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func generateServerTeamReport(db *sql.DB, cfg ConsumerConfig, teamID, leaderID, targetDate string) (string, error) {
	fmt.Printf("[report-generator] generating team report team_id=%s leader_id=%s date=%s\n", teamID, leaderID, targetDate)
	leader, err := getReportUser(db, leaderID)
	if err != nil {
		return "", fmt.Errorf("leader not found: %w", err)
	}
	leaderSessions, err := listUserReportSessions(db, leaderID, targetDate, cfg.TimeZone)
	if err != nil {
		return "", fmt.Errorf("query leader sessions: %w", err)
	}

	rows, err := db.Query(`
		SELECT dr.id::text, u.name, dr.submitted_content
		FROM daily_reports dr
		JOIN users u ON u.id = dr.user_id
		WHERE u.team_id = $1
			AND u.role = 'employee'
			AND dr.report_date = $2
			AND dr.submitted_at IS NOT NULL
			AND dr.submitted_content IS NOT NULL
		ORDER BY u.name`, teamID, targetDate)
	if err != nil {
		return "", fmt.Errorf("query member reports: %w", err)
	}
	defer rows.Close()

	var memberReports []teamMemberDailyReport
	for rows.Next() {
		var mr teamMemberDailyReport
		if err := rows.Scan(&mr.ID, &mr.UserName, &mr.Content); err != nil {
			return "", err
		}
		memberReports = append(memberReports, mr)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	var teamName string
	if err := db.QueryRow("SELECT name FROM teams WHERE id = $1", teamID).Scan(&teamName); err != nil {
		teamName = "unknown"
	}

	prompt := buildTeamReportPrompt(targetDate, teamName, *leader, leaderSessions, memberReports)
	teamCfg := cfg
	if teamCfg.ClaudeTimeout > 20*time.Second {
		teamCfg.ClaudeTimeout = 20 * time.Second
	}
	content, err := generateDailyReportWithClaude(teamCfg, prompt)
	if err != nil {
		fmt.Printf("[report-generator] claude team report generation failed, using fallback: %v\n", err)
		content = buildTeamReportFallback(targetDate, teamName, *leader, leaderSessions, memberReports)
	}
	if strings.TrimSpace(content) == "" {
		content = buildTeamReportFallback(targetDate, teamName, *leader, leaderSessions, memberReports)
	}

	sessionIDs := make([]string, 0, len(leaderSessions))
	for _, s := range leaderSessions {
		sessionIDs = append(sessionIDs, s.ID)
	}
	memberReportIDs := make([]string, 0)
	for _, mr := range memberReports {
		if mr.ID != "" {
			memberReportIDs = append(memberReportIDs, mr.ID)
		}
	}

	var reportID string
	err = db.QueryRow(`
		INSERT INTO team_reports (team_id, leader_id, report_date, content, member_report_ids, source_daily_report_ids, session_ids, status, saved_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'saved', now())
		ON CONFLICT (team_id, report_date)
		DO UPDATE SET content = EXCLUDED.content,
			member_report_ids = EXCLUDED.member_report_ids,
			source_daily_report_ids = EXCLUDED.source_daily_report_ids,
			session_ids = EXCLUDED.session_ids,
			status = 'saved',
			saved_at = now(),
			updated_at = now()
		RETURNING id::text`,
		teamID, leaderID, targetDate, content,
		pq.Array(memberReportIDs), pq.Array(memberReportIDs), pq.Array(sessionIDs)).Scan(&reportID)
	if err != nil {
		return "", fmt.Errorf("upsert team_reports: %w", err)
	}
	return reportID, nil
}

func buildTeamReportFallback(reportDate, teamName string, leader reportUser, sessions []reportSession, memberReports []teamMemberDailyReport) string {
	var b strings.Builder
	totalTokens := int64(0)
	for _, s := range sessions {
		totalTokens += s.TotalTokens
	}
	memberReportCount := 0
	for _, mr := range memberReports {
		if strings.TrimSpace(mr.Content) != "" {
			memberReportCount++
		}
	}

	fmt.Fprintf(&b, "# %s 团队日报\n\n", reportDate)
	fmt.Fprintf(&b, "## 团队总结\n\n")
	fmt.Fprintf(&b, "%s 今日汇总了 TL 工作记录 %d 条、组员日报 %d 份。", teamName, len(sessions), memberReportCount)
	if len(sessions) == 0 && memberReportCount == 0 {
		b.WriteString("当前没有可用于生成日报的工作数据。")
	}
	b.WriteString("\n\n")

	b.WriteString("## TL 工作\n\n")
	fmt.Fprintf(&b, "负责人：%s\n\n", leader.Name)
	if len(sessions) == 0 {
		b.WriteString("暂无 TL 当日工作记录。\n\n")
	} else {
		fmt.Fprintf(&b, "- 工作记录数：%d\n", len(sessions))
		fmt.Fprintf(&b, "- Token 总量：%d\n\n", totalTokens)
		for i, s := range sessions {
			fmt.Fprintf(&b, "### 记录 %d\n\n", i+1)
			fmt.Fprintf(&b, "- Session：%s\n", s.SessionRef)
			fmt.Fprintf(&b, "- 时间：%s", s.StartedAt.Format(time.RFC3339))
			if s.EndedAt.Valid {
				fmt.Fprintf(&b, " - %s", s.EndedAt.Time.Format(time.RFC3339))
			}
			b.WriteString("\n")
			if s.TaskTitle.Valid && s.TaskTitle.String != "" {
				fmt.Fprintf(&b, "- 任务：%s\n", s.TaskTitle.String)
			}
			if s.RequirementTitle.Valid && s.RequirementTitle.String != "" {
				fmt.Fprintf(&b, "- 需求：%s\n", s.RequirementTitle.String)
			}
			fmt.Fprintf(&b, "- 摘要：%s\n\n", nullStringValue(s.Summary, "暂无"))
		}
	}

	b.WriteString("## 组员日报\n\n")
	if len(memberReports) == 0 {
		b.WriteString("暂无组员日报。\n\n")
	} else {
		for _, mr := range memberReports {
			fmt.Fprintf(&b, "### %s\n\n", mr.UserName)
			if strings.TrimSpace(mr.Content) == "" {
				b.WriteString("暂无日报。\n\n")
			} else {
				fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(mr.Content))
			}
		}
	}

	b.WriteString("## 问题与风险\n\n暂无。\n\n")
	b.WriteString("## 明日计划\n\n暂无。\n")
	return b.String()
}

func generateServerPersonalWeeklyReportPreview(db *sql.DB, cfg ConsumerConfig, req personalWeeklyReportGenerateRequest) (string, error) {
	start, err := time.Parse("2006-01-02", req.WeekStart)
	if err != nil {
		return "", fmt.Errorf("invalid week_start: %w", err)
	}
	weekEnd := start.AddDate(0, 0, 6).Format("2006-01-02")
	user, err := getReportUser(db, req.UserID)
	if err != nil {
		return "", err
	}
	if len(req.SourceDailyReportIDs) == 0 {
		return "", fmt.Errorf("source_daily_report_ids is required")
	}
	dailyReports, err := listPersonalWeeklyDailyReports(db, req.UserID, req.WeekStart, weekEnd, req.SourceDailyReportIDs)
	if err != nil {
		return "", err
	}
	if len(dailyReports) == 0 {
		return "", fmt.Errorf("source_daily_report_ids is required")
	}
	prompt := buildPersonalWeeklyReportPrompt(*user, req.WeekStart, weekEnd, dailyReports)
	weeklyCfg := cfg
	if weeklyCfg.ClaudeTimeout > 30*time.Second {
		weeklyCfg.ClaudeTimeout = 30 * time.Second
	}
	content, err := generateDailyReportWithClaude(weeklyCfg, prompt)
	if err != nil {
		fmt.Printf("[report-generator] claude personal weekly generation failed, using fallback: %v\n", err)
		content = buildPersonalWeeklyReportFallback(*user, req.WeekStart, weekEnd, dailyReports)
	}
	if strings.TrimSpace(content) == "" {
		content = buildPersonalWeeklyReportFallback(*user, req.WeekStart, weekEnd, dailyReports)
	}
	return content, nil
}

func listPersonalWeeklyDailyReports(db *sql.DB, userID, weekStart, weekEnd string, ids []string) ([]weeklyDailyReport, error) {
	query := `
		SELECT dr.id::text, u.name, dr.report_date, dr.content
		FROM daily_reports dr
		JOIN users u ON u.id = dr.user_id
		WHERE dr.user_id = $1 AND dr.report_date BETWEEN $2 AND $3 AND dr.status IS NOT NULL`
	args := []any{userID, weekStart, weekEnd}
	if len(ids) > 0 {
		query += " AND dr.id = ANY($4)"
		args = append(args, pq.Array(ids))
	}
	query += " ORDER BY dr.report_date"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []weeklyDailyReport{}
	for rows.Next() {
		var item weeklyDailyReport
		if err := rows.Scan(&item.ID, &item.UserName, &item.ReportDate, &item.Content); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func buildPersonalWeeklyReportPrompt(user reportUser, weekStart, weekEnd string, dailyReports []weeklyDailyReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是个人研发周报生成助手。请为用户“%s”生成 %s 至 %s 的个人周报。\n", user.Name, weekStart, weekEnd)
	b.WriteString("要求：只输出 Markdown；使用中文；只能基于用户选择的个人日报内容归纳，不要遍历或推测 session，不要引入任务列表或额外风险摘要。结构包含：本周完成、关键进展、风险与阻塞、下周计划、来源覆盖情况。\n\n")
	b.WriteString("## 用户选择的本周个人日报\n\n")
	for _, report := range dailyReports {
		fmt.Fprintf(&b, "### %s\n\n%s\n\n", report.ReportDate, strings.TrimSpace(report.Content))
	}
	return b.String()
}

func buildPersonalWeeklyReportFallback(user reportUser, weekStart, weekEnd string, dailyReports []weeklyDailyReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s 的个人周报（%s 至 %s）\n\n", user.Name, weekStart, weekEnd)
	fmt.Fprintf(&b, "## 本周完成\n\n本周共汇总 %d 份已选择个人日报。\n\n", len(dailyReports))
	if len(dailyReports) == 0 {
		b.WriteString("暂无已选择的个人日报。\n\n")
	} else {
		for _, report := range dailyReports {
			fmt.Fprintf(&b, "- %s：%s\n", report.ReportDate, firstLine(report.Content))
		}
		b.WriteString("\n")
	}
	b.WriteString("## 关键进展\n\n")
	b.WriteString("请基于上述日报内容补充关键进展。\n\n")
	b.WriteString("## 风险与阻塞\n\n")
	b.WriteString("请基于上述日报内容补充风险与阻塞。\n\n")
	b.WriteString("## 下周计划\n\n暂无。\n")
	return b.String()
}

func generateServerTeamWeeklyReport(db *sql.DB, cfg ConsumerConfig, req teamWeeklyReportGenerateRequest) (string, error) {
	start, err := time.Parse("2006-01-02", req.WeekStart)
	if err != nil {
		return "", fmt.Errorf("invalid week_start: %w", err)
	}
	weekEnd := start.AddDate(0, 0, 6).Format("2006-01-02")
	leader, err := getReportUser(db, req.LeaderID)
	if err != nil {
		return "", err
	}
	var teamName string
	if err := db.QueryRow("SELECT name FROM teams WHERE id = $1", req.TeamID).Scan(&teamName); err != nil {
		return "", err
	}
	if len(req.SourcePersonalWeeklyReportIDs) == 0 {
		return "", fmt.Errorf("source_personal_weekly_report_ids is required")
	}
	personalReports, err := listTeamWeeklyPersonalReports(db, req.TeamID, req.LeaderID, req.WeekStart, req.SourcePersonalWeeklyReportIDs)
	if err != nil {
		return "", err
	}
	if len(personalReports) == 0 {
		return "", fmt.Errorf("source_personal_weekly_report_ids is required")
	}
	prompt := buildTeamWeeklyReportPrompt(teamName, *leader, req.WeekStart, weekEnd, personalReports)
	weeklyCfg := cfg
	if weeklyCfg.ClaudeTimeout > 30*time.Second {
		weeklyCfg.ClaudeTimeout = 30 * time.Second
	}
	content, err := generateDailyReportWithClaude(weeklyCfg, prompt)
	if err != nil {
		fmt.Printf("[report-generator] claude team weekly generation failed, using fallback: %v\n", err)
		content = buildTeamWeeklyReportFallback(teamName, *leader, req.WeekStart, weekEnd, personalReports)
	}
	if strings.TrimSpace(content) == "" {
		content = buildTeamWeeklyReportFallback(teamName, *leader, req.WeekStart, weekEnd, personalReports)
	}
	return content, nil
}

func listTeamWeeklyPersonalReports(db *sql.DB, teamID, leaderID, weekStart string, ids []string) ([]weeklyPersonalReport, error) {
	rows, err := db.Query(`
		SELECT pwr.id::text, u.name,
			CASE WHEN u.id = $2 THEN 'leader' ELSE 'member' END AS source_role,
			pwr.week_start, pwr.week_end, COALESCE(pwr.submitted_content, pwr.content, '')
		FROM personal_weekly_reports pwr
		JOIN users u ON u.id = pwr.user_id
		WHERE pwr.id = ANY($4)
		  AND pwr.week_start = $3
		  AND pwr.status = 'submitted'
		  AND pwr.submitted_at IS NOT NULL
		  AND u.team_id = $1
		  AND u.role IN ('team_leader', 'employee')
		ORDER BY CASE WHEN u.id = $2 THEN 0 ELSE 1 END, u.name`, teamID, leaderID, weekStart, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []weeklyPersonalReport{}
	for rows.Next() {
		var item weeklyPersonalReport
		if err := rows.Scan(&item.ID, &item.UserName, &item.Role, &item.WeekStart, &item.WeekEnd, &item.Content); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func generateServerDepartmentWeeklyReport(db *sql.DB, cfg ConsumerConfig, weekStart string) (string, error) {
	start, err := time.Parse("2006-01-02", weekStart)
	if err != nil {
		return "", fmt.Errorf("invalid week_start: %w", err)
	}
	weekEnd := start.AddDate(0, 0, 6).Format("2006-01-02")
	teamReports, sourceIDs, err := listSubmittedTeamWeeklyReports(db, weekStart)
	if err != nil {
		return "", err
	}
	prompt := buildDepartmentWeeklyReportPrompt(weekStart, weekEnd, teamReports)
	weeklyCfg := cfg
	if weeklyCfg.ClaudeTimeout > 30*time.Second {
		weeklyCfg.ClaudeTimeout = 30 * time.Second
	}
	content, err := generateDailyReportWithClaude(weeklyCfg, prompt)
	if err != nil {
		fmt.Printf("[report-generator] claude department weekly generation failed, using fallback: %v\n", err)
		content = buildDepartmentWeeklyReportFallback(weekStart, weekEnd, teamReports)
	}
	if strings.TrimSpace(content) == "" {
		content = buildDepartmentWeeklyReportFallback(weekStart, weekEnd, teamReports)
	}
	return upsertDepartmentWeeklyReport(db, weekStart, content, sourceIDs)
}

func listUserReportSessionsRange(db *sql.DB, userID, fromDate, toDate, timeZone string) ([]reportSession, error) {
	rows, err := db.Query(`
		SELECT s.id::text, s.session_ref, s.started_at, s.ended_at, s.duration_secs,
			s.model, s.summary, COALESCE(s.tool_calls_json::text, '{}'),
			COALESCE(t.title, ''), COALESCE(r.title, ''),
			COALESCE(tu.input_tokens, 0), COALESCE(tu.output_tokens, 0), COALESCE(tu.total_tokens, 0)
		FROM sessions s
		LEFT JOIN tasks t ON t.id = s.task_id
		LEFT JOIN requirements r ON r.id = s.requirement_id
		LEFT JOIN (
			SELECT session_id, SUM(input_tokens) input_tokens, SUM(output_tokens) output_tokens, SUM(total_tokens) total_tokens
			FROM token_usage
			GROUP BY session_id
		) tu ON tu.session_id = s.id
		WHERE s.user_id = $1 AND DATE(s.started_at AT TIME ZONE $2) BETWEEN $3 AND $4
		ORDER BY s.started_at`, userID, timeZone, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []reportSession
	for rows.Next() {
		var s reportSession
		if err := rows.Scan(&s.ID, &s.SessionRef, &s.StartedAt, &s.EndedAt, &s.DurationSecs,
			&s.Model, &s.Summary, &s.ToolCallsJSON, &s.TaskTitle, &s.RequirementTitle,
			&s.InputTokens, &s.OutputTokens, &s.TotalTokens); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func listWeeklyDailyReports(db *sql.DB, teamID, weekStart, weekEnd string) ([]weeklyDailyReport, []string, error) {
	rows, err := db.Query(`
		SELECT dr.id::text, u.name, dr.report_date, dr.content
		FROM daily_reports dr
		JOIN users u ON u.id = dr.user_id
		WHERE u.team_id = $1 AND dr.report_date BETWEEN $2 AND $3
		ORDER BY dr.report_date, u.name`, teamID, weekStart, weekEnd)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items := []weeklyDailyReport{}
	ids := []string{}
	for rows.Next() {
		var item weeklyDailyReport
		if err := rows.Scan(&item.ID, &item.UserName, &item.ReportDate, &item.Content); err != nil {
			return nil, nil, err
		}
		items = append(items, item)
		ids = append(ids, item.ID)
	}
	return items, ids, rows.Err()
}

func listWeeklyTeamDailyReports(db *sql.DB, teamID, weekStart, weekEnd string) ([]weeklyTeamDailyReport, []string, error) {
	rows, err := db.Query(`
		SELECT id::text, report_date, content
		FROM team_reports
		WHERE team_id = $1 AND report_date BETWEEN $2 AND $3
		ORDER BY report_date`, teamID, weekStart, weekEnd)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items := []weeklyTeamDailyReport{}
	ids := []string{}
	for rows.Next() {
		var item weeklyTeamDailyReport
		if err := rows.Scan(&item.ID, &item.ReportDate, &item.Content); err != nil {
			return nil, nil, err
		}
		items = append(items, item)
		ids = append(ids, item.ID)
	}
	return items, ids, rows.Err()
}

func listWeeklyTaskSummaries(db *sql.DB, teamID, weekStart, weekEnd string) ([]weeklyTaskSummary, []string, error) {
	rows, err := db.Query(`
		SELECT DISTINCT task.id::text, task.title, req.title, COALESCE(assignee.name, ''), task.status, task.priority
		FROM tasks task
		JOIN requirements req ON req.id = task.requirement_id
		LEFT JOIN users assignee ON assignee.id = task.assignee_id
		WHERE (assignee.team_id = $1 OR task.creator_tl_id IN (SELECT id FROM users WHERE team_id = $1))
		  AND (DATE(task.updated_at) BETWEEN $2 AND $3 OR task.status = 'blocked' OR task.priority = 'high')
		ORDER BY task.status, task.priority DESC, task.title`, teamID, weekStart, weekEnd)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items := []weeklyTaskSummary{}
	ids := []string{}
	for rows.Next() {
		var item weeklyTaskSummary
		if err := rows.Scan(&item.ID, &item.Title, &item.RequirementTitle, &item.AssigneeName, &item.Status, &item.Priority); err != nil {
			return nil, nil, err
		}
		items = append(items, item)
		ids = append(ids, item.ID)
	}
	return items, ids, rows.Err()
}

func listSubmittedTeamWeeklyReports(db *sql.DB, weekStart string) ([]departmentTeamReport, []string, error) {
	rows, err := db.Query(`
		SELECT twr.id::text, t.name, COALESCE(u.name, ''), twr.content
		FROM team_weekly_reports twr
		JOIN teams t ON t.id = twr.team_id
		JOIN users u ON u.id = twr.leader_id
		WHERE twr.week_start = $1 AND twr.submitted_at IS NOT NULL
		ORDER BY t.name`, weekStart)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items := []departmentTeamReport{}
	ids := []string{}
	for rows.Next() {
		var item departmentTeamReport
		if err := rows.Scan(&item.ID, &item.TeamName, &item.LeaderName, &item.Content); err != nil {
			return nil, nil, err
		}
		items = append(items, item)
		ids = append(ids, item.ID)
	}
	return items, ids, rows.Err()
}

func upsertTeamWeeklyReport(db *sql.DB, teamID, leaderID, weekStart, content string, dailyIDs, teamReportIDs, taskIDs []string) (string, error) {
	var reportID string
	err := db.QueryRow(`
		INSERT INTO team_weekly_reports (team_id, leader_id, week_start, content, source_daily_report_ids, source_team_report_ids, source_task_ids)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (team_id, week_start)
		DO UPDATE SET content = EXCLUDED.content,
			leader_id = EXCLUDED.leader_id,
			source_daily_report_ids = EXCLUDED.source_daily_report_ids,
			source_team_report_ids = EXCLUDED.source_team_report_ids,
			source_task_ids = EXCLUDED.source_task_ids,
			submitted_at = NULL,
			updated_at = now()
		RETURNING id::text`,
		teamID, leaderID, weekStart, content, pq.Array(dailyIDs), pq.Array(teamReportIDs), pq.Array(taskIDs)).Scan(&reportID)
	return reportID, err
}

func upsertDepartmentWeeklyReport(db *sql.DB, weekStart, content string, sourceIDs []string) (string, error) {
	var reportID string
	err := db.QueryRow(`
		INSERT INTO department_weekly_reports (week_start, content, source_team_weekly_report_ids)
		VALUES ($1, $2, $3)
		ON CONFLICT (week_start)
		DO UPDATE SET content = EXCLUDED.content,
			source_team_weekly_report_ids = EXCLUDED.source_team_weekly_report_ids,
			archived_at = NULL,
			updated_at = now()
		RETURNING id::text`, weekStart, content, pq.Array(sourceIDs)).Scan(&reportID)
	return reportID, err
}

func buildTeamWeeklyReportPrompt(teamName string, leader reportUser, weekStart, weekEnd string, personalReports []weeklyPersonalReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是小组周报生成助手。请为小组 %s（TL：%s）生成 %s 至 %s 的小组周报。\n", teamName, leader.Name, weekStart, weekEnd)
	b.WriteString("要求：只输出 Markdown；使用中文；只能基于 TL 选择的本人和成员个人周报内容归纳，不要遍历或推测 session，不要引入日报、小组日报、任务或额外风险摘要。结构包含：本周概览、主要进展、成员贡献、风险与阻塞、下周计划、数据覆盖情况。\n\n")
	b.WriteString("## TL 选择的个人周报\n\n")
	for _, report := range personalReports {
		roleLabel := "成员"
		if report.Role == "leader" {
			roleLabel = "TL本人"
		}
		fmt.Fprintf(&b, "### %s · %s（%s）\n\n%s\n\n", report.UserName, report.WeekStart, roleLabel, strings.TrimSpace(report.Content))
	}
	return b.String()
}

func buildTeamWeeklyReportFallback(teamName string, leader reportUser, weekStart, weekEnd string, personalReports []weeklyPersonalReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s 小组周报（%s 至 %s）\n\n", teamName, weekStart, weekEnd)
	fmt.Fprintf(&b, "## 本周概览\n\n本周共汇总 %d 份已选择个人周报。\n\n", len(personalReports))
	b.WriteString("## 主要进展\n\n")
	if len(personalReports) == 0 {
		b.WriteString("暂无已选择个人周报。\n\n")
	} else {
		for _, report := range personalReports {
			fmt.Fprintf(&b, "- %s：%s\n", report.UserName, firstLine(report.Content))
		}
		b.WriteString("\n")
	}
	b.WriteString("## 成员贡献\n\n")
	b.WriteString("请基于上述个人周报内容补充成员贡献。\n\n")
	b.WriteString("## 风险与阻塞\n\n")
	b.WriteString("请基于上述个人周报内容补充风险与阻塞。\n\n")
	fmt.Fprintf(&b, "## 下周计划\n\n由 TL %s 结合本周进展继续补充。\n", leader.Name)
	return b.String()
}

func buildDepartmentWeeklyReportPrompt(weekStart, weekEnd string, teamReports []departmentTeamReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是部门周报生成助手。请根据各 TL 已提交的小组周报，生成 %s 至 %s 的部门周报。\n", weekStart, weekEnd)
	b.WriteString("要求：只输出 Markdown；使用中文；主来源只能是已提交小组周报。结构包含：部门概览、各团队进展、跨团队风险、下周重点。缺少信息时写 暂无。\n\n")
	b.WriteString("## 已提交小组周报\n\n")
	for _, report := range teamReports {
		fmt.Fprintf(&b, "### %s（TL：%s）\n\n%s\n\n", report.TeamName, report.LeaderName, strings.TrimSpace(report.Content))
	}
	return b.String()
}

func buildDepartmentWeeklyReportFallback(weekStart, weekEnd string, teamReports []departmentTeamReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 部门周报（%s 至 %s）\n\n", weekStart, weekEnd)
	fmt.Fprintf(&b, "## 部门概览\n\n本周共汇总 %d 份已提交小组周报。\n\n", len(teamReports))
	b.WriteString("## 各团队进展\n\n")
	if len(teamReports) == 0 {
		b.WriteString("暂无已提交小组周报。\n\n")
	} else {
		for _, report := range teamReports {
			fmt.Fprintf(&b, "### %s\n\n负责人：%s\n\n%s\n\n", report.TeamName, report.LeaderName, strings.TrimSpace(report.Content))
		}
	}
	b.WriteString("## 跨团队风险\n\n暂无。\n\n")
	b.WriteString("## 下周重点\n\n暂无。\n")
	return b.String()
}

func currentWeekStart() time.Time {
	now := time.Now()
	daysFromMonday := (int(now.Weekday()) + 6) % 7
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -daysFromMonday)
}

func firstLine(content string) string {
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return "暂无"
}

func generateServerDepartmentReport(db *sql.DB, cfg ConsumerConfig, targetDate string) (string, error) {
	fmt.Printf("[report-generator] generating department report date=%s\n", targetDate)
	rows, err := db.Query(`
		SELECT tr.id::text, t.name, COALESCE(u.name, ''), tr.submitted_content
		FROM team_reports tr
		JOIN teams t ON t.id = tr.team_id
		JOIN users u ON u.id = tr.leader_id
		WHERE tr.report_date = $1
			AND tr.submitted_at IS NOT NULL
			AND tr.submitted_content IS NOT NULL
		ORDER BY t.name`, targetDate)
	if err != nil {
		return "", fmt.Errorf("query submitted team reports: %w", err)
	}
	defer rows.Close()

	var teamReports []departmentTeamReport
	for rows.Next() {
		var tr departmentTeamReport
		if err := rows.Scan(&tr.ID, &tr.TeamName, &tr.LeaderName, &tr.Content); err != nil {
			return "", err
		}
		teamReports = append(teamReports, tr)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	prompt := buildDepartmentReportPrompt(targetDate, teamReports)
	departmentCfg := cfg
	if departmentCfg.ClaudeTimeout > 20*time.Second {
		departmentCfg.ClaudeTimeout = 20 * time.Second
	}
	content, err := generateDailyReportWithClaude(departmentCfg, prompt)
	if err != nil {
		fmt.Printf("[report-generator] claude department report generation failed, using fallback: %v\n", err)
		content = buildDepartmentReportFallback(targetDate, teamReports)
	}
	if strings.TrimSpace(content) == "" {
		content = buildDepartmentReportFallback(targetDate, teamReports)
	}

	sourceIDs := make([]string, 0, len(teamReports))
	for _, tr := range teamReports {
		sourceIDs = append(sourceIDs, tr.ID)
	}

	var reportID string
	err = db.QueryRow(`
		INSERT INTO department_reports (report_date, content, source_team_report_ids)
		VALUES ($1, $2, $3)
		ON CONFLICT (report_date)
		DO UPDATE SET content = EXCLUDED.content,
			source_team_report_ids = EXCLUDED.source_team_report_ids,
			status = NULL,
			saved_at = NULL,
			archived_at = NULL,
			updated_at = now()
		RETURNING id::text`,
		targetDate, content, pq.Array(sourceIDs)).Scan(&reportID)
	if err != nil {
		return "", fmt.Errorf("upsert department_reports: %w", err)
	}
	return reportID, nil
}

func buildDepartmentReportFallback(reportDate string, teamReports []departmentTeamReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s 部门日报\n\n", reportDate)
	fmt.Fprintf(&b, "## 部门总结\n\n今日共汇总 %d 份已提交小组日报。", len(teamReports))
	if len(teamReports) == 0 {
		b.WriteString("当前没有已提交的小组日报。")
	}
	b.WriteString("\n\n")
	b.WriteString("## 各组进展\n\n")
	if len(teamReports) == 0 {
		b.WriteString("暂无。\n\n")
	} else {
		for _, tr := range teamReports {
			fmt.Fprintf(&b, "### %s\n\n负责人：%s\n\n%s\n\n", tr.TeamName, tr.LeaderName, strings.TrimSpace(tr.Content))
		}
	}
	b.WriteString("## 重点风险\n\n暂无。\n\n")
	b.WriteString("## 明日重点\n\n暂无。\n")
	return b.String()
}

func buildDepartmentReportPrompt(reportDate string, teamReports []departmentTeamReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是部门日报生成助手。请根据下面已由 TL 提交的小组日报，生成 %s 的部门日报。\n", reportDate)
	b.WriteString("要求：只输出 Markdown；使用中文；不要直接展开员工个人日报；只基于已提交的小组日报归纳。\n")
	b.WriteString("结构必须包含：\n")
	b.WriteString("1. 部门总结 — 一段话概述部门今日整体进展\n")
	b.WriteString("2. 各组进展 — 按小组汇总核心进展\n")
	b.WriteString("3. 重点风险 — 归纳跨组风险、阻塞和需总监关注事项\n")
	b.WriteString("4. 明日重点 — 给出部门层面的下一步重点\n")
	b.WriteString("缺少信息时写 暂无。\n\n")
	b.WriteString("## 已提交小组日报\n\n")
	if len(teamReports) == 0 {
		b.WriteString("暂无已提交小组日报。\n")
		return b.String()
	}
	for i, tr := range teamReports {
		fmt.Fprintf(&b, "### 小组 %d: %s\n", i+1, tr.TeamName)
		fmt.Fprintf(&b, "- TL: %s\n", tr.LeaderName)
		fmt.Fprintf(&b, "- Team Report ID: %s\n\n", tr.ID)
		fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(tr.Content))
	}
	return b.String()
}

func buildTeamReportPrompt(reportDate, teamName string, leader reportUser, sessions []reportSession, memberReports []teamMemberDailyReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是团队日报生成助手。请根据下面的数据为团队 %s (负责人: %s) 生成 %s 的团队日报。\n", teamName, leader.Name, reportDate)
	b.WriteString("要求：只输出 Markdown；使用中文；结构必须包含：\n")
	b.WriteString("1. 团队总结 — 一段话概述团队今日整体工作进展\n")
	b.WriteString("2. TL 工作 — 负责人的 session 明细和工作摘要\n")
	b.WriteString("3. 组员日报 — 每个组员的工作内容\n")
	b.WriteString("内容要具体，避免夸大；缺少信息时写 暂无。\n\n")

	b.WriteString("## TL Session 数据\n\n")
	if len(sessions) == 0 {
		b.WriteString("负责人当天没有已上报的 session 数据。\n\n")
	} else {
		for i, s := range sessions {
			fmt.Fprintf(&b, "Session %d\n", i+1)
			fmt.Fprintf(&b, "- ID: %s\n", s.SessionRef)
			fmt.Fprintf(&b, "- Time: %s", s.StartedAt.Format(time.RFC3339))
			if s.EndedAt.Valid {
				fmt.Fprintf(&b, " - %s", s.EndedAt.Time.Format(time.RFC3339))
			}
			b.WriteString("\n")
			fmt.Fprintf(&b, "- Model: %s\n", nullStringValue(s.Model, "unknown"))
			fmt.Fprintf(&b, "- Tokens: input=%d output=%d total=%d\n", s.InputTokens, s.OutputTokens, s.TotalTokens)
			if s.TaskTitle.Valid && s.TaskTitle.String != "" {
				fmt.Fprintf(&b, "- Task: %s\n", s.TaskTitle.String)
			}
			if s.RequirementTitle.Valid && s.RequirementTitle.String != "" {
				fmt.Fprintf(&b, "- Requirement: %s\n", s.RequirementTitle.String)
			}
			fmt.Fprintf(&b, "- Summary: %s\n\n", nullStringValue(s.Summary, ""))
		}
	}

	b.WriteString("## 组员日报\n\n")
	if len(memberReports) == 0 {
		b.WriteString("没有找到组员日报数据。\n")
	} else {
		for _, mr := range memberReports {
			fmt.Fprintf(&b, "### %s\n", mr.UserName)
			if mr.Content != "" {
				fmt.Fprintf(&b, "%s\n\n", mr.Content)
			} else {
				b.WriteString("暂无日报\n\n")
			}
		}
	}
	return b.String()
}

func getReportUser(db *sql.DB, userID string) (*reportUser, error) {
	var u reportUser
	err := db.QueryRow(`
		SELECT id::text, name
		FROM users
		WHERE id = $1`, userID).Scan(&u.ID, &u.Name)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func listReportUsers(db *sql.DB) ([]reportUser, error) {
	rows, err := db.Query(`
		SELECT id::text, name
		FROM users
		WHERE role = 'employee'
		ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []reportUser
	for rows.Next() {
		var u reportUser
		if err := rows.Scan(&u.ID, &u.Name); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func listUserReportSessions(db *sql.DB, userID, reportDate, timeZone string) ([]reportSession, error) {
	rows, err := db.Query(`
		SELECT s.id::text, s.session_ref, s.started_at, s.ended_at, s.duration_secs,
			s.model, s.summary, COALESCE(s.tool_calls_json::text, '{}'),
			COALESCE(t.title, ''), COALESCE(r.title, ''),
			COALESCE(tu.input_tokens, 0), COALESCE(tu.output_tokens, 0), COALESCE(tu.total_tokens, 0)
		FROM sessions s
		LEFT JOIN tasks t ON t.id = s.task_id
		LEFT JOIN requirements r ON r.id = s.requirement_id
		LEFT JOIN (
			SELECT session_id, SUM(input_tokens) input_tokens, SUM(output_tokens) output_tokens, SUM(total_tokens) total_tokens
			FROM token_usage
			GROUP BY session_id
		) tu ON tu.session_id = s.id
		WHERE s.user_id = $1 AND DATE(s.started_at AT TIME ZONE $2) = $3
		ORDER BY s.started_at`, userID, timeZone, reportDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []reportSession
	for rows.Next() {
		var s reportSession
		if err := rows.Scan(&s.ID, &s.SessionRef, &s.StartedAt, &s.EndedAt, &s.DurationSecs,
			&s.Model, &s.Summary, &s.ToolCallsJSON, &s.TaskTitle, &s.RequirementTitle,
			&s.InputTokens, &s.OutputTokens, &s.TotalTokens); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func upsertDailyReport(db *sql.DB, userID, reportDate, content string, sessions []reportSession) (string, error) {
	sessionIDs := make([]string, 0, len(sessions))
	for _, s := range sessions {
		sessionIDs = append(sessionIDs, s.ID)
	}
	var reportID string
	err := db.QueryRow(`
		INSERT INTO daily_reports (user_id, report_date, content, session_ids, edited, updated_at)
		VALUES ($1, $2, $3, $4, false, now())
		ON CONFLICT (user_id, report_date)
		DO UPDATE SET content = EXCLUDED.content,
			session_ids = EXCLUDED.session_ids,
			edited = false,
			updated_at = now()
		RETURNING id::text`,
		userID, reportDate, content, pq.Array(sessionIDs)).Scan(&reportID)
	return reportID, err
}

func buildServerDailyReportPrompt(reportDate string, user reportUser, sessions []reportSession) string {
	var b strings.Builder
	fmt.Fprintf(&b, "请根据平台中已上报的 Claude Code session 数据，为用户“%s”生成 %s 的个人工作日报。\n", user.Name, reportDate)
	b.WriteString("要求：只输出 Markdown；使用中文；结构包含“今日完成”“问题与风险”“明日计划”“Session 明细”；内容要具体，避免夸大；没有信息时写“暂无”。\n\n")
	if len(sessions) == 0 {
		b.WriteString("当天没有已上报的 session 数据。\n")
		return b.String()
	}
	for i, s := range sessions {
		fmt.Fprintf(&b, "Session %d\n", i+1)
		fmt.Fprintf(&b, "- ID: %s\n", s.SessionRef)
		fmt.Fprintf(&b, "- Time: %s", s.StartedAt.Format(time.RFC3339))
		if s.EndedAt.Valid {
			fmt.Fprintf(&b, " - %s", s.EndedAt.Time.Format(time.RFC3339))
		}
		b.WriteString("\n")
		if s.DurationSecs.Valid {
			fmt.Fprintf(&b, "- Duration seconds: %d\n", s.DurationSecs.Int64)
		}
		fmt.Fprintf(&b, "- Model: %s\n", nullStringValue(s.Model, "unknown"))
		fmt.Fprintf(&b, "- Tokens: input=%d output=%d total=%d\n", s.InputTokens, s.OutputTokens, s.TotalTokens)
		if s.TaskTitle.Valid && s.TaskTitle.String != "" {
			fmt.Fprintf(&b, "- Task: %s\n", s.TaskTitle.String)
		}
		if s.RequirementTitle.Valid && s.RequirementTitle.String != "" {
			fmt.Fprintf(&b, "- Requirement: %s\n", s.RequirementTitle.String)
		}
		fmt.Fprintf(&b, "- Tool calls JSON: %s\n", nullStringValue(s.ToolCallsJSON, "{}"))
		fmt.Fprintf(&b, "- Summary: %s\n\n", nullStringValue(s.Summary, ""))
	}
	return b.String()
}

func nullStringValue(v sql.NullString, fallback string) string {
	if v.Valid && v.String != "" {
		return v.String
	}
	return fallback
}

func parseReportDraftOutput(raw string, req reportDraftGenerateRequest) (*reportDraftResponse, error) {
	clean := strings.TrimSpace(raw)
	var draft reportDraftResponse
	if err := json.Unmarshal([]byte(clean), &draft); err != nil {
		extracted, extractErr := extractFirstJSONObject(clean)
		if extractErr != nil {
			return nil, fmt.Errorf("parse report draft json: %w", err)
		}
		if err := json.Unmarshal([]byte(extracted), &draft); err != nil {
			return nil, fmt.Errorf("parse report draft json: %w", err)
		}
	}
	if strings.TrimSpace(draft.ReportMarkdown) == "" {
		return nil, fmt.Errorf("report_markdown is required")
	}

	selectedIDs := make([]string, 0, len(req.Sessions))
	sessionTitleByID := make(map[string]string, len(req.Sessions))
	for _, session := range req.Sessions {
		selectedIDs = append(selectedIDs, session.ID)
		sessionTitleByID[session.ID] = draftSessionTitle(session)
	}
	draft.SelectedSessionIDs = selectedIDs
	draft.SkillName = "默认日报 Skill"
	if !req.IncludeTaskProgress {
		draft.TaskProgressSuggestions = []reportDraftTaskProgressItem{}
		return &draft, nil
	}

	taskByID := make(map[string]reportDraftTaskCandidate, len(req.TaskCandidates))
	for _, task := range req.TaskCandidates {
		taskByID[task.TaskID] = task
	}
	normalized := make([]reportDraftTaskProgressItem, 0, len(draft.TaskProgressSuggestions))
	for _, suggestion := range draft.TaskProgressSuggestions {
		task, ok := taskByID[suggestion.TaskID]
		if !ok {
			continue
		}
		if !isDraftTaskStatus(suggestion.SuggestedStatus) {
			continue
		}
		evidenceIDs := make([]string, 0, len(suggestion.EvidenceSessionIDs))
		evidenceTitles := make([]string, 0, len(suggestion.EvidenceSessionIDs))
		seen := map[string]bool{}
		for _, sessionID := range suggestion.EvidenceSessionIDs {
			title, ok := sessionTitleByID[sessionID]
			if !ok || seen[sessionID] {
				continue
			}
			seen[sessionID] = true
			evidenceIDs = append(evidenceIDs, sessionID)
			evidenceTitles = append(evidenceTitles, title)
		}
		if len(evidenceIDs) == 0 {
			continue
		}
		suggestion.TaskTitle = task.TaskTitle
		suggestion.RequirementID = task.RequirementID
		suggestion.RequirementTitle = task.RequirementTitle
		suggestion.SuggestedProgress = clampDraftProgress(suggestion.SuggestedProgress)
		suggestion.EvidenceSessionIDs = evidenceIDs
		suggestion.EvidenceSessionTitles = evidenceTitles
		normalized = append(normalized, suggestion)
	}
	draft.TaskProgressSuggestions = normalized
	return &draft, nil
}

func extractFirstJSONObject(raw string) (string, error) {
	start := strings.Index(raw, "{")
	if start < 0 {
		return "", fmt.Errorf("json object not found")
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1], nil
			}
		}
	}
	return "", fmt.Errorf("json object is incomplete")
}

func draftSessionTitle(session reportDraftSession) string {
	agent := session.AgentType
	if agent == "" {
		agent = "session"
	}
	start := session.StartedAt.Format("15:04")
	if session.StartedAt.IsZero() {
		return agent
	}
	if session.EndedAt != nil {
		return fmt.Sprintf("%s %s - %s", agent, start, session.EndedAt.Format("15:04"))
	}
	return fmt.Sprintf("%s %s", agent, start)
}

func isDraftTaskStatus(status string) bool {
	return status == "todo" || status == "in_progress" || status == "done"
}

func clampDraftProgress(progress int) int {
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

func generateDailyReportWithClaude(cfg ConsumerConfig, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ClaudeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cfg.ClaudeBin, "-p", prompt)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("claude timed out after %s", cfg.ClaudeTimeout)
	}
	if err != nil {
		return "", fmt.Errorf("claude failed: %w: %s", err, truncate(stderr.String(), 500))
	}
	return strings.TrimSpace(string(out)), nil
}

func writePlainJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
