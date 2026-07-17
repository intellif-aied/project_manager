package handler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/reportsource"
	"github.com/lib/pq"
)

// reportAIRun is the validated AI run after aiRunGuard.
type reportAIRun struct {
	ID           string
	BusinessType string
	AgentID      string
	ModelID      *string
	Status       string
	InputRef     map[string]any
	OutputRef    map[string]any
	CreatedAt    time.Time
}

type writeReportResultArgs struct {
	ReportType string       `json:"report_type"`
	Period     periodArgs   `json:"period"`
	Target     reportTarget `json:"target,omitempty"`
	RunID      string       `json:"run_id"`
	Content    string       `json:"content"`
	Summary    string       `json:"summary,omitempty"`
}

type writeReportFailureArgs struct {
	ReportType   string       `json:"report_type"`
	Period       periodArgs   `json:"period"`
	Target       reportTarget `json:"target,omitempty"`
	RunID        string       `json:"run_id"`
	ErrorCode    string       `json:"error_code,omitempty"`
	ErrorMessage string       `json:"error_message"`
}

func (h *ReportMCPHandler) toolWriteReportResult(r *http.Request, rawArgs json.RawMessage) (any, error) {
	u, err := requireUser(r)
	if err != nil {
		return nil, err
	}
	var args writeReportResultArgs
	if err := decodeArguments(rawArgs, &args); err != nil {
		return nil, err
	}
	if err := validateReportType(args.ReportType); err != nil {
		return nil, err
	}
	date, ws, we, err := resolveReportPeriod(args.ReportType, args.Period)
	if err != nil {
		return nil, err
	}
	target, err := resolveTarget(u, args.Target, args.ReportType, true)
	if err != nil {
		return nil, err
	}
	run, err := h.aiRunGuard(r, args.RunID, u.ID)
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(args.Content)
	if content == "" {
		return nil, mcpErr("INVALID_ARGUMENT", "content is required")
	}
	validationIssues := reportContentValidationIssues(content, args.ReportType, date, ws, we)
	if summary := strings.TrimSpace(args.Summary); summary != "" {
		for _, issue := range reportContentValidationIssues(summary, args.ReportType, date, ws, we) {
			validationIssues = append(validationIssues, "摘要："+issue)
		}
	}
	consistencyText := content
	if summary := strings.TrimSpace(args.Summary); summary != "" {
		consistencyText += "\n" + summary
	}
	sourceIssues, err := reportSourceConsistencyIssues(r.Context(), h.db, args.ReportType, date, ws, we, target, consistencyText)
	if err != nil {
		return nil, errMCPInternal
	}
	validationIssues = append(validationIssues, sourceIssues...)
	activityIssues, err := reportPersonalSourceActivityIssues(r.Context(), h.db, run.InputRef, args.ReportType, consistencyText)
	if err != nil {
		return nil, errMCPInternal
	}
	validationIssues = append(validationIssues, activityIssues...)
	coverageIssues, err := reportPersonalDigestOutcomeCoverageIssues(
		r.Context(), h.db, run.InputRef, args.ReportType, date, content,
	)
	if err != nil {
		return nil, errMCPInternal
	}
	validationIssues = append(validationIssues, coverageIssues...)
	if len(validationIssues) > 0 {
		return nil, mcpErr("REPORT_CONTENT_INVALID", strings.Join(validationIssues, "；"))
	}
	resultHash := reportResultHash(content)
	if idempotent, reportID, err := validateReportWriteAllowed(run, args.ReportType, date, ws, we, target, resultHash); err != nil {
		return nil, err
	} else if idempotent {
		return mcpTextResult(map[string]any{
			"status":          "saved",
			"report_id":       reportID,
			"report_type":     args.ReportType,
			"agent_run_id":    run.ID,
			"already_written": true,
		}), nil
	}

	ctx := r.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errMCPInternal
	}
	defer tx.Rollback()
	if selectionID := strings.TrimSpace(stringFromAny(run.InputRef["report_source_selection_id"])); selectionID != "" {
		if h.reportSource == nil {
			return nil, mcpErr("REPORT_SOURCE_MISMATCH", "report source selection is unavailable")
		}
		period, periodErr := reportsource.ReportPeriod(args.ReportType, date, ws, we)
		if periodErr != nil {
			return nil, mcpErr("REPORT_SOURCE_MISMATCH", "report source period is invalid")
		}
		if err := h.reportSource.ValidateAttachedSelectionTx(
			ctx, tx, u.ID, selectionID, run.ID, args.ReportType, period,
		); err != nil {
			switch {
			case errors.Is(err, reportsource.ErrSourceIncomplete):
				return nil, mcpErr("REPORT_SOURCE_INCOMPLETE", "the report source must be read completely in the run's required mode before writing the report")
			case errors.Is(err, reportsource.ErrSourceUnavailable):
				return nil, mcpErr("CONTENT_CLEARED", "report source content is no longer available")
			case errors.Is(err, reportsource.ErrDigestVersionMismatch):
				return nil, mcpErr("REPORT_SOURCE_DIGEST_VERSION_MISMATCH", "report source digest version does not match this run")
			case errors.Is(err, reportsource.ErrDigestCorrupt):
				return nil, mcpErr("REPORT_SOURCE_DIGEST_FAILED", "report source digest integrity check failed")
			case errors.Is(err, reportsource.ErrSelectionMismatch), errors.Is(err, reportsource.ErrSelectionNotFound):
				return nil, mcpErr("REPORT_SOURCE_MISMATCH", "report source selection does not match this run")
			default:
				return nil, errMCPInternal
			}
		}
	}

	existing, err := selectReportForUpdate(ctx, tx, args.ReportType, date, ws, we, target)
	if err != nil {
		return nil, errMCPInternal
	}
	if existing != nil && existing.Edited && existing.UpdatedAt.After(run.CreatedAt) {
		msg := "报告已被用户编辑，AI 回写已取消"
		if err := markAIRunFailedTx(ctx, tx, run.ID, u.ID, reportEditConflictCode, msg); err != nil {
			return nil, errMCPInternal
		}
		if err := tx.Commit(); err != nil {
			return nil, errMCPInternal
		}
		return nil, errReportEditConflict
	}

	reportID, err := upsertReportContent(ctx, tx, args.ReportType, date, ws, we, target, content, run, u.ID)
	if err != nil {
		return nil, errMCPInternal
	}

	outputPayload := map[string]any{
		"report_type":        args.ReportType,
		"report_id":          reportID,
		"date":               date,
		"week_start":         ws,
		"week_end":           we,
		"target":             target,
		"summary":            args.Summary,
		"report_result_hash": resultHash,
	}
	copyReportRunMetadata(outputPayload, run.InputRef)
	outputRef, _ := json.Marshal(outputPayload)
	if _, err := tx.ExecContext(ctx, `
		UPDATE ai_runs
		SET status = 'succeeded',
		    business_id = $1,
		    output_ref_json = $2,
		    error_message = NULL,
		    finished_at = now()
		WHERE id = $3 AND user_id = $4`, reportID, outputRef, run.ID, u.ID); err != nil {
		return nil, errMCPInternal
	}
	if err := tx.Commit(); err != nil {
		return nil, errMCPInternal
	}

	return mcpTextResult(map[string]any{
		"status":               "saved",
		"report_id":            reportID,
		"report_type":          args.ReportType,
		"agent_run_id":         run.ID,
		"managed_agent_run_id": run.ID,
		"product_status":       "ai_generated",
		"origin":               "ai",
		"updated_by_user":      false,
	}), nil
}

