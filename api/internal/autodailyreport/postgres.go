package autodailyreport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/lib/pq"
)

var errStateLeaseLost = errors.New("auto daily report state lease was lost")

type postgresRepository struct {
	db *sql.DB
}

func newPostgresRepository(database *sql.DB) *postgresRepository {
	return &postgresRepository{db: database}
}

func (r *postgresRepository) GetConfig(ctx context.Context) (Config, error) {
	var config Config
	var enabledSince, updatedAt sql.NullTime
	var updatedBy sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT enabled, enabled_since, updated_by::text, updated_at
		FROM auto_daily_report_config WHERE id = 1`,
	).Scan(&config.Enabled, &enabledSince, &updatedBy, &updatedAt)
	if err != nil {
		return Config{}, err
	}
	if enabledSince.Valid {
		value := enabledSince.Time
		config.EnabledSince = &value
	}
	if updatedBy.Valid {
		value := updatedBy.String
		config.UpdatedBy = &value
	}
	if updatedAt.Valid {
		config.UpdatedAt = updatedAt.Time
	}
	return config, nil
}

func (r *postgresRepository) SetEnabled(
	ctx context.Context, enabled bool, operatorID string, changedAt time.Time,
) (Config, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Config{}, err
	}
	defer tx.Rollback()
	var previousEnabled bool
	var previousEnabledSince sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT enabled, enabled_since
		FROM auto_daily_report_config WHERE id = 1 FOR UPDATE`,
	).Scan(&previousEnabled, &previousEnabledSince); err != nil {
		return Config{}, err
	}
	var enabledSince any
	if enabled {
		if previousEnabled && previousEnabledSince.Valid {
			enabledSince = previousEnabledSince.Time
		} else {
			enabledSince = changedAt
		}
	}
	var config Config
	var storedEnabledSince, updatedAt sql.NullTime
	var updatedBy sql.NullString
	if err := tx.QueryRowContext(ctx, `
		UPDATE auto_daily_report_config
		SET enabled = $1, enabled_since = $2, updated_by = NULLIF($3, '')::bigint, updated_at = $4
		WHERE id = 1
		RETURNING enabled, enabled_since, updated_by::text, updated_at`,
		enabled, enabledSince, operatorID, changedAt,
	).Scan(&config.Enabled, &storedEnabledSince, &updatedBy, &updatedAt); err != nil {
		return Config{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO auto_daily_report_config_events (
			enabled, enabled_since, changed_by, changed_at
		) VALUES ($1, $2, NULLIF($3, '')::bigint, $4)`,
		enabled, enabledSince, operatorID, changedAt,
	); err != nil {
		return Config{}, err
	}
	if !enabled {
		if _, err := tx.ExecContext(ctx, `
			UPDATE auto_daily_report_states
			SET status = 'suppressed', due_at = NULL, last_error = NULL, updated_at = $1
			WHERE active_run_id IS NULL AND status IN ('pending', 'failed')`, changedAt); err != nil {
			return Config{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Config{}, err
	}
	if storedEnabledSince.Valid {
		value := storedEnabledSince.Time
		config.EnabledSince = &value
	}
	if updatedBy.Valid {
		value := updatedBy.String
		config.UpdatedBy = &value
	}
	if updatedAt.Valid {
		config.UpdatedAt = updatedAt.Time
	}
	return config, nil
}

func (r *postgresRepository) SuppressPending(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE auto_daily_report_states state
		SET status = 'running', active_run_id = run.id,
			active_source_fingerprint = state.claimed_source_fingerprint,
			claimed_source_fingerprint = NULL, claimed_source_slice_keys = NULL,
			lease_owner = NULL, lease_until = NULL, last_error = NULL, updated_at = now()
		FROM ai_runs run
		WHERE state.status = 'submitting'
			AND state.claimed_source_fingerprint IS NOT NULL
			AND run.user_id = state.user_id
			AND run.business_type = 'report_agent_run'
			AND run.idempotency_key = 'auto-daily:' || state.report_date::text || ':' || state.claimed_source_fingerprint`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE auto_daily_report_states
		SET status = 'suppressed', due_at = NULL,
			claimed_source_fingerprint = NULL, claimed_source_slice_keys = NULL,
			lease_owner = NULL, lease_until = NULL, last_error = NULL, updated_at = now()
		WHERE active_run_id IS NULL AND (
			status IN ('pending', 'failed')
			OR (status = 'submitting' AND lease_until <= now())
		)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *postgresRepository) DiscoverSourceSnapshots(
	ctx context.Context, reportDate string, enabledSince time.Time,
) ([]SourceSnapshot, error) {
	dayStart, dayEnd, err := biztime.DateBounds(reportDate, reportDate)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT catalog.user_id::text,
			encode(digest(string_agg(
				jsonb_build_array(
					catalog.slice_id::text, s.id::text, catalog.source_id::text,
					s.session_ref, s.agent_type, catalog.generation_id::text,
					catalog.content_projection_revision_id::text, catalog.content_epoch,
					catalog.start_cursor, catalog.end_cursor
				)::text,
				E'\n' ORDER BY catalog.slice_id, catalog.content_projection_revision_id
			), 'sha256'), 'hex') AS source_fingerprint,
			array_agg(catalog.slice_id::text ORDER BY catalog.slice_id),
			max(catalog.ready_at)
		FROM report_source_slice_catalog catalog
		JOIN sessions s
			ON s.id = catalog.session_id AND s.user_id = catalog.user_id
		JOIN session_sources source
			ON source.id = catalog.source_id AND source.session_id = s.id
			AND source.active_generation_id = catalog.generation_id
			AND source.active_content_projection_revision_id = catalog.content_projection_revision_id
		JOIN session_source_generations generation
			ON generation.id = catalog.generation_id AND generation.status = 'active'
		JOIN session_content_projection_revisions revision
			ON revision.id = catalog.content_projection_revision_id
			AND revision.generation_id = generation.id AND revision.status = 'active'
		WHERE catalog.status = 'ready'
			AND s.content_status = 'available'
			AND s.content_epoch = catalog.content_epoch
			AND catalog.activity_end_at >= $1
			AND catalog.activity_start_at < $2
		GROUP BY catalog.user_id
		HAVING max(catalog.ready_at) >= $3
		ORDER BY catalog.user_id`, dayStart, dayEnd, enabledSince)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	snapshots := []SourceSnapshot{}
	for rows.Next() {
		var snapshot SourceSnapshot
		var keys pq.StringArray
		if err := rows.Scan(
			&snapshot.UserID, &snapshot.Fingerprint, &keys, &snapshot.LatestReadyAt,
		); err != nil {
			return nil, err
		}
		snapshot.ReportDate = reportDate
		snapshot.SourceSliceKeys = append([]string(nil), keys...)
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, rows.Err()
}

func (r *postgresRepository) ObserveSourceSnapshot(
	ctx context.Context, snapshot SourceSnapshot, quietPeriod time.Duration,
) error {
	if len(snapshot.SourceSliceKeys) == 0 {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO auto_daily_report_states (
			user_id, report_date, desired_source_fingerprint, desired_source_slice_keys,
			last_source_ready_at, due_at, status
		) VALUES (
			$1, $2, $3, $4,
			$5::timestamptz,
			$5::timestamptz + make_interval(secs => $6),
			'pending'
		)
		ON CONFLICT (user_id, report_date) DO UPDATE
		SET desired_source_fingerprint = EXCLUDED.desired_source_fingerprint,
			desired_source_slice_keys = EXCLUDED.desired_source_slice_keys,
			last_source_ready_at = EXCLUDED.last_source_ready_at,
			due_at = EXCLUDED.due_at,
			status = CASE
				WHEN auto_daily_report_states.status IN ('submitting', 'running')
					THEN auto_daily_report_states.status
				ELSE 'pending'
			END,
			last_error = NULL,
			updated_at = now()
		WHERE auto_daily_report_states.desired_source_fingerprint IS DISTINCT FROM EXCLUDED.desired_source_fingerprint`,
		snapshot.UserID, snapshot.ReportDate, snapshot.Fingerprint,
		pq.Array(snapshot.SourceSliceKeys), snapshot.LatestReadyAt, int(quietPeriod.Seconds()),
	)
	return err
}

