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
	ContentEpoch            sql.NullInt64
	Payload                 []byte
	Attempts                int
	MaxAttempts             int
	LeaseOwner              string
	LeaseUntil              time.Time
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
			SELECT id, created_at
			FROM session_processing_jobs
			WHERE attempts < max_attempts
			  AND job_type = ANY($5)
			  AND (
				status = 'pending'
				OR (status = 'retry_wait' AND COALESCE(next_retry_at, created_at) <= $1)
				OR (status = 'leased' AND lease_until <= $1)
			  )
			ORDER BY created_at, id
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
			j.target_revision_id, j.target_metrics_revision_id,
			j.content_epoch, j.payload, j.attempts,
			j.max_attempts, j.lease_owner, j.lease_until
		)
		SELECT updated.id, updated.job_type, updated.session_id, updated.generation_id, updated.chunk_id,
			updated.target_revision_id, updated.target_metrics_revision_id,
			updated.content_epoch, updated.payload, updated.attempts,
			updated.max_attempts, updated.lease_owner, updated.lease_until
		FROM updated
		JOIN candidates ON candidates.id = updated.id
		ORDER BY candidates.created_at, candidates.id`,
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
			&job.TargetRevisionID, &job.TargetMetricsRevisionID,
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