func copyReportRunMetadata(out map[string]any, input map[string]any) {
	if out == nil || input == nil {
		return
	}
	for _, key := range []string{"trigger_source", "scheduled_trigger_at", "schedule_id", "schedule_name"} {
		if value := strings.TrimSpace(stringFromAny(input[key])); value != "" {
			out[key] = value
		}
	}
}

func (h *ReportMCPHandler) toolWriteReportFailure(r *http.Request, rawArgs json.RawMessage) (any, error) {
	u, err := requireUser(r)
	if err != nil {
		return nil, err
	}
	var args writeReportFailureArgs
	if err := decodeArguments(rawArgs, &args); err != nil {
		return nil, err
	}
	if err := validateReportType(args.ReportType); err != nil {
		return nil, err
	}
	if _, _, _, err := resolveReportPeriod(args.ReportType, args.Period); err != nil {
		return nil, err
	}
	if _, err := resolveTarget(u, args.Target, args.ReportType, true); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.RunID) == "" {
		return nil, mcpErr("INVALID_ARGUMENT", "run_id is required")
	}
	run, err := h.aiRunGuard(r, args.RunID, u.ID)
	if err != nil {
		return nil, err
	}
	if err := validateReportFailureAllowed(run); err != nil {
		return nil, err
	}
	errorMessage := strings.TrimSpace(args.ErrorMessage)
	if errorMessage == "" {
		errorMessage = "Agent 生成失败"
	}
	errorCode := strings.TrimSpace(args.ErrorCode)
	formatted := errorMessage
	if errorCode != "" {
		formatted = errorCode + ": " + errorMessage
	}
	if _, err := h.db.ExecContext(r.Context(), `
		UPDATE ai_runs
		SET status = 'failed',
		    error_message = $1,
		    finished_at = now()
		WHERE id::text = $2 AND user_id = $3`, formatted, strings.TrimSpace(args.RunID), u.ID); err != nil {
		return nil, errMCPInternal
	}
	return mcpTextResult(map[string]any{
		"run_id":    args.RunID,
		"status":    "failed",
		"retryable": true,
	}), nil
}

