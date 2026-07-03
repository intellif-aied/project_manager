package handler

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/service"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

type ReportHandler struct {
	db                 *sql.DB
	reportGeneratorURL string
}

func NewReportHandler(db *sql.DB, reportGeneratorURL string) *ReportHandler {
	return &ReportHandler{db: db, reportGeneratorURL: reportGeneratorURL}
}

func (h *ReportHandler) List(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	page, pageSize := parsePagination(r, 20, 100)

	where := " WHERE dr.status IS NOT NULL"
	args := []any{}
	argIdx := 1

	if r.URL.Query().Get("scope") == "mine" {
		where += fmt.Sprintf(" AND dr.user_id = $%d", argIdx)
		args = append(args, u.ID)
		argIdx++
	} else if u.Role == "employee" {
		where += fmt.Sprintf(" AND dr.user_id = $%d", argIdx)
		args = append(args, u.ID)
		argIdx++
	} else if u.Role == "team_leader" || u.Role == "pm" {
		if u.TeamID == nil {
			where += fmt.Sprintf(" AND dr.user_id = $%d", argIdx)
			args = append(args, u.ID)
			argIdx++
		} else {
			where += fmt.Sprintf(" AND dr.user_id IN (SELECT id FROM users WHERE team_id = $%d)", argIdx)
			args = append(args, *u.TeamID)
			argIdx++
		}
	}

	if from := r.URL.Query().Get("from"); from != "" {
		where += fmt.Sprintf(" AND dr.report_date >= $%d", argIdx)
		args = append(args, from)
		argIdx++
	}
	if to := r.URL.Query().Get("to"); to != "" {
		where += fmt.Sprintf(" AND dr.report_date <= $%d", argIdx)
		args = append(args, to)
		argIdx++
	}

	var total int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM daily_reports dr"+where, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	query := `
		SELECT dr.id::text, dr.user_id::text, COALESCE(NULLIF(u.nickname,''), u.username), dr.report_date, dr.status, dr.submitted_to, dr.edited,
			COALESCE(cardinality(dr.session_ids), 0), COALESCE(dr.session_ids, '{}'),
			dr.saved_at, dr.submitted_at, dr.created_at, dr.updated_at
		FROM daily_reports dr
		JOIN users u ON u.id = dr.user_id` + where + fmt.Sprintf(" ORDER BY dr.report_date DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	reports := []model.DailyReportListItem{}
	for rows.Next() {
		var dr model.DailyReportListItem
		var sessionIDsStr string
		var status, submittedTo sql.NullString
		var savedAt, submittedAt sql.NullTime
		if err := rows.Scan(&dr.ID, &dr.UserID, &dr.UserName, &dr.ReportDate, &status, &submittedTo, &dr.Edited, &dr.SourceSessionCount, &sessionIDsStr, &savedAt, &submittedAt, &dr.CreatedAt, &dr.UpdatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		dr.Status = nullStringPtr(status)
		dr.SubmittedTo = nullStringPtr(submittedTo)
		dr.SavedAt = nullTimePtr(savedAt)
		dr.SubmittedAt = nullTimePtr(submittedAt)
		dr.SessionIDs = parseUUIDArray(sessionIDsStr)
		reports = append(reports, dr)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, model.PaginatedDailyReports{
		Items:    reports,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

func (h *ReportHandler) ListMine(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	page, pageSize := parsePagination(r, 20, 100)

	where := " WHERE dr.status IS NOT NULL AND dr.user_id = $1"
	args := []any{u.ID}
	argIdx := 2

	if from := r.URL.Query().Get("from"); from != "" {
		where += fmt.Sprintf(" AND dr.report_date >= $%d", argIdx)
		args = append(args, from)
		argIdx++
	}
	if to := r.URL.Query().Get("to"); to != "" {
		where += fmt.Sprintf(" AND dr.report_date <= $%d", argIdx)
		args = append(args, to)
		argIdx++
	}

	var total int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM daily_reports dr"+where, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	query := `
		SELECT dr.id::text, dr.user_id::text, COALESCE(NULLIF(u.nickname,''), u.username), dr.report_date, dr.status, dr.submitted_to, dr.edited,
			COALESCE(cardinality(dr.session_ids), 0), COALESCE(dr.session_ids, '{}'),
			dr.saved_at, dr.submitted_at, dr.created_at, dr.updated_at
		FROM daily_reports dr
		JOIN users u ON u.id = dr.user_id` + where + fmt.Sprintf(" ORDER BY dr.report_date DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	reports := []model.DailyReportListItem{}
	for rows.Next() {
		var dr model.DailyReportListItem
		var sessionIDsStr string
		var status, submittedTo sql.NullString
		var savedAt, submittedAt sql.NullTime
		if err := rows.Scan(&dr.ID, &dr.UserID, &dr.UserName, &dr.ReportDate, &status, &submittedTo, &dr.Edited, &dr.SourceSessionCount, &sessionIDsStr, &savedAt, &submittedAt, &dr.CreatedAt, &dr.UpdatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		dr.Status = nullStringPtr(status)
		dr.SubmittedTo = nullStringPtr(submittedTo)
		dr.SavedAt = nullTimePtr(savedAt)
		dr.SubmittedAt = nullTimePtr(submittedAt)
		dr.SessionIDs = parseUUIDArray(sessionIDsStr)
		reports = append(reports, dr)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, model.PaginatedDailyReports{
		Items:    reports,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

func (h *ReportHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)
	var dr model.DailyReport
	var feishuURL, submittedContent, status, submittedTo sql.NullString
	var generationMode, managedAgentRunID, agentID, modelID sql.NullString
	var agentVersionID sql.NullInt64
	var savedAt, submittedAt, generatedAt sql.NullTime
	var sessionIDsStr string
	var reportUserTeamID sql.NullString

	err := h.db.QueryRow(`
		SELECT dr.id, dr.user_id, COALESCE(NULLIF(report_user.nickname,''), report_user.username), report_user.team_id::text, dr.report_date, dr.content, dr.edited,
			dr.feishu_doc_url, COALESCE(dr.session_ids, '{}'), dr.status, dr.submitted_content, dr.saved_at, dr.submitted_at, dr.submitted_to,
			COALESCE(dr.generation_mode, ''), dr.managed_agent_run_id::text, dr.agent_id, dr.agent_version_id, dr.model_id, ar.finished_at,
			dr.created_at, dr.updated_at
		FROM daily_reports dr
		JOIN users report_user ON report_user.id = dr.user_id
		LEFT JOIN ai_runs ar ON ar.id = dr.managed_agent_run_id
		WHERE dr.id = $1`, id).Scan(
		&dr.ID, &dr.UserID, &dr.UserName, &reportUserTeamID, &dr.ReportDate, &dr.Content, &dr.Edited,
		&feishuURL, &sessionIDsStr, &status, &submittedContent, &savedAt, &submittedAt, &submittedTo,
		&generationMode, &managedAgentRunID, &agentID, &agentVersionID, &modelID, &generatedAt,
		&dr.CreatedAt, &dr.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if u.Role == "employee" && dr.UserID != u.ID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}
	if (u.Role == "team_leader" || u.Role == "pm") && dr.UserID != u.ID && (u.TeamID == nil || !reportUserTeamID.Valid || reportUserTeamID.String != *u.TeamID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}
	dr.FeishuDocURL = nullStringPtr(feishuURL)
	dr.Status = nullStringPtr(status)
	dr.SubmittedContent = nullStringPtr(submittedContent)
	dr.SavedAt = nullTimePtr(savedAt)
	dr.SubmittedAt = nullTimePtr(submittedAt)
	dr.SubmittedTo = nullStringPtr(submittedTo)
	dr.SessionIDs = parseUUIDArray(sessionIDsStr)
	fillDailyReportProductFields(&dr, generationMode, managedAgentRunID, agentID, agentVersionID, modelID, generatedAt)
	writeJSON(w, http.StatusOK, dr)
}

func (h *ReportHandler) GetOrCreateToday(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	reportDate := reportDateFromRequest(r)

	var dr model.DailyReport
	var feishuURL, submittedContent, status, submittedTo sql.NullString
	var generationMode, managedAgentRunID, agentID, modelID sql.NullString
	var agentVersionID sql.NullInt64
	var savedAt, submittedAt, generatedAt sql.NullTime
	var sessionIDsStr string

	err := h.db.QueryRow(`
		SELECT dr.id, dr.user_id, COALESCE(NULLIF(u.nickname,''), u.username), dr.report_date, dr.content, dr.edited,
			dr.feishu_doc_url, COALESCE(dr.session_ids, '{}'), dr.status, dr.submitted_content, dr.saved_at, dr.submitted_at, dr.submitted_to,
			COALESCE(dr.generation_mode, ''), dr.managed_agent_run_id::text, dr.agent_id, dr.agent_version_id, dr.model_id, ar.finished_at,
			dr.created_at, dr.updated_at
		FROM daily_reports dr
		JOIN users u ON u.id = dr.user_id
		LEFT JOIN ai_runs ar ON ar.id = dr.managed_agent_run_id
		WHERE dr.user_id = $1 AND dr.report_date = $2`, u.ID, reportDate).Scan(
		&dr.ID, &dr.UserID, &dr.UserName, &dr.ReportDate, &dr.Content, &dr.Edited,
		&feishuURL, &sessionIDsStr, &status, &submittedContent, &savedAt, &submittedAt, &submittedTo,
		&generationMode, &managedAgentRunID, &agentID, &agentVersionID, &modelID, &generatedAt,
		&dr.CreatedAt, &dr.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		content := h.generateReportContent(u.ID, reportDate)
		var reportID string
		err := h.db.QueryRow(`
			INSERT INTO daily_reports (user_id, report_date, content)
			VALUES ($1, $2, $3) RETURNING id`, u.ID, reportDate, content).Scan(&reportID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		dr = model.DailyReport{
			ID:         reportID,
			UserID:     u.ID,
			UserName:   u.Name,
			ReportDate: reportDate,
			Content:    content,
		}
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	dr.FeishuDocURL = nullStringPtr(feishuURL)
	dr.Status = nullStringPtr(status)
	dr.SubmittedContent = nullStringPtr(submittedContent)
	dr.SavedAt = nullTimePtr(savedAt)
	dr.SubmittedAt = nullTimePtr(submittedAt)
	dr.SubmittedTo = nullStringPtr(submittedTo)
	dr.SessionIDs = parseUUIDArray(sessionIDsStr)
	fillDailyReportProductFields(&dr, generationMode, managedAgentRunID, agentID, agentVersionID, modelID, generatedAt)
	writeJSON(w, http.StatusOK, dr)
}

func fillDailyReportProductFields(dr *model.DailyReport, generationMode, managedAgentRunID, agentID sql.NullString, agentVersionID sql.NullInt64, modelID sql.NullString, generatedAt sql.NullTime) {
	dr.GenerationMode = generationMode.String
	dr.ManagedAgentRunID = nullStringPtr(managedAgentRunID)
	dr.AgentRunID = dr.ManagedAgentRunID
	dr.AgentID = nullStringPtr(agentID)
	dr.ModelID = nullStringPtr(modelID)
	dr.GeneratedAt = nullTimePtr(generatedAt)
	if agentVersionID.Valid {
		v := int(agentVersionID.Int64)
		dr.AgentVersionID = &v
	}
	dr.UpdatedByUser = dr.Edited
	if dr.GenerationMode == "managed_agent" {
		dr.Origin = "ai"
		if dr.Edited {
			dr.ProductStatus = "modified"
		} else {
			dr.ProductStatus = "ai_generated"
		}
		return
	}
	dr.Origin = "manual"
	dr.ProductStatus = "manual"
}

type reportProductFields struct {
	GenerationMode    string
	ManagedAgentRunID *string
	AgentRunID        *string
	AgentID           *string
	AgentVersionID    *int
	ModelID           *string
	GeneratedAt       *time.Time
	ProductStatus     string
	Origin            string
	UpdatedByUser     bool
}

func buildReportProductFields(edited bool, generationMode, managedAgentRunID, agentID sql.NullString, agentVersionID sql.NullInt64, modelID sql.NullString, generatedAt sql.NullTime) reportProductFields {
	fields := reportProductFields{
		GenerationMode:    generationMode.String,
		ManagedAgentRunID: nullStringPtr(managedAgentRunID),
		AgentID:           nullStringPtr(agentID),
		ModelID:           nullStringPtr(modelID),
		GeneratedAt:       nullTimePtr(generatedAt),
		UpdatedByUser:     edited,
	}
	fields.AgentRunID = fields.ManagedAgentRunID
	if agentVersionID.Valid {
		v := int(agentVersionID.Int64)
		fields.AgentVersionID = &v
	}
	if fields.GenerationMode == "managed_agent" {
		fields.Origin = "ai"
		if edited {
			fields.ProductStatus = "modified"
		} else {
			fields.ProductStatus = "ai_generated"
		}
	} else {
		fields.Origin = "manual"
		fields.ProductStatus = "manual"
	}
	return fields
}

func fillTeamReportProductFields(tr *model.TeamReport, generationMode, managedAgentRunID, agentID sql.NullString, agentVersionID sql.NullInt64, modelID sql.NullString, generatedAt sql.NullTime) {
	fields := buildReportProductFields(tr.Edited, generationMode, managedAgentRunID, agentID, agentVersionID, modelID, generatedAt)
	tr.GenerationMode = fields.GenerationMode
	tr.ManagedAgentRunID = fields.ManagedAgentRunID
	tr.AgentRunID = fields.AgentRunID
	tr.AgentID = fields.AgentID
	tr.AgentVersionID = fields.AgentVersionID
	tr.ModelID = fields.ModelID
	tr.GeneratedAt = fields.GeneratedAt
	tr.ProductStatus = fields.ProductStatus
	tr.Origin = fields.Origin
	tr.UpdatedByUser = fields.UpdatedByUser
}

func fillDepartmentReportProductFields(dr *model.DepartmentReport, generationMode, managedAgentRunID, agentID sql.NullString, agentVersionID sql.NullInt64, modelID sql.NullString, generatedAt sql.NullTime) {
	fields := buildReportProductFields(dr.Edited, generationMode, managedAgentRunID, agentID, agentVersionID, modelID, generatedAt)
	dr.GenerationMode = fields.GenerationMode
	dr.ManagedAgentRunID = fields.ManagedAgentRunID
	dr.AgentRunID = fields.AgentRunID
	dr.AgentID = fields.AgentID
	dr.AgentVersionID = fields.AgentVersionID
	dr.ModelID = fields.ModelID
	dr.GeneratedAt = fields.GeneratedAt
	dr.ProductStatus = fields.ProductStatus
	dr.Origin = fields.Origin
	dr.UpdatedByUser = fields.UpdatedByUser
}

func fillPersonalWeeklyReportProductFields(report *model.PersonalWeeklyReport, generationMode, managedAgentRunID, agentID sql.NullString, agentVersionID sql.NullInt64, modelID sql.NullString, generatedAt sql.NullTime) {
	fields := buildReportProductFields(report.Edited, generationMode, managedAgentRunID, agentID, agentVersionID, modelID, generatedAt)
	report.GenerationMode = fields.GenerationMode
	report.ManagedAgentRunID = fields.ManagedAgentRunID
	report.AgentRunID = fields.AgentRunID
	report.AgentID = fields.AgentID
	report.AgentVersionID = fields.AgentVersionID
	report.ModelID = fields.ModelID
	report.GeneratedAt = fields.GeneratedAt
	report.ProductStatus = fields.ProductStatus
	report.Origin = fields.Origin
	report.UpdatedByUser = fields.UpdatedByUser
}

func fillTeamWeeklyReportProductFields(report *model.TeamWeeklyReport, generationMode, managedAgentRunID, agentID sql.NullString, agentVersionID sql.NullInt64, modelID sql.NullString, generatedAt sql.NullTime) {
	fields := buildReportProductFields(report.Edited, generationMode, managedAgentRunID, agentID, agentVersionID, modelID, generatedAt)
	report.GenerationMode = fields.GenerationMode
	report.ManagedAgentRunID = fields.ManagedAgentRunID
	report.AgentRunID = fields.AgentRunID
	report.AgentID = fields.AgentID
	report.AgentVersionID = fields.AgentVersionID
	report.ModelID = fields.ModelID
	report.GeneratedAt = fields.GeneratedAt
	report.ProductStatus = fields.ProductStatus
	report.Origin = fields.Origin
	report.UpdatedByUser = fields.UpdatedByUser
}

func fillDepartmentWeeklyReportProductFields(report *model.DepartmentWeeklyReport, generationMode, managedAgentRunID, agentID sql.NullString, agentVersionID sql.NullInt64, modelID sql.NullString, generatedAt sql.NullTime) {
	fields := buildReportProductFields(report.Edited, generationMode, managedAgentRunID, agentID, agentVersionID, modelID, generatedAt)
	report.GenerationMode = fields.GenerationMode
	report.ManagedAgentRunID = fields.ManagedAgentRunID
	report.AgentRunID = fields.AgentRunID
	report.AgentID = fields.AgentID
	report.AgentVersionID = fields.AgentVersionID
	report.ModelID = fields.ModelID
	report.GeneratedAt = fields.GeneratedAt
	report.ProductStatus = fields.ProductStatus
	report.Origin = fields.Origin
	report.UpdatedByUser = fields.UpdatedByUser
}

func (h *ReportHandler) GenerateToday(w http.ResponseWriter, r *http.Request) {
	if h.reportGeneratorURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "report generator is not configured"})
		return
	}

	u := getUser(r)
	reportDate := reportDateFromRequest(r)

	body, _ := json.Marshal(map[string]string{
		"user_id":     u.ID,
		"report_date": reportDate,
	})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.reportGeneratorURL+"/reports/generate", bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "report generator request failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("report generator returned HTTP %d: %s", resp.StatusCode, truncateForError(string(respBody), 200))})
		return
	}

	dr, err := h.getReportByUserDate(u.ID, reportDate)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, dr)
}

func (h *ReportHandler) GenerateTodayDraft(w http.ResponseWriter, r *http.Request) {
	var req model.GenerateReportDraftRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	sessionIDs := uniqueStringsPreserveOrder(req.SessionIDs)
	if len(sessionIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_ids is required"})
		return
	}
	if err := service.ValidateDraftSkillID(req.SkillID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if h.reportGeneratorURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "report generator is not configured"})
		return
	}

	u := getUser(r)
	reportDate := req.ReportDate
	if reportDate == "" {
		reportDate = service.TodayInLocalDate()
	}
	skillID := req.SkillID
	if skillID == "" {
		skillID = service.DefaultDailyReportSkillID
	}

	sessions, err := h.loadDraftSessions(u.ID, sessionIDs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(sessions) != len(sessionIDs) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "one or more sessions are not accessible"})
		return
	}

	tasks, err := h.loadDraftTaskCandidates(u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	generatorReq := model.ReportDraftGeneratorRequest{
		UserID:              u.ID,
		UserName:            u.Name,
		ReportDate:          reportDate,
		Sessions:            orderDraftSessions(sessions, sessionIDs),
		TaskCandidates:      tasks,
		SkillID:             skillID,
		SkillContent:        req.SkillContent,
		IncludeTaskProgress: req.IncludeTaskProgress,
	}

	body, _ := json.Marshal(generatorReq)
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.reportGeneratorURL+"/reports/draft", bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "report generator request failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("report generator returned HTTP %d: %s", resp.StatusCode, truncateForError(string(respBody), 300))})
		return
	}

	var draftResp model.GenerateReportDraftResponse
	if err := json.Unmarshal(respBody, &draftResp); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "report generator returned invalid response"})
		return
	}
	if draftResp.ReportMarkdown == "" {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "report generator returned empty report_markdown"})
		return
	}

	draftResp = service.NormalizeDraftResponse(draftResp, generatorReq.Sessions, tasks, req.IncludeTaskProgress)
	writeJSON(w, http.StatusOK, draftResp)
}

func (h *ReportHandler) loadDraftSessions(userID string, sessionIDs []string) ([]model.ReportDraftSession, error) {
	rows, err := h.db.Query(`
		SELECT s.id::text, s.session_ref, s.agent_type, s.started_at, s.ended_at, s.duration_secs,
			COALESCE(s.model, ''), COALESCE(s.summary, ''), COALESCE(s.tool_calls_json::text, '{}'),
			s.task_id::text, COALESCE(t.title, ''), s.requirement_id::text, COALESCE(r.title, ''),
			COALESCE(tu.input_tokens, 0), COALESCE(tu.output_tokens, 0), COALESCE(tu.total_tokens, 0)
		FROM sessions s
		LEFT JOIN tasks t ON t.id = s.task_id
		LEFT JOIN requirements r ON r.id = s.requirement_id
		LEFT JOIN (
			SELECT session_id, SUM(input_tokens) input_tokens, SUM(output_tokens) output_tokens, SUM(total_tokens) total_tokens
			FROM token_usage
			GROUP BY session_id
		) tu ON tu.session_id = s.id
		WHERE s.user_id = $1 AND s.id::text = ANY($2)
		ORDER BY s.started_at`, userID, pq.Array(sessionIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := []model.ReportDraftSession{}
	for rows.Next() {
		var s model.ReportDraftSession
		var endedAt sql.NullTime
		var durationSecs sql.NullInt64
		var toolCallsRaw string
		var taskID, requirementID sql.NullString
		if err := rows.Scan(&s.ID, &s.SessionRef, &s.AgentType, &s.StartedAt, &endedAt, &durationSecs,
			&s.Model, &s.Summary, &toolCallsRaw,
			&taskID, &s.TaskTitle, &requirementID, &s.RequirementTitle,
			&s.InputTokens, &s.OutputTokens, &s.TotalTokens); err != nil {
			return nil, err
		}
		if endedAt.Valid {
			s.EndedAt = &endedAt.Time
		}
		if durationSecs.Valid {
			v := int(durationSecs.Int64)
			s.DurationSecs = &v
		}
		if toolCallsRaw != "" {
			_ = json.Unmarshal([]byte(toolCallsRaw), &s.ToolCallsJSON)
		}
		s.TaskID = nullStringPtr(taskID)
		s.RequirementID = nullStringPtr(requirementID)
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func (h *ReportHandler) loadDraftTaskCandidates(userID string) ([]model.ReportDraftTaskCandidate, error) {
	rows, err := h.db.Query(`
		SELECT t.id::text, t.title, r.id::text, r.title, t.status, t.progress, COALESCE(COALESCE(NULLIF(u.nickname,''), u.username), '')
		FROM tasks t
		JOIN requirements r ON r.id = t.requirement_id
		LEFT JOIN users u ON u.id = t.assignee_id
		WHERE t.assignee_id = $1 AND t.status IN ('todo', 'in_progress')
		ORDER BY t.updated_at DESC, t.created_at DESC
		LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := []model.ReportDraftTaskCandidate{}
	for rows.Next() {
		var task model.ReportDraftTaskCandidate
		if err := rows.Scan(&task.TaskID, &task.TaskTitle, &task.RequirementID, &task.RequirementTitle,
			&task.CurrentStatus, &task.CurrentProgress, &task.Owner); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func loadDraftSessions(db *sql.DB, userID string, sessionIDs []string) ([]model.ReportDraftSession, error) {
	return (&ReportHandler{db: db}).loadDraftSessions(userID, sessionIDs)
}

func loadDraftTaskCandidates(db *sql.DB, userID string) ([]model.ReportDraftTaskCandidate, error) {
	return (&ReportHandler{db: db}).loadDraftTaskCandidates(userID)
}

func (h *ReportHandler) getReportByUserDate(userID, reportDate string) (*model.DailyReport, error) {
	var dr model.DailyReport
	var feishuURL, submittedContent, status, submittedTo sql.NullString
	var savedAt, submittedAt sql.NullTime
	var sessionIDsStr string
	err := h.db.QueryRow(`
		SELECT dr.id, dr.user_id, COALESCE(NULLIF(u.nickname,''), u.username), dr.report_date, dr.content, dr.edited,
			dr.feishu_doc_url, COALESCE(dr.session_ids, '{}'), dr.status, dr.submitted_content, dr.saved_at, dr.submitted_at, dr.submitted_to,
			dr.created_at, dr.updated_at
		FROM daily_reports dr
		JOIN users u ON u.id = dr.user_id
		WHERE dr.user_id = $1 AND dr.report_date = $2`, userID, reportDate).Scan(
		&dr.ID, &dr.UserID, &dr.UserName, &dr.ReportDate, &dr.Content, &dr.Edited,
		&feishuURL, &sessionIDsStr, &status, &submittedContent, &savedAt, &submittedAt, &submittedTo,
		&dr.CreatedAt, &dr.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	dr.FeishuDocURL = nullStringPtr(feishuURL)
	dr.Status = nullStringPtr(status)
	dr.SubmittedContent = nullStringPtr(submittedContent)
	dr.SavedAt = nullTimePtr(savedAt)
	dr.SubmittedAt = nullTimePtr(submittedAt)
	dr.SubmittedTo = nullStringPtr(submittedTo)
	dr.SessionIDs = parseUUIDArray(sessionIDsStr)
	return &dr, nil
}

func (h *ReportHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)
	var req model.UpdateReportRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	if req.SessionIDs != nil {
		sessionIDs := uniqueStringsPreserveOrder(*req.SessionIDs)
		if err := h.validateReportSessionIDs(u.ID, sessionIDs); err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
			return
		}
		req.SessionIDs = &sessionIDs
	}

	sets := []string{"status = 'saved'", "saved_at = now()", "updated_at = now()"}
	args := []any{}
	argIdx := 1

	if req.Content != nil {
		sets = append(sets, fmt.Sprintf("content = $%d, edited = true", argIdx))
		args = append(args, *req.Content)
		argIdx++
	}
	if req.FeishuDocURL != nil {
		sets = append(sets, fmt.Sprintf("feishu_doc_url = $%d", argIdx))
		args = append(args, *req.FeishuDocURL)
		argIdx++
	}
	if req.SessionIDs != nil {
		sets = append(sets, fmt.Sprintf("session_ids = $%d", argIdx))
		args = append(args, pq.Array(*req.SessionIDs))
		argIdx++
	}

	args = append(args, id)
	where := fmt.Sprintf("id = $%d", argIdx)
	argIdx++
	args = append(args, u.ID)
	where += fmt.Sprintf(" AND user_id = $%d", argIdx)
	query := fmt.Sprintf("UPDATE daily_reports SET %s WHERE %s", joinWithCommas(sets), where)

	res, err := h.db.Exec(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	h.Get(w, r)
}

func (h *ReportHandler) SubmitReport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)
	submittedTo := ""
	switch u.Role {
	case "employee":
		submittedTo = "team_leader"
	case "team_leader", "pm":
		submittedTo = "director"
	default:
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "current role cannot submit personal daily report"})
		return
	}

	var req model.SubmitReportRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	if req.SessionIDs != nil {
		sessionIDs := uniqueStringsPreserveOrder(*req.SessionIDs)
		if err := h.validateReportSessionIDs(u.ID, sessionIDs); err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
			return
		}
		req.SessionIDs = &sessionIDs
	}

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	sets := []string{
		"status = 'submitted'",
		"saved_at = now()",
		"submitted_at = now()",
		"updated_at = now()",
	}
	args := []any{}
	argIdx := 1

	if req.Content != nil {
		sets = append(sets, fmt.Sprintf("content = $%d, submitted_content = $%d, edited = true", argIdx, argIdx))
		args = append(args, *req.Content)
		argIdx++
	} else {
		sets = append(sets, "submitted_content = content")
	}
	if req.SessionIDs != nil {
		sets = append(sets, fmt.Sprintf("session_ids = $%d", argIdx))
		args = append(args, pq.Array(*req.SessionIDs))
		argIdx++
	}
	sets = append(sets, fmt.Sprintf("submitted_to = $%d", argIdx))
	args = append(args, submittedTo)
	argIdx++

	args = append(args, id)
	where := fmt.Sprintf("id = $%d", argIdx)
	argIdx++
	args = append(args, u.ID)
	where += fmt.Sprintf(" AND user_id = $%d", argIdx)

	query := fmt.Sprintf("UPDATE daily_reports SET %s WHERE %s", joinWithCommas(sets), where)
	res, err := tx.ExecContext(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.Get(w, r)
}

