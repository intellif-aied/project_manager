package reportrun

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"github.com/aidashboard/api/internal/observability"
	"github.com/aidashboard/api/internal/sessiondigestv2"
)

const (
	reconcileInterval = 30 * time.Second
	reconcileBatch    = 100
)

type Reconciler struct {
	db       *sql.DB
	interval time.Duration
}

func NewReconciler(database *sql.DB) (*Reconciler, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &Reconciler{db: database, interval: reconcileInterval}, nil
}

func (r *Reconciler) Start(ctx context.Context) {
	go func() {
		if _, err := r.RunOnce(ctx, time.Now().UTC()); err != nil {
			log.Printf("report run reconciler failed: %v", err)
		}
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if _, err := r.RunOnce(ctx, now.UTC()); err != nil {
					log.Printf("report run reconciler failed: %v", err)
				}
			}
		}
	}()
}

func (r *Reconciler) RunOnce(ctx context.Context, now time.Time) (int64, error) {
	var changed int64
	statements := []struct {
		query      string
		args       []any
		reason     string
		leaseStage string
	}{
		{query: `
			WITH candidates AS (
				SELECT id FROM ai_runs
				WHERE business_type = 'report_agent_run' AND status = 'pending'
					AND execution_stage = 'waiting_digest'
					AND digest_wait_deadline_at <= $1
					AND (execution_lease_until IS NULL OR execution_lease_until <= $1)
				ORDER BY digest_wait_deadline_at, id LIMIT $2
				FOR UPDATE SKIP LOCKED
			)
			UPDATE ai_runs r
			SET status = 'timeout', execution_stage = 'completed', stage_updated_at = $1,
				failure_stage = 'waiting_digest', error_code = 'DIGEST_WAIT_TIMEOUT',
				error_message = 'report source preparation timed out', finished_at = $1,
				next_attempt_at = NULL, execution_lease_owner = NULL, execution_lease_until = NULL
			FROM candidates c WHERE r.id = c.id`, args: []any{now, reconcileBatch}, reason: "deadline"},
		{query: `
			WITH candidates AS (
				SELECT r.id
				FROM ai_runs r
				JOIN report_source_selections sel ON sel.attached_run_id = r.id
				WHERE r.business_type = 'report_agent_run' AND r.status = 'pending'
					AND r.execution_stage = 'waiting_digest'
					AND (r.execution_lease_until IS NULL OR r.execution_lease_until <= $1)
					AND (
						EXISTS (
							SELECT 1 FROM report_source_selection_items i
							JOIN session_slice_digest_revisions d
							  ON d.session_content_slice_id = i.session_content_slice_id
							 AND d.generation_id = i.source_generation_id
							 AND d.content_projection_revision_id = i.content_projection_revision_id
							 AND d.content_epoch = i.content_epoch_snapshot
							 AND d.digest_version = $3 AND d.redaction_version = $4
							WHERE i.selection_id = sel.id AND d.status = 'failed'
						) OR NOT EXISTS (
							SELECT 1 FROM report_source_selection_items i
							WHERE i.selection_id = sel.id AND NOT EXISTS (
								SELECT 1 FROM session_slice_digest_revisions d
								WHERE d.session_content_slice_id = i.session_content_slice_id
								  AND d.generation_id = i.source_generation_id
								  AND d.content_projection_revision_id = i.content_projection_revision_id
								  AND d.content_epoch = i.content_epoch_snapshot
								  AND d.digest_version = $3 AND d.redaction_version = $4
								  AND d.status = 'ready'
							)
						)
					)
				ORDER BY r.stage_updated_at, r.id LIMIT $2
				FOR UPDATE OF r SKIP LOCKED
			)
			UPDATE ai_runs r SET next_attempt_at = $1
			FROM candidates c WHERE r.id = c.id`,
			args: []any{now, reconcileBatch, sessiondigestv2.Version, sessiondigestv2.RedactionVersion}, reason: "digest_terminal"},
		{query: `
			WITH candidates AS (
				SELECT id FROM ai_runs
				WHERE business_type = 'report_agent_run' AND status = 'pending'
					AND execution_stage = 'submitting_agent'
					AND external_session_id IS NULL
					AND execution_input_json ? 'external_submission_started_at'
					AND execution_lease_until <= $1
				ORDER BY execution_lease_until, id LIMIT $2
				FOR UPDATE SKIP LOCKED
			)
			UPDATE ai_runs r
			SET status = 'failed', execution_stage = 'completed', stage_updated_at = $1,
				failure_stage = 'submitting_agent', error_code = 'EXTERNAL_SUBMISSION_STATE_UNKNOWN',
				error_message = 'AIHub submission lease expired with an unknown external state',
				finished_at = $1, next_attempt_at = NULL,
				execution_lease_owner = NULL, execution_lease_until = NULL
			FROM candidates c WHERE r.id = c.id`, args: []any{now, reconcileBatch}, reason: "lease_expired", leaseStage: "submitting_agent"},
		{query: `
			WITH candidates AS (
				SELECT id FROM ai_runs
				WHERE business_type = 'report_agent_run' AND status = 'pending'
					AND execution_stage = 'building_context'
					AND execution_lease_until <= $1
				ORDER BY execution_lease_until, id LIMIT $2
				FOR UPDATE SKIP LOCKED
			)
			UPDATE ai_runs r
			SET next_attempt_at = $1, execution_lease_owner = NULL, execution_lease_until = NULL
			FROM candidates c WHERE r.id = c.id`, args: []any{now, reconcileBatch}, reason: "lease_expired", leaseStage: "building_context"},
		{query: `
			WITH candidates AS (
				SELECT id FROM ai_runs
				WHERE business_type = 'report_agent_run' AND status = 'pending'
					AND execution_stage = 'submitting_agent'
					AND execution_lease_until <= $1
					AND external_session_id IS NULL
					AND NOT (execution_input_json ? 'external_submission_started_at')
				ORDER BY execution_lease_until, id LIMIT $2
				FOR UPDATE SKIP LOCKED
			)
			UPDATE ai_runs r
			SET next_attempt_at = $1, execution_lease_owner = NULL, execution_lease_until = NULL
			FROM candidates c WHERE r.id = c.id`, args: []any{now, reconcileBatch}, reason: "lease_expired", leaseStage: "submitting_agent"},
		{query: `
			WITH candidates AS (
				SELECT r.id, c.context_hash, c.context_bytes
				FROM ai_runs r JOIN report_run_contexts c ON c.run_id = r.id
				WHERE r.business_type = 'report_agent_run' AND r.status = 'pending'
					AND r.execution_stage = 'building_context'
					AND (r.execution_lease_until IS NULL OR r.execution_lease_until <= $1)
				ORDER BY r.stage_updated_at, r.id LIMIT $2
				FOR UPDATE OF r SKIP LOCKED
			)
			UPDATE ai_runs r
			SET input_ref_json = input_ref_json || jsonb_build_object(
					'report_context_schema_version', 'report-context/v1',
					'report_context_hash', c.context_hash,
					'report_context_bytes', c.context_bytes)
				|| CASE WHEN c.context_bytes > 1048576
					THEN jsonb_build_object('large_context_warning', true)
					ELSE '{}'::jsonb END,
				execution_stage = 'submitting_agent', stage_updated_at = $1,
				stage_attempts = 0, next_attempt_at = $1,
				execution_lease_owner = NULL, execution_lease_until = NULL
			FROM candidates c WHERE r.id = c.id`, args: []any{now, reconcileBatch}, reason: "context_persisted"},
		{query: `
			WITH candidates AS (
				SELECT id FROM ai_runs
				WHERE business_type = 'report_agent_run' AND status = 'pending'
					AND execution_stage = 'submitting_agent' AND external_session_id IS NOT NULL
					AND (execution_lease_until IS NULL OR execution_lease_until <= $1)
				ORDER BY stage_updated_at, id LIMIT $2
				FOR UPDATE SKIP LOCKED
			)
			UPDATE ai_runs r
			SET status = 'running', execution_stage = 'agent_running', stage_updated_at = $1,
				stage_attempts = 0, next_attempt_at = NULL, started_at = COALESCE(started_at, $1),
				execution_lease_owner = NULL, execution_lease_until = NULL
			FROM candidates c WHERE r.id = c.id`, args: []any{now, reconcileBatch}, reason: "session_persisted"},
	}
	for _, statement := range statements {
		result, err := r.db.ExecContext(ctx, statement.query, statement.args...)
		if err != nil {
			observability.ObserveReportReconcile(statement.reason, "failure", 1)
			return changed, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			observability.ObserveReportReconcile(statement.reason, "failure", 1)
			return changed, err
		}
		changed += rows
		if rows > 0 {
			observability.ObserveReportReconcile(statement.reason, "success", rows)
			if statement.leaseStage != "" {
				observability.ObserveReportLeaseExpired(statement.leaseStage, rows)
			}
		}
	}
	return changed, nil
}
