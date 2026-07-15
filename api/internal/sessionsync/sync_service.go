package sessionsync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const ContentParserVersion = "session-content-v2"

var (
	ErrSessionNotFound    = errors.New("session not found")
	ErrSourceKeyConflict  = errors.New("source key conflicts with the existing source role")
	ErrFinalizeConflict   = errors.New("generation cannot be finalized")
	ErrInvalidSyncRequest = errors.New("invalid session sync request")
)

type PrepareBatchRequest struct {
	ClientVersion string                  `json:"client_version"`
	Sessions      []PrepareSessionRequest `json:"sessions"`
}

type PrepareSessionRequest struct {
	SessionRef       string                 `json:"session_ref"`
	AgentType        string                 `json:"agent_type"`
	ParentSessionRef string                 `json:"parent_session_ref,omitempty"`
	Summary          string                 `json:"summary,omitempty"`
	StartedAt        *time.Time             `json:"started_at,omitempty"`
	LastActivityAt   *time.Time             `json:"last_activity_at,omitempty"`
	CWD              string                 `json:"cwd,omitempty"`
	ProjectName      string                 `json:"project_name,omitempty"`
	Sources          []PrepareSourceRequest `json:"sources"`
}

type PrepareSourceRequest struct {
	SourceRole                       string `json:"source_role"`
	SourceKey                        string `json:"source_key"`
	LocalSize                        int64  `json:"local_size"`
	PrefixCheckpointHash             string `json:"prefix_checkpoint_hash"`
	PrefixCheckpointAlgorithmVersion string `json:"prefix_checkpoint_algorithm_version"`
}

type PrepareSourceResponse struct {
	SessionRef                string        `json:"session_ref"`
	SourceKey                 string        `json:"source_key"`
	GenerationID              string        `json:"generation_id,omitempty"`
	GenerationStatus          string        `json:"generation_status,omitempty"`
	ExpectedCursor            int64         `json:"expected_cursor"`
	PrefixCheckpointHash      string        `json:"prefix_checkpoint_hash,omitempty"`
	PrefixCheckpointAlgorithm string        `json:"prefix_checkpoint_algorithm_version,omitempty"`
	ContentStatus             ContentStatus `json:"content_status"`
	Action                    PrepareAction `json:"action"`
	ErrorCode                 string        `json:"error_code,omitempty"`
	NextAction                string        `json:"next_action,omitempty"`
}

type FinalizeRequest struct {
	DeclaredEndCursor                int64  `json:"declared_end_cursor"`
	PrefixCheckpointHash             string `json:"prefix_checkpoint_hash"`
	PrefixCheckpointAlgorithmVersion string `json:"prefix_checkpoint_algorithm_version"`
}

type FinalizeResponse struct {
	GenerationID   string        `json:"generation_id"`
	SourceKey      string        `json:"source_key"`
	Status         string        `json:"status"`
	ContentStatus  ContentStatus `json:"content_status"`
	ExpectedCursor int64         `json:"expected_cursor"`
	SliceKey       string        `json:"slice_key,omitempty"`
	SliceCreated   bool          `json:"slice_created"`
}

type SyncService struct {
	db *sql.DB
}

func NewSyncService(database *sql.DB) (*SyncService, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &SyncService{db: database}, nil
}

