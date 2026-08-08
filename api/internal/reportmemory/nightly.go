package reportmemory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/biztime"
)

const (
	defaultMemoryPollInterval = time.Minute
	defaultMemoryLeaseTTL     = 3 * time.Minute
	defaultMemoryClaimBatch   = 3
	maxMemoryAttempts         = 2
)

type NightlyService struct {
	db       *sql.DB
	resolver Resolver
	config   NightlyConfig
}

func NewNightlyService(database *sql.DB, resolver Resolver, config NightlyConfig) (*NightlyService, error) {
	if !config.Enabled {
		return &NightlyService{db: database, resolver: resolver, config: normalizedNightlyConfig(config)}, nil
	}
	if database == nil || resolver == nil || strings.TrimSpace(config.AgentID) == "" || strings.TrimSpace(config.WorkerID) == "" {
		return nil, errors.New("enabled project memory service requires database, resolver, agent ID, and worker ID")
	}
	return &NightlyService{db: database, resolver: resolver, config: normalizedNightlyConfig(config)}, nil
}

func normalizedNightlyConfig(config NightlyConfig) NightlyConfig {
	config.AgentID = strings.TrimSpace(config.AgentID)
	config.ModelID = strings.TrimSpace(config.ModelID)
	config.WorkerID = strings.TrimSpace(config.WorkerID)
	if config.PollInterval <= 0 {
		config.PollInterval = defaultMemoryPollInterval
	}
	if config.LeaseTTL <= 0 {
		config.LeaseTTL = defaultMemoryLeaseTTL
	}
	if config.ClaimBatch <= 0 || config.ClaimBatch > 5 {
		config.ClaimBatch = defaultMemoryClaimBatch
	}
	if config.StartHour < 0 || config.StartHour > 23 {
		config.StartHour = 2
	}
	if config.EndHour <= config.StartHour || config.EndHour > 24 {
		config.EndHour = 6
	}
	return config
}

func (service *NightlyService) Start(ctx context.Context) {
	if service == nil || !service.config.Enabled {
		return
	}
	go func() {
		run := func(now time.Time) {
			if err := service.RunOnce(ctx, now); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("project memory nightly worker failed: %v", err)
			}
		}
		run(time.Now())
		ticker := time.NewTicker(service.config.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				run(now)
			}
		}
	}()
}

func (service *NightlyService) RunOnce(ctx context.Context, now time.Time) error {
	if service == nil || !service.config.Enabled {
		return nil
	}
	for refreshed := 0; refreshed < service.config.ClaimBatch; refreshed++ {
		job, found, err := service.claimRunning(ctx, now)
		if err != nil {
			return fmt.Errorf("claim running project memory job: %w", err)
		}
		if !found {
			break
		}
		if err := service.refreshRunning(ctx, job, now); err != nil {
			log.Printf("project memory task %s refresh failed: %v", job.ExternalTaskID, err)
		}
	}
	if !service.inSubmissionWindow(now) {
		return nil
	}
	for submitted := 0; submitted < service.config.ClaimBatch; submitted++ {
		job, found, err := service.claimPending(ctx, now)
		if err != nil {
			return fmt.Errorf("claim pending project memory job: %w", err)
		}
		if !found {
			break
		}
		if err := service.submit(ctx, job, now); err != nil {
			log.Printf("project memory job %s/%s submit failed: %v", job.UserID, job.ReportDate, err)
			if markErr := service.failJob(ctx, job, now, err.Error()); markErr != nil {
				return fmt.Errorf("mark project memory submission failed: %w", markErr)
			}
		}
	}
	return nil
}

func (service *NightlyService) inSubmissionWindow(now time.Time) bool {
	hour := biztime.InLocation(now).Hour()
	return hour >= service.config.StartHour && hour < service.config.EndHour
}