func (h *ReportHandler) ListPersonalWeeklyReports(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	page, pageSize := parsePagination(r, 20, 100)
	where := " WHERE pwr.user_id = $1"
	args := []any{u.ID}
	argIdx := 2

	if from := r.URL.Query().Get("from_week"); from != "" {
		where += fmt.Sprintf(" AND pwr.week_start >= $%d", argIdx)
		args = append(args, from)
		argIdx++
	}
	if to := r.URL.Query().Get("to_week"); to != "" {
		where += fmt.Sprintf(" AND pwr.week_start <= $%d", argIdx)
		args = append(args, to)
		argIdx++
	}

	var total int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM personal_weekly_reports pwr"+where, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	query := `
		SELECT pwr.id::text, pwr.user_id::text, COALESCE(NULLIF(u.nickname,''), u.username), pwr.week_start, pwr.week_end, pwr.status,
			pwr.saved_at, pwr.submitted_at, pwr.submitted_to,
			COALESCE(cardinality(pwr.source_daily_report_ids), 0),
			COALESCE(cardinality(pwr.source_session_ids), 0),
			COALESCE(cardinality(pwr.source_task_ids), 0),
			pwr.created_at, pwr.updated_at
		FROM personal_weekly_reports pwr
		JOIN users u ON u.id = pwr.user_id` + where + fmt.Sprintf(" ORDER BY pwr.week_start DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	items := []model.PersonalWeeklyReportListItem{}
	for rows.Next() {
		var item model.PersonalWeeklyReportListItem
		var savedAt, submittedAt sql.NullTime
		var submittedTo sql.NullString
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.UserName, &item.WeekStart, &item.WeekEnd, &item.Status,
			&savedAt, &submittedAt, &submittedTo,
			&item.SourceDailyCount, &item.SourceSessionCount, &item.SourceTaskCount,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		item.SavedAt = nullTimePtr(savedAt)
		item.SubmittedAt = nullTimePtr(submittedAt)
		item.SubmittedTo = nullStringPtr(submittedTo)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, model.PaginatedPersonalWeeklyReports{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

func (h *ReportHandler) GetPersonalWeeklyReportCurrent(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	weekStart, _, ok := weeklyRangeFromRequest(w, r)
	if !ok {
		return
	}
	report, err := h.getPersonalWeeklyReportByUserWeek(u.ID, weekStart)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *ReportHandler) GetPersonalWeeklyReportSources(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	weekStart, weekEnd, ok := weeklyRangeFromRequest(w, r)
	if !ok {
		return
	}
	sources, err := h.buildPersonalWeeklyReportSources(u.ID, u.Name, weekStart, weekEnd)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

func (h *ReportHandler) GeneratePersonalWeeklyReportPreview(w http.ResponseWriter, r *http.Request) {
	if h.reportGeneratorURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "report generator is not configured"})
		return
	}
	u := getUser(r)
	var req model.GeneratePersonalWeeklyReportRequest
	if r.Body != nil {
		if err := readJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
	}
	weekStart, weekEnd, ok := weeklyRangeFromValue(w, req.WeekStart)
	if !ok {
		return
	}
	sources, err := h.buildPersonalWeeklyReportSources(u.ID, u.Name, weekStart, weekEnd)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	dailyIDs := personalWeeklyDailySourceIDs(sources, req.SourceDailyReportIDs)
	if len(dailyIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source_daily_report_ids is required"})
		return
	}
	body, _ := json.Marshal(map[string]any{
		"user_id":                 u.ID,
		"week_start":              weekStart,
		"source_daily_report_ids": dailyIDs,
	})
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.reportGeneratorURL+"/reports/weekly/generate", bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "report generator request failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("report generator returned HTTP %d: %s", resp.StatusCode, truncateForError(string(respBody), 200))})
		return
	}
	var genResp struct {
		ReportMarkdown string `json:"report_markdown"`
	}
	if err := json.Unmarshal(respBody, &genResp); err != nil || genResp.ReportMarkdown == "" {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "report generator returned invalid response"})
		return
	}
	writeJSON(w, http.StatusOK, model.PersonalWeeklyReportPreview{
		ReportMarkdown:       genResp.ReportMarkdown,
		WeekStart:            weekStart,
		WeekEnd:              weekEnd,
		SourceDailyReportIDs: dailyIDs,
	})
}

func (h *ReportHandler) SavePersonalWeeklyReportCurrent(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	var req model.SavePersonalWeeklyReportRequest
	if err := readJSON(r, &req); err != nil || req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}
	weekStart, weekEnd, ok := weeklyRangeFromValue(w, req.WeekStart)
	if !ok {
		return
	}
	report, err := h.upsertPersonalWeeklyReport(u.ID, weekStart, weekEnd, req, "saved", nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *ReportHandler) SubmitPersonalWeeklyReportCurrent(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	submittedTo := ""
	switch u.Role {
	case "employee":
		submittedTo = "team_leader"
	case "team_leader", "pm":
		submittedTo = "director"
	default:
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "current role cannot submit personal weekly report"})
		return
	}
	var req model.SavePersonalWeeklyReportRequest
	if err := readJSON(r, &req); err != nil || req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}
	weekStart, weekEnd, ok := weeklyRangeFromValue(w, req.WeekStart)
	if !ok {
		return
	}
	report, err := h.upsertPersonalWeeklyReport(u.ID, weekStart, weekEnd, req, "submitted", &submittedTo)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *ReportHandler) validateReportSessionIDs(userID string, sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	var count int
	if err := h.db.QueryRow(`
		SELECT COUNT(*)
		FROM sessions
		WHERE user_id = $1 AND id::text = ANY($2)`, userID, pq.Array(sessionIDs)).Scan(&count); err != nil {
		return err
	}
	if count != len(sessionIDs) {
		return fmt.Errorf("one or more sessions are not accessible")
	}
	return nil
}

func (h *ReportHandler) generateReportContent(userID, date string) string {
	rows, err := h.db.Query(`
		SELECT s.session_ref, s.started_at, s.ended_at, s.model, s.summary,
			s.task_id, COALESCE(t.title,'')
		FROM sessions s
		LEFT JOIN tasks t ON t.id = s.task_id
		WHERE s.user_id = $1 AND DATE(s.started_at) = $2
		ORDER BY s.started_at`, userID, date)
	if err != nil {
		return fmt.Sprintf("# 日报 %s\n\n暂无 session 数据。", date)
	}
	defer rows.Close()

	content := fmt.Sprintf("# 日报 %s\n\n## 今日 Session\n\n", date)
	count := 0
	for rows.Next() {
		var ref, model string
		var startedAt, endedAt sql.NullString
		var summary, taskID, taskTitle sql.NullString
		rows.Scan(&ref, &startedAt, &endedAt, &model, &summary, &taskID, &taskTitle)
		count++
		taskInfo := ""
		if taskTitle.Valid && taskTitle.String != "" {
			taskInfo = fmt.Sprintf(" [%s]", taskTitle.String)
		}
		summaryText := "无摘要"
		if summary.Valid && summary.String != "" {
			summaryText = summary.String
		}
		content += fmt.Sprintf("%d. `%s` (%s)%s - %s\n", count, ref[:12], model, taskInfo, summaryText)
	}

	if count == 0 {
		content += "暂无 session 数据。\n"
	}

	var totalTokens int64
	h.db.QueryRow(`
		SELECT COALESCE(SUM(total_tokens), 0) FROM token_usage
		WHERE user_id = $1 AND DATE(recorded_at) = $2`, userID, date).Scan(&totalTokens)

	content += fmt.Sprintf("\n## Token 消耗\n\n今日合计: %d tokens\n", totalTokens)
	return content
}

func (h *ReportHandler) ListTeamMemberReports(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u.Role != "team_leader" && u.Role != "pm" && u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}

	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	teamID := u.TeamID
	if u.Role == "director" || u.Role == "admin" {
		if tid := r.URL.Query().Get("team_id"); tid != "" {
			teamID = &tid
		}
	}
	if teamID == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no team specified"})
		return
	}

	rows, err := h.db.Query(`
		SELECT u.id, COALESCE(NULLIF(u.nickname,''), u.username),
			dr.id, dr.submitted_content, dr.submitted_at,
			CASE WHEN dr.id IS NOT NULL THEN true ELSE false END
		FROM users u
		LEFT JOIN daily_reports dr ON dr.user_id = u.id
			AND dr.report_date = $1
			AND dr.submitted_at IS NOT NULL
			AND dr.submitted_content IS NOT NULL
		WHERE u.team_id = $2 AND u.app_role = 'employee'
		ORDER BY COALESCE(NULLIF(u.nickname,''), u.username)`, date, *teamID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	reports := []model.TeamMemberReport{}
	for rows.Next() {
		var tmr model.TeamMemberReport
		var reportID, content sql.NullString
		var submittedAt sql.NullTime
		rows.Scan(&tmr.UserID, &tmr.UserName, &reportID, &content, &submittedAt, &tmr.HasReport)
		tmr.ReportID = nullStringPtr(reportID)
		if content.Valid {
			tmr.Content = content.String
		}
		tmr.SubmittedAt = nullTimePtr(submittedAt)
		reports = append(reports, tmr)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, reports)
}

func (h *ReportHandler) GetTeamReportSources(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u.Role != "team_leader" && u.Role != "pm" && u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}

	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	teamID := u.TeamID
	if u.Role == "director" || u.Role == "admin" {
		if tid := r.URL.Query().Get("team_id"); tid != "" {
			teamID = &tid
		}
	}
	if teamID == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no team specified"})
		return
	}

	var teamName string
	if err := h.db.QueryRow("SELECT name FROM teams WHERE id = $1", *teamID).Scan(&teamName); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "team not found"})
		return
	}

	rows, err := h.db.Query(`
		SELECT u.id, COALESCE(NULLIF(u.nickname,''), u.username), dr.id, dr.submitted_content, dr.submitted_at,
			CASE WHEN dr.id IS NOT NULL THEN true ELSE false END
		FROM users u
		LEFT JOIN daily_reports dr ON dr.user_id = u.id
			AND dr.report_date = $1
			AND dr.submitted_at IS NOT NULL
			AND dr.submitted_content IS NOT NULL
		WHERE u.team_id = $2 AND u.app_role = 'employee'
		ORDER BY COALESCE(NULLIF(u.nickname,''), u.username)`, date, *teamID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	sources := model.TeamReportSources{
		TeamID:           *teamID,
		TeamName:         teamName,
		ReportDate:       date,
		Members:          []model.TeamMemberReport{},
		SubmittedReports: []model.TeamMemberReport{},
		MissingMembers:   []model.TeamMemberReport{},
	}
	for rows.Next() {
		var item model.TeamMemberReport
		var reportID, content sql.NullString
		var submittedAt sql.NullTime
		if err := rows.Scan(&item.UserID, &item.UserName, &reportID, &content, &submittedAt, &item.HasReport); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		item.ReportID = nullStringPtr(reportID)
		item.SubmittedAt = nullTimePtr(submittedAt)
		if content.Valid {
			item.Content = content.String
		}
		sources.TotalMemberCount++
		if item.HasReport {
			sources.Submitted++
			sources.SubmittedCount++
			sources.SubmittedReports = append(sources.SubmittedReports, item)
		} else {
			sources.Missing++
			sources.MissingCount++
			sources.MissingMembers = append(sources.MissingMembers, item)
		}
		sources.Members = append(sources.Members, item)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

func (h *ReportHandler) GetTeamReportToday(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u.TeamID == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no team"})
		return
	}
	reportDate := reportDateFromRequest(r)
	tr, err := h.getTeamReportByTeamDate(*u.TeamID, reportDate)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, tr)
}

func (h *ReportHandler) GenerateTeamReport(w http.ResponseWriter, r *http.Request) {
	if h.reportGeneratorURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "report generator is not configured"})
		return
	}

	u := getUser(r)
	if (u.Role != "team_leader" && u.Role != "pm") || u.TeamID == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only team leaders can generate team reports"})
		return
	}
	reportDate := reportDateFromRequest(r)

	body, _ := json.Marshal(map[string]string{
		"team_id":     *u.TeamID,
		"leader_id":   u.ID,
		"report_date": reportDate,
	})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.reportGeneratorURL+"/reports/team/generate", bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "report generator request failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("report generator returned HTTP %d: %s", resp.StatusCode, truncateForError(string(respBody), 200))})
		return
	}

	var genResp struct {
		ReportID string `json:"report_id"`
	}
	if err := json.Unmarshal(respBody, &genResp); err != nil || genResp.ReportID == "" {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "report generator returned invalid response"})
		return
	}

	tr, err := h.getTeamReportByID(genResp.ReportID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, tr)
}

func (h *ReportHandler) SaveTeamReportToday(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if (u.Role != "team_leader" && u.Role != "pm") || u.TeamID == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only team leaders can save team reports"})
		return
	}

	var req struct {
		ReportDate string  `json:"report_date"`
		Content    *string `json:"content"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Content == nil || strings.TrimSpace(*req.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}

	reportDate := req.ReportDate
	if reportDate == "" {
		reportDate = reportDateFromRequest(r)
	}

	sourceDailyIDs, err := h.loadSubmittedDailyReportIDsByTeam(*u.TeamID, reportDate)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if _, err := h.db.ExecContext(r.Context(), `
		INSERT INTO team_reports (
			team_id, leader_id, report_date, content, status, member_report_ids, source_daily_report_ids, saved_at, updated_at
		) VALUES ($1, $2, $3, $4, 'saved', $5::uuid[], $5::uuid[], now(), now())
		ON CONFLICT (team_id, report_date)
		DO UPDATE SET
			leader_id = EXCLUDED.leader_id,
			content = EXCLUDED.content,
			status = 'saved',
			member_report_ids = EXCLUDED.member_report_ids,
			source_daily_report_ids = EXCLUDED.source_daily_report_ids,
			saved_at = now(),
			updated_at = now()`,
		*u.TeamID, u.ID, reportDate, *req.Content, pq.Array(sourceDailyIDs),
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	report, err := h.getTeamReportByTeamDate(*u.TeamID, reportDate)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *ReportHandler) SubmitTeamReport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)
	if (u.Role != "team_leader" && u.Role != "pm") || u.TeamID == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only team leaders can submit team reports"})
		return
	}

	var req model.SubmitTeamReportRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := readJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
	}

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	sets := []string{
		"status = 'submitted'",
		"saved_at = now()",
		"submitted_at = now()",
		"submitted_to = 'director'",
		"updated_at = now()",
	}
	args := []any{}
	argIdx := 1
	if req.Content != nil {
		sets = append(sets, fmt.Sprintf("content = $%d, submitted_content = $%d", argIdx, argIdx))
		args = append(args, *req.Content)
		argIdx++
	} else {
		sets = append(sets, "submitted_content = content")
	}

	args = append(args, id)
	where := fmt.Sprintf("id = $%d", argIdx)
	argIdx++
	args = append(args, *u.TeamID)
	where += fmt.Sprintf(" AND team_id = $%d", argIdx)
	query := fmt.Sprintf("UPDATE team_reports SET %s WHERE %s", joinWithCommas(sets), where)

	res, err := tx.ExecContext(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	tr, err := h.getTeamReportByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, tr)
}

