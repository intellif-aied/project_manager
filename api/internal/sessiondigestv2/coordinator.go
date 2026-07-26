package sessiondigestv2

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/observability"
	"github.com/aidashboard/api/internal/sessionsync"
)

const (
	UrgencyBackground  = "background"
	UrgencyInteractive = "interactive"

	digestRebuildMaxRounds = 2
	digestRebuildCooldown  = 15 * time.Minute
)

type EnsureState string

const (
	EnsureReady   EnsureState = "ready"
	EnsureWaiting EnsureState = "waiting"
	EnsureFailed  EnsureState = "failed"
)

type DigestIdentity struct {
	SliceID              string
	SessionID            string
	GenerationID         string
	ProjectionRevisionID string
	ContentEpoch         int64
	RunID                string
	RunCreatedAt         time.Time
}

type EnsureResult struct {
	State        EnsureState
	RevisionID   string
	ErrorCode    string
	FailureClass string
}

type Coordinator struct {
	db     *sql.DB
	config Config
}

func NewCoordinator(database *sql.DB, config Config) (*Coordinator, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	normalized, err := config.Normalized()
	if err != nil {
		return nil, err
	}
	return &Coordinator{db: database, config: normalized}, nil
}

func (c *Coordinator) EnsureRunDigests(
	ctx context.Context,
	runID, urgency string,
) (EnsureResult, error) {
	if runID == "" || !validUrgency(urgency) {
		return EnsureResult{}, errors.New("run ID and valid urgency are required")
	}
	var selectionID string
	var runCreatedAt time.Time
	err := c.db.QueryRowContext(ctx, `
		SELECT sel.id::text, r.created_at
		FROM ai_runs r
		JOIN report_source_selections sel ON sel.attached_run_id = r.id
		WHERE r.id = $1 AND r.business_type = 'report_agent_run'`, runID,
	).Scan(&selectionID, &runCreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return EnsureResult{State: EnsureReady}, nil
	}
	if err != nil {
		return EnsureResult{}, err
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT session_content_slice_id::text, session_id::text,
			source_generation_id::text, content_projection_revision_id::text,
			content_epoch_snapshot
		FROM report_source_selection_items
		WHERE selection_id = $1
		ORDER BY session_content_slice_id, source_generation_id,
			content_projection_revision_id, content_epoch_snapshot, id`, selectionID)
	if err != nil {
		return EnsureResult{}, err
	}
	defer rows.Close()
	identities := make([]DigestIdentity, 0)
	for rows.Next() {
		identity := DigestIdentity{RunID: runID, RunCreatedAt: runCreatedAt}
		if err := rows.Scan(
			&identity.SliceID, &identity.SessionID, &identity.GenerationID,
			&identity.ProjectionRevisionID, &identity.ContentEpoch,
		); err != nil {
			return EnsureResult{}, err
		}
		identities = append(identities, identity)
	}
	if err := rows.Err(); err != nil {
		return EnsureResult{}, err
	}
	if len(identities) == 0 {
		return EnsureResult{
			State: EnsureFailed, ErrorCode: "DIGEST_PERMANENT_FAILURE", FailureClass: "permanent",
		}, nil
	}
	state := EnsureReady
	for _, identity := range identities {
		result, err := c.EnsureDigest(ctx, identity, urgency)
		if err != nil {
			return EnsureResult{}, err
		}
		if result.State == EnsureFailed {
			return result, nil
		}
		if result.State == EnsureWaiting {
			state = EnsureWaiting
		}
	}
	return EnsureResult{State: state}, nil
}

func (c *Coordinator) EnsureDigest(
	ctx context.Context,
	identity DigestIdentity,
	urgency string,
) (EnsureResult, error) {
	if identity.SliceID == "" || identity.SessionID == "" || identity.GenerationID == "" ||
		identity.ProjectionRevisionID == "" || identity.ContentEpoch < 0 || !validUrgency(urgency) {
		return EnsureResult{}, errors.New("complete digest identity and valid urgency are required")
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return EnsureResult{}, err
	}
	defer tx.Rollback()
	// Keep every Session-scoped writer on the same lock order. Digest job
	// insertion takes a foreign-key lock on sessions, so acquiring it only
	// after locking a Digest Revision can deadlock with Usage activation,
	// which locks Session before Generation and Metrics Revision.
	if err := sessionsync.LockSessionForUpdate(ctx, tx, identity.SessionID); err != nil {
		return EnsureResult{}, err
	}

	var revisionID, status string
	var failureClass, errorCode sql.NullString
	var failedAt sql.NullTime
	var rebuildCount int
	err = tx.QueryRowContext(ctx, `
		SELECT id::text, status, failure_class, error_code, failed_at, rebuild_count
		FROM session_slice_digest_revisions
		WHERE session_content_slice_id = $1 AND content_projection_revision_id = $2
			AND content_epoch = $3 AND digest_version = $4 AND redaction_version = $5
		FOR UPDATE`, identity.SliceID, identity.ProjectionRevisionID, identity.ContentEpoch,
		c.config.DigestVersion, c.config.RedactionVersion,
	).Scan(&revisionID, &status, &failureClass, &errorCode, &failedAt, &rebuildCount)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO session_slice_digest_revisions (
				session_content_slice_id, content_projection_revision_id, generation_id,
				content_epoch, digest_version, redaction_version
			) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (
				session_content_slice_id, content_projection_revision_id, content_epoch,
				digest_version, redaction_version
			) DO UPDATE SET generation_id = session_slice_digest_revisions.generation_id
			RETURNING id::text, status, failure_class, error_code, failed_at, rebuild_count`,
			identity.SliceID, identity.ProjectionRevisionID, identity.GenerationID,
			identity.ContentEpoch, c.config.DigestVersion, c.config.RedactionVersion,
		).Scan(&revisionID, &status, &failureClass, &errorCode, &failedAt, &rebuildCount)
	}
	if err != nil {
		return EnsureResult{}, err
	}
	if status == "ready" {
		if err := tx.Commit(); err != nil {
			return EnsureResult{}, err
		}
		return EnsureResult{State: EnsureReady, RevisionID: revisionID}, nil
	}
	if status == "superseded" {
		return EnsureResult{
			State: EnsureFailed, RevisionID: revisionID,
			ErrorCode: "DIGEST_PERMANENT_FAILURE", FailureClass: "permanent",
		}, tx.Commit()
	}
	if status == "failed" {
		result, err := c.scheduleControlledRebuild(
			ctx, tx, identity, revisionID, failureClass, errorCode, failedAt, rebuildCount, urgency,
		)
		if err != nil {
			return EnsureResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return EnsureResult{}, err
		}
		if result.State == EnsureWaiting && strings.HasPrefix(result.FailureClass, "rebuild_") {
			reason := strings.TrimPrefix(result.FailureClass, "rebuild_")
			decision := "scheduled"
			if reason == "active_job" {
				decision = "rejected"
			}
			observability.ObserveDigestRebuild(decision, reason)
			result.FailureClass = ""
		}
		return result, nil
	}
	if status != "pending" && status != "building" {
		return EnsureResult{}, fmt.Errorf("unsupported digest revision status %q", status)
	}
	if err := ensureActiveDigestJob(ctx, tx, identity, revisionID, urgency, nil); err != nil {
		return EnsureResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return EnsureResult{}, err
	}
	return EnsureResult{State: EnsureWaiting, RevisionID: revisionID}, nil
}

