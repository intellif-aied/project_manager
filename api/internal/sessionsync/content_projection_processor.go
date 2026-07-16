package sessionsync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"
)

var (
	ErrStaleContentEpoch     = errors.New("content projection job belongs to a stale content epoch")
	ErrProjectionOutOfOrder  = errors.New("content projection chunk is out of order")
	ErrProjectionUnavailable = errors.New("content projection revision is unavailable")
)

type ContentObjectStore interface {
	Download(context.Context, string) (io.ReadCloser, error)
}

type ContentProjectionProcessor struct {
	db    *sql.DB
	store ContentObjectStore
}

func NewContentProjectionProcessor(database *sql.DB, store ContentObjectStore) (*ContentProjectionProcessor, error) {
	if database == nil || store == nil {
		return nil, errors.New("database and object store are required")
	}
	return &ContentProjectionProcessor{db: database, store: store}, nil
}

func (p *ContentProjectionProcessor) Process(ctx context.Context, job ProcessingJob) error {
	switch job.Type {
	case JobIndexContentChunk:
		return p.processChunk(ctx, job)
	case JobRebuildContentRevision:
		return p.processActivation(ctx, job)
	default:
		return fmt.Errorf("unsupported content projection job type %q", job.Type)
	}
}

type projectionChunk struct {
	ID           string
	GenerationID string
	StartCursor  int64
	EndCursor    int64
	ObjectKey    string
	ObjectStatus string
	EventStartAt sql.NullTime
}

