package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	ManagedAgentPendingTimeout         = 10 * time.Minute
	ManagedAgentSessionTimeout         = 30 * time.Minute
	ManagedAgentRunTimeout             = 2 * time.Hour
	ManagedAgentReportWritebackGrace   = 2 * time.Minute
	managedReportAgentRunBusinessType  = "report_agent_run"
	reportWritebackMissingErrorMessage = "managed agent session completed without report writeback"
)

type ManagedAgentRunStatusSyncer struct {
	db         *sql.DB
	client     *ManagedAgentClient
	interval   time.Duration
	timeout    time.Duration
	batchLimit int
}

type managedAgentRunStatusRow struct {
	ID                string
	ExternalTaskID    string
	ExternalSessionID string
	Status            string
	BusinessType      string
	BusinessID        string
	OutputRefJSON     []byte
	StartedAt         time.Time
}

func NewManagedAgentRunStatusSyncer(db *sql.DB, client *ManagedAgentClient) *ManagedAgentRunStatusSyncer {
	return &ManagedAgentRunStatusSyncer{
		db:         db,
		client:     client,
		interval:   time.Minute,
		timeout:    ManagedAgentRunTimeout,
		batchLimit: 100,
	}
}

func (s *ManagedAgentRunStatusSyncer) Start(ctx context.Context) {
	if s == nil || s.db == nil || s.client == nil || !s.client.Configured() {
		return
	}
	go func() {
		if err := s.RunOnce(ctx, time.Now()); err != nil {
			log.Printf("managed agent run status syncer failed: %v", err)
		}

		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if err := s.RunOnce(ctx, now); err != nil {
					log.Printf("managed agent run status syncer failed: %v", err)
				}
			}
		}
	}()
}

func (s *ManagedAgentRunStatusSyncer) RunOnce(ctx context.Context, now time.Time) error {
	rows, err := s.db.QueryContext(ctx, `
			SELECT id::text, COALESCE(external_task_id, ''), COALESCE(external_session_id, ''), status,
			       business_type, COALESCE(business_id::text, ''), COALESCE(output_ref_json, '{}'::jsonb),
			       COALESCE(started_at, created_at)
			FROM ai_runs
			WHERE status IN ('pending', 'running')
			ORDER BY created_at ASC
			LIMIT $1`, s.batchLimit)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var run managedAgentRunStatusRow
		if err := rows.Scan(&run.ID, &run.ExternalTaskID, &run.ExternalSessionID, &run.Status, &run.BusinessType, &run.BusinessID, &run.OutputRefJSON, &run.StartedAt); err != nil {
			return err
		}
		if err := s.refreshRun(ctx, run, now); err != nil {
			log.Printf("managed agent run %s status refresh failed: %v", run.ID, err)
		}
	}
	return rows.Err()
}

func (s *ManagedAgentRunStatusSyncer) refreshRun(ctx context.Context, run managedAgentRunStatusRow, now time.Time) error {
	externalRunID := run.ExternalTaskID
	if externalRunID == "" {
		externalRunID = run.ExternalSessionID
	}
	if externalRunID == "" {
		if run.Status == "pending" && !now.Before(run.StartedAt.Add(ManagedAgentPendingTimeout)) {
			return s.updateRunStatus(ctx, run, nil, "timeout", "managed agent run pending submit timed out after 10m", now)
		}
		if run.Status == "running" && !now.Before(run.StartedAt.Add(s.timeout)) {
			return s.updateRunStatus(ctx, run, nil, "timeout", "managed agent run timed out after 2h", now)
		}
		return nil
	}
	task, err := s.client.GetTaskStatus(ctx, externalRunID)
	if err != nil {
		if s.isTimedOut(run, now) {
			msg := "managed agent run timed out after 2h while refreshing status: " + err.Error()
			return s.updateRunStatus(ctx, run, nil, "timeout", msg, now)
		}
		return err
	}

	status := NormalizeManagedRunStatus(task.Status)
	errMsg := ""
	if status == "failed" && strings.TrimSpace(task.Error) != "" {
		errMsg = task.Error
	}
	if run.isReportAgentRun() && status == "succeeded" && !run.hasReportWriteback() {
		if !reportWritebackGraceElapsed(task, now) {
			status = "running"
		} else {
			reportID, fallbackErr := s.fallbackReportWriteback(ctx, run, externalRunID, task)
			if fallbackErr != nil {
				status = "failed"
				errMsg = reportWritebackMissingErrorMessage + ": " + fallbackErr.Error()
			} else if reportID != "" {
				status = "succeeded"
				run.BusinessID = reportID
			} else {
				status = "failed"
				errMsg = reportWritebackMissingErrorMessage
			}
		}
	}
	if !IsTerminalManagedRunStatus(status) && s.isTimedOut(run, now) {
		status = "timeout"
		errMsg = "managed agent run timed out after 2h"
	}
	return s.updateRunStatus(ctx, run, task, status, errMsg, now)
}