func (service *NightlyService) claimPending(ctx context.Context, now time.Time) (queuedJob, bool, error) {
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return queuedJob{}, false, err
	}
	defer tx.Rollback()
	var job queuedJob
	err = tx.QueryRowContext(ctx, `
		SELECT user_id::text, report_id::text, report_date::text, dirty_from_date::text,
		       desired_source_fingerprint, desired_evidence_watermark, rebuild_required, attempts
		FROM report_project_memory_jobs
		WHERE (
			status IN ('pending', 'failed')
			OR (status = 'submitting' AND (lease_until IS NULL OR lease_until <= $1))
		) AND due_at <= $1 AND attempts < $2
		ORDER BY due_at, report_date, user_id
		FOR UPDATE SKIP LOCKED LIMIT 1`, now, maxMemoryAttempts).Scan(
		&job.UserID, &job.ReportID, &job.ReportDate, &job.DirtyFromDate, &job.DesiredSourceFingerprint,
		&job.DesiredEvidenceWatermark, &job.RebuildRequired, &job.Attempts,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return queuedJob{}, false, nil
	}
	if err != nil {
		return queuedJob{}, false, err
	}
	if job.RebuildRequired {
		if _, err := tx.ExecContext(ctx, `UPDATE report_project_memory_jobs SET snapshot_id = NULL WHERE user_id = $1`, job.UserID); err != nil {
			return queuedJob{}, false, err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM report_project_memory_snapshots
			WHERE user_id = $1 AND resolver_version = $2`, job.UserID, ResolverVersion); err != nil {
			return queuedJob{}, false, err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM report_projects
			WHERE user_id = $1 AND memory_schema_version = 'project-memory/v2'`, job.UserID); err != nil {
			return queuedJob{}, false, err
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT min(report_date)::text FROM (
				SELECT report_date
				FROM daily_reports
				WHERE user_id = $1 AND report_date <= $2::date
				  AND status IN ('saved', 'submitted')
				  AND NULLIF(BTRIM(COALESCE(NULLIF(submitted_content, ''), content, '')), '') IS NOT NULL
				ORDER BY report_date DESC, updated_at DESC
				LIMIT $3
			) recent`, job.UserID, job.ReportDate, maxMemorySnapshotDepth).Scan(&job.DirtyFromDate); err != nil {
			return queuedJob{}, false, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE report_project_memory_jobs
			SET dirty_from_date = $2::date, rebuild_required = false
			WHERE user_id = $1`, job.UserID, job.DirtyFromDate); err != nil {
			return queuedJob{}, false, err
		}
		job.RebuildRequired = false
	}
	job.ClaimedSourceFingerprint = job.DesiredSourceFingerprint
	job.ClaimedEvidenceWatermark = job.DesiredEvidenceWatermark
	job.Attempts++
	job.StartedAt = now
	result, err := tx.ExecContext(ctx, `
		UPDATE report_project_memory_jobs SET
			status = 'submitting', claimed_source_fingerprint = desired_source_fingerprint,
			claimed_evidence_watermark = desired_evidence_watermark,
			attempts = attempts + 1, lease_owner = $3, lease_until = $4,
			started_at = $1, finished_at = NULL, last_error = NULL, updated_at = now()
		WHERE user_id = $2`, now, job.UserID,
		service.config.WorkerID, now.Add(service.config.LeaseTTL))
	if err != nil {
		return queuedJob{}, false, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return queuedJob{}, false, errors.New("project memory job lease was lost")
	}
	return job, true, tx.Commit()
}

func (service *NightlyService) submit(ctx context.Context, job queuedJob, now time.Time) error {
	input, payload, estimate, err := buildConsolidationInput(ctx, service.db, job)
	if err != nil {
		return err
	}
	if len(input.CurrentThemes) == 0 {
		return errors.New("project memory input has no current themes")
	}
	prepared, err := service.db.ExecContext(ctx, `
		UPDATE report_project_memory_jobs SET
			input_json = $1::jsonb, input_token_estimate = $2,
			proposal_json = NULL, resolver_version = $3, model_id = NULLIF($4, ''),
			updated_at = now()
		WHERE user_id = $5 AND status = 'submitting'
		  AND claimed_source_fingerprint = $6`, string(payload), estimate,
		ResolverVersion, service.config.ModelID, job.UserID, job.ClaimedSourceFingerprint)
	if err != nil {
		return err
	}
	if changed, _ := prepared.RowsAffected(); changed != 1 {
		return errors.New("project memory job changed before submit")
	}
	submission, err := service.resolver.Submit(ctx, ResolverRequest{
		AgentID: service.config.AgentID, ModelID: service.config.ModelID,
		UserID: job.UserID, JobRef: JobRef(job.ReportDate, job.ClaimedSourceFingerprint),
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(submission.TaskID) == "" {
		return errors.New("memory resolver returned an empty task ID")
	}
	result, err := service.db.ExecContext(ctx, `
		UPDATE report_project_memory_jobs SET
			status = 'running', external_task_id = $1,
			lease_owner = NULL, lease_until = NULL, updated_at = now()
		WHERE user_id = $2 AND status = 'submitting'
		  AND claimed_source_fingerprint = $3`, submission.TaskID, job.UserID,
		job.ClaimedSourceFingerprint)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("project memory job changed during submit")
	}
	return nil
}

func (service *NightlyService) claimRunning(ctx context.Context, now time.Time) (queuedJob, bool, error) {
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return queuedJob{}, false, err
	}
	defer tx.Rollback()
	var job queuedJob
	var input []byte
	var started sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT user_id::text, report_id::text, report_date::text, dirty_from_date::text,
		       desired_source_fingerprint, COALESCE(claimed_source_fingerprint, ''),
		       desired_evidence_watermark, COALESCE(claimed_evidence_watermark, 0),
		       external_task_id, attempts, input_json, COALESCE(proposal_json, 'null'::jsonb), started_at
		FROM report_project_memory_jobs
		WHERE status = 'running' AND (lease_until IS NULL OR lease_until <= $1)
		ORDER BY updated_at, user_id
		FOR UPDATE SKIP LOCKED LIMIT 1`, now).Scan(
		&job.UserID, &job.ReportID, &job.ReportDate, &job.DirtyFromDate, &job.DesiredSourceFingerprint,
		&job.ClaimedSourceFingerprint, &job.DesiredEvidenceWatermark, &job.ClaimedEvidenceWatermark,
		&job.ExternalTaskID, &job.Attempts, &input, &job.ProposalJSON, &started,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return queuedJob{}, false, nil
	}
	if err != nil {
		return queuedJob{}, false, err
	}
	job.InputJSON = append(json.RawMessage(nil), input...)
	if started.Valid {
		job.StartedAt = started.Time
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE report_project_memory_jobs SET lease_owner = $1, lease_until = $2, updated_at = now()
		WHERE user_id = $3 AND status = 'running'`,
		service.config.WorkerID, now.Add(service.config.LeaseTTL), job.UserID); err != nil {
		return queuedJob{}, false, err
	}
	return job, true, tx.Commit()
}