func (r *postgresRepository) ReconcileRuns(
	ctx context.Context, enabled bool, now time.Time, quietPeriod time.Duration,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE auto_daily_report_states state
		SET status = CASE
				WHEN NOT $1 THEN 'suppressed'
				WHEN state.desired_source_fingerprint IS DISTINCT FROM state.active_source_fingerprint THEN 'pending'
				WHEN run.status = 'succeeded' THEN 'idle'
				ELSE 'failed'
			END,
			due_at = CASE
				WHEN $1 AND state.desired_source_fingerprint IS DISTINCT FROM state.active_source_fingerprint
					THEN GREATEST(state.last_source_ready_at + make_interval(secs => $3), $2)
				ELSE NULL
			END,
			last_completed_source_fingerprint = CASE
				WHEN run.status = 'succeeded' THEN state.active_source_fingerprint
				ELSE state.last_completed_source_fingerprint
			END,
			active_run_id = NULL,
			active_source_fingerprint = NULL,
			last_error = CASE
				WHEN run.status = 'succeeded' THEN NULL
				ELSE COALESCE(run.error_message, 'automatic report run ' || run.status)
			END,
			updated_at = $2
		FROM ai_runs run
		WHERE state.active_run_id = run.id
			AND run.status IN ('succeeded', 'failed', 'timeout')`,
		enabled, now, int(quietPeriod.Seconds()))
	return err
}

func (r *postgresRepository) ClaimDue(
	ctx context.Context, now time.Time, workerID string, leaseTTL time.Duration,
) (claimedJob, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return claimedJob{}, false, err
	}
	defer tx.Rollback()
	var job claimedJob
	var status, desiredFingerprint string
	var desiredKeys pq.StringArray
	var claimedFingerprint sql.NullString
	var claimedKeys pq.StringArray
	err = tx.QueryRowContext(ctx, `
		SELECT state.user_id::text, state.report_date::text, state.status,
			state.desired_source_fingerprint, state.desired_source_slice_keys,
			state.claimed_source_fingerprint, state.claimed_source_slice_keys
		FROM auto_daily_report_states state
		WHERE EXISTS (
			SELECT 1 FROM auto_daily_report_config config WHERE config.id = 1 AND config.enabled = true
		) AND (
			(state.status = 'pending' AND state.due_at IS NOT NULL AND state.due_at <= $1)
			OR (state.status = 'submitting' AND state.lease_until IS NOT NULL AND state.lease_until <= $1)
		)
		ORDER BY state.due_at NULLS FIRST, state.report_date, state.user_id
		FOR UPDATE SKIP LOCKED
		LIMIT 1`, now,
	).Scan(
		&job.UserID, &job.ReportDate, &status, &desiredFingerprint, &desiredKeys,
		&claimedFingerprint, &claimedKeys,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return claimedJob{}, false, nil
	}
	if err != nil {
		return claimedJob{}, false, err
	}
	job.SourceFingerprint = desiredFingerprint
	job.SourceSliceKeys = append([]string(nil), desiredKeys...)
	if status == "submitting" && claimedFingerprint.Valid && len(claimedKeys) > 0 {
		job.SourceFingerprint = claimedFingerprint.String
		job.SourceSliceKeys = append([]string(nil), claimedKeys...)
	}
	job.LeaseOwner = workerID
	result, err := tx.ExecContext(ctx, `
		UPDATE auto_daily_report_states
		SET status = 'submitting', claimed_source_fingerprint = $3,
			claimed_source_slice_keys = $4, lease_owner = $5, lease_until = $6, updated_at = $1
		WHERE user_id = $2 AND report_date = $7`,
		now, job.UserID, job.SourceFingerprint, pq.Array(job.SourceSliceKeys),
		workerID, now.Add(leaseTTL), job.ReportDate,
	)
	if err != nil {
		return claimedJob{}, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return claimedJob{}, false, errStateLeaseLost
	}
	if err := tx.Commit(); err != nil {
		return claimedJob{}, false, err
	}
	return job, true, nil
}

type existingDailyReport struct {
	ID             string
	Edited         bool
	Status         string
	GenerationMode string
	TriggerSource  string
	HasUserOutcome bool
	UpdatedAt      time.Time
}

func reportEligibilityFor(existing *existingDailyReport) reportEligibility {
	if existing == nil {
		return reportEligibility{Allowed: true, Guard: ReportGuard{Mode: GuardModeAbsent}}
	}
	if existing.GenerationMode == "managed_agent" && !existing.Edited && !existing.HasUserOutcome && existing.Status == "saved" &&
		existing.TriggerSource == TriggerSource {
		updatedAt := existing.UpdatedAt
		return reportEligibility{Allowed: true, Guard: ReportGuard{
			Mode: GuardModeReplace, ReportID: existing.ID, UpdatedAt: &updatedAt,
		}}
	}
	return reportEligibility{Reason: "daily report is user-owned, edited, submitted, or generated by another trigger"}
}

func (r *postgresRepository) LoadReportEligibility(
	ctx context.Context, job claimedJob,
) (reportEligibility, error) {
	var existing existingDailyReport
	err := r.db.QueryRowContext(ctx, `
		SELECT report.id::text, report.edited, COALESCE(report.status, ''),
			COALESCE(report.generation_mode, ''),
			COALESCE(run.input_ref_json->>'trigger_source', ''),
			EXISTS (
				SELECT 1 FROM report_user_outcome_events outcome
				WHERE outcome.report_id = report.id
			), report.updated_at
		FROM daily_reports report
		LEFT JOIN ai_runs run ON run.id = report.managed_agent_run_id
		WHERE report.user_id = $1 AND report.report_date = $2`,
		job.UserID, job.ReportDate,
	).Scan(
		&existing.ID, &existing.Edited, &existing.Status, &existing.GenerationMode,
		&existing.TriggerSource, &existing.HasUserOutcome, &existing.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return reportEligibilityFor(nil), nil
	}
	if err != nil {
		return reportEligibility{}, err
	}
	return reportEligibilityFor(&existing), nil
}

func (r *postgresRepository) MarkRunning(ctx context.Context, job claimedJob, runID string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE auto_daily_report_states
		SET status = 'running', active_source_fingerprint = claimed_source_fingerprint,
			active_run_id = $1, claimed_source_fingerprint = NULL,
			claimed_source_slice_keys = NULL, lease_owner = NULL, lease_until = NULL,
			last_error = NULL, updated_at = now()
		WHERE user_id = $2 AND report_date = $3 AND status = 'submitting'
			AND lease_owner = $4 AND claimed_source_fingerprint = $5`,
		runID, job.UserID, job.ReportDate, job.LeaseOwner, job.SourceFingerprint,
	)
	return requireOneChanged(result, err)
}