func (s *SyncService) Prepare(ctx context.Context, userID string, request PrepareSessionRequest) ([]PrepareSourceResponse, error) {
	request.SessionRef = strings.TrimSpace(request.SessionRef)
	request.AgentType = strings.TrimSpace(request.AgentType)
	request.Summary = strings.ReplaceAll(strings.TrimSpace(request.Summary), "\x00", "\uFFFD")
	if request.AgentType == "" {
		request.AgentType = "claude_code"
	}
	if userID == "" || request.SessionRef == "" || len(request.Sources) == 0 {
		return nil, fmt.Errorf("%w: user, session_ref, and sources are required", ErrInvalidSyncRequest)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	identity := fmt.Sprintf("%q|%q|%q", userID, request.AgentType, request.SessionRef)
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, identity); err != nil {
		return nil, err
	}

	sessionID, contentStatus, err := lockOrCreateSession(ctx, tx, userID, request)
	if err != nil {
		return nil, err
	}
	responses := make([]PrepareSourceResponse, 0, len(request.Sources))
	for _, sourceRequest := range request.Sources {
		response, err := prepareSource(ctx, tx, sessionID, request.SessionRef, contentStatus, sourceRequest)
		if err != nil {
			return nil, err
		}
		responses = append(responses, response)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return responses, nil
}

func lockOrCreateSession(
	ctx context.Context,
	tx *sql.Tx,
	userID string,
	request PrepareSessionRequest,
) (string, ContentStatus, error) {
	var sessionID string
	var contentStatus ContentStatus
	err := tx.QueryRowContext(ctx, `
		SELECT id, content_status
		FROM sessions
		WHERE user_id = $1 AND agent_type = $2 AND session_ref = $3
		FOR UPDATE`, userID, request.AgentType, request.SessionRef).Scan(&sessionID, &contentStatus)
	if err == nil {
		lastActivity := request.LastActivityAt
		if lastActivity == nil {
			lastActivity = request.StartedAt
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE sessions
			SET parent_session_ref = COALESCE(NULLIF($1, ''), parent_session_ref),
				cwd = COALESCE(NULLIF($2, ''), cwd),
				project_name = COALESCE(NULLIF($3, ''), project_name),
				summary = COALESCE(NULLIF($4, ''), summary),
				last_activity_at = CASE WHEN $5::timestamptz IS NULL THEN last_activity_at ELSE GREATEST(last_activity_at, $5) END,
				updated_at = now()
			WHERE id = $6`, request.ParentSessionRef, request.CWD, request.ProjectName, request.Summary, lastActivity, sessionID)
		return sessionID, contentStatus, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}

	now := time.Now().UTC()
	startedAt := now
	if request.StartedAt != nil && !request.StartedAt.IsZero() {
		startedAt = request.StartedAt.UTC()
	}
	lastActivityAt := startedAt
	if request.LastActivityAt != nil && !request.LastActivityAt.IsZero() {
		lastActivityAt = request.LastActivityAt.UTC()
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO sessions (
			session_ref, user_id, agent_type, parent_session_ref, started_at,
			last_activity_at, cwd, project_name, summary
		) VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''))
		RETURNING id, content_status`,
		request.SessionRef, userID, request.AgentType, request.ParentSessionRef,
		startedAt, lastActivityAt, request.CWD, request.ProjectName, request.Summary,
	).Scan(&sessionID, &contentStatus)
	return sessionID, contentStatus, err
}

type sourceState struct {
	ID                  string
	SourceKey           string
	ActiveGenerationID  sql.NullString
	StagingGenerationID sql.NullString
}

func prepareSource(
	ctx context.Context,
	tx *sql.Tx,
	sessionID string,
	sessionRef string,
	contentStatus ContentStatus,
	request PrepareSourceRequest,
) (PrepareSourceResponse, error) {
	request.SourceRole = strings.TrimSpace(request.SourceRole)
	request.SourceKey = strings.TrimSpace(request.SourceKey)
	if request.SourceRole == "" || request.SourceKey == "" || request.LocalSize < 0 {
		return PrepareSourceResponse{}, fmt.Errorf("%w: source_role, source_key, and non-negative local_size are required", ErrInvalidSyncRequest)
	}
	if request.PrefixCheckpointAlgorithmVersion == "" {
		request.PrefixCheckpointAlgorithmVersion = PrefixCheckpointAlgorithm
	}

	var source sourceState
	err := tx.QueryRowContext(ctx, `
		SELECT id, source_key, active_generation_id, staging_generation_id
		FROM session_sources
		WHERE session_id = $1 AND source_role = $2
		FOR UPDATE`, sessionID, request.SourceRole).Scan(
		&source.ID, &source.SourceKey, &source.ActiveGenerationID, &source.StagingGenerationID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO session_sources (session_id, source_role, source_key)
			VALUES ($1, $2, $3)
			RETURNING id, source_key, active_generation_id, staging_generation_id`,
			sessionID, request.SourceRole, request.SourceKey,
		).Scan(&source.ID, &source.SourceKey, &source.ActiveGenerationID, &source.StagingGenerationID)
	}
	if err != nil {
		return PrepareSourceResponse{}, err
	}
	if source.SourceKey != request.SourceKey {
		return PrepareSourceResponse{}, ErrSourceKeyConflict
	}

	active, err := generationCheckpoint(ctx, tx, source.ActiveGenerationID)
	if err != nil {
		return PrepareSourceResponse{}, err
	}
	restore, err := loadRestoreState(ctx, tx, sessionID, source.ID)
	if err != nil {
		return PrepareSourceResponse{}, err
	}
	if contentStatus == ContentCleared && restore.Allowed && restore.Generation == nil {
		staging, stagingErr := ensureStagingGeneration(ctx, tx, source, request)
		if stagingErr != nil {
			return PrepareSourceResponse{}, stagingErr
		}
		if _, updateErr := tx.ExecContext(ctx, `
			UPDATE session_content_tombstones
			SET restore_generation_id = $1
			WHERE session_id = $2 AND restored_at IS NULL
				AND restore_status = 'waiting_upload' AND restore_expires_at > now()`,
			staging.ID, sessionID); updateErr != nil {
			return PrepareSourceResponse{}, updateErr
		}
		restore.Generation = staging
	}
	decision := DecidePrepare(PrepareState{
		ContentStatus:     contentStatus,
		ActiveGeneration:  active,
		RestoreGeneration: restore.Generation,
	}, PrepareInput{
		LocalSize:            request.LocalSize,
		PrefixCheckpointHash: request.PrefixCheckpointHash,
		PrefixAlgorithm:      request.PrefixCheckpointAlgorithmVersion,
	})

	if decision.Action == PrepareRebuildRequired {
		staging, stagingErr := ensureStagingGeneration(ctx, tx, source, request)
		if stagingErr != nil {
			return PrepareSourceResponse{}, stagingErr
		}
		decision.Generation = staging
	} else if decision.Action == PrepareRestore && decision.Generation == nil {
		return PrepareSourceResponse{}, ErrFinalizeConflict
	} else if decision.Generation != nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_source_generations SET source_size = $1 WHERE id = $2`,
			request.LocalSize, decision.Generation.ID); err != nil {
			return PrepareSourceResponse{}, err
		}
	}

	response := PrepareSourceResponse{
		SessionRef:    sessionRef,
		SourceKey:     request.SourceKey,
		ContentStatus: contentStatus,
		Action:        decision.Action,
		ErrorCode:     decision.ErrorCode,
		NextAction:    decision.NextAction,
	}
	if decision.Generation != nil {
		response.GenerationID = decision.Generation.ID
		response.GenerationStatus = decision.Generation.Status
		response.ExpectedCursor = decision.Generation.ExpectedCursor
		response.PrefixCheckpointHash = decision.Generation.PrefixCheckpointHash
		response.PrefixCheckpointAlgorithm = decision.Generation.PrefixAlgorithm
	}
	return response, nil
}

func generationCheckpoint(ctx context.Context, tx *sql.Tx, generationID sql.NullString) (*GenerationCheckpoint, error) {
	if !generationID.Valid {
		return nil, nil
	}
	var checkpoint GenerationCheckpoint
	err := tx.QueryRowContext(ctx, `
		SELECT id, status, expected_cursor, prefix_checkpoint_hash, prefix_checkpoint_algorithm_version
		FROM session_source_generations WHERE id = $1`, generationID.String).Scan(
		&checkpoint.ID, &checkpoint.Status, &checkpoint.ExpectedCursor,
		&checkpoint.PrefixCheckpointHash, &checkpoint.PrefixAlgorithm,
	)
	return &checkpoint, err
}

type restoreState struct {
	Allowed    bool
	Generation *GenerationCheckpoint
}

func loadRestoreState(ctx context.Context, tx *sql.Tx, sessionID, sourceID string) (restoreState, error) {
	var generationID sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT t.restore_generation_id
		FROM session_content_tombstones t
		LEFT JOIN session_source_generations g ON g.id = t.restore_generation_id
		WHERE t.session_id = $1 AND t.restored_at IS NULL
			AND t.restore_status IN ('waiting_upload', 'building')
			AND t.restore_expires_at > now()
			AND (g.source_id = $2 OR g.id IS NULL)
		ORDER BY t.cleared_at DESC LIMIT 1`, sessionID, sourceID).Scan(&generationID)
	if errors.Is(err, sql.ErrNoRows) {
		return restoreState{}, nil
	}
	if err != nil {
		return restoreState{}, err
	}
	state := restoreState{Allowed: true}
	if !generationID.Valid {
		return state, nil
	}
	state.Generation, err = generationCheckpoint(ctx, tx, generationID)
	return state, err
}

func ensureStagingGeneration(
	ctx context.Context,
	tx *sql.Tx,
	source sourceState,
	request PrepareSourceRequest,
) (*GenerationCheckpoint, error) {
	if source.StagingGenerationID.Valid {
		staging, err := generationCheckpoint(ctx, tx, source.StagingGenerationID)
		if err != nil {
			return nil, err
		}
		decision := DecidePrepare(PrepareState{ContentStatus: ContentAvailable, ActiveGeneration: staging}, PrepareInput{
			LocalSize:            request.LocalSize,
			PrefixCheckpointHash: request.PrefixCheckpointHash,
			PrefixAlgorithm:      request.PrefixCheckpointAlgorithmVersion,
		})
		if decision.Action != PrepareRebuildRequired && decision.Action != PrepareRejected {
			_, err = tx.ExecContext(ctx, `UPDATE session_source_generations SET source_size = $1 WHERE id = $2`, request.LocalSize, staging.ID)
			if err != nil {
				return nil, err
			}
			_, err = ensureContentProjectionRevision(ctx, tx, staging.ID, staging.ExpectedCursor)
			return staging, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_source_generations SET status = 'abandoned', superseded_at = now() WHERE id = $1`, staging.ID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_content_projection_revisions
			SET status = 'failed' WHERE generation_id = $1 AND status = 'building'`, staging.ID); err != nil {
			return nil, err
		}
	}

	var staging GenerationCheckpoint
	err := tx.QueryRowContext(ctx, `
		INSERT INTO session_source_generations (
			source_id, status, expected_cursor, prefix_checkpoint_hash,
			prefix_checkpoint_algorithm_version, source_size
		) VALUES ($1, 'staging', 0, $2, $3, $4)
		RETURNING id, status, expected_cursor, prefix_checkpoint_hash, prefix_checkpoint_algorithm_version`,
		source.ID, HashBytes(nil), PrefixCheckpointAlgorithm, request.LocalSize,
	).Scan(&staging.ID, &staging.Status, &staging.ExpectedCursor, &staging.PrefixCheckpointHash, &staging.PrefixAlgorithm)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_sources SET staging_generation_id = $1, updated_at = now() WHERE id = $2`,
		staging.ID, source.ID); err != nil {
		return nil, err
	}
	if _, err := ensureContentProjectionRevision(ctx, tx, staging.ID, 0); err != nil {
		return nil, err
	}
	return &staging, nil
}

