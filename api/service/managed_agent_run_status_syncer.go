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
	ManagedAgentInfrastructureTimeout  = 9*time.Minute + 30*time.Second
	managedReportAgentRunBusinessType  = "report_agent_run"
	reportWritebackMissingErrorMessage = "managed agent session completed without report writeback"
	reportAgentTimeoutErrorMessage     = "报告生成超时，请稍后重试"
	reportAgentFailureErrorMessage     = "报告生成失败，请稍后重试"
	managedInfrastructureFailure       = "infrastructure_failure"
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
	StartedAt         sql.NullTime
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
			       started_at
			FROM ai_runs
			WHERE status IN ('pending', 'running')
			  AND (
				external_task_id IS NOT NULL OR external_session_id IS NOT NULL
				OR business_type <> 'report_agent_run' OR execution_stage IS NULL
			  )
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
		if run.Status == "pending" && run.StartedAt.Valid &&
			!now.Before(run.StartedAt.Time.Add(ManagedAgentPendingTimeout)) {
			return s.updateRunStatus(
				ctx, run, nil, "timeout",
				"managed agent run pending submit timed out after 10m", now,
			)
		}
		if run.Status == "running" && s.isTimedOut(run, now) {
			return s.updateRunStatus(
				ctx, run, nil, "timeout", "managed agent run timed out after 2h", now,
			)
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
		if run.isReportAgentRun() {
			if strings.EqualFold(strings.TrimSpace(task.Error), managedInfrastructureFailure) {
				log.Printf("managed agent report run %s upstream failure: %s", run.ID, task.Error)
			}
			status, errMsg = normalizeManagedAgentFailure(task)
		} else {
			errMsg = task.Error
		}
	}
	if run.isReportAgentRun() && status == "succeeded" && !run.hasReportWriteback() {
		if !reportWritebackGraceElapsed(task, now) {
			status = "running"
		} else {
			status = "failed"
			errMsg = reportWritebackMissingErrorMessage
		}
	}
	if !IsTerminalManagedRunStatus(status) && s.isTimedOut(run, now) {
		status = "timeout"
		errMsg = "managed agent run timed out after 2h"
	}
	return s.updateRunStatus(ctx, run, task, status, errMsg, now)
}

func normalizeManagedAgentFailure(task *ManagedTaskStatus) (string, string) {
	if task == nil {
		return "failed", ""
	}
	errorMessage := strings.TrimSpace(task.Error)
	if !strings.EqualFold(errorMessage, managedInfrastructureFailure) {
		return "failed", errorMessage
	}
	if task.StartedAt > 0 && task.FinishedAt >= task.StartedAt &&
		time.Duration(task.FinishedAt-task.StartedAt)*time.Second >= ManagedAgentInfrastructureTimeout {
		return "timeout", reportAgentTimeoutErrorMessage
	}
	return "failed", reportAgentFailureErrorMessage
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
	return run.StartedAt.Valid && !now.Before(run.StartedAt.Time.Add(s.timeout))
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
		if run.isReportAgentRun() {
			errorCode := ""
			switch status {
			case "timeout":
				errorCode = "AGENT_EXECUTION_TIMEOUT"
			case "failed":
				errorCode = "AGENT_EXECUTION_FAILED"
				if errorMessage == reportWritebackMissingErrorMessage {
					errorCode = "REPORT_WRITEBACK_MISSING"
				}
			}
			sets = append(sets,
				"execution_stage = 'completed'",
				"stage_updated_at = now()",
				"failure_stage = CASE WHEN $1 IN ('failed', 'timeout') THEN 'agent_running' ELSE failure_stage END",
				"next_attempt_at = NULL",
				"execution_lease_owner = NULL",
				"execution_lease_until = NULL",
			)
			if errorCode != "" {
				sets = append(sets, fmt.Sprintf("error_code = $%d", argIdx))
				args = append(args, errorCode)
				argIdx++
			}
		}
	}
	args = append(args, run.ID)
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("UPDATE ai_runs SET %s WHERE id = $%d AND status IN ('pending', 'running')", strings.Join(sets, ", "), argIdx), args...)
	return err
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