func (r *postgresRepository) MarkBlocked(ctx context.Context, job claimedJob, reason string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE auto_daily_report_states
		SET status = 'blocked', due_at = NULL, claimed_source_fingerprint = NULL,
			claimed_source_slice_keys = NULL, lease_owner = NULL, lease_until = NULL,
			last_error = $1, updated_at = now()
		WHERE user_id = $2 AND report_date = $3 AND status = 'submitting'
			AND lease_owner = $4 AND claimed_source_fingerprint = $5`,
		reason, job.UserID, job.ReportDate, job.LeaseOwner, job.SourceFingerprint,
	)
	return requireOneChanged(result, err)
}

func (r *postgresRepository) MarkSubmissionFailed(
	ctx context.Context, job claimedJob, message string, now time.Time, quietPeriod time.Duration,
) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE auto_daily_report_states state
		SET status = CASE
				WHEN NOT config.enabled THEN 'suppressed'
				WHEN state.desired_source_fingerprint IS DISTINCT FROM state.claimed_source_fingerprint THEN 'pending'
				ELSE 'failed'
			END,
			due_at = CASE
				WHEN config.enabled AND state.desired_source_fingerprint IS DISTINCT FROM state.claimed_source_fingerprint
					THEN GREATEST(state.last_source_ready_at + make_interval(secs => $1), $2)
				ELSE NULL
			END,
			claimed_source_fingerprint = NULL, claimed_source_slice_keys = NULL,
			lease_owner = NULL, lease_until = NULL, last_error = $3, updated_at = $2
		FROM auto_daily_report_config config
		WHERE config.id = 1 AND state.user_id = $4 AND state.report_date = $5
			AND state.status = 'submitting' AND state.lease_owner = $6
			AND state.claimed_source_fingerprint = $7`,
		int(quietPeriod.Seconds()), now, strings.TrimSpace(message),
		job.UserID, job.ReportDate, job.LeaseOwner, job.SourceFingerprint,
	)
	return requireOneChanged(result, err)
}

func requireOneChanged(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%w: changed %d rows", errStateLeaseLost, changed)
	}
	return nil
}