// aiRunGuard validates that runID belongs to the current user.
func (h *ReportMCPHandler) aiRunGuard(r *http.Request, runID, userID string) (*reportAIRun, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, mcpErr("INVALID_ARGUMENT", "run_id is required")
	}
	var run reportAIRun
	var modelID sql.NullString
	var createdAt sql.NullTime
	var inputRaw, outputRaw []byte
	err := h.db.QueryRowContext(r.Context(), `
		SELECT id::text, business_type, COALESCE(agent_id, ''), model_id, status, input_ref_json, output_ref_json, created_at
		FROM ai_runs
		WHERE id::text = $1 AND user_id = $2`, runID, userID).
		Scan(&run.ID, &run.BusinessType, &run.AgentID, &modelID, &run.Status, &inputRaw, &outputRaw, &createdAt)
	if err == sql.ErrNoRows {
		return nil, errRunNotFound
	}
	if err != nil {
		return nil, errMCPInternal
	}
	if modelID.Valid && modelID.String != "" {
		s := modelID.String
		run.ModelID = &s
	}
	if createdAt.Valid {
		run.CreatedAt = createdAt.Time
	}
	_ = json.Unmarshal(inputRaw, &run.InputRef)
	_ = json.Unmarshal(outputRaw, &run.OutputRef)
	if run.InputRef == nil {
		run.InputRef = map[string]any{}
	}
	if run.OutputRef == nil {
		run.OutputRef = map[string]any{}
	}
	return &run, nil
}

func reportResultHash(content string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return fmt.Sprintf("%x", sum[:])
}

func validateReportWriteAllowed(run *reportAIRun, reportType, date, ws, we string, target reportTarget, resultHash string) (bool, string, error) {
	if run.BusinessType != reportAgentRunBusinessType {
		return false, "", mcpErr("RUN_NOT_WRITABLE", "run is not a Report Agent run")
	}
	if run.Status == "failed" || run.Status == "timeout" || run.Status == "pending" {
		return false, "", mcpErr("RUN_NOT_WRITABLE", "run status does not allow report write")
	}
	if err := validateRunReportIdentity(run.InputRef, reportType, date, ws, we, target); err != nil {
		return false, "", err
	}
	if run.Status != "succeeded" {
		if run.Status == "running" {
			return false, "", nil
		}
		return false, "", mcpErr("RUN_NOT_WRITABLE", "run status does not allow report write")
	}
	if stringFromAny(run.OutputRef["report_result_hash"]) != resultHash {
		return false, "", mcpErr("REPORT_WRITE_CONFLICT", "report payload hash conflicts with existing result")
	}
	if err := validateRunReportIdentity(run.OutputRef, reportType, date, ws, we, target); err != nil {
		return false, "", err
	}
	reportID := stringFromAny(run.OutputRef["report_id"])
	return true, reportID, nil
}

func validateReportFailureAllowed(run *reportAIRun) error {
	if run.BusinessType != reportAgentRunBusinessType || run.Status == "failed" || run.Status == "timeout" || run.Status == "succeeded" {
		return mcpErr("RUN_NOT_WRITABLE", "run status does not allow failure write")
	}
	return nil
}