func (h *ReportHandler) ListTeamReports(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u.Role != "team_leader" && u.Role != "pm" && u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}
	page, pageSize := parsePagination(r, 20, 100)

	where := " WHERE 1=1"
	args := []any{}
	argIdx := 1

	if u.Role == "team_leader" || u.Role == "pm" {
		if u.TeamID == nil {
			writeJSON(w, http.StatusOK, model.PaginatedTeamReports{Items: []model.TeamReportListItem{}, Total: 0, Page: page, PageSize: pageSize})
			return
		}
		where += fmt.Sprintf(" AND tr.team_id = $%d", argIdx)
		args = append(args, *u.TeamID)
		argIdx++
	} else if tid := r.URL.Query().Get("team_id"); tid != "" {
		where += fmt.Sprintf(" AND tr.team_id = $%d", argIdx)
		args = append(args, tid)
		argIdx++
	}

	if from := r.URL.Query().Get("from"); from != "" {
		where += fmt.Sprintf(" AND tr.report_date >= $%d", argIdx)
		args = append(args, from)
		argIdx++
	}
	if to := r.URL.Query().Get("to"); to != "" {
		where += fmt.Sprintf(" AND tr.report_date <= $%d", argIdx)
		args = append(args, to)
		argIdx++
	}

	var total int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM team_reports tr"+where, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	query := `
		SELECT tr.id::text, tr.team_id::text, t.name, tr.leader_id::text, COALESCE(NULLIF(u.nickname,''), u.username),
			tr.report_date,
			(SELECT COUNT(*) FROM users member WHERE member.team_id = tr.team_id AND member.app_role = 'employee') AS member_count,
			COALESCE(cardinality(tr.source_daily_report_ids), 0) AS submitted_count,
			GREATEST((SELECT COUNT(*) FROM users member WHERE member.team_id = tr.team_id AND member.app_role = 'employee') - COALESCE(cardinality(tr.source_daily_report_ids), 0), 0) AS missing_count,
			tr.status, tr.saved_at, tr.submitted_at, tr.submitted_to, tr.created_at, tr.updated_at
		FROM team_reports tr
		JOIN teams t ON t.id = tr.team_id
		JOIN users u ON u.id = tr.leader_id` + where + fmt.Sprintf(" ORDER BY tr.report_date DESC, t.name LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	reports := []model.TeamReportListItem{}
	for rows.Next() {
		var tr model.TeamReportListItem
		var status, submittedTo sql.NullString
		var savedAt, submittedAt sql.NullTime
		if err := rows.Scan(&tr.ID, &tr.TeamID, &tr.TeamName, &tr.LeaderID, &tr.LeaderName,
			&tr.ReportDate, &tr.MemberCount, &tr.SubmittedCount, &tr.MissingCount,
			&status, &savedAt, &submittedAt, &submittedTo, &tr.CreatedAt, &tr.UpdatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		tr.Status = nullStringPtr(status)
		tr.SavedAt = nullTimePtr(savedAt)
		tr.SubmittedAt = nullTimePtr(submittedAt)
		tr.SubmittedTo = nullStringPtr(submittedTo)
		reports = append(reports, tr)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.PaginatedTeamReports{
		Items:    reports,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

func (h *ReportHandler) GetTeamReport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)
	if u.Role != "team_leader" && u.Role != "pm" && u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}

	report, err := h.getTeamReportByID(id)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if (u.Role == "team_leader" || u.Role == "pm") && (u.TeamID == nil || report.TeamID != *u.TeamID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *ReportHandler) UpdateTeamReport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)
	if (u.Role != "team_leader" && u.Role != "pm") || u.TeamID == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only team leaders can update team reports"})
		return
	}

	var req model.UpdateTeamReportRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	sets := []string{"status = 'saved'", "saved_at = now()", "updated_at = now()"}
	args := []any{}
	argIdx := 1

	if req.Content != nil {
		sets = append(sets, fmt.Sprintf("content = $%d", argIdx))
		args = append(args, *req.Content)
		argIdx++
	}
	if req.FeishuDocURL != nil {
		sets = append(sets, fmt.Sprintf("feishu_doc_url = $%d", argIdx))
		args = append(args, *req.FeishuDocURL)
		argIdx++
	}

	args = append(args, id)
	where := fmt.Sprintf("id = $%d", argIdx)
	argIdx++
	args = append(args, *u.TeamID)
	where += fmt.Sprintf(" AND team_id = $%d", argIdx)
	query := fmt.Sprintf("UPDATE team_reports SET %s WHERE %s", joinWithCommas(sets), where)

	res, err := h.db.Exec(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	tr, err := h.getTeamReportByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, tr)
}

func (h *ReportHandler) getTeamReportByTeamDate(teamID, reportDate string) (*model.TeamReport, error) {
	var tr model.TeamReport
	var feishuURL, submittedContent, status, submittedTo sql.NullString
	var generationMode, managedAgentRunID, agentID, modelID sql.NullString
	var agentVersionID sql.NullInt64
	var savedAt, submittedAt, generatedAt sql.NullTime
	var memberIDsStr, sourceDailyIDsStr, sessionIDsStr string
	err := h.db.QueryRow(`
		SELECT tr.id, tr.team_id, t.name, tr.leader_id, COALESCE(NULLIF(u.nickname,''), u.username),
			tr.report_date, tr.content, tr.submitted_content, tr.status, tr.feishu_doc_url,
			tr.member_report_ids, tr.source_daily_report_ids, tr.session_ids,
			tr.saved_at, tr.submitted_at, tr.submitted_to,
			tr.edited, COALESCE(tr.generation_mode, ''), tr.managed_agent_run_id::text, tr.agent_id, tr.agent_version_id, tr.model_id, ar.finished_at,
			tr.created_at, tr.updated_at
		FROM team_reports tr
		JOIN teams t ON t.id = tr.team_id
		JOIN users u ON u.id = tr.leader_id
		LEFT JOIN ai_runs ar ON ar.id = tr.managed_agent_run_id
		WHERE tr.team_id = $1 AND tr.report_date = $2`, teamID, reportDate).Scan(
		&tr.ID, &tr.TeamID, &tr.TeamName, &tr.LeaderID, &tr.LeaderName,
		&tr.ReportDate, &tr.Content, &submittedContent, &status, &feishuURL,
		&memberIDsStr, &sourceDailyIDsStr, &sessionIDsStr,
		&savedAt, &submittedAt, &submittedTo,
		&tr.Edited, &generationMode, &managedAgentRunID, &agentID, &agentVersionID, &modelID, &generatedAt,
		&tr.CreatedAt, &tr.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	tr.FeishuDocURL = nullStringPtr(feishuURL)
	tr.SubmittedContent = nullStringPtr(submittedContent)
	tr.Status = nullStringPtr(status)
	tr.MemberReportIDs = parseUUIDArray(memberIDsStr)
	tr.SourceDailyReportIDs = parseUUIDArray(sourceDailyIDsStr)
	tr.SessionIDs = parseUUIDArray(sessionIDsStr)
	tr.SavedAt = nullTimePtr(savedAt)
	tr.SubmittedAt = nullTimePtr(submittedAt)
	tr.SubmittedTo = nullStringPtr(submittedTo)
	fillTeamReportProductFields(&tr, generationMode, managedAgentRunID, agentID, agentVersionID, modelID, generatedAt)
	return &tr, nil
}

func (h *ReportHandler) loadSubmittedDailyReportIDsByTeam(teamID, reportDate string) ([]string, error) {
	rows, err := h.db.Query(`
		SELECT dr.id::text
		FROM users u
		JOIN daily_reports dr ON dr.user_id = u.id
		WHERE u.team_id = $1
			AND u.app_role = 'employee'
			AND dr.report_date = $2
			AND dr.submitted_at IS NOT NULL
			AND dr.submitted_content IS NOT NULL
		ORDER BY COALESCE(NULLIF(u.nickname,''), u.username), dr.created_at`, teamID, reportDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (h *ReportHandler) getTeamReportByID(id string) (*model.TeamReport, error) {
	var tr model.TeamReport
	var feishuURL, submittedContent, status, submittedTo sql.NullString
	var generationMode, managedAgentRunID, agentID, modelID sql.NullString
	var agentVersionID sql.NullInt64
	var savedAt, submittedAt, generatedAt sql.NullTime
	var memberIDsStr, sourceDailyIDsStr, sessionIDsStr string
	err := h.db.QueryRow(`
		SELECT tr.id, tr.team_id, t.name, tr.leader_id, COALESCE(NULLIF(u.nickname,''), u.username),
			tr.report_date, tr.content, tr.submitted_content, tr.status, tr.feishu_doc_url,
			tr.member_report_ids, tr.source_daily_report_ids, tr.session_ids,
			tr.saved_at, tr.submitted_at, tr.submitted_to,
			tr.edited, COALESCE(tr.generation_mode, ''), tr.managed_agent_run_id::text, tr.agent_id, tr.agent_version_id, tr.model_id, ar.finished_at,
			tr.created_at, tr.updated_at
		FROM team_reports tr
		JOIN teams t ON t.id = tr.team_id
		JOIN users u ON u.id = tr.leader_id
		LEFT JOIN ai_runs ar ON ar.id = tr.managed_agent_run_id
		WHERE tr.id = $1`, id).Scan(
		&tr.ID, &tr.TeamID, &tr.TeamName, &tr.LeaderID, &tr.LeaderName,
		&tr.ReportDate, &tr.Content, &submittedContent, &status, &feishuURL,
		&memberIDsStr, &sourceDailyIDsStr, &sessionIDsStr,
		&savedAt, &submittedAt, &submittedTo,
		&tr.Edited, &generationMode, &managedAgentRunID, &agentID, &agentVersionID, &modelID, &generatedAt,
		&tr.CreatedAt, &tr.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	tr.FeishuDocURL = nullStringPtr(feishuURL)
	tr.SubmittedContent = nullStringPtr(submittedContent)
	tr.Status = nullStringPtr(status)
	tr.MemberReportIDs = parseUUIDArray(memberIDsStr)
	tr.SourceDailyReportIDs = parseUUIDArray(sourceDailyIDsStr)
	tr.SessionIDs = parseUUIDArray(sessionIDsStr)
	tr.SavedAt = nullTimePtr(savedAt)
	tr.SubmittedAt = nullTimePtr(submittedAt)
	tr.SubmittedTo = nullStringPtr(submittedTo)
	fillTeamReportProductFields(&tr, generationMode, managedAgentRunID, agentID, agentVersionID, modelID, generatedAt)
	return &tr, nil
}

func (h *ReportHandler) GetDepartmentReportSources(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}

	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	rows, err := h.db.Query(`
		SELECT t.id::text, t.name, tr.leader_id::text, COALESCE(COALESCE(NULLIF(u.nickname,''), u.username), ''),
			tr.id::text, COALESCE(tr.submitted_content, ''), tr.submitted_at,
			CASE WHEN tr.id IS NOT NULL THEN true ELSE false END
		FROM teams t
		LEFT JOIN team_reports tr ON tr.team_id = t.id
			AND tr.report_date = $1
			AND tr.submitted_at IS NOT NULL
			AND tr.submitted_content IS NOT NULL
		LEFT JOIN users u ON u.id = tr.leader_id
		ORDER BY t.name`, date)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	sources := model.DepartmentReportSources{
		ReportDate:           date,
		SubmittedTeamReports: []model.DepartmentTeamReportSource{},
		MissingTeams:         []model.DepartmentMissingTeam{},
	}
	for rows.Next() {
		var item model.DepartmentTeamReportSource
		var leaderID, reportID, content sql.NullString
		var submittedAt sql.NullTime
		if err := rows.Scan(&item.TeamID, &item.TeamName, &leaderID, &item.LeaderName, &reportID, &content, &submittedAt, &item.HasReport); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		item.LeaderID = nullStringPtr(leaderID)
		item.ReportID = nullStringPtr(reportID)
		item.TeamReportID = item.ReportID
		item.TeamLeaderName = item.LeaderName
		item.SubmittedAt = nullTimePtr(submittedAt)
		if content.Valid {
			item.Content = content.String
		}
		sources.TotalTeamCount++
		if item.HasReport {
			sources.SubmittedTeamCount++
			sources.SubmittedTeamReports = append(sources.SubmittedTeamReports, item)
		} else {
			sources.MissingTeams = append(sources.MissingTeams, model.DepartmentMissingTeam{
				TeamID:   item.TeamID,
				TeamName: item.TeamName,
			})
		}
	}
	sources.MissingTeamCount = len(sources.MissingTeams)
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

func (h *ReportHandler) GetDepartmentReportToday(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}
	reportDate := reportDateFromRequest(r)
	dr, err := h.getDepartmentReportByDate(reportDate)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, dr)
}

func (h *ReportHandler) GenerateDepartmentReport(w http.ResponseWriter, r *http.Request) {
	if h.reportGeneratorURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "report generator is not configured"})
		return
	}
	u := getUser(r)
	if u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only directors can generate department reports"})
		return
	}
	reportDate := reportDateFromRequest(r)
	body, _ := json.Marshal(map[string]string{"report_date": reportDate})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.reportGeneratorURL+"/reports/department/generate", bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "report generator request failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("report generator returned HTTP %d: %s", resp.StatusCode, truncateForError(string(respBody), 200))})
		return
	}

	var genResp struct {
		ReportID string `json:"report_id"`
	}
	if err := json.Unmarshal(respBody, &genResp); err != nil || genResp.ReportID == "" {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "report generator returned invalid response"})
		return
	}
	dr, err := h.getDepartmentReportByID(genResp.ReportID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, dr)
}

func (h *ReportHandler) SaveDepartmentReportToday(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only directors can save department reports"})
		return
	}
	var req struct {
		ReportDate string `json:"report_date"`
		Content    string `json:"content"`
		Archive    bool   `json:"archive,omitempty"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	reportDate := req.ReportDate
	if strings.TrimSpace(reportDate) == "" {
		reportDate = time.Now().Format("2006-01-02")
	}

	sources, err := h.buildDepartmentReportSources(reportDate)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	sourceIDs := make([]string, 0, len(sources.SubmittedTeamReports))
	for _, item := range sources.SubmittedTeamReports {
		if item.ReportID != nil && *item.ReportID != "" {
			sourceIDs = append(sourceIDs, *item.ReportID)
		}
	}

	var reportID string
	err = h.db.QueryRow(`
		INSERT INTO department_reports (
			report_date, content, status, source_team_report_ids, saved_at, archived_at
		)
		VALUES ($1, $2, 'saved', $3, now(), CASE WHEN $4 THEN now() ELSE NULL END)
		ON CONFLICT (report_date)
		DO UPDATE SET
			content = EXCLUDED.content,
			status = 'saved',
			source_team_report_ids = EXCLUDED.source_team_report_ids,
			saved_at = now(),
			archived_at = CASE
				WHEN $4 THEN COALESCE(department_reports.archived_at, now())
				ELSE department_reports.archived_at
			END,
			updated_at = now()
		RETURNING id::text`,
		reportDate, req.Content, pq.Array(sourceIDs), req.Archive,
	).Scan(&reportID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	dr, err := h.getDepartmentReportByID(reportID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, dr)
}

