package sessionsync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrGenerationNotFound = errors.New("session generation not found")

type PostgresChunkRepository struct {
	db *sql.DB
}

func NewPostgresChunkRepository(database *sql.DB) (*PostgresChunkRepository, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &PostgresChunkRepository{db: database}, nil
}

func (r *PostgresChunkRepository) InspectChunk(
	ctx context.Context,
	userID string,
	generationID string,
	chunk ChunkMetadata,
) (ChunkSnapshot, error) {
	var snapshot ChunkSnapshot
	var existingStart, existingEnd sql.NullInt64
	var existingHash sql.NullString
	var stateFormat sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT g.expected_cursor, s.content_status, s.content_epoch,
			EXISTS (
				SELECT 1 FROM session_content_tombstones t
				WHERE t.session_id = s.id AND t.restored_at IS NULL
					AND t.restore_status IN ('waiting_upload', 'building')
					AND t.restore_generation_id = g.id AND t.restore_expires_at > now()
			),
			g.prefix_checkpoint_hash, g.prefix_checkpoint_algorithm_version,
			g.prefix_checkpoint_state, g.prefix_checkpoint_state_format,
			c.start_cursor, c.end_cursor, c.content_sha256
		FROM session_source_generations g
		JOIN session_sources src ON src.id = g.source_id
		JOIN sessions s ON s.id = src.session_id
		LEFT JOIN session_upload_chunks c
			ON c.generation_id = g.id AND c.start_cursor = $3 AND c.end_cursor = $4
		WHERE g.id = $1 AND s.user_id = $2 AND g.status IN ('active', 'staging')`,
		generationID, userID, chunk.StartCursor, chunk.EndCursor,
	).Scan(
		&snapshot.ExpectedCursor,
		&snapshot.ContentStatus,
		&snapshot.ContentEpoch,
		&snapshot.RestoreWritable,
		&snapshot.PrefixCheckpointHash,
		&snapshot.PrefixAlgorithm,
		&snapshot.PrefixCheckpointState,
		&stateFormat,
		&existingStart,
		&existingEnd,
		&existingHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ChunkSnapshot{}, ErrGenerationNotFound
	}
	if err != nil {
		return ChunkSnapshot{}, err
	}
	if stateFormat.Valid {
		snapshot.PrefixStateFormat = stateFormat.String
	}
	if existingStart.Valid && existingEnd.Valid && existingHash.Valid {
		snapshot.Existing = &AcceptedChunk{
			StartCursor:   existingStart.Int64,
			EndCursor:     existingEnd.Int64,
			ContentSHA256: existingHash.String,
		}
	}
	return snapshot, nil
}

func (r *PostgresChunkRepository) CommitChunk(
	ctx context.Context,
	request CommitChunkRequest,
) (ChunkDecision, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ChunkDecision{}, err
	}
	defer tx.Rollback()

	var sessionID, sourceID, generationStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT s.id, src.id, g.status
		FROM session_source_generations g
		JOIN session_sources src ON src.id = g.source_id
		JOIN sessions s ON s.id = src.session_id
		WHERE g.id = $1 AND s.user_id = $2 AND g.status IN ('active', 'staging')`,
		request.GenerationID, request.UserID,
	).Scan(&sessionID, &sourceID, &generationStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ChunkDecision{}, ErrGenerationNotFound
		}
		return ChunkDecision{}, err
	}

	var snapshot ChunkSnapshot
	if err := tx.QueryRowContext(ctx, `
		SELECT content_status, content_epoch,
			EXISTS (
				SELECT 1 FROM session_content_tombstones t
				WHERE t.session_id = sessions.id AND t.restored_at IS NULL
					AND t.restore_status IN ('waiting_upload', 'building')
					AND t.restore_generation_id = $3 AND t.restore_expires_at > now()
			)
		FROM sessions
		WHERE id = $1 AND user_id = $2
		FOR UPDATE`, sessionID, request.UserID,
		request.GenerationID,
	).Scan(&snapshot.ContentStatus, &snapshot.ContentEpoch, &snapshot.RestoreWritable); err != nil {
		return ChunkDecision{}, err
	}
	var stateFormat sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT expected_cursor, prefix_checkpoint_hash,
			prefix_checkpoint_algorithm_version, prefix_checkpoint_state,
			prefix_checkpoint_state_format
		FROM session_source_generations
		WHERE id = $1 AND status IN ('active', 'staging')
		FOR UPDATE`, request.GenerationID,
	).Scan(
		&snapshot.ExpectedCursor,
		&snapshot.PrefixCheckpointHash,
		&snapshot.PrefixAlgorithm,
		&snapshot.PrefixCheckpointState,
		&stateFormat,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ChunkDecision{}, ErrGenerationNotFound
		}
		return ChunkDecision{}, err
	}
	if stateFormat.Valid {
		snapshot.PrefixStateFormat = stateFormat.String
	}

	var existingStart, existingEnd sql.NullInt64
	var existingHash sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT start_cursor, end_cursor, content_sha256
		FROM session_upload_chunks
		WHERE generation_id = $1 AND start_cursor = $2 AND end_cursor = $3`,
		request.GenerationID, request.Chunk.StartCursor, request.Chunk.EndCursor,
	).Scan(&existingStart, &existingEnd, &existingHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ChunkDecision{}, err
	}
	if err == nil {
		snapshot.Existing = &AcceptedChunk{
			StartCursor:   existingStart.Int64,
			EndCursor:     existingEnd.Int64,
			ContentSHA256: existingHash.String,
		}
	}

	if decision := writableContentDecision(snapshot); decision != nil {
		return *decision, nil
	}
	if request.ObservedEpoch != snapshot.ContentEpoch {
		return ChunkDecision{
			Status:         ChunkRejected,
			ExpectedCursor: snapshot.ExpectedCursor,
			ErrorCode:      ErrorContentNotWritable,
			NextAction:     "reload the session state before uploading",
		}, nil
	}
	decision := DecideChunk(snapshot.ExpectedCursor, snapshot.Existing, request.Chunk)
	if decision.Status != ChunkAccepted {
		return decision, nil
	}
	if !validSHA256(request.NextPrefixCheckpointHash) ||
		request.NextPrefixAlgorithm != PrefixCheckpointAlgorithm ||
		request.NextPrefixStateFormat != PrefixCheckpointStateFormat ||
		len(request.NextPrefixState) == 0 {
		return invalidCheckpointDecision(snapshot.ExpectedCursor), nil
	}

	var chunkID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO session_upload_chunks (
			generation_id, start_cursor, end_cursor, start_line, end_line,
			content_sha256, content_epoch, event_start_at, event_end_at,
			raw_object_key, object_status, content_index_status, usage_parse_status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'available', 'pending', 'pending')
		RETURNING id`,
		request.GenerationID,
		request.Chunk.StartCursor,
		request.Chunk.EndCursor,
		request.Chunk.StartLine,
		request.Chunk.EndLine,
		request.Chunk.ContentSHA256,
		snapshot.ContentEpoch,
		request.Chunk.EventStartAt,
		request.Chunk.EventEndAt,
		request.ObjectKey,
	).Scan(&chunkID); err != nil {
		return ChunkDecision{}, err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE session_source_generations
		SET expected_cursor = $1,
			prefix_checkpoint_hash = $2,
			prefix_checkpoint_algorithm_version = $3,
			prefix_checkpoint_state = $4,
			prefix_checkpoint_state_format = $5
		WHERE id = $6 AND expected_cursor = $7`,
		request.Chunk.EndCursor,
		request.NextPrefixCheckpointHash,
		request.NextPrefixAlgorithm,
		request.NextPrefixState,
		request.NextPrefixStateFormat,
		request.GenerationID,
		request.Chunk.StartCursor,
	)
	if err != nil {
		return ChunkDecision{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ChunkDecision{}, err
	}
	if rows != 1 {
		return ChunkDecision{}, fmt.Errorf("generation cursor CAS affected %d rows", rows)
	}
	contentRevisionID, err := ensureContentProjectionRevision(
		ctx, tx, request.GenerationID, request.Chunk.EndCursor,
	)
	if err != nil {
		return ChunkDecision{}, err
	}

	for _, job := range request.Jobs {
		var targetRevisionID any
		if job.Type == JobIndexContentChunk {
			targetRevisionID = contentRevisionID
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO session_processing_jobs (
				job_type, session_id, generation_id, chunk_id, target_revision_id, content_epoch
			) VALUES ($1, $2, $3, $4, $5, $6)`,
			job.Type, sessionID, request.GenerationID, chunkID, targetRevisionID, job.ContentEpoch,
		); err != nil {
			return ChunkDecision{}, err
		}
	}
	metricsStatus := "pending"
	if generationStatus == "staging" {
		metricsStatus = "rebuilding"
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_source_metrics_states (
			source_id, target_generation_id, status, active_usage_parsed_cursor, source_high_water_cursor
		) VALUES ($1, $2, $3, 0, $4)
		ON CONFLICT (source_id) DO UPDATE
		SET status = $3,
			source_high_water_cursor = CASE
				WHEN session_source_metrics_states.target_generation_id = $2
					THEN GREATEST(session_source_metrics_states.source_high_water_cursor, $4)
				ELSE $4 END,
			target_generation_id = $2,
			updated_at = now()`, sourceID, request.GenerationID, metricsStatus, request.Chunk.EndCursor); err != nil {
		return ChunkDecision{}, err
	}

	if request.Chunk.EventEndAt != nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE sessions
			SET last_activity_at = GREATEST(last_activity_at, $1), updated_at = now()
			WHERE id = $2`, *request.Chunk.EventEndAt, sessionID); err != nil {
			return ChunkDecision{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ChunkDecision{}, err
	}
	return decision, nil
}