func validateRunReportIdentity(ref map[string]any, reportType, date, ws, we string, target reportTarget) error {
	if len(ref) == 0 {
		return nil
	}
	if existing := stringFromAny(ref["report_type"]); existing != "" && existing != reportType {
		return mcpErr("REPORT_WRITE_CONFLICT", "report_type does not match run")
	}
	if existing := stringFromAny(ref["date"]); existing != "" && date != "" && existing != date {
		return mcpErr("REPORT_WRITE_CONFLICT", "period does not match run")
	}
	if existing := stringFromAny(ref["week_start"]); existing != "" && ws != "" && existing != ws {
		return mcpErr("REPORT_WRITE_CONFLICT", "period does not match run")
	}
	if existing := stringFromAny(ref["week_end"]); existing != "" && we != "" && existing != we {
		return mcpErr("REPORT_WRITE_CONFLICT", "period does not match run")
	}
	if periodRaw, ok := ref["period"]; ok {
		if period, ok := stringMapFromAny(periodRaw); ok {
			if existing := period["date"]; existing != "" && date != "" && existing != date {
				return mcpErr("REPORT_WRITE_CONFLICT", "period does not match run")
			}
			if existing := period["week_start"]; existing != "" && ws != "" && existing != ws {
				return mcpErr("REPORT_WRITE_CONFLICT", "period does not match run")
			}
			if existing := period["week_end"]; existing != "" && we != "" && existing != we {
				return mcpErr("REPORT_WRITE_CONFLICT", "period does not match run")
			}
		}
	}
	if targetRaw, ok := ref["target"]; ok {
		if existing, ok := stringMapFromAny(targetRaw); ok {
			if value := existing["type"]; value != "" && value != target.Type {
				return mcpErr("REPORT_WRITE_CONFLICT", "target does not match run")
			}
			if value := existing["user_id"]; value != "" && value != target.UserID {
				return mcpErr("REPORT_WRITE_CONFLICT", "target does not match run")
			}
			if value := existing["team_id"]; value != "" && value != target.TeamID {
				return mcpErr("REPORT_WRITE_CONFLICT", "target does not match run")
			}
			if value := existing["department_id"]; value != "" && value != target.DepartmentID {
				return mcpErr("REPORT_WRITE_CONFLICT", "target does not match run")
			}
		}
	}
	return nil
}

func stringMapFromAny(value any) (map[string]string, bool) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	out := map[string]string{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false
	}
	return out, true
}