func (h *ReportHandler) GetDepartmentReport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)
	if u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}
	report, err := h.getDepartmentReportByID(id)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *ReportHandler) UpdateDepartmentReport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)
	if u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only directors can update department reports"})
		return
	}
	var req model.UpdateDepartmentReportRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	sets := []string{"status = 'saved'", "saved_at = now()", "archived_at = now()", "updated_at = now()"}
	args := []any{}
	argIdx := 1
	if req.Content != nil {
		sets = append(sets, fmt.Sprintf("content = $%d", argIdx))
		args = append(args, *req.Content)
		argIdx++
	}
	args = append(args, id)
	query := fmt.Sprintf("UPDATE department_reports SET %s WHERE id = $%d", joinWithCommas(sets), argIdx)
	res, err := h.db.Exec(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	dr, err := h.getDepartmentReportByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, dr)
}

func (h *ReportHandler) getDepartmentReportByDate(reportDate string) (*model.DepartmentReport, error) {
	var dr model.DepartmentReport
	var status sql.NullString
	var generationMode, managedAgentRunID, agentID, modelID sql.NullString
	var agentVersionID sql.NullInt64
	var savedAt, archivedAt, generatedAt sql.NullTime
	var sourceIDsStr string
	err := h.db.QueryRow(`
		SELECT dr.id::text, dr.report_date, dr.content, dr.status, dr.source_team_report_ids, dr.saved_at, dr.archived_at,
			dr.edited, COALESCE(dr.generation_mode, ''), dr.managed_agent_run_id::text, dr.agent_id, dr.agent_version_id, dr.model_id, ar.finished_at,
			dr.created_at, dr.updated_at
		FROM department_reports dr
		LEFT JOIN ai_runs ar ON ar.id = dr.managed_agent_run_id
		WHERE dr.report_date = $1`, reportDate).Scan(
		&dr.ID, &dr.ReportDate, &dr.Content, &status, &sourceIDsStr, &savedAt, &archivedAt,
		&dr.Edited, &generationMode, &managedAgentRunID, &agentID, &agentVersionID, &modelID, &generatedAt,
		&dr.CreatedAt, &dr.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	dr.Status = nullStringPtr(status)
	dr.SourceTeamReportIDs = parseUUIDArray(sourceIDsStr)
	dr.SavedAt = nullTimePtr(savedAt)
	dr.ArchivedAt = nullTimePtr(archivedAt)
	fillDepartmentReportProductFields(&dr, generationMode, managedAgentRunID, agentID, agentVersionID, modelID, generatedAt)
	return &dr, nil
}

func (h *ReportHandler) buildDepartmentReportSources(reportDate string) (*model.DepartmentReportSources, error) {
	rows, err := h.db.Query(`
		SELECT t.id::text, t.name, tr.leader_id::text, COALESCE(COALESCE(NULLIF(u.nickname,''), u.username), ''),
			tr.id::text, COALESCE(tr.submitted_content, ''), tr.submitted_at,
			CASE WHEN tr.id IS NOT NULL THEN true ELSE false END
		FROM teams t
		LEFT JOIN team_reports tr ON tr.team_id = t.id
			AND tr.report_date = $1
			AND tr.submitted_at IS NOT NULL
			AND tr.submitted_content IS NOT NULL
		LEFT JOIN users u ON u.id = tr.leader_id
		ORDER BY t.name`, reportDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sources := &model.DepartmentReportSources{
		ReportDate:           reportDate,
		SubmittedTeamReports: []model.DepartmentTeamReportSource{},
		MissingTeams:         []model.DepartmentMissingTeam{},
	}
	for rows.Next() {
		var item model.DepartmentTeamReportSource
		var leaderID, reportID, content sql.NullString
		var submittedAt sql.NullTime
		if err := rows.Scan(&item.TeamID, &item.TeamName, &leaderID, &item.LeaderName, &reportID, &content, &submittedAt, &item.HasReport); err != nil {
			return nil, err
		}
		item.LeaderID = nullStringPtr(leaderID)
		item.ReportID = nullStringPtr(reportID)
		item.TeamReportID = item.ReportID
		item.TeamLeaderName = item.LeaderName
		item.SubmittedAt = nullTimePtr(submittedAt)
		if content.Valid {
			item.Content = content.String
		}
		sources.TotalTeamCount++
		if item.HasReport {
			sources.SubmittedTeamCount++
			sources.SubmittedTeamReports = append(sources.SubmittedTeamReports, item)
		} else {
			sources.MissingTeams = append(sources.MissingTeams, model.DepartmentMissingTeam{
				TeamID:   item.TeamID,
				TeamName: item.TeamName,
			})
		}
	}
	sources.MissingTeamCount = len(sources.MissingTeams)
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sources, nil
}

func (h *ReportHandler) getDepartmentReportByID(id string) (*model.DepartmentReport, error) {
	var dr model.DepartmentReport
	var status sql.NullString
	var generationMode, managedAgentRunID, agentID, modelID sql.NullString
	var agentVersionID sql.NullInt64
	var savedAt, archivedAt, generatedAt sql.NullTime
	var sourceIDsStr string
	err := h.db.QueryRow(`
		SELECT dr.id::text, dr.report_date, dr.content, dr.status, dr.source_team_report_ids, dr.saved_at, dr.archived_at,
			dr.edited, COALESCE(dr.generation_mode, ''), dr.managed_agent_run_id::text, dr.agent_id, dr.agent_version_id, dr.model_id, ar.finished_at,
			dr.created_at, dr.updated_at
		FROM department_reports dr
		LEFT JOIN ai_runs ar ON ar.id = dr.managed_agent_run_id
		WHERE dr.id = $1`, id).Scan(
		&dr.ID, &dr.ReportDate, &dr.Content, &status, &sourceIDsStr, &savedAt, &archivedAt,
		&dr.Edited, &generationMode, &managedAgentRunID, &agentID, &agentVersionID, &modelID, &generatedAt,
		&dr.CreatedAt, &dr.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	dr.Status = nullStringPtr(status)
	dr.SourceTeamReportIDs = parseUUIDArray(sourceIDsStr)
	dr.SavedAt = nullTimePtr(savedAt)
	dr.ArchivedAt = nullTimePtr(archivedAt)
	fillDepartmentReportProductFields(&dr, generationMode, managedAgentRunID, agentID, agentVersionID, modelID, generatedAt)
	return &dr, nil
}

func (h *ReportHandler) ListDepartmentReports(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}
	page, pageSize := parsePagination(r, 20, 100)

	where := " WHERE 1=1"
	args := []any{}
	argIdx := 1
	if from := r.URL.Query().Get("from"); from != "" {
		where += fmt.Sprintf(" AND dr.report_date >= $%d", argIdx)
		args = append(args, from)
		argIdx++
	}
	if to := r.URL.Query().Get("to"); to != "" {
		where += fmt.Sprintf(" AND dr.report_date <= $%d", argIdx)
		args = append(args, to)
		argIdx++
	}

	var total int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM department_reports dr"+where, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	query := `
		SELECT dr.id::text, dr.report_date,
			(SELECT COUNT(*) FROM teams) AS team_count,
			COALESCE(cardinality(dr.source_team_report_ids), 0) AS submitted_team_count,
			GREATEST((SELECT COUNT(*) FROM teams) - COALESCE(cardinality(dr.source_team_report_ids), 0), 0) AS missing_team_count,
			dr.status, dr.saved_at, dr.archived_at, dr.created_at, dr.updated_at
		FROM department_reports dr` + where + fmt.Sprintf(" ORDER BY dr.report_date DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	reports := []model.DepartmentReportListItem{}
	for rows.Next() {
		var dr model.DepartmentReportListItem
		var status sql.NullString
		var savedAt, archivedAt sql.NullTime
		if err := rows.Scan(&dr.ID, &dr.ReportDate, &dr.TeamCount, &dr.SubmittedTeamCount, &dr.MissingTeamCount, &status, &savedAt, &archivedAt, &dr.CreatedAt, &dr.UpdatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		dr.Status = nullStringPtr(status)
		dr.SavedAt = nullTimePtr(savedAt)
		dr.ArchivedAt = nullTimePtr(archivedAt)
		reports = append(reports, dr)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.PaginatedDepartmentReports{
		Items:    reports,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

func (h *ReportHandler) GetTeamWeeklyReportSources(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	teamID, ok := h.resolveWeeklyTeamID(w, r, u, true)
	if !ok {
		return
	}
	weekStart, weekEnd, ok := weeklyRangeFromRequest(w, r)
	if !ok {
		return
	}
	sources, err := h.buildTeamWeeklyReportSources(teamID, weekStart, weekEnd)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

func (h *ReportHandler) GetTeamWeeklyReportCurrent(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	teamID, ok := h.resolveWeeklyTeamID(w, r, u, true)
	if !ok {
		return
	}
	weekStart, _, ok := weeklyRangeFromRequest(w, r)
	if !ok {
		return
	}
	report, err := h.getTeamWeeklyReportByTeamWeek(teamID, weekStart)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *ReportHandler) GenerateTeamWeeklyReport(w http.ResponseWriter, r *http.Request) {
	if h.reportGeneratorURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "report generator is not configured"})
		return
	}
	u := getUser(r)
	if (u.Role != "team_leader" && u.Role != "pm") || u.TeamID == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only team leaders can generate team weekly reports"})
		return
	}
	var req model.GenerateTeamWeeklyReportRequest
	if r.Body != nil {
		if err := readJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
	}
	if req.WeekStart == "" {
		req.WeekStart = r.URL.Query().Get("week_start")
	}
	weekStart, weekEnd, ok := weeklyRangeFromValue(w, req.WeekStart)
	if !ok {
		return
	}
	sources, err := h.buildTeamWeeklyReportSources(*u.TeamID, weekStart, weekEnd)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	sourceIDs := teamWeeklyPersonalSourceIDs(sources, req.SourcePersonalWeeklyReportIDs)
	if len(sourceIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source_personal_weekly_report_ids is required"})
		return
	}

	body, _ := json.Marshal(map[string]any{
		"team_id":                           *u.TeamID,
		"leader_id":                         u.ID,
		"week_start":                        weekStart,
		"source_personal_weekly_report_ids": sourceIDs,
	})
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.reportGeneratorURL+"/reports/team/weekly/generate", bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "report generator request failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("report generator returned HTTP %d: %s", resp.StatusCode, truncateForError(string(respBody), 200))})
		return
	}
	var genResp struct {
		ReportMarkdown string `json:"report_markdown"`
	}
	if err := json.Unmarshal(respBody, &genResp); err != nil || genResp.ReportMarkdown == "" {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "report generator returned invalid response"})
		return
	}
	writeJSON(w, http.StatusOK, model.TeamWeeklyReportPreview{
		ReportMarkdown:                genResp.ReportMarkdown,
		WeekStart:                     weekStart,
		WeekEnd:                       weekEnd,
		SourcePersonalWeeklyReportIDs: sourceIDs,
	})
}

func (h *ReportHandler) SaveTeamWeeklyReportCurrent(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if (u.Role != "team_leader" && u.Role != "pm") || u.TeamID == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only team leaders can save team weekly reports"})
		return
	}
	var req model.UpdateTeamWeeklyReportRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Content == nil || strings.TrimSpace(*req.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}
	weekStart, weekEnd, ok := weeklyRangeFromValue(w, req.WeekStart)
	if !ok {
		return
	}
	report, err := h.upsertTeamWeeklyReport(*u.TeamID, u.ID, weekStart, weekEnd, *req.Content, req.SourcePersonalWeeklyReportIDs, false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *ReportHandler) SubmitTeamWeeklyReportCurrent(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if (u.Role != "team_leader" && u.Role != "pm") || u.TeamID == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only team leaders can submit team weekly reports"})
		return
	}
	var req model.UpdateTeamWeeklyReportRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Content == nil || strings.TrimSpace(*req.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}
	weekStart, weekEnd, ok := weeklyRangeFromValue(w, req.WeekStart)
	if !ok {
		return
	}
	report, err := h.upsertTeamWeeklyReport(*u.TeamID, u.ID, weekStart, weekEnd, *req.Content, req.SourcePersonalWeeklyReportIDs, true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *ReportHandler) UpdateTeamWeeklyReport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)
	if (u.Role != "team_leader" && u.Role != "pm") || u.TeamID == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only team leaders can update team weekly reports"})
		return
	}
	var req model.UpdateTeamWeeklyReportRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	sets := []string{"updated_at = now()"}
	args := []any{}
	argIdx := 1
	if req.Content != nil {
		sets = append(sets, fmt.Sprintf("content = $%d", argIdx))
		args = append(args, *req.Content)
		argIdx++
	}
	args = append(args, id)
	where := fmt.Sprintf("id = $%d", argIdx)
	argIdx++
	args = append(args, *u.TeamID)
	where += fmt.Sprintf(" AND team_id = $%d", argIdx)
	query := fmt.Sprintf("UPDATE team_weekly_reports SET %s WHERE %s", joinWithCommas(sets), where)
	res, err := h.db.Exec(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	report, err := h.getTeamWeeklyReportByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *ReportHandler) SubmitTeamWeeklyReport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)
	if (u.Role != "team_leader" && u.Role != "pm") || u.TeamID == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only team leaders can submit team weekly reports"})
		return
	}
	res, err := h.db.Exec(`
		UPDATE team_weekly_reports
		SET submitted_at = now(), updated_at = now()
		WHERE id = $1 AND team_id = $2`, id, *u.TeamID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	report, err := h.getTeamWeeklyReportByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *ReportHandler) ListTeamWeeklyReports(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u.Role != "team_leader" && u.Role != "pm" && u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}
	query := `
		SELECT twr.id::text, twr.team_id::text, t.name, twr.leader_id::text, COALESCE(NULLIF(u.nickname,''), u.username),
			twr.week_start, twr.content, twr.source_daily_report_ids, twr.source_team_report_ids,
			twr.source_task_ids, twr.source_personal_weekly_report_ids,
			twr.submitted_at, twr.created_at, twr.updated_at,
			twr.edited, COALESCE(twr.generation_mode, ''), twr.managed_agent_run_id::text, twr.agent_id, twr.agent_version_id, twr.model_id, ar.finished_at
		FROM team_weekly_reports twr
		JOIN teams t ON t.id = twr.team_id
		JOIN users u ON u.id = twr.leader_id
		LEFT JOIN ai_runs ar ON ar.id = twr.managed_agent_run_id
		WHERE 1=1`
	args := []any{}
	argIdx := 1
	if u.Role == "team_leader" || u.Role == "pm" {
		if u.TeamID == nil {
			writeJSON(w, http.StatusOK, []model.TeamWeeklyReport{})
			return
		}
		query += fmt.Sprintf(" AND twr.team_id = $%d", argIdx)
		args = append(args, *u.TeamID)
		argIdx++
	} else if tid := r.URL.Query().Get("team_id"); tid != "" {
		query += fmt.Sprintf(" AND twr.team_id = $%d", argIdx)
		args = append(args, tid)
		argIdx++
	}
	if from := r.URL.Query().Get("from_week"); from != "" {
		query += fmt.Sprintf(" AND twr.week_start >= $%d", argIdx)
		args = append(args, from)
		argIdx++
	}
	if to := r.URL.Query().Get("to_week"); to != "" {
		query += fmt.Sprintf(" AND twr.week_start <= $%d", argIdx)
		args = append(args, to)
		argIdx++
	}
	query += " ORDER BY twr.week_start DESC, t.name"
	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	reports := []model.TeamWeeklyReport{}
	for rows.Next() {
		report, err := scanTeamWeeklyReport(rows)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, reports)
}

func (h *ReportHandler) GetDepartmentWeeklyReportSources(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}
	weekStart, weekEnd, ok := weeklyRangeFromRequest(w, r)
	if !ok {
		return
	}
	sources, err := h.buildDepartmentWeeklyReportSources(weekStart, weekEnd)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

func (h *ReportHandler) GetDepartmentWeeklyReportCurrent(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}
	weekStart, _, ok := weeklyRangeFromRequest(w, r)
	if !ok {
		return
	}
	report, err := h.getDepartmentWeeklyReportByWeek(weekStart)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *ReportHandler) GenerateDepartmentWeeklyReport(w http.ResponseWriter, r *http.Request) {
	if h.reportGeneratorURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "report generator is not configured"})
		return
	}
	u := getUser(r)
	if u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only directors can generate department weekly reports"})
		return
	}
	weekStart, _, ok := weeklyRangeFromRequest(w, r)
	if !ok {
		return
	}
	body, _ := json.Marshal(map[string]string{"week_start": weekStart})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.reportGeneratorURL+"/reports/department/weekly/generate", bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "report generator request failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("report generator returned HTTP %d: %s", resp.StatusCode, truncateForError(string(respBody), 200))})
		return
	}
	var genResp struct {
		ReportID string `json:"report_id"`
	}
	if err := json.Unmarshal(respBody, &genResp); err != nil || genResp.ReportID == "" {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "report generator returned invalid response"})
		return
	}
	report, err := h.getDepartmentWeeklyReportByID(genResp.ReportID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *ReportHandler) SaveDepartmentWeeklyReportCurrent(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only directors can save department weekly reports"})
		return
	}
	var req struct {
		WeekStart string `json:"week_start"`
		Content   string `json:"content"`
		Archive   bool   `json:"archive,omitempty"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	weekStart, weekEnd, ok := weeklyRangeFromValue(w, req.WeekStart)
	if !ok {
		return
	}
	sources, err := h.buildDepartmentWeeklyReportSources(weekStart, weekEnd)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	sourceIDs := make([]string, 0, len(sources.SubmittedTeamReports))
	for _, item := range sources.SubmittedTeamReports {
		if item.ReportID != nil && *item.ReportID != "" {
			sourceIDs = append(sourceIDs, *item.ReportID)
		}
	}
	var reportID string
	err = h.db.QueryRow(`
		INSERT INTO department_weekly_reports (
			week_start, week_end, content, source_team_weekly_report_ids, archived_at
		)
		VALUES ($1, $2, $3, $4, CASE WHEN $5 THEN now() ELSE NULL END)
		ON CONFLICT (week_start)
		DO UPDATE SET
			week_end = EXCLUDED.week_end,
			content = EXCLUDED.content,
			source_team_weekly_report_ids = EXCLUDED.source_team_weekly_report_ids,
			archived_at = CASE
				WHEN $5 THEN COALESCE(department_weekly_reports.archived_at, now())
				ELSE department_weekly_reports.archived_at
			END,
			updated_at = now()
		RETURNING id::text`,
		weekStart, weekEnd, req.Content, pq.Array(sourceIDs), req.Archive,
	).Scan(&reportID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	report, err := h.getDepartmentWeeklyReportByID(reportID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *ReportHandler) UpdateDepartmentWeeklyReport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)
	if u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only directors can update department weekly reports"})
		return
	}
	var req model.UpdateDepartmentWeeklyReportRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	sets := []string{"updated_at = now()"}
	args := []any{}
	argIdx := 1
	if req.Content != nil {
		sets = append(sets, fmt.Sprintf("content = $%d", argIdx))
		args = append(args, *req.Content)
		argIdx++
	}
	if req.Archive {
		sets = append(sets, "archived_at = COALESCE(archived_at, now())")
	}
	args = append(args, id)
	query := fmt.Sprintf("UPDATE department_weekly_reports SET %s WHERE id = $%d", joinWithCommas(sets), argIdx)
	res, err := h.db.Exec(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	report, err := h.getDepartmentWeeklyReportByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *ReportHandler) ListDepartmentWeeklyReports(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}
	query := `
		SELECT dwr.id::text, dwr.week_start, dwr.content, dwr.source_team_weekly_report_ids,
			dwr.archived_at, dwr.created_at, dwr.updated_at,
			dwr.edited, COALESCE(dwr.generation_mode, ''), dwr.managed_agent_run_id::text,
			dwr.agent_id, dwr.agent_version_id, dwr.model_id, ar.finished_at
		FROM department_weekly_reports dwr
		LEFT JOIN ai_runs ar ON ar.id = dwr.managed_agent_run_id
		WHERE 1=1`
	args := []any{}
	argIdx := 1
	if from := r.URL.Query().Get("from_week"); from != "" {
		query += fmt.Sprintf(" AND dwr.week_start >= $%d", argIdx)
		args = append(args, from)
		argIdx++
	}
	if to := r.URL.Query().Get("to_week"); to != "" {
		query += fmt.Sprintf(" AND dwr.week_start <= $%d", argIdx)
		args = append(args, to)
		argIdx++
	}
	query += " ORDER BY dwr.week_start DESC"
	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	reports := []model.DepartmentWeeklyReport{}
	for rows.Next() {
		report, err := scanDepartmentWeeklyReport(rows)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, reports)
}

func (h *ReportHandler) resolveWeeklyTeamID(w http.ResponseWriter, r *http.Request, u *model.User, allowDirectorTeamParam bool) (string, bool) {
	if u.Role != "team_leader" && u.Role != "pm" && u.Role != "director" && u.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return "", false
	}
	if u.Role == "director" || u.Role == "admin" {
		if allowDirectorTeamParam {
			if teamID := r.URL.Query().Get("team_id"); teamID != "" {
				return teamID, true
			}
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "team_id is required"})
		return "", false
	}
	if u.TeamID == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no team specified"})
		return "", false
	}
	return *u.TeamID, true
}