func (p *ContentProjectionProcessor) processChunk(ctx context.Context, job ProcessingJob) error {
	if !job.ChunkID.Valid || !job.GenerationID.Valid || !job.ContentEpoch.Valid {
		return ErrProjectionUnavailable
	}
	var chunk projectionChunk
	err := p.db.QueryRowContext(ctx, `
		SELECT id, generation_id, start_cursor, end_cursor, raw_object_key,
			object_status, event_start_at
		FROM session_upload_chunks
		WHERE id = $1 AND generation_id = $2`, job.ChunkID.String, job.GenerationID.String).Scan(
		&chunk.ID, &chunk.GenerationID, &chunk.StartCursor, &chunk.EndCursor,
		&chunk.ObjectKey, &chunk.ObjectStatus, &chunk.EventStartAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrProjectionUnavailable
	}
	if err != nil {
		return err
	}
	if chunk.ObjectStatus != "available" {
		return ErrProjectionUnavailable
	}
	object, err := p.store.Download(ctx, chunk.ObjectKey)
	if err != nil {
		return err
	}
	var fallback *time.Time
	if chunk.EventStartAt.Valid {
		fallback = &chunk.EventStartAt.Time
	}
	parsed, parseErr := ParseContentChunk(object, chunk.StartCursor, fallback)
	closeErr := object.Close()
	if parseErr != nil {
		return parseErr
	}
	if closeErr != nil {
		return closeErr
	}
	if parsed.EndCursor != chunk.EndCursor {
		return fmt.Errorf("%w: parsed cursor %d, chunk end %d", ErrProjectionUnavailable, parsed.EndCursor, chunk.EndCursor)
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	contentStatus, err := lockContentEpoch(ctx, tx, job.SessionID, job.ContentEpoch.Int64)
	if err != nil {
		return err
	}
	if contentStatus != ContentUploading && contentStatus != ContentUploadFailed &&
		contentStatus != ContentAvailable && contentStatus != ContentCleared {
		return ErrStaleContentEpoch
	}

	revisionID, err := resolveContentRevision(ctx, tx, job)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "content-projection:"+revisionID); err != nil {
		return err
	}
	var revisionStatus string
	var indexedCursor int64
	if err := tx.QueryRowContext(ctx, `
		SELECT status, content_indexed_cursor
		FROM session_content_projection_revisions
		WHERE id = $1 AND generation_id = $2
		FOR UPDATE`, revisionID, job.GenerationID.String).Scan(&revisionStatus, &indexedCursor); err != nil {
		return err
	}
	if revisionStatus != "building" && revisionStatus != "active" {
		return ErrProjectionUnavailable
	}
	if indexedCursor >= chunk.EndCursor {
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_upload_chunks SET content_index_status = 'indexed' WHERE id = $1`, chunk.ID); err != nil {
			return err
		}
		return tx.Commit()
	}
	if indexedCursor != chunk.StartCursor {
		return fmt.Errorf("%w: indexed cursor %d, chunk start %d", ErrProjectionOutOfOrder, indexedCursor, chunk.StartCursor)
	}

	for _, event := range parsed.Events {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO session_content_events (
				content_projection_revision_id, chunk_id, source_start_cursor,
				source_end_cursor, occurred_at, event_type, summary, excerpt,
				content_payload, content_sha256
			) VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), $9, $10)
			ON CONFLICT (
				content_projection_revision_id, chunk_id, source_start_cursor, source_end_cursor
			) DO NOTHING`,
			revisionID, chunk.ID, event.SourceStartCursor, event.SourceEndCursor,
			event.OccurredAt, event.EventType, event.Summary, event.Excerpt,
			[]byte(event.Payload), event.ContentSHA256); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE session_content_projection_revisions
		SET content_indexed_cursor = $1,
			source_high_water_cursor = GREATEST(source_high_water_cursor, $1),
			event_count = event_count + $2,
			malformed_event_count = malformed_event_count + $3
		WHERE id = $4 AND content_indexed_cursor = $5`,
		chunk.EndCursor, len(parsed.Events), parsed.MalformedEventCount, revisionID, chunk.StartCursor)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrProjectionOutOfOrder
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_upload_chunks SET content_index_status = 'indexed' WHERE id = $1`, chunk.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func lockContentEpoch(ctx context.Context, tx *sql.Tx, sessionID string, expectedEpoch int64) (ContentStatus, error) {
	var status ContentStatus
	var currentEpoch int64
	err := tx.QueryRowContext(ctx, `
		SELECT content_status, content_epoch FROM sessions WHERE id = $1 FOR UPDATE`, sessionID,
	).Scan(&status, &currentEpoch)
	if err != nil {
		return "", err
	}
	if currentEpoch != expectedEpoch {
		return status, ErrStaleContentEpoch
	}
	return status, nil
}

func resolveContentRevision(ctx context.Context, tx *sql.Tx, job ProcessingJob) (string, error) {
	if job.TargetRevisionID.Valid {
		return job.TargetRevisionID.String, nil
	}
	if !job.GenerationID.Valid {
		return "", ErrProjectionUnavailable
	}
	return ensureContentProjectionRevision(ctx, tx, job.GenerationID.String, 0)
}

func (p *ContentProjectionProcessor) processActivation(ctx context.Context, job ProcessingJob) error {
	if !job.GenerationID.Valid || !job.TargetRevisionID.Valid || !job.ContentEpoch.Valid {
		return ErrProjectionUnavailable
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	contentStatus, err := lockContentEpoch(ctx, tx, job.SessionID, job.ContentEpoch.Int64)
	if err != nil {
		return err
	}
	if contentStatus != ContentUploading && contentStatus != ContentUploadFailed &&
		contentStatus != ContentAvailable && contentStatus != ContentCleared {
		return ErrStaleContentEpoch
	}

	var sourceID string
	var activeGenerationID, activeRevisionID sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT id, active_generation_id, active_content_projection_revision_id
		FROM session_sources
		WHERE session_id = $1 AND active_generation_id = $2
		FOR UPDATE`, job.SessionID, job.GenerationID.String).Scan(
		&sourceID, &activeGenerationID, &activeRevisionID,
	); err != nil {
		return ErrProjectionUnavailable
	}
	var sourceHighWater int64
	if err := tx.QueryRowContext(ctx, `
		SELECT expected_cursor FROM session_source_generations
		WHERE id = $1 AND status = 'active' FOR UPDATE`, job.GenerationID.String).Scan(&sourceHighWater); err != nil {
		return ErrProjectionUnavailable
	}
	var revisionStatus string
	var indexedCursor int64
	if err := tx.QueryRowContext(ctx, `
		SELECT status, content_indexed_cursor
		FROM session_content_projection_revisions
		WHERE id = $1 AND generation_id = $2
		FOR UPDATE`, job.TargetRevisionID.String, job.GenerationID.String).Scan(&revisionStatus, &indexedCursor); err != nil {
		return ErrProjectionUnavailable
	}
	if revisionStatus == "active" && activeRevisionID.Valid && activeRevisionID.String == job.TargetRevisionID.String {
		return tx.Commit()
	}
	if revisionStatus != "building" && revisionStatus != "validated" {
		return ErrProjectionUnavailable
	}
	decision := DecideProjectionActivation(sourceHighWater, indexedCursor)
	if decision.Status == ProjectionCatchUp {
		return ErrProjectionOutOfOrder
	}
	if decision.Status != ProjectionReady {
		return ErrProjectionUnavailable
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_content_projection_revisions
		SET source_high_water_cursor = $1, status = 'validated', validated_at = now()
		WHERE id = $2`, sourceHighWater, job.TargetRevisionID.String); err != nil {
		return err
	}
	if activeRevisionID.Valid && activeRevisionID.String != job.TargetRevisionID.String {
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_content_projection_revisions
			SET status = 'superseded', superseded_at = now()
			WHERE id = $1 AND status = 'active'`, activeRevisionID.String); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_content_projection_revisions
		SET status = 'active', activated_at = now()
		WHERE id = $1 AND status = 'validated'`, job.TargetRevisionID.String); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_sources
		SET active_content_projection_revision_id = $1, updated_at = now()
		WHERE id = $2`, job.TargetRevisionID.String, sourceID); err != nil {
		return err
	}
	if contentStatus == ContentCleared {
		result, err := tx.ExecContext(ctx, `
			UPDATE session_content_tombstones
			SET restore_status = 'restored', restored_at = now()
			WHERE session_id = $1 AND restored_at IS NULL
				AND restore_status = 'building' AND restore_generation_id = $2`,
			job.SessionID, job.GenerationID.String)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return ErrProjectionUnavailable
		}
	}
	if contentStatus != ContentAvailable {
		if _, err := tx.ExecContext(ctx, `
			UPDATE sessions SET content_status = 'available', updated_at = now() WHERE id = $1`, job.SessionID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