func (service *NightlyService) refreshRunning(ctx context.Context, job queuedJob, now time.Time) error {
	task, err := service.resolver.Status(ctx, job.ExternalTaskID)
	if err != nil {
		return service.releaseRunningLease(ctx, job, err.Error())
	}
	switch normalizeResolverStatus(task.Status) {
	case "running":
		return service.releaseRunningLease(ctx, job, "")
	case "failed":
		message := strings.TrimSpace(task.Error)
		if message == "" {
			message = "memory resolver failed"
		}
		return service.failJob(ctx, job, now, message)
	case "succeeded":
		var input ConsolidationInput
		if err := json.Unmarshal(job.InputJSON, &input); err != nil {
			return service.failJob(ctx, job, now, "stored memory input is invalid")
		}
		rawProposal := strings.TrimSpace(string(job.ProposalJSON))
		if rawProposal == "" || rawProposal == "null" {
			// Transitional fallback for tasks submitted before dedicated MCP writeback.
			rawProposal = task.Result
		}
		proposal, proposalPayload, outputEstimate, err := parseAndValidateProposal(rawProposal, input)
		if err != nil {
			return service.failJob(ctx, job, now, err.Error())
		}
		started := job.StartedAt
		if !task.StartedAt.IsZero() {
			started = task.StartedAt
		}
		finished := now
		if !task.EndedAt.IsZero() {
			finished = task.EndedAt
		}
		_, err = applyProposal(ctx, service.db, job, input, proposal, job.InputJSON, proposalPayload,
			estimateTokens(string(job.InputJSON)), outputEstimate, service.config.ModelID,
			job.ExternalTaskID, started, finished)
		return err
	default:
		return service.releaseRunningLease(ctx, job, "unknown resolver status")
	}
}

func normalizeResolverStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "complete", "done", "success", "succeeded":
		return "succeeded"
	case "failed", "error", "cancelled", "canceled", "timeout", "timed_out":
		return "failed"
	default:
		return "running"
	}
}

func (service *NightlyService) releaseRunningLease(ctx context.Context, job queuedJob, message string) error {
	_, err := service.db.ExecContext(ctx, `
		UPDATE report_project_memory_jobs SET
			lease_owner = NULL, lease_until = NULL,
			last_error = NULLIF($1, ''), updated_at = now()
		WHERE user_id = $2 AND status = 'running'
		  AND external_task_id = $3`, limitRunes(message, 1000), job.UserID, job.ExternalTaskID)
	return err
}

func (service *NightlyService) failJob(ctx context.Context, job queuedJob, now time.Time, message string) error {
	status := "failed"
	if job.Attempts < maxMemoryAttempts {
		status = "pending"
	}
	_, err := service.db.ExecContext(ctx, `
		UPDATE report_project_memory_jobs SET
			status = $1, due_at = $2, claimed_source_fingerprint = NULL,
			claimed_evidence_watermark = NULL,
			external_task_id = NULL, lease_owner = NULL, lease_until = NULL,
			last_error = $3, finished_at = $4, updated_at = now()
		WHERE user_id = $5
		  AND status IN ('submitting', 'running')`, status, nextNightlyWindow(now),
		limitRunes(message, 1000), now, job.UserID)
	return err
}