func (c *Coordinator) scheduleControlledRebuild(
	ctx context.Context,
	tx *sql.Tx,
	identity DigestIdentity,
	revisionID string,
	failureClass, previousError sql.NullString,
	failedAt sql.NullTime,
	rebuildCount int,
	urgency string,
) (EnsureResult, error) {
	if !failureClass.Valid || failureClass.String == "permanent" || !failedAt.Valid {
		observability.ObserveDigestRebuild("rejected", "permanent")
		return EnsureResult{
			State: EnsureFailed, RevisionID: revisionID,
			ErrorCode: "DIGEST_PERMANENT_FAILURE", FailureClass: "permanent",
		}, nil
	}
	if identity.RunID == "" || !identity.RunCreatedAt.After(failedAt.Time) {
		observability.ObserveDigestRebuild("rejected", "cooldown")
		return EnsureResult{
			State: EnsureFailed, RevisionID: revisionID,
			ErrorCode: "REPORT_SOURCE_DIGEST_FAILED", FailureClass: failureClass.String,
		}, nil
	}
	if rebuildCount >= digestRebuildMaxRounds {
		observability.ObserveDigestRebuild("rejected", "exhausted")
		return EnsureResult{
			State: EnsureFailed, RevisionID: revisionID,
			ErrorCode: "DIGEST_REBUILD_EXHAUSTED", FailureClass: failureClass.String,
		}, nil
	}
	var activeJobID, activeUrgency string
	err := tx.QueryRowContext(ctx, `
		SELECT id::text, urgency
		FROM session_processing_jobs
		WHERE job_type = $1 AND target_digest_revision_id = $2
			AND status IN ('pending', 'leased', 'retry_wait')
		FOR UPDATE`, JobType, revisionID,
	).Scan(&activeJobID, &activeUrgency)
	if err == nil {
		if urgency == UrgencyInteractive && activeUrgency == UrgencyBackground {
			if _, err := tx.ExecContext(ctx, `
				UPDATE session_processing_jobs
				SET urgency = 'interactive', urgency_raised_at = COALESCE(urgency_raised_at, now())
				WHERE id = $1 AND urgency = 'background'`, activeJobID,
			); err != nil {
				return EnsureResult{}, err
			}
		}
		return EnsureResult{
			State: EnsureWaiting, RevisionID: revisionID, FailureClass: "rebuild_active_job",
		}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return EnsureResult{}, err
	}
	var previousJobID sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT id::text FROM session_processing_jobs
		WHERE job_type = $1 AND target_digest_revision_id = $2
		ORDER BY created_at DESC, id DESC LIMIT 1`, JobType, revisionID,
	).Scan(&previousJobID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return EnsureResult{}, err
	}
	payload, _ := json.Marshal(map[string]any{
		"rebuild_round": rebuildCount + 1, "trigger_run_id": identity.RunID,
		"previous_job_id":     nullableSQLString(previousJobID),
		"previous_error_code": nullableSQLString(previousError),
		"scheduled_by":        "report_run_coordinator",
	})
	nextRetryAt := failedAt.Time.Add(digestRebuildCooldown)
	reason := "cooldown"
	if nextRetryAt.Before(time.Now().UTC()) {
		nextRetryAt = time.Time{}
		reason = "scheduled"
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE session_slice_digest_revisions
		SET status = 'pending', error_code = NULL, failure_class = NULL,
			failed_at = NULL, build_started_at = NULL,
			rebuild_count = rebuild_count + 1,
			last_rebuild_at = now(), last_rebuild_run_id = $2
		WHERE id = $1 AND status = 'failed' AND rebuild_count = $3`,
		revisionID, identity.RunID, rebuildCount,
	)
	if err != nil {
		return EnsureResult{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return EnsureResult{}, ErrDigestStatePersistence
	}
	if err := insertDigestJob(ctx, tx, identity, revisionID, urgency, payload, nextRetryAt); err != nil {
		return EnsureResult{}, err
	}
	return EnsureResult{State: EnsureWaiting, RevisionID: revisionID, FailureClass: "rebuild_" + reason}, nil
}

