package sessionsync

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
)

var (
	ErrContentTransition       = errors.New("session content transition is not allowed")
	ErrContentLifecyclePending = errors.New("session content lifecycle operation is already pending")
	ErrIncrementalSourceNeeded = errors.New("session has no incremental source that can be cleared safely")
	ErrLegacyContentMigration  = errors.New("legacy raw log is not represented by incremental sync chunks")
)

const defaultRestoreWindow = 24 * time.Hour

type ContentLifecycleService struct {
	db *sql.DB
}

type ClearContentResponse struct {
	SessionID     string `json:"session_id"`
	ContentStatus string `json:"content_status"`
	ContentEpoch  int64  `json:"content_epoch"`
	TombstoneID   string `json:"tombstone_id"`
	PendingJobs   int    `json:"pending_jobs"`
}

type RestoreContentResponse struct {
	SessionID        string    `json:"session_id"`
	ContentStatus    string    `json:"content_status"`
	ContentEpoch     int64     `json:"content_epoch"`
	TombstoneID      string    `json:"tombstone_id"`
	RestoreStatus    string    `json:"restore_status"`
	RestoreExpiresAt time.Time `json:"restore_expires_at"`
}

func NewContentLifecycleService(database *sql.DB) (*ContentLifecycleService, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &ContentLifecycleService{db: database}, nil
}

func (s *ContentLifecycleService) RequestClear(
	ctx context.Context,
	userID, sessionID, reason string,
) (ClearContentResponse, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" || len(reason) > 1000 {
		return ClearContentResponse{}, ErrInvalidSyncRequest
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ClearContentResponse{}, err
	}
	defer tx.Rollback()

	var ownerID string
	var status ContentStatus
	var epoch int64
	var rawLogURL sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT user_id::text, content_status, content_epoch, raw_log_url
		FROM sessions WHERE id = $1 FOR UPDATE`, sessionID).Scan(&ownerID, &status, &epoch, &rawLogURL); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ClearContentResponse{}, ErrSessionNotFound
		}
		return ClearContentResponse{}, err
	}
	if ownerID != userID {
		return ClearContentResponse{}, ErrSessionNotFound
	}
	if status == ContentAvailable || status == ContentClearingFailed {
		covered, err := legacyRawLogCoveredByIncrementalChunk(ctx, tx, sessionID, rawLogURL)
		if err != nil {
			return ClearContentResponse{}, err
		}
		if !covered {
			return ClearContentResponse{}, ErrLegacyContentMigration
		}
	}
	if status == ContentClearing {
		var response ClearContentResponse
		response.SessionID = sessionID
		response.ContentStatus = string(status)
		response.ContentEpoch = epoch
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM session_content_tombstones
			WHERE session_id = $1 AND restored_at IS NULL`, sessionID).Scan(&response.TombstoneID); err != nil {
			return ClearContentResponse{}, err
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM session_processing_jobs
			WHERE session_id = $1 AND job_type = $3
				AND content_epoch = $2 AND status IN ('pending', 'leased', 'retry_wait')`,
			sessionID, epoch, JobBuildMeteringEnvelope).Scan(&response.PendingJobs); err != nil {
			return ClearContentResponse{}, err
		}
		if err := tx.Commit(); err != nil {
			return ClearContentResponse{}, err
		}
		return response, nil
	}
	if status == ContentClearingFailed {
		generationIDs, err := generationsWithAvailableObjects(ctx, tx, sessionID)
		if err != nil {
			return ClearContentResponse{}, err
		}
		if len(generationIDs) == 0 {
			return ClearContentResponse{}, ErrIncrementalSourceNeeded
		}
		var tombstoneID string
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM session_content_tombstones
			WHERE session_id = $1 AND restored_at IS NULL FOR UPDATE`, sessionID).Scan(&tombstoneID); err != nil {
			return ClearContentResponse{}, err
		}
		nextEpoch := epoch + 1
		if _, err := tx.ExecContext(ctx, `
			UPDATE sessions SET content_status = 'clearing', content_epoch = $2, updated_at = now()
			WHERE id = $1`, sessionID, nextEpoch); err != nil {
			return ClearContentResponse{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_content_tombstones
			SET reason = COALESCE(NULLIF($2, ''), reason), objects_deleted_at = NULL
			WHERE id = $1`, tombstoneID, strings.TrimSpace(reason)); err != nil {
			return ClearContentResponse{}, err
		}
		for _, generationID := range generationIDs {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO session_processing_jobs (
					job_type, session_id, generation_id, content_epoch, payload
				) VALUES ($1, $2, $3, $4, '{}'::jsonb)
				ON CONFLICT DO NOTHING`, JobBuildMeteringEnvelope, sessionID, generationID, nextEpoch); err != nil {
				return ClearContentResponse{}, err
			}
		}
		if err := tx.Commit(); err != nil {
			return ClearContentResponse{}, err
		}
		return ClearContentResponse{
			SessionID: sessionID, ContentStatus: string(ContentClearing), ContentEpoch: nextEpoch,
			TombstoneID: tombstoneID, PendingJobs: len(generationIDs),
		}, nil
	}
	if status != ContentAvailable {
		return ClearContentResponse{}, ErrContentTransition
	}

	generationIDs, err := generationsWithAvailableObjects(ctx, tx, sessionID)
	if err != nil {
		return ClearContentResponse{}, err
	}
	if len(generationIDs) == 0 {
		return ClearContentResponse{}, ErrIncrementalSourceNeeded
	}
	activeGenerationIDs, err := activeGenerations(ctx, tx, sessionID)
	if err != nil {
		return ClearContentResponse{}, err
	}
	nextEpoch := epoch + 1
	if _, err := tx.ExecContext(ctx, `
		UPDATE sessions SET content_status = 'clearing', content_epoch = $2, updated_at = now()
		WHERE id = $1`, sessionID, nextEpoch); err != nil {
		return ClearContentResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_source_generations SET status = 'abandoned', superseded_at = now()
		WHERE source_id IN (SELECT id FROM session_sources WHERE session_id = $1)
			AND status = 'staging'`, sessionID); err != nil {
		return ClearContentResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_sources SET staging_generation_id = NULL, updated_at = now()
		WHERE session_id = $1`, sessionID); err != nil {
		return ClearContentResponse{}, err
	}

	var tombstoneID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO session_content_tombstones (
			session_id, cleared_by, reason, last_active_generation_ids
		) VALUES ($1, $2, NULLIF($3, ''), $4)
		RETURNING id`, sessionID, userID, strings.TrimSpace(reason), pq.Array(activeGenerationIDs)).Scan(&tombstoneID); err != nil {
		return ClearContentResponse{}, err
	}
	for _, generationID := range generationIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO session_processing_jobs (
				job_type, session_id, generation_id, content_epoch, payload
			) VALUES ($1, $2, $3, $4, '{}'::jsonb)
			ON CONFLICT DO NOTHING`, JobBuildMeteringEnvelope, sessionID, generationID, nextEpoch); err != nil {
			return ClearContentResponse{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ClearContentResponse{}, err
	}
	return ClearContentResponse{
		SessionID: sessionID, ContentStatus: string(ContentClearing), ContentEpoch: nextEpoch,
		TombstoneID: tombstoneID, PendingJobs: len(generationIDs),
	}, nil
}