func (h *ReportHandler) buildTeamWeeklyReportSources(teamID, weekStart, weekEnd string) (*model.TeamWeeklyReportSources, error) {
	var teamName string
	if err := h.db.QueryRow("SELECT name FROM teams WHERE id = $1", teamID).Scan(&teamName); err != nil {
		return nil, err
	}
	sources := &model.TeamWeeklyReportSources{
		TeamID:                         teamID,
		TeamName:                       teamName,
		WeekStart:                      weekStart,
		WeekEnd:                        weekEnd,
		SubmittedPersonalWeeklyReports: []model.TeamPersonalWeeklySource{},
		MissingPeople:                  []model.TeamWeeklyMissingPerson{},
	}

	rows, err := h.db.Query(`
		WITH eligible_people AS (
			SELECT u.id, COALESCE(NULLIF(u.nickname,''), u.username) AS display_name,
				CASE WHEN u.app_role = 'team_leader' THEN 'leader' ELSE 'member' END AS source_role
			FROM users u
			WHERE u.team_id = $1 AND u.app_role IN ('team_leader', 'employee')
		)
		SELECT ep.id::text, ep.display_name, ep.source_role,
			pwr.id::text, pwr.week_start, pwr.week_end, pwr.submitted_at,
			COALESCE(pwr.submitted_content, pwr.content, '')
		FROM eligible_people ep
		LEFT JOIN personal_weekly_reports pwr
			ON pwr.user_id = ep.id
			AND pwr.week_start = $2
			AND pwr.status = 'submitted'
			AND pwr.submitted_at IS NOT NULL
		ORDER BY CASE WHEN ep.source_role = 'leader' THEN 0 ELSE 1 END, ep.display_name`, teamID, weekStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var userID, userName, sourceRole string
		var reportID, submittedContent sql.NullString
		var reportWeekStart, reportWeekEnd, submittedAt sql.NullTime
		if err := rows.Scan(&userID, &userName, &sourceRole, &reportID, &reportWeekStart, &reportWeekEnd, &submittedAt, &submittedContent); err != nil {
			return nil, err
		}
		if reportID.Valid {
			item := model.TeamPersonalWeeklySource{
				ReportID:         reportID.String,
				UserID:           userID,
				UserName:         userName,
				SourceRole:       sourceRole,
				SubmittedAt:      nullTimePtr(submittedAt),
				SubmittedContent: submittedContent.String,
			}
			if reportWeekStart.Valid {
				item.WeekStart = reportWeekStart.Time.Format("2006-01-02")
			}
			if reportWeekEnd.Valid {
				item.WeekEnd = reportWeekEnd.Time.Format("2006-01-02")
			}
			sources.SubmittedPersonalWeeklyReports = append(sources.SubmittedPersonalWeeklyReports, item)
		} else {
			sources.MissingPeople = append(sources.MissingPeople, model.TeamWeeklyMissingPerson{
				UserID:     userID,
				UserName:   userName,
				SourceRole: sourceRole,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sources.SubmittedPersonalWeeklyCount = len(sources.SubmittedPersonalWeeklyReports)
	sources.MissingPeopleCount = len(sources.MissingPeople)
	return sources, nil
}

func teamWeeklyPersonalSourceIDs(sources *model.TeamWeeklyReportSources, ids []string) []string {
	available := map[string]bool{}
	for _, item := range sources.SubmittedPersonalWeeklyReports {
		available[item.ReportID] = true
	}
	return filterAvailableIDs(uniqueStringsPreserveOrder(ids), available)
}

func (h *ReportHandler) upsertTeamWeeklyReport(teamID, leaderID, weekStart, weekEnd, content string, sourcePersonalWeeklyIDs []string, submitted bool) (*model.TeamWeeklyReport, error) {
	sources, err := h.buildTeamWeeklyReportSources(teamID, weekStart, weekEnd)
	if err != nil {
		return nil, err
	}
	sourceIDs := teamWeeklyPersonalSourceIDs(sources, sourcePersonalWeeklyIDs)
	sourceDailyIDs := []string{}
	sourceTeamReportIDs := []string{}
	sourceTaskIDs := []string{}
	var reportID string
	if submitted {
		err = h.db.QueryRow(`
			INSERT INTO team_weekly_reports (
				team_id, leader_id, week_start, week_end, content,
				source_daily_report_ids, source_team_report_ids, source_task_ids,
				source_personal_weekly_report_ids, submitted_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
			ON CONFLICT (team_id, week_start)
			DO UPDATE SET
				leader_id = EXCLUDED.leader_id,
				week_end = EXCLUDED.week_end,
				content = EXCLUDED.content,
				source_daily_report_ids = EXCLUDED.source_daily_report_ids,
				source_team_report_ids = EXCLUDED.source_team_report_ids,
				source_task_ids = EXCLUDED.source_task_ids,
				source_personal_weekly_report_ids = EXCLUDED.source_personal_weekly_report_ids,
				submitted_at = now(),
				updated_at = now()
			RETURNING id::text`,
			teamID, leaderID, weekStart, weekEnd, content,
			pq.Array(sourceDailyIDs), pq.Array(sourceTeamReportIDs), pq.Array(sourceTaskIDs), pq.Array(sourceIDs),
		).Scan(&reportID)
	} else {
		err = h.db.QueryRow(`
			INSERT INTO team_weekly_reports (
				team_id, leader_id, week_start, week_end, content,
				source_daily_report_ids, source_team_report_ids, source_task_ids,
				source_personal_weekly_report_ids, submitted_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULL)
			ON CONFLICT (team_id, week_start)
			DO UPDATE SET
				leader_id = EXCLUDED.leader_id,
				week_end = EXCLUDED.week_end,
				content = EXCLUDED.content,
				source_daily_report_ids = EXCLUDED.source_daily_report_ids,
				source_team_report_ids = EXCLUDED.source_team_report_ids,
				source_task_ids = EXCLUDED.source_task_ids,
				source_personal_weekly_report_ids = EXCLUDED.source_personal_weekly_report_ids,
				submitted_at = NULL,
				updated_at = now()
			RETURNING id::text`,
			teamID, leaderID, weekStart, weekEnd, content,
			pq.Array(sourceDailyIDs), pq.Array(sourceTeamReportIDs), pq.Array(sourceTaskIDs), pq.Array(sourceIDs),
		).Scan(&reportID)
	}
	if err != nil {
		return nil, err
	}
	return h.getTeamWeeklyReportByID(reportID)
}

func (h *ReportHandler) buildDepartmentWeeklyReportSources(weekStart, weekEnd string) (*model.DepartmentWeeklyReportSources, error) {
	rows, err := h.db.Query(`
		SELECT t.id::text, t.name, twr.leader_id::text, COALESCE(COALESCE(NULLIF(u.nickname,''), u.username), ''),
			twr.id::text, COALESCE(twr.content, ''), twr.submitted_at,
			CASE WHEN twr.id IS NOT NULL THEN true ELSE false END
		FROM teams t
		LEFT JOIN team_weekly_reports twr ON twr.team_id = t.id AND twr.week_start = $1 AND twr.submitted_at IS NOT NULL
		LEFT JOIN users u ON u.id = twr.leader_id
		ORDER BY t.name`, weekStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := &model.DepartmentWeeklyReportSources{
		WeekStart:            weekStart,
		WeekEnd:              weekEnd,
		SubmittedTeamReports: []model.DepartmentTeamWeeklyReportSource{},
		MissingTeams:         []model.DepartmentMissingTeam{},
	}
	for rows.Next() {
		var item model.DepartmentTeamWeeklyReportSource
		var leaderID, reportID, content sql.NullString
		var submittedAt sql.NullTime
		if err := rows.Scan(&item.TeamID, &item.TeamName, &leaderID, &item.LeaderName, &reportID, &content, &submittedAt, &item.HasReport); err != nil {
			return nil, err
		}
		item.LeaderID = nullStringPtr(leaderID)
		item.ReportID = nullStringPtr(reportID)
		item.SubmittedAt = nullTimePtr(submittedAt)
		if content.Valid {
			item.Content = content.String
		}
		sources.TotalTeamCount++
		if item.HasReport {
			sources.SubmittedTeamCount++
			sources.SubmittedTeamReports = append(sources.SubmittedTeamReports, item)
		} else {
			sources.MissingTeams = append(sources.MissingTeams, model.DepartmentMissingTeam{TeamID: item.TeamID, TeamName: item.TeamName})
		}
	}
	return sources, rows.Err()
}

func (h *ReportHandler) buildPersonalWeeklyReportSources(userID, userName, weekStart, weekEnd string) (*model.PersonalWeeklyReportSources, error) {
	sources := &model.PersonalWeeklyReportSources{
		UserID:       userID,
		UserName:     userName,
		WeekStart:    weekStart,
		WeekEnd:      weekEnd,
		DailyReports: []model.WeeklyDailyReportSource{},
	}

	dailyRows, err := h.db.Query(`
		SELECT dr.id::text, dr.user_id::text, COALESCE(NULLIF(u.nickname,''), u.username), dr.report_date, dr.content
		FROM daily_reports dr
		JOIN users u ON u.id = dr.user_id
		WHERE dr.user_id = $1 AND dr.report_date BETWEEN $2 AND $3 AND dr.status IS NOT NULL
		ORDER BY dr.report_date`, userID, weekStart, weekEnd)
	if err != nil {
		return nil, err
	}
	defer dailyRows.Close()
	for dailyRows.Next() {
		var item model.WeeklyDailyReportSource
		if err := dailyRows.Scan(&item.ReportID, &item.UserID, &item.UserName, &item.ReportDate, &item.Content); err != nil {
			return nil, err
		}
		sources.DailyReports = append(sources.DailyReports, item)
	}
	if err := dailyRows.Err(); err != nil {
		return nil, err
	}
	sources.DailyCount = len(sources.DailyReports)

	return sources, nil
}

func personalWeeklyDailySourceIDs(sources *model.PersonalWeeklyReportSources, dailyIDs []string) []string {
	availableDaily := map[string]bool{}
	for _, item := range sources.DailyReports {
		availableDaily[item.ReportID] = true
	}
	return filterAvailableIDs(uniqueStringsPreserveOrder(dailyIDs), availableDaily)
}

func filterAvailableIDs(ids []string, available map[string]bool) []string {
	result := []string{}
	for _, id := range ids {
		if available[id] {
			result = append(result, id)
		}
	}
	return result
}

func (h *ReportHandler) upsertPersonalWeeklyReport(userID, weekStart, weekEnd string, req model.SavePersonalWeeklyReportRequest, status string, submittedTo *string) (*model.PersonalWeeklyReport, error) {
	sourceSessionIDs := []string{}
	sourceDailyIDs := uniqueStringsPreserveOrder(req.SourceDailyReportIDs)
	sourceTaskIDs := []string{}
	var reportID string
	if status == "submitted" {
		err := h.db.QueryRow(`
			INSERT INTO personal_weekly_reports (
				user_id, week_start, week_end, content, submitted_content, status, saved_at, submitted_at, submitted_to,
				source_daily_report_ids, source_session_ids, source_task_ids
			)
			VALUES ($1, $2, $3, $4, $4, 'submitted', now(), now(), $5, $6, $7, $8)
			ON CONFLICT (user_id, week_start)
			DO UPDATE SET
				week_end = EXCLUDED.week_end,
				content = EXCLUDED.content,
				submitted_content = EXCLUDED.submitted_content,
				status = 'submitted',
				saved_at = now(),
				submitted_at = now(),
				submitted_to = EXCLUDED.submitted_to,
				source_daily_report_ids = EXCLUDED.source_daily_report_ids,
				source_session_ids = EXCLUDED.source_session_ids,
				source_task_ids = EXCLUDED.source_task_ids,
				updated_at = now()
			RETURNING id::text`,
			userID, weekStart, weekEnd, req.Content, *submittedTo, pq.Array(sourceDailyIDs), pq.Array(sourceSessionIDs), pq.Array(sourceTaskIDs)).Scan(&reportID)
		if err != nil {
			return nil, err
		}
	} else {
		err := h.db.QueryRow(`
			INSERT INTO personal_weekly_reports (
				user_id, week_start, week_end, content, status, saved_at,
				source_daily_report_ids, source_session_ids, source_task_ids
			)
			VALUES ($1, $2, $3, $4, 'saved', now(), $5, $6, $7)
			ON CONFLICT (user_id, week_start)
			DO UPDATE SET
				week_end = EXCLUDED.week_end,
				content = EXCLUDED.content,
				status = 'saved',
				saved_at = now(),
				source_daily_report_ids = EXCLUDED.source_daily_report_ids,
				source_session_ids = EXCLUDED.source_session_ids,
				source_task_ids = EXCLUDED.source_task_ids,
				updated_at = now()
			RETURNING id::text`,
			userID, weekStart, weekEnd, req.Content, pq.Array(sourceDailyIDs), pq.Array(sourceSessionIDs), pq.Array(sourceTaskIDs)).Scan(&reportID)
		if err != nil {
			return nil, err
		}
	}
	return h.getPersonalWeeklyReportByID(reportID)
}

func (h *ReportHandler) getPersonalWeeklyReportByUserWeek(userID, weekStart string) (*model.PersonalWeeklyReport, error) {
	row := h.db.QueryRow(`
		SELECT pwr.id::text, pwr.user_id::text, COALESCE(NULLIF(u.nickname,''), u.username), pwr.week_start, pwr.week_end, pwr.content,
			pwr.submitted_content, pwr.status, pwr.saved_at, pwr.submitted_at, pwr.submitted_to,
			pwr.source_daily_report_ids, pwr.source_session_ids, pwr.source_task_ids,
			pwr.created_at, pwr.updated_at,
			pwr.edited, COALESCE(pwr.generation_mode, ''), pwr.managed_agent_run_id::text, pwr.agent_id, pwr.agent_version_id, pwr.model_id, ar.finished_at
		FROM personal_weekly_reports pwr
		JOIN users u ON u.id = pwr.user_id
		LEFT JOIN ai_runs ar ON ar.id = pwr.managed_agent_run_id
		WHERE pwr.user_id = $1 AND pwr.week_start = $2`, userID, weekStart)
	report, err := scanPersonalWeeklyReport(row)
	if err != nil {
		return nil, err
	}
	return &report, nil
}

func (h *ReportHandler) getPersonalWeeklyReportByID(id string) (*model.PersonalWeeklyReport, error) {
	row := h.db.QueryRow(`
		SELECT pwr.id::text, pwr.user_id::text, COALESCE(NULLIF(u.nickname,''), u.username), pwr.week_start, pwr.week_end, pwr.content,
			pwr.submitted_content, pwr.status, pwr.saved_at, pwr.submitted_at, pwr.submitted_to,
			pwr.source_daily_report_ids, pwr.source_session_ids, pwr.source_task_ids,
			pwr.created_at, pwr.updated_at,
			pwr.edited, COALESCE(pwr.generation_mode, ''), pwr.managed_agent_run_id::text, pwr.agent_id, pwr.agent_version_id, pwr.model_id, ar.finished_at
		FROM personal_weekly_reports pwr
		JOIN users u ON u.id = pwr.user_id
		LEFT JOIN ai_runs ar ON ar.id = pwr.managed_agent_run_id
		WHERE pwr.id = $1`, id)
	report, err := scanPersonalWeeklyReport(row)
	if err != nil {
		return nil, err
	}
	return &report, nil
}

func (h *ReportHandler) getTeamWeeklyReportByTeamWeek(teamID, weekStart string) (*model.TeamWeeklyReport, error) {
	row := h.db.QueryRow(`
		SELECT twr.id::text, twr.team_id::text, t.name, twr.leader_id::text, COALESCE(NULLIF(u.nickname,''), u.username),
			twr.week_start, twr.content, twr.source_daily_report_ids, twr.source_team_report_ids,
			twr.source_task_ids, twr.source_personal_weekly_report_ids,
			twr.submitted_at, twr.created_at, twr.updated_at,
			twr.edited, COALESCE(twr.generation_mode, ''), twr.managed_agent_run_id::text, twr.agent_id, twr.agent_version_id, twr.model_id, ar.finished_at
		FROM team_weekly_reports twr
		JOIN teams t ON t.id = twr.team_id
		JOIN users u ON u.id = twr.leader_id
		LEFT JOIN ai_runs ar ON ar.id = twr.managed_agent_run_id
		WHERE twr.team_id = $1 AND twr.week_start = $2`, teamID, weekStart)
	report, err := scanTeamWeeklyReport(row)
	return &report, err
}

func (h *ReportHandler) getTeamWeeklyReportByID(id string) (*model.TeamWeeklyReport, error) {
	row := h.db.QueryRow(`
		SELECT twr.id::text, twr.team_id::text, t.name, twr.leader_id::text, COALESCE(NULLIF(u.nickname,''), u.username),
			twr.week_start, twr.content, twr.source_daily_report_ids, twr.source_team_report_ids,
			twr.source_task_ids, twr.source_personal_weekly_report_ids,
			twr.submitted_at, twr.created_at, twr.updated_at,
			twr.edited, COALESCE(twr.generation_mode, ''), twr.managed_agent_run_id::text, twr.agent_id, twr.agent_version_id, twr.model_id, ar.finished_at
		FROM team_weekly_reports twr
		JOIN teams t ON t.id = twr.team_id
		JOIN users u ON u.id = twr.leader_id
		LEFT JOIN ai_runs ar ON ar.id = twr.managed_agent_run_id
		WHERE twr.id = $1`, id)
	report, err := scanTeamWeeklyReport(row)
	return &report, err
}

func (h *ReportHandler) getDepartmentWeeklyReportByWeek(weekStart string) (*model.DepartmentWeeklyReport, error) {
	row := h.db.QueryRow(`
		SELECT dwr.id::text, dwr.week_start, dwr.content, dwr.source_team_weekly_report_ids, dwr.archived_at, dwr.created_at, dwr.updated_at,
			dwr.edited, COALESCE(dwr.generation_mode, ''), dwr.managed_agent_run_id::text, dwr.agent_id, dwr.agent_version_id, dwr.model_id, ar.finished_at
		FROM department_weekly_reports dwr
		LEFT JOIN ai_runs ar ON ar.id = dwr.managed_agent_run_id
		WHERE dwr.week_start = $1`, weekStart)
	report, err := scanDepartmentWeeklyReport(row)
	return &report, err
}

func (h *ReportHandler) getDepartmentWeeklyReportByID(id string) (*model.DepartmentWeeklyReport, error) {
	row := h.db.QueryRow(`
		SELECT dwr.id::text, dwr.week_start, dwr.content, dwr.source_team_weekly_report_ids, dwr.archived_at, dwr.created_at, dwr.updated_at,
			dwr.edited, COALESCE(dwr.generation_mode, ''), dwr.managed_agent_run_id::text, dwr.agent_id, dwr.agent_version_id, dwr.model_id, ar.finished_at
		FROM department_weekly_reports dwr
		LEFT JOIN ai_runs ar ON ar.id = dwr.managed_agent_run_id
		WHERE dwr.id = $1`, id)
	report, err := scanDepartmentWeeklyReport(row)
	return &report, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanPersonalWeeklyReport(row scanner) (model.PersonalWeeklyReport, error) {
	var report model.PersonalWeeklyReport
	var submittedContent, submittedTo sql.NullString
	var generationMode, managedAgentRunID, agentID, modelID sql.NullString
	var agentVersionID sql.NullInt64
	var savedAt, submittedAt, generatedAt sql.NullTime
	var dailyIDsStr, sessionIDsStr, taskIDsStr string
	err := row.Scan(
		&report.ID, &report.UserID, &report.UserName, &report.WeekStart, &report.WeekEnd, &report.Content,
		&submittedContent, &report.Status, &savedAt, &submittedAt, &submittedTo,
		&dailyIDsStr, &sessionIDsStr, &taskIDsStr,
		&report.CreatedAt, &report.UpdatedAt,
		&report.Edited, &generationMode, &managedAgentRunID, &agentID, &agentVersionID, &modelID, &generatedAt,
	)
	if err != nil {
		return report, err
	}
	report.SubmittedContent = nullStringPtr(submittedContent)
	report.SavedAt = nullTimePtr(savedAt)
	report.SubmittedAt = nullTimePtr(submittedAt)
	report.SubmittedTo = nullStringPtr(submittedTo)
	report.SourceDailyReportIDs = parseUUIDArray(dailyIDsStr)
	report.SourceSessionIDs = parseUUIDArray(sessionIDsStr)
	report.SourceTaskIDs = parseUUIDArray(taskIDsStr)
	fillPersonalWeeklyReportProductFields(&report, generationMode, managedAgentRunID, agentID, agentVersionID, modelID, generatedAt)
	return report, nil
}

func scanTeamWeeklyReport(row scanner) (model.TeamWeeklyReport, error) {
	var report model.TeamWeeklyReport
	var generationMode, managedAgentRunID, agentID, modelID sql.NullString
	var agentVersionID sql.NullInt64
	var generatedAt sql.NullTime
	var dailyIDsStr, teamIDsStr, taskIDsStr, personalWeeklyIDsStr string
	var submittedAt sql.NullTime
	err := row.Scan(&report.ID, &report.TeamID, &report.TeamName, &report.LeaderID, &report.LeaderName,
		&report.WeekStart, &report.Content, &dailyIDsStr, &teamIDsStr, &taskIDsStr, &personalWeeklyIDsStr,
		&submittedAt, &report.CreatedAt, &report.UpdatedAt,
		&report.Edited, &generationMode, &managedAgentRunID, &agentID, &agentVersionID, &modelID, &generatedAt)
	if err != nil {
		return report, err
	}
	report.SourceDailyReportIDs = parseUUIDArray(dailyIDsStr)
	report.SourceTeamReportIDs = parseUUIDArray(teamIDsStr)
	report.SourceTaskIDs = parseUUIDArray(taskIDsStr)
	report.SourcePersonalWeeklyReportIDs = parseUUIDArray(personalWeeklyIDsStr)
	report.SubmittedAt = nullTimePtr(submittedAt)
	fillTeamWeeklyReportProductFields(&report, generationMode, managedAgentRunID, agentID, agentVersionID, modelID, generatedAt)
	return report, nil
}

func scanDepartmentWeeklyReport(row scanner) (model.DepartmentWeeklyReport, error) {
	var report model.DepartmentWeeklyReport
	var generationMode, managedAgentRunID, agentID, modelID sql.NullString
	var agentVersionID sql.NullInt64
	var generatedAt sql.NullTime
	var sourceIDsStr string
	var archivedAt sql.NullTime
	err := row.Scan(&report.ID, &report.WeekStart, &report.Content, &sourceIDsStr, &archivedAt, &report.CreatedAt, &report.UpdatedAt,
		&report.Edited, &generationMode, &managedAgentRunID, &agentID, &agentVersionID, &modelID, &generatedAt)
	if err != nil {
		return report, err
	}
	report.SourceTeamWeeklyReportIDs = parseUUIDArray(sourceIDsStr)
	report.ArchivedAt = nullTimePtr(archivedAt)
	fillDepartmentWeeklyReportProductFields(&report, generationMode, managedAgentRunID, agentID, agentVersionID, modelID, generatedAt)
	return report, nil
}

func weeklyRangeFromRequest(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	return weeklyRangeFromValue(w, r.URL.Query().Get("week_start"))
}

func weeklyRangeFromValue(w http.ResponseWriter, weekStart string) (string, string, bool) {
	var start time.Time
	var err error
	if weekStart == "" {
		now := time.Now()
		daysFromMonday := (int(now.Weekday()) + 6) % 7
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -daysFromMonday)
	} else {
		start, err = time.Parse("2006-01-02", weekStart)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid week_start"})
			return "", "", false
		}
	}
	end := start.AddDate(0, 0, 6)
	return start.Format("2006-01-02"), end.Format("2006-01-02"), true
}

func reportDateFromRequest(r *http.Request) string {
	if reportDate := r.URL.Query().Get("report_date"); reportDate != "" {
		return reportDate
	}
	if reportDate := r.URL.Query().Get("date"); reportDate != "" {
		return reportDate
	}
	return time.Now().Format("2006-01-02")
}

func parseUUIDArray(pgArray string) []string {
	return parseTextArray(pgArray)
}

func joinWithCommas(items []string) string {
	result := ""
	for i, item := range items {
		if i > 0 {
			result += ", "
		}
		result += item
	}
	return result
}

func uniqueStringsPreserveOrder(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func orderDraftSessions(sessions []model.ReportDraftSession, orderedIDs []string) []model.ReportDraftSession {
	byID := make(map[string]model.ReportDraftSession, len(sessions))
	for _, session := range sessions {
		byID[session.ID] = session
	}
	ordered := make([]model.ReportDraftSession, 0, len(sessions))
	for _, id := range orderedIDs {
		if session, ok := byID[id]; ok {
			ordered = append(ordered, session)
		}
	}
	return ordered
}
