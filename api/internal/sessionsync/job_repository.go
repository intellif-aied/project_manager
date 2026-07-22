package sessionsync

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

type ProcessingJob struct {
	ID                      string
	Type                    string
	SessionID               string
	GenerationID            sql.NullString
	ChunkID                 sql.NullString
	TargetRevisionID        sql.NullString
	TargetMetricsRevisionID sql.NullString
	TargetDigestRevisionID  sql.NullString
	ContentEpoch            sql.NullInt64
	Payload                 []byte
	Attempts                int
	MaxAttempts             int
	LeaseOwner              string
	LeaseUntil              time.Time
	Urgency                 string
	UrgencyRaisedAt         sql.NullTime
	EligibleAt              time.Time
}

type DigestFailureResult struct {
	Changed    bool
	Terminal   bool
	RevisionID string
}

func (r *PostgresJobRepository) ClaimDigest(
	ctx context.Context,
	owner, urgency string,
	now time.Time,
	leaseTTL time.Duration,
	limit int,
	jobType string,
) ([]ProcessingJob, error) {
	if owner == "" || (urgency != "background" && urgency != "interactive") ||
		leaseTTL <= 0 || limit <= 0 || jobType == "" {
		return nil, errors.New("valid owner, urgency, job type, lease TTL, and limit are required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		UPDATE session_processing_jobs j
		SET status = CASE
				WHEN d.status IN ('ready', 'superseded') THEN 'completed'
				ELSE 'dead'
			END,
			lease_owner = NULL, lease_until = NULL,
			completed_at = COALESCE(j.completed_at, $1),
			last_error = CASE
				WHEN d.status IN ('ready', 'superseded') THEN NULL
				ELSE COALESCE(j.last_error, 'digest_v2_lease_expired')
			END
		FROM session_slice_digest_revisions d
		WHERE d.id = j.target_digest_revision_id
			AND j.job_type = $2 AND j.urgency = $3 AND j.status = 'leased'
			AND j.lease_until <= $1 AND j.attempts >= j.max_attempts
		RETURNING j.target_digest_revision_id::text, j.status`,
		now, jobType, urgency,
	)
	if err != nil {
		return nil, err
	}
	expiredRevisionIDs := make([]string, 0)
	for rows.Next() {
		var revisionID sql.NullString
		var jobStatus string
		if err := rows.Scan(&revisionID, &jobStatus); err != nil {
			rows.Close()
			return nil, err
		}
		if revisionID.Valid && jobStatus == "dead" {
			expiredRevisionIDs = append(expiredRevisionIDs, revisionID.String)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, revisionID := range expiredRevisionIDs {
		if err := failDigestRevisionAndWake(
			ctx, tx, revisionID, "digest_v2_lease_expired", "retryable", now,
		); err != nil {
			return nil, err
		}
	}
	rows, err = tx.QueryContext(ctx, `
		WITH candidates AS MATERIALIZED (
			SELECT id, created_at,
				CASE
					WHEN status = 'retry_wait' THEN COALESCE(next_retry_at, created_at)
					WHEN status = 'leased' THEN COALESCE(lease_until, created_at)
					ELSE created_at
				END AS ready_at
			FROM session_processing_jobs
			WHERE job_type = $5 AND urgency = $6 AND attempts < max_attempts
			  AND (
				status = 'pending'
				OR (status = 'retry_wait' AND COALESCE(next_retry_at, created_at) <= $1)
				OR (status = 'leased' AND lease_until <= $1)
			  )
			ORDER BY ready_at, created_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		), updated AS (
			UPDATE session_processing_jobs j
			SET status = 'leased', attempts = attempts + 1, lease_owner = $3,
				lease_until = $4, heartbeat_at = $1, next_retry_at = NULL,
				started_at = COALESCE(started_at, $1)
			FROM candidates c
			WHERE j.id = c.id
			RETURNING j.id, j.job_type, j.session_id, j.generation_id, j.chunk_id,
				j.target_revision_id, j.target_metrics_revision_id, j.target_digest_revision_id,
				j.content_epoch, j.payload, j.attempts, j.max_attempts,
				j.lease_owner, j.lease_until, j.urgency, j.urgency_raised_at
		)
		SELECT u.id, u.job_type, u.session_id, u.generation_id, u.chunk_id,
			u.target_revision_id, u.target_metrics_revision_id, u.target_digest_revision_id,
			u.content_epoch, u.payload, u.attempts, u.max_attempts,
			u.lease_owner, u.lease_until, u.urgency, u.urgency_raised_at, c.ready_at
		FROM updated u JOIN candidates c ON c.id = u.id
		ORDER BY c.ready_at, c.created_at, c.id`,
		now, limit, owner, now.Add(leaseTTL), jobType, urgency,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]ProcessingJob, 0, limit)
	for rows.Next() {
		var job ProcessingJob
		if err := rows.Scan(
			&job.ID, &job.Type, &job.SessionID, &job.GenerationID, &job.ChunkID,
			&job.TargetRevisionID, &job.TargetMetricsRevisionID, &job.TargetDigestRevisionID,
			&job.ContentEpoch, &job.Payload, &job.Attempts, &job.MaxAttempts,
			&job.LeaseOwner, &job.LeaseUntil, &job.Urgency, &job.UrgencyRaisedAt, &job.EligibleAt,
		); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *PostgresJobRepository) FailDigest(
	ctx context.Context,
	jobID, owner string,
	now time.Time,
	retryAfter time.Duration,
	preserveAttempt bool,
	failureCode, failureClass string,
) (DigestFailureResult, error) {
	if jobID == "" || owner == "" || retryAfter < 0 || failureCode == "" ||
		(failureClass != "retryable" && failureClass != "permanent") {
		return DigestFailureResult{}, errors.New("complete Digest failure state is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return DigestFailureResult{}, err
	}
	defer tx.Rollback()

	var revisionID, status string
	err = tx.QueryRowContext(ctx, `
		UPDATE session_processing_jobs
		SET status = CASE WHEN $2 OR attempts < max_attempts THEN 'retry_wait' ELSE 'dead' END,
			attempts = CASE WHEN $2 THEN GREATEST(attempts - 1, 0) ELSE attempts END,
			lease_owner = NULL, lease_until = NULL,
			next_retry_at = CASE WHEN $2 OR attempts < max_attempts THEN $1::timestamptz ELSE NULL END,
			completed_at = CASE WHEN NOT $2 AND attempts >= max_attempts THEN $7 ELSE NULL END,
			last_error = $3
		WHERE id = $4 AND status = 'leased' AND lease_owner = $5 AND lease_until > $7
			AND job_type = $6 AND target_digest_revision_id IS NOT NULL
		RETURNING target_digest_revision_id::text, status`,
		now.Add(retryAfter), preserveAttempt, failureCode, jobID, owner,
		JobBuildContentSliceDigestV2, now,
	).Scan(&revisionID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return DigestFailureResult{}, nil
	}
	if err != nil {
		return DigestFailureResult{}, err
	}
	terminal := status == "dead"
	if terminal {
		if err := failDigestRevisionAndWake(
			ctx, tx, revisionID, failureCode, failureClass, now,
		); err != nil {
			return DigestFailureResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return DigestFailureResult{}, err
	}
	return DigestFailureResult{Changed: true, Terminal: terminal, RevisionID: revisionID}, nil
}

func failDigestRevisionAndWake(
	ctx context.Context,
	tx *sql.Tx,
	revisionID, failureCode, failureClass string,
	failedAt time.Time,
) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE session_slice_digest_revisions
		SET status = 'failed', error_code = $2, failure_class = $3, failed_at = $4
		WHERE id = $1 AND status IN ('pending', 'building')`,
		revisionID, failureCode, failureClass, failedAt,
	)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		var status string
		if err := tx.QueryRowContext(ctx, `
			SELECT status FROM session_slice_digest_revisions WHERE id = $1`, revisionID,
		).Scan(&status); err != nil {
			return err
		}
		if status != "failed" {
			return errors.New("Digest Revision terminal state was not persisted")
		}
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE ai_runs r
		SET next_attempt_at = $2
		FROM report_source_selections sel
		WHERE sel.attached_run_id = r.id
			AND r.business_type = 'report_agent_run' AND r.status = 'pending'
			AND r.execution_stage = 'waiting_digest'
			AND EXISTS (
				SELECT 1
				FROM report_source_selection_items i
				JOIN session_slice_digest_revisions d
				  ON d.id = $1
				 AND d.session_content_slice_id = i.session_content_slice_id
				 AND d.generation_id = i.source_generation_id
				 AND d.content_projection_revision_id = i.content_projection_revision_id
				 AND d.content_epoch = i.content_epoch_snapshot
				WHERE i.selection_id = sel.id
			)`, revisionID, failedAt)
	return err
}

type PostgresJobRepository struct {
	db *sql.DB
}

func NewPostgresJobRepository(database *sql.DB) (*PostgresJobRepository, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &PostgresJobRepository{db: database}, nil
}

func (r *PostgresJobRepository) Claim(
	ctx context.Context,
	owner string,
	now time.Time,
	leaseTTL time.Duration,
	limit int,
) ([]ProcessingJob, error) {
	return r.ClaimTypes(ctx, owner, now, leaseTTL, limit, []string{
		JobIndexContentChunk,
		JobParseUsageChunk,
		JobRebuildContentRevision,
		JobRebuildMetricsRevision,
		JobBuildMeteringEnvelope,
		JobDeleteObject,
		"purge_session",
	})
}

func (r *PostgresJobRepository) ClaimTypes(
	ctx context.Context,
	owner string,
	now time.Time,
	leaseTTL time.Duration,
	limit int,
	jobTypes []string,
) ([]ProcessingJob, error) {
	if owner == "" || leaseTTL <= 0 || limit <= 0 {
		return nil, errors.New("owner, positive lease TTL, and positive limit are required")
	}
	if len(jobTypes) == 0 {
		return nil, errors.New("at least one job type is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE session_processing_jobs
		SET status = 'dead', lease_owner = NULL, lease_until = NULL,
			last_error = COALESCE(last_error, 'lease expired after maximum attempts')
		WHERE status = 'leased' AND lease_until <= $1 AND attempts >= max_attempts`, now); err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx, `
		WITH candidates AS MATERIALIZED (
			SELECT id, created_at,
				CASE
					WHEN status = 'retry_wait' THEN COALESCE(next_retry_at, created_at)
					WHEN status = 'leased' THEN COALESCE(lease_until, created_at)
					ELSE created_at
				END AS ready_at
			FROM session_processing_jobs
			WHERE attempts < max_attempts
			  AND job_type = ANY($5)
			  AND (
				status = 'pending'
				OR (status = 'retry_wait' AND COALESCE(next_retry_at, created_at) <= $1)
				OR (status = 'leased' AND lease_until <= $1)
			  )
			ORDER BY ready_at, created_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		), updated AS (
		UPDATE session_processing_jobs j
		SET status = 'leased', attempts = attempts + 1, lease_owner = $3,
			lease_until = $4, heartbeat_at = $1, next_retry_at = NULL,
			started_at = COALESCE(started_at, $1)
		FROM candidates c
		WHERE j.id = c.id
		RETURNING j.id, j.job_type, j.session_id, j.generation_id, j.chunk_id,
			j.target_revision_id, j.target_metrics_revision_id, j.target_digest_revision_id,
			j.content_epoch, j.payload, j.attempts,
			j.max_attempts, j.lease_owner, j.lease_until
		)
		SELECT updated.id, updated.job_type, updated.session_id, updated.generation_id, updated.chunk_id,
			updated.target_revision_id, updated.target_metrics_revision_id, updated.target_digest_revision_id,
			updated.content_epoch, updated.payload, updated.attempts,
			updated.max_attempts, updated.lease_owner, updated.lease_until
		FROM updated
		JOIN candidates ON candidates.id = updated.id
		ORDER BY candidates.ready_at, candidates.created_at, candidates.id`,
		now, limit, owner, now.Add(leaseTTL), pq.Array(jobTypes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]ProcessingJob, 0, limit)
	for rows.Next() {
		var job ProcessingJob
		if err := rows.Scan(
			&job.ID, &job.Type, &job.SessionID, &job.GenerationID, &job.ChunkID,
			&job.TargetRevisionID, &job.TargetMetricsRevisionID, &job.TargetDigestRevisionID,
			&job.ContentEpoch, &job.Payload, &job.Attempts,
			&job.MaxAttempts, &job.LeaseOwner, &job.LeaseUntil,
		); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *PostgresJobRepository) Heartbeat(
	ctx context.Context,
	jobID, owner string,
	now time.Time,
	leaseTTL time.Duration,
) (bool, error) {
	if leaseTTL <= 0 {
		return false, errors.New("positive lease TTL is required")
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE session_processing_jobs
		SET heartbeat_at = $1, lease_until = $2
		WHERE id = $3 AND status = 'leased' AND lease_owner = $4 AND lease_until > $1`,
		now, now.Add(leaseTTL), jobID, owner)
	return oneRowChanged(result, err)
}

func (r *PostgresJobRepository) Complete(ctx context.Context, jobID, owner string, now time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE session_processing_jobs
		SET status = 'completed', lease_owner = NULL, lease_until = NULL,
			next_retry_at = NULL, last_error = NULL, completed_at = $1
		WHERE id = $2 AND status = 'leased' AND lease_owner = $3 AND lease_until > $1`,
		now, jobID, owner)
	return oneRowChanged(result, err)
}

func (r *PostgresJobRepository) Fail(
	ctx context.Context,
	jobID, owner string,
	now time.Time,
	retryAfter time.Duration,
	preserveAttempt bool,
	failure string,
) (bool, error) {
	if retryAfter < 0 {
		return false, errors.New("retry delay must not be negative")
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE session_processing_jobs
		SET status = CASE WHEN $2 OR attempts < max_attempts THEN 'retry_wait' ELSE 'dead' END,
			attempts = CASE WHEN $2 THEN GREATEST(attempts - 1, 0) ELSE attempts END,
			lease_owner = NULL, lease_until = NULL,
			next_retry_at = CASE WHEN $2 OR attempts < max_attempts THEN $1::timestamptz ELSE NULL END,
			completed_at = CASE WHEN NOT $2 AND attempts >= max_attempts THEN $6 ELSE NULL END,
			last_error = $3
		WHERE id = $4 AND status = 'leased' AND lease_owner = $5 AND lease_until > $6`,
		now.Add(retryAfter), preserveAttempt, failure, jobID, owner, now)
	return oneRowChanged(result, err)
}

func oneRowChanged(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}