func (run managedAgentRunStatusRow) isReportAgentRun() bool {
	return strings.TrimSpace(run.BusinessType) == managedReportAgentRunBusinessType
}

func (run managedAgentRunStatusRow) hasReportWriteback() bool {
	if strings.TrimSpace(run.BusinessID) != "" {
		return true
	}
	var output map[string]any
	if err := json.Unmarshal(run.OutputRefJSON, &output); err != nil {
		return false
	}
	return strings.TrimSpace(managedStringFromAny(output["report_id"])) != ""
}

func reportWritebackGraceElapsed(task *ManagedTaskStatus, now time.Time) bool {
	if task == nil || task.FinishedAt <= 0 {
		return true
	}
	return !now.Before(time.Unix(task.FinishedAt, 0).Add(ManagedAgentReportWritebackGrace))
}

func managedStringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func (s *ManagedAgentRunStatusSyncer) isTimedOut(run managedAgentRunStatusRow, now time.Time) bool {
	return !run.StartedAt.IsZero() && !now.Before(run.StartedAt.Add(s.timeout))
}

func (s *ManagedAgentRunStatusSyncer) updateRunStatus(ctx context.Context, run managedAgentRunStatusRow, task *ManagedTaskStatus, status string, errorMessage string, now time.Time) error {
	output := map[string]any{
		"task_id":   run.ExternalTaskID,
		"status":    status,
		"synced_at": now.UTC().Format(time.RFC3339),
	}
	if run.ExternalSessionID != "" {
		output["session_id"] = run.ExternalSessionID
	}
	agentVersionID := 0
	modelID := ""
	if task != nil {
		if task.TaskID != "" {
			output["task_id"] = task.TaskID
		}
		if task.Status != "" {
			output["status"] = task.Status
		}
		if task.Progress != "" {
			output["progress"] = task.Progress
		}
		if task.Error != "" {
			output["error"] = task.Error
		}
		agentVersionID = task.AgentVersionID
		modelID = strings.TrimSpace(task.ModelID)
	}
	if errorMessage != "" {
		output["error"] = errorMessage
	}
	if reportID := strings.TrimSpace(run.BusinessID); reportID != "" {
		output["report_id"] = reportID
	}
	outputJSON, _ := json.Marshal(output)

	sets := []string{"status = $1", "output_ref_json = $2", "agent_version_id = $3"}
	args := []any{status, outputJSON, nullableManagedInt(agentVersionID)}
	argIdx := 4
	if modelID != "" {
		sets = append(sets, fmt.Sprintf("model_id = $%d", argIdx))
		args = append(args, modelID)
		argIdx++
	}
	if errorMessage != "" {
		sets = append(sets, fmt.Sprintf("error_message = $%d", argIdx))
		args = append(args, errorMessage)
		argIdx++
	}
	if strings.TrimSpace(run.BusinessID) != "" {
		sets = append(sets, fmt.Sprintf("business_id = $%d", argIdx))
		args = append(args, run.BusinessID)
		argIdx++
	}
	if IsTerminalManagedRunStatus(status) {
		sets = append(sets, fmt.Sprintf("finished_at = $%d", argIdx))
		args = append(args, now)
		argIdx++
	}
	args = append(args, run.ID)
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("UPDATE ai_runs SET %s WHERE id = $%d AND status IN ('pending', 'running')", strings.Join(sets, ", "), argIdx), args...)
	return err
}

type reportWritebackFallbackRun struct {
	UserID       string
	AgentID      string
	InputRefJSON []byte
}

type reportWritebackFallbackInput struct {
	ReportType string         `json:"report_type"`
	Period     map[string]any `json:"period"`
	Target     map[string]any `json:"target"`
}