func stringFromAny(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// existingReportRow is the SELECT FOR UPDATE result used for the防覆盖 check.
type existingReportRow struct {
	ID        string
	Edited    bool
	UpdatedAt time.Time
}

func selectReportForUpdate(ctx context.Context, tx *sql.Tx, reportType, date, ws, we string, target reportTarget) (*existingReportRow, error) {
	q, args := selectForUpdateQuery(reportType, date, ws, we, target)
	row := tx.QueryRowContext(ctx, q, args...)
	var e existingReportRow
	err := row.Scan(&e.ID, &e.Edited, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func selectForUpdateQuery(reportType, date, ws, we string, target reportTarget) (string, []any) {
	switch reportType {
	case reportTypePersonalDaily:
		return `SELECT id::text, edited, updated_at FROM daily_reports WHERE user_id = $1 AND report_date = $2 FOR UPDATE`, []any{target.UserID, date}
	case reportTypePersonalWeekly:
		return `SELECT id::text, edited, updated_at FROM personal_weekly_reports WHERE user_id = $1 AND week_start = $2 AND week_end = $3 FOR UPDATE`, []any{target.UserID, ws, we}
	case reportTypeTeamDaily:
		return `SELECT id::text, edited, updated_at FROM team_reports WHERE team_id = $1 AND report_date = $2 FOR UPDATE`, []any{target.TeamID, date}
	case reportTypeTeamWeekly:
		return `SELECT id::text, edited, updated_at FROM team_weekly_reports WHERE team_id = $1 AND week_start = $2 AND week_end = $3 FOR UPDATE`, []any{target.TeamID, ws, we}
	case reportTypeDepartmentDaily:
		return `SELECT id::text, edited, updated_at FROM department_reports WHERE department_id=$1 AND report_date=$2 FOR UPDATE`, []any{target.DepartmentID, date}
	case reportTypeDepartmentWeekly:
		return `SELECT id::text, edited, updated_at FROM department_weekly_reports WHERE department_id=$1 AND week_start=$2 AND week_end=$3 FOR UPDATE`, []any{target.DepartmentID, ws, we}
	}
	return "", nil
}

// upsertReportContent writes content + agent metadata into the target table and returns the report ID.
// leaderID is the current user's ID, used for team_reports / team_weekly_reports leader_id column.
func upsertReportContent(ctx context.Context, tx *sql.Tx, reportType, date, ws, we string, target reportTarget, content string, run *reportAIRun, leaderID string) (string, error) {
	switch reportType {
	case reportTypePersonalDaily:
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO daily_reports (user_id, report_date, content, edited, generation_mode, managed_agent_run_id, agent_id, model_id, status, saved_at)
			VALUES ($1, $2, $3, false, 'managed_agent', $4, $5, $6, 'saved', now())
			ON CONFLICT (user_id, report_date) DO UPDATE
			SET content = EXCLUDED.content,
			    edited = false,
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    model_id = EXCLUDED.model_id,
			    status = 'saved',
			    saved_at = now(),
			    updated_at = now()
			RETURNING id::text`, target.UserID, date, content, run.ID, nullableValue(run.AgentID), nullablePtrValue(run.ModelID)).Scan(&reportID)
		return reportID, err
	case reportTypePersonalWeekly:
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO personal_weekly_reports (user_id, week_start, week_end, content, status, generation_mode, managed_agent_run_id, agent_id, model_id, edited, saved_at)
			VALUES ($1, $2, $3, $4, 'saved', 'managed_agent', $5, $6, $7, false, now())
			ON CONFLICT (user_id, week_start) DO UPDATE
			SET content = EXCLUDED.content,
			    week_end = EXCLUDED.week_end,
			    status = 'saved',
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    model_id = EXCLUDED.model_id,
			    edited = false,
			    saved_at = now(),
			    updated_at = now()
			RETURNING id::text`, target.UserID, ws, we, content, run.ID, nullableValue(run.AgentID), nullablePtrValue(run.ModelID)).Scan(&reportID)
		return reportID, err
	case reportTypeTeamDaily:
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO team_reports (team_id, leader_id, report_date, content, generation_mode, managed_agent_run_id, agent_id, model_id, edited, status, saved_at)
			VALUES ($1, $2, $3, $4, 'managed_agent', $5, $6, $7, false, 'saved', now())
			ON CONFLICT (team_id, report_date) DO UPDATE
			SET content = EXCLUDED.content,
			    leader_id = EXCLUDED.leader_id,
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    model_id = EXCLUDED.model_id,
			    edited = false,
			    status = 'saved',
			    saved_at = now(),
			    updated_at = now()
			RETURNING id::text`, target.TeamID, leaderID, date, content, run.ID, nullableValue(run.AgentID), nullablePtrValue(run.ModelID)).Scan(&reportID)
		return reportID, err
	case reportTypeTeamWeekly:
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO team_weekly_reports (team_id, leader_id, week_start, week_end, content, generation_mode, managed_agent_run_id, agent_id, model_id, edited)
			VALUES ($1, $2, $3, $4, $5, 'managed_agent', $6, $7, $8, false)
			ON CONFLICT (team_id, week_start) DO UPDATE
			SET content = EXCLUDED.content,
			    leader_id = EXCLUDED.leader_id,
			    week_end = EXCLUDED.week_end,
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    model_id = EXCLUDED.model_id,
			    edited = false,
			    updated_at = now()
			RETURNING id::text`, target.TeamID, leaderID, ws, we, content, run.ID, nullableValue(run.AgentID), nullablePtrValue(run.ModelID)).Scan(&reportID)
		return reportID, err
	case reportTypeDepartmentDaily:
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO department_reports (department_id, report_date, content, generation_mode, managed_agent_run_id, agent_id, model_id, edited, status, saved_at)
			VALUES ($1, $2, $3, 'managed_agent', $4, $5, $6, false, 'saved', now())
			ON CONFLICT (department_id, report_date) WHERE department_id IS NOT NULL DO UPDATE
			SET content = EXCLUDED.content,
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    model_id = EXCLUDED.model_id,
			    edited = false,
			    status = 'saved',
			    saved_at = now(),
			    updated_at = now()
			RETURNING id::text`, target.DepartmentID, date, content, run.ID, nullableValue(run.AgentID), nullablePtrValue(run.ModelID)).Scan(&reportID)
		return reportID, err
	case reportTypeDepartmentWeekly:
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO department_weekly_reports (department_id, week_start, week_end, content, generation_mode, managed_agent_run_id, agent_id, model_id, edited)
			VALUES ($1, $2, $3, $4, 'managed_agent', $5, $6, $7, false)
			ON CONFLICT (department_id, week_start) WHERE department_id IS NOT NULL DO UPDATE
			SET content = EXCLUDED.content,
			    week_end = EXCLUDED.week_end,
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    model_id = EXCLUDED.model_id,
			    edited = false,
			    updated_at = now()
			RETURNING id::text`, target.DepartmentID, ws, we, content, run.ID, nullableValue(run.AgentID), nullablePtrValue(run.ModelID)).Scan(&reportID)
		return reportID, err
	}
	return "", fmt.Errorf("unsupported report_type: %s", reportType)
}

func markAIRunFailedTx(ctx context.Context, tx *sql.Tx, runID, userID, code, message string) error {
	formatted := message
	if code != "" {
		formatted = code + ": " + message
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE ai_runs
		SET status = 'failed',
		    error_message = $1,
		    finished_at = now()
		WHERE id::text = $2 AND user_id = $3`, formatted, runID, userID)
	return err
}

func nullableValue(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullablePtrValue(p *string) any {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}

var _ = pq.Array