func ensureContentProjectionRevision(
	ctx context.Context,
	tx *sql.Tx,
	generationID string,
	highWater int64,
) (string, error) {
	var revisionID string
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM session_content_projection_revisions
		WHERE generation_id = $1 AND status IN ('building', 'active')
		ORDER BY CASE status WHEN 'active' THEN 0 ELSE 1 END, created_at DESC
		LIMIT 1`, generationID).Scan(&revisionID)
	if err == nil {
		_, updateErr := tx.ExecContext(ctx, `
			UPDATE session_content_projection_revisions
			SET source_high_water_cursor = GREATEST(source_high_water_cursor, $2)
			WHERE id = $1`, revisionID, highWater)
		return revisionID, updateErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO session_content_projection_revisions (
			generation_id, content_parser_version, status, source_high_water_cursor
		) VALUES ($1, $2, 'building', $3)
		RETURNING id`, generationID, ContentParserVersion, highWater).Scan(&revisionID)
	return revisionID, err
}

func (s *SyncService) Finalize(ctx context.Context, userID, generationID string, request FinalizeRequest) (FinalizeResponse, error) {
	if userID == "" || generationID == "" || request.DeclaredEndCursor < 0 ||
		request.PrefixCheckpointAlgorithmVersion != PrefixCheckpointAlgorithm || !validSHA256(request.PrefixCheckpointHash) {
		return FinalizeResponse{}, ErrFinalizeConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FinalizeResponse{}, err
	}
	defer tx.Rollback()

	var sessionID, sourceID, sourceKey string
	err = tx.QueryRowContext(ctx, `
		SELECT s.id, src.id, src.source_key
		FROM session_source_generations g
		JOIN session_sources src ON src.id = g.source_id
		JOIN sessions s ON s.id = src.session_id
		WHERE g.id = $1 AND s.user_id = $2`, generationID, userID).Scan(&sessionID, &sourceID, &sourceKey)
	if errors.Is(err, sql.ErrNoRows) {
		return FinalizeResponse{}, ErrGenerationNotFound
	}
	if err != nil {
		return FinalizeResponse{}, err
	}

	var contentStatus ContentStatus
	var contentEpoch int64
	if err := tx.QueryRowContext(ctx, `
		SELECT content_status, content_epoch FROM sessions WHERE id = $1 FOR UPDATE`, sessionID,
	).Scan(&contentStatus, &contentEpoch); err != nil {
		return FinalizeResponse{}, err
	}
	var activeID, stagingID sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT active_generation_id, staging_generation_id FROM session_sources WHERE id = $1 FOR UPDATE`, sourceID,
	).Scan(&activeID, &stagingID); err != nil {
		return FinalizeResponse{}, err
	}
	var status string
	var expectedCursor int64
	var prefixHash, prefixAlgorithm string
	var finalizedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT status, expected_cursor, prefix_checkpoint_hash,
			prefix_checkpoint_algorithm_version, finalized_at
		FROM session_source_generations WHERE id = $1 FOR UPDATE`, generationID,
	).Scan(&status, &expectedCursor, &prefixHash, &prefixAlgorithm, &finalizedAt); err != nil {
		return FinalizeResponse{}, err
	}

	if status == "active" && activeID.Valid && activeID.String == generationID && finalizedAt.Valid {
		if expectedCursor != request.DeclaredEndCursor || prefixHash != request.PrefixCheckpointHash ||
			prefixAlgorithm != request.PrefixCheckpointAlgorithmVersion {
			return FinalizeResponse{}, ErrFinalizeConflict
		}
		sliceKey, sliceCreated, err := createContentSlice(ctx, tx, sessionID, sourceID, generationID, expectedCursor)
		if err != nil {
			return FinalizeResponse{}, err
		}
		return commitFinalizeResponse(tx, FinalizeResponse{
			GenerationID: generationID, SourceKey: sourceKey, Status: "active",
			ContentStatus: contentStatus, ExpectedCursor: expectedCursor,
			SliceKey: sliceKey, SliceCreated: sliceCreated,
		})
	}
	if status != "staging" || !stagingID.Valid || stagingID.String != generationID ||
		expectedCursor != request.DeclaredEndCursor || prefixHash != request.PrefixCheckpointHash ||
		prefixAlgorithm != request.PrefixCheckpointAlgorithmVersion {
		return FinalizeResponse{}, ErrFinalizeConflict
	}
	if contentStatus != ContentAvailable && contentStatus != ContentCleared {
		return FinalizeResponse{}, ErrFinalizeConflict
	}
	if err := verifyChunkContinuity(ctx, tx, generationID, request.DeclaredEndCursor); err != nil {
		return FinalizeResponse{}, err
	}

	if activeID.Valid {
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_source_generations
			SET status = 'superseded', superseded_at = now()
			WHERE id = $1 AND status = 'active'`, activeID.String); err != nil {
			return FinalizeResponse{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_source_generations
		SET status = 'active', finalized_at = now(), source_size = $2
		WHERE id = $1`, generationID, request.DeclaredEndCursor); err != nil {
		return FinalizeResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_sources
		SET active_generation_id = $1, staging_generation_id = NULL, updated_at = now()
		WHERE id = $2`, generationID, sourceID); err != nil {
		return FinalizeResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE sessions
		SET active_source_count = (
			SELECT COUNT(*) FROM session_sources WHERE session_id = $1 AND active_generation_id IS NOT NULL
		), updated_at = now()
		WHERE id = $1`, sessionID); err != nil {
		return FinalizeResponse{}, err
	}

	revisionID, err := ensureContentProjectionRevision(ctx, tx, generationID, request.DeclaredEndCursor)
	if err != nil {
		return FinalizeResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_processing_jobs (
			job_type, session_id, generation_id, target_revision_id, content_epoch,
			payload
		) VALUES ('rebuild_content_revision', $1, $2, $3, $4, jsonb_build_object('end_cursor', $5::bigint))`,
		sessionID, generationID, revisionID, contentEpoch, request.DeclaredEndCursor); err != nil {
		return FinalizeResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_processing_jobs (
			job_type, session_id, generation_id, payload
		) VALUES (
			$1, $2, $3,
			jsonb_build_object('end_cursor', $4::bigint)
		)
		ON CONFLICT DO NOTHING`, JobRebuildMetricsRevision, sessionID, generationID, request.DeclaredEndCursor); err != nil {
		return FinalizeResponse{}, err
	}
	if contentStatus == ContentCleared {
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_content_tombstones
			SET restore_status = 'building', restore_generation_id = $1
			WHERE session_id = $2 AND restored_at IS NULL`, generationID, sessionID); err != nil {
			return FinalizeResponse{}, err
		}
	}
	sliceKey, sliceCreated, err := createContentSlice(ctx, tx, sessionID, sourceID, generationID, expectedCursor)
	if err != nil {
		return FinalizeResponse{}, err
	}

	return commitFinalizeResponse(tx, FinalizeResponse{
		GenerationID: generationID, SourceKey: sourceKey, Status: "active",
		ContentStatus: contentStatus, ExpectedCursor: expectedCursor,
		SliceKey: sliceKey, SliceCreated: sliceCreated,
	})
}

func createContentSlice(
	ctx context.Context,
	tx *sql.Tx,
	sessionID, sourceID, generationID string,
	endCursor int64,
) (string, bool, error) {
	var lastSliceID sql.NullString
	var startCursor int64
	err := tx.QueryRowContext(ctx, `
		SELECT id::text, end_cursor
		FROM session_content_slices
		WHERE generation_id = $1
		ORDER BY end_cursor DESC, created_at DESC
		LIMIT 1
		FOR UPDATE`, generationID).Scan(&lastSliceID, &startCursor)
	if errors.Is(err, sql.ErrNoRows) {
		startCursor = 0
	} else if err != nil {
		return "", false, err
	}
	if endCursor <= startCursor {
		return lastSliceID.String, false, nil
	}
	var sliceID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO session_content_slices (
			session_id, source_id, generation_id, start_cursor, end_cursor
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (generation_id, start_cursor, end_cursor) DO UPDATE
		SET end_cursor = EXCLUDED.end_cursor
		RETURNING id::text`, sessionID, sourceID, generationID, startCursor, endCursor).Scan(&sliceID)
	if err != nil {
		return "", false, err
	}
	return sliceID, true, nil
}

func verifyChunkContinuity(ctx context.Context, tx *sql.Tx, generationID string, declaredEnd int64) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT start_cursor, end_cursor, object_status
		FROM session_upload_chunks
		WHERE generation_id = $1
		ORDER BY start_cursor, end_cursor`, generationID)
	if err != nil {
		return err
	}
	defer rows.Close()
	cursor := int64(0)
	for rows.Next() {
		var start, end int64
		var objectStatus string
		if err := rows.Scan(&start, &end, &objectStatus); err != nil {
			return err
		}
		if start != cursor || end <= start || objectStatus != "available" {
			return ErrFinalizeConflict
		}
		cursor = end
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if cursor != declaredEnd {
		return fmt.Errorf("%w: chunk cursor %d does not reach %d", ErrFinalizeConflict, cursor, declaredEnd)
	}
	return nil
}

func commitFinalizeResponse(tx *sql.Tx, response FinalizeResponse) (FinalizeResponse, error) {
	if err := tx.Commit(); err != nil {
		return FinalizeResponse{}, err
	}
	return response, nil
}