func (s *ManagedAgentRunStatusSyncer) fallbackReportWriteback(ctx context.Context, run managedAgentRunStatusRow, externalRunID string, task *ManagedTaskStatus) (string, error) {
	if task == nil || strings.TrimSpace(task.Result) == "" {
		if strings.TrimSpace(externalRunID) == "" {
			return "", nil
		}
		resultTask, err := s.client.GetTaskResult(ctx, externalRunID)
		if err != nil {
			return "", err
		}
		task = resultTask
	}
	content := strings.TrimSpace(task.Result)
	if content == "" {
		return "", nil
	}

	var fallbackRun reportWritebackFallbackRun
	if err := s.db.QueryRowContext(ctx, `
		SELECT user_id::text, agent_id, COALESCE(input_ref_json, '{}'::jsonb)
		FROM ai_runs
		WHERE id::text = $1`, run.ID).Scan(&fallbackRun.UserID, &fallbackRun.AgentID, &fallbackRun.InputRefJSON); err != nil {
		return "", err
	}

	var input reportWritebackFallbackInput
	if err := json.Unmarshal(fallbackRun.InputRefJSON, &input); err != nil {
		return "", err
	}
	reportType := strings.TrimSpace(input.ReportType)
	date := strings.TrimSpace(managedStringFromAny(input.Period["date"]))
	weekStart := strings.TrimSpace(managedStringFromAny(input.Period["week_start"]))
	weekEnd := strings.TrimSpace(managedStringFromAny(input.Period["week_end"]))
	targetUserID := strings.TrimSpace(managedStringFromAny(input.Target["user_id"]))
	targetTeamID := strings.TrimSpace(managedStringFromAny(input.Target["team_id"]))
	if targetUserID == "" {
		targetUserID = fallbackRun.UserID
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	reportID, err := fallbackUpsertReportContent(ctx, tx, reportType, date, weekStart, weekEnd, targetUserID, targetTeamID, fallbackRun.UserID, content, run.ID, fallbackRun.AgentID, nullableManagedInt(task.AgentVersionID), strings.TrimSpace(task.ModelID))
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return reportID, nil
}

func fallbackUpsertReportContent(ctx context.Context, tx *sql.Tx, reportType, date, weekStart, weekEnd, targetUserID, targetTeamID, leaderID, content, runID, agentID string, agentVersionID any, modelID string) (string, error) {
	switch reportType {
	case "personal_daily":
		if targetUserID == "" || date == "" {
			return "", fmt.Errorf("missing personal_daily target user or date")
		}
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO daily_reports (user_id, report_date, content, edited, generation_mode, managed_agent_run_id, agent_id, agent_version_id, model_id, status, saved_at)
			VALUES ($1, $2, $3, false, 'managed_agent', $4, $5, $6, $7, 'saved', now())
			ON CONFLICT (user_id, report_date) DO UPDATE
			SET content = EXCLUDED.content,
			    edited = false,
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    agent_version_id = EXCLUDED.agent_version_id,
			    model_id = EXCLUDED.model_id,
			    status = 'saved',
			    saved_at = now(),
			    updated_at = now()
			WHERE daily_reports.edited = false
			RETURNING id::text`, targetUserID, date, content, runID, nullableManagedString(agentID), agentVersionID, nullableManagedString(modelID)).Scan(&reportID)
		return reportID, err
	case "personal_weekly":
		if targetUserID == "" || weekStart == "" || weekEnd == "" {
			return "", fmt.Errorf("missing personal_weekly target user or week range")
		}
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO personal_weekly_reports (user_id, week_start, week_end, content, status, generation_mode, managed_agent_run_id, agent_id, agent_version_id, model_id, edited, saved_at)
			VALUES ($1, $2, $3, $4, 'saved', 'managed_agent', $5, $6, $7, $8, false, now())
			ON CONFLICT (user_id, week_start) DO UPDATE
			SET content = EXCLUDED.content,
			    week_end = EXCLUDED.week_end,
			    status = 'saved',
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    agent_version_id = EXCLUDED.agent_version_id,
			    model_id = EXCLUDED.model_id,
			    edited = false,
			    saved_at = now(),
			    updated_at = now()
			WHERE personal_weekly_reports.edited = false
			RETURNING id::text`, targetUserID, weekStart, weekEnd, content, runID, nullableManagedString(agentID), agentVersionID, nullableManagedString(modelID)).Scan(&reportID)
		return reportID, err
	case "team_daily":
		if targetTeamID == "" || leaderID == "" || date == "" {
			return "", fmt.Errorf("missing team_daily target team, leader, or date")
		}
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO team_reports (team_id, leader_id, report_date, content, generation_mode, managed_agent_run_id, agent_id, agent_version_id, model_id, edited, status, saved_at)
			VALUES ($1, $2, $3, $4, 'managed_agent', $5, $6, $7, $8, false, 'saved', now())
			ON CONFLICT (team_id, report_date) DO UPDATE
			SET content = EXCLUDED.content,
			    leader_id = EXCLUDED.leader_id,
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    agent_version_id = EXCLUDED.agent_version_id,
			    model_id = EXCLUDED.model_id,
			    edited = false,
			    status = 'saved',
			    saved_at = now(),
			    updated_at = now()
			WHERE team_reports.edited = false
			RETURNING id::text`, targetTeamID, leaderID, date, content, runID, nullableManagedString(agentID), agentVersionID, nullableManagedString(modelID)).Scan(&reportID)
		return reportID, err
	case "team_weekly":
		if targetTeamID == "" || leaderID == "" || weekStart == "" || weekEnd == "" {
			return "", fmt.Errorf("missing team_weekly target team, leader, or week range")
		}
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO team_weekly_reports (team_id, leader_id, week_start, week_end, content, generation_mode, managed_agent_run_id, agent_id, agent_version_id, model_id, edited)
			VALUES ($1, $2, $3, $4, $5, 'managed_agent', $6, $7, $8, $9, false)
			ON CONFLICT (team_id, week_start) DO UPDATE
			SET content = EXCLUDED.content,
			    leader_id = EXCLUDED.leader_id,
			    week_end = EXCLUDED.week_end,
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    agent_version_id = EXCLUDED.agent_version_id,
			    model_id = EXCLUDED.model_id,
			    edited = false,
			    updated_at = now()
			WHERE team_weekly_reports.edited = false
			RETURNING id::text`, targetTeamID, leaderID, weekStart, weekEnd, content, runID, nullableManagedString(agentID), agentVersionID, nullableManagedString(modelID)).Scan(&reportID)
		return reportID, err
	case "department_daily":
		if date == "" {
			return "", fmt.Errorf("missing department_daily date")
		}
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO department_reports (report_date, content, generation_mode, managed_agent_run_id, agent_id, agent_version_id, model_id, edited, status, saved_at)
			VALUES ($1, $2, 'managed_agent', $3, $4, $5, $6, false, 'saved', now())
			ON CONFLICT (report_date) DO UPDATE
			SET content = EXCLUDED.content,
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    agent_version_id = EXCLUDED.agent_version_id,
			    model_id = EXCLUDED.model_id,
			    edited = false,
			    status = 'saved',
			    saved_at = now(),
			    updated_at = now()
			WHERE department_reports.edited = false
			RETURNING id::text`, date, content, runID, nullableManagedString(agentID), agentVersionID, nullableManagedString(modelID)).Scan(&reportID)
		return reportID, err
	case "department_weekly":
		if weekStart == "" || weekEnd == "" {
			return "", fmt.Errorf("missing department_weekly week range")
		}
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO department_weekly_reports (week_start, week_end, content, generation_mode, managed_agent_run_id, agent_id, agent_version_id, model_id, edited)
			VALUES ($1, $2, $3, 'managed_agent', $4, $5, $6, $7, false)
			ON CONFLICT (week_start) DO UPDATE
			SET content = EXCLUDED.content,
			    week_end = EXCLUDED.week_end,
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    agent_version_id = EXCLUDED.agent_version_id,
			    model_id = EXCLUDED.model_id,
			    edited = false,
			    updated_at = now()
			WHERE department_weekly_reports.edited = false
			RETURNING id::text`, weekStart, weekEnd, content, runID, nullableManagedString(agentID), agentVersionID, nullableManagedString(modelID)).Scan(&reportID)
		return reportID, err
	default:
		return "", fmt.Errorf("unsupported report_type: %s", reportType)
	}
}

func NormalizeManagedRunStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "complete", "done", "success", "succeeded":
		return "succeeded"
	case "failed", "error", "cancelled", "canceled":
		return "failed"
	case "timeout", "timed_out":
		return "timeout"
	case "running", "in_progress", "processing", "queued", "submitted", "pending", "created", "active":
		return "running"
	default:
		return "pending"
	}
}

func IsTerminalManagedRunStatus(status string) bool {
	return status == "succeeded" || status == "failed" || status == "timeout"
}

func nullableManagedInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullableManagedString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