func ensureActiveDigestJob(
	ctx context.Context,
	tx *sql.Tx,
	identity DigestIdentity,
	revisionID, urgency string,
	payload []byte,
) error {
	var jobID, currentUrgency string
	err := tx.QueryRowContext(ctx, `
		SELECT id::text, urgency
		FROM session_processing_jobs
		WHERE job_type = $1 AND target_digest_revision_id = $2
			AND status IN ('pending', 'leased', 'retry_wait')
		FOR UPDATE`, JobType, revisionID,
	).Scan(&jobID, &currentUrgency)
	if errors.Is(err, sql.ErrNoRows) {
		return insertDigestJob(ctx, tx, identity, revisionID, urgency, payload, time.Time{})
	}
	if err != nil {
		return err
	}
	if urgency == UrgencyInteractive && currentUrgency == UrgencyBackground {
		_, err = tx.ExecContext(ctx, `
			UPDATE session_processing_jobs
			SET urgency = 'interactive', urgency_raised_at = COALESCE(urgency_raised_at, now())
			WHERE id = $1 AND urgency = 'background'`, jobID)
	}
	return err
}

func insertDigestJob(
	ctx context.Context,
	tx *sql.Tx,
	identity DigestIdentity,
	revisionID, urgency string,
	payload []byte,
	nextRetryAt time.Time,
) error {
	status := "pending"
	var retryAt any
	if !nextRetryAt.IsZero() {
		status = "retry_wait"
		retryAt = nextRetryAt
	}
	var raisedAt any
	if urgency == UrgencyInteractive {
		raisedAt = time.Now().UTC()
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO session_processing_jobs (
			job_type, session_id, generation_id, target_digest_revision_id,
			content_epoch, payload, status, max_attempts, next_retry_at,
			urgency, urgency_raised_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 5, $8, $9, $10)
		ON CONFLICT DO NOTHING`,
		JobType, identity.SessionID, identity.GenerationID, revisionID,
		identity.ContentEpoch, payloadOrObject(payload), status, retryAt, urgency, raisedAt,
	)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrDigestStatePersistence
	}
	return nil
}

func (c *Coordinator) WakeRevision(ctx context.Context, revisionID string) (int64, error) {
	result, err := c.db.ExecContext(ctx, `
		UPDATE ai_runs r
		SET next_attempt_at = now()
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
			)`, revisionID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func validUrgency(value string) bool {
	return value == UrgencyBackground || value == UrgencyInteractive
}

func payloadOrObject(payload []byte) []byte {
	if len(payload) == 0 {
		return []byte(`{}`)
	}
	return payload
}

func nullableSQLString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}