func legacyRawLogCoveredByIncrementalChunk(
	ctx context.Context,
	tx *sql.Tx,
	sessionID string,
	rawLogURL sql.NullString,
) (bool, error) {
	objectKey := strings.TrimSpace(rawLogURL.String)
	if !rawLogURL.Valid || objectKey == "" {
		return true, nil
	}
	var covered bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM session_upload_chunks c
			JOIN session_source_generations g ON g.id = c.generation_id
			JOIN session_sources src ON src.id = g.source_id
			WHERE src.session_id = $1 AND c.raw_object_key = $2
		)`, sessionID, objectKey).Scan(&covered)
	return covered, err
}

func (s *ContentLifecycleService) RequestRestore(
	ctx context.Context,
	userID, sessionID string,
) (RestoreContentResponse, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return RestoreContentResponse{}, ErrInvalidSyncRequest
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RestoreContentResponse{}, err
	}
	defer tx.Rollback()
	var ownerID string
	var status ContentStatus
	var epoch int64
	if err := tx.QueryRowContext(ctx, `
		SELECT user_id::text, content_status, content_epoch
		FROM sessions WHERE id = $1 FOR UPDATE`, sessionID).Scan(&ownerID, &status, &epoch); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RestoreContentResponse{}, ErrSessionNotFound
		}
		return RestoreContentResponse{}, err
	}
	if ownerID != userID {
		return RestoreContentResponse{}, ErrSessionNotFound
	}
	if status != ContentCleared {
		return RestoreContentResponse{}, ErrContentTransition
	}
	var tombstoneID string
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM session_content_tombstones
		WHERE session_id = $1 AND restored_at IS NULL
		ORDER BY cleared_at DESC LIMIT 1 FOR UPDATE`, sessionID).Scan(&tombstoneID); err != nil {
		return RestoreContentResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_source_generations SET status = 'abandoned', superseded_at = now()
		WHERE source_id IN (SELECT id FROM session_sources WHERE session_id = $1)
			AND status = 'staging'`, sessionID); err != nil {
		return RestoreContentResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_sources SET staging_generation_id = NULL, updated_at = now()
		WHERE session_id = $1`, sessionID); err != nil {
		return RestoreContentResponse{}, err
	}
	expiresAt := time.Now().UTC().Add(defaultRestoreWindow)
	nextEpoch := epoch + 1
	if _, err := tx.ExecContext(ctx, `
		UPDATE sessions SET content_epoch = $2, updated_at = now() WHERE id = $1`, sessionID, nextEpoch); err != nil {
		return RestoreContentResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_content_tombstones
		SET restore_status = 'waiting_upload', restore_generation_id = NULL,
			restore_requested_at = now(), restore_expires_at = $2
		WHERE id = $1`, tombstoneID, expiresAt); err != nil {
		return RestoreContentResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return RestoreContentResponse{}, err
	}
	return RestoreContentResponse{
		SessionID: sessionID, ContentStatus: string(ContentCleared), ContentEpoch: nextEpoch,
		TombstoneID: tombstoneID, RestoreStatus: "waiting_upload", RestoreExpiresAt: expiresAt,
	}, nil
}

func generationsWithAvailableObjects(ctx context.Context, tx *sql.Tx, sessionID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT g.id
		FROM session_source_generations g
		JOIN session_sources src ON src.id = g.source_id
		JOIN session_upload_chunks c ON c.generation_id = g.id
		WHERE src.session_id = $1 AND c.object_status IN ('available', 'delete_pending')
		ORDER BY g.id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var generationID string
		if err := rows.Scan(&generationID); err != nil {
			return nil, err
		}
		result = append(result, generationID)
	}
	return result, rows.Err()
}

func activeGenerations(ctx context.Context, tx *sql.Tx, sessionID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT active_generation_id
		FROM session_sources
		WHERE session_id = $1 AND active_generation_id IS NOT NULL
		ORDER BY source_role`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var generationID string
		if err := rows.Scan(&generationID); err != nil {
			return nil, err
		}
		result = append(result, generationID)
	}
	return result, rows.Err()
}
