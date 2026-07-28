package sessionsync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/reportsourcecatalog"
)

const ContentParserVersion = "session-content-v2"

var (
	ErrSessionNotFound    = errors.New("session not found")
	ErrSourceKeyConflict  = errors.New("source key conflicts with the existing source role")
	ErrFinalizeConflict   = errors.New("generation cannot be finalized")
	ErrInvalidSyncRequest = errors.New("invalid session sync request")
	ErrTeamRequired       = errors.New("current user does not belong to a team")
	ErrTeamContextChanged = errors.New("team upload context changed")
)

const (
	UploadModePersonal = "personal"
	UploadModeTeam     = "team"

	ErrorTeamDirectoryUnmapped       = "TEAM_DIRECTORY_UNMAPPED"
	ErrorTeamSessionIdentityConflict = "TEAM_SESSION_IDENTITY_CONFLICT"
	ErrorTeamContextChanged          = "TEAM_CONTEXT_CHANGED"
)

type PrepareBatchRequest struct {
	ClientVersion string                  `json:"client_version"`
	UploadMode    string                  `json:"upload_mode,omitempty"`
	Sessions      []PrepareSessionRequest `json:"sessions"`
}

type PrepareSessionRequest struct {
	SessionRef       string                 `json:"session_ref"`
	AgentType        string                 `json:"agent_type"`
	ParentSessionRef string                 `json:"parent_session_ref,omitempty"`
	ForkedAt         *time.Time             `json:"forked_at,omitempty"`
	ForkSource       string                 `json:"fork_source,omitempty"`
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

type GenerationStatusResponse struct {
	GenerationID            string        `json:"generation_id"`
	SessionRef              string        `json:"session_ref"`
	GenerationStatus        string        `json:"generation_status"`
	ContentStatus           ContentStatus `json:"content_status"`
	ContentProjectionStatus string        `json:"content_projection_status"`
	ExpectedCursor          int64         `json:"expected_cursor"`
	ContentIndexedCursor    int64         `json:"content_indexed_cursor"`
	ReadyForReports         bool          `json:"ready_for_reports"`
	ErrorCode               string        `json:"error_code,omitempty"`
	ErrorMessage            string        `json:"error_message,omitempty"`
}

type AbortResponse struct {
	GenerationID  string        `json:"generation_id"`
	Status        string        `json:"status"`
	ContentStatus ContentStatus `json:"content_status"`
	DeletedChunks int           `json:"deleted_chunks"`
	ObjectKeys    []string      `json:"-"`
}

type SyncService struct {
	db *sql.DB
}

type queryRowContexter interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func generationAccessError(ctx context.Context, database queryRowContexter, generationID, actorID string) error {
	var uploadTeamID, actorTeamID, ownerTeamID sql.NullString
	err := database.QueryRowContext(ctx, `
		SELECT g.upload_team_id, actor.team_id, owner.team_id
		FROM session_source_generations g
		JOIN session_sources src ON src.id = g.source_id
		JOIN sessions sess ON sess.id = src.session_id
		JOIN users owner ON owner.id = sess.user_id
		LEFT JOIN users actor ON actor.id = $2
		WHERE g.id = $1`, generationID, actorID).Scan(&uploadTeamID, &actorTeamID, &ownerTeamID)
	if err == nil && uploadTeamID.Valid && (!actorTeamID.Valid || !ownerTeamID.Valid ||
		actorTeamID.String != uploadTeamID.String || ownerTeamID.String != uploadTeamID.String) {
		return ErrTeamContextChanged
	}
	return ErrGenerationNotFound
}

type teamUploadContext struct {
	TeamID  string
	PathID  string
	ActorID string
}

func NewSyncService(database *sql.DB) (*SyncService, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &SyncService{db: database}, nil
}

func (s *SyncService) Prepare(ctx context.Context, userID string, request PrepareSessionRequest) ([]PrepareSourceResponse, error) {
	return s.PrepareWithMode(ctx, userID, UploadModePersonal, request)
}

func (s *SyncService) PrepareWithMode(ctx context.Context, userID, uploadMode string, request PrepareSessionRequest) ([]PrepareSourceResponse, error) {
	uploadMode = strings.TrimSpace(uploadMode)
	if uploadMode == "" {
		uploadMode = UploadModePersonal
	}
	if uploadMode != UploadModePersonal && uploadMode != UploadModeTeam {
		return nil, fmt.Errorf("%w: unsupported upload_mode", ErrInvalidSyncRequest)
	}
	return retryPostgresConflict(ctx, func() ([]PrepareSourceResponse, error) {
		return s.prepareOnce(ctx, userID, uploadMode, request)
	})
}

func (s *SyncService) prepareOnce(ctx context.Context, userID, uploadMode string, request PrepareSessionRequest) ([]PrepareSourceResponse, error) {
	request.SessionRef = strings.TrimSpace(request.SessionRef)
	request.AgentType = strings.TrimSpace(request.AgentType)
	request.Summary = strings.ReplaceAll(strings.TrimSpace(request.Summary), "\x00", "\uFFFD")
	request.ParentSessionRef = strings.TrimSpace(request.ParentSessionRef)
	request.ForkSource = strings.TrimSpace(request.ForkSource)
	request.CWD = strings.TrimSpace(request.CWD)
	if uploadMode == UploadModeTeam && filepath.IsAbs(request.CWD) {
		request.CWD = filepath.Clean(request.CWD)
	}
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

	identityOwner := userID
	var uploadContext *teamUploadContext
	if uploadMode == UploadModeTeam {
		resolved, ownerID, rejection, err := resolveTeamUpload(ctx, tx, userID, request)
		if err != nil {
			return nil, err
		}
		if rejection != "" {
			return commitRejectedPrepare(tx, request, rejection)
		}
		uploadContext = resolved
		identityOwner = ownerID
	}

	identity := fmt.Sprintf("%q|%q|%q", identityOwner, request.AgentType, request.SessionRef)
	if uploadContext != nil {
		identity = fmt.Sprintf("team:%q|%q|%q", uploadContext.TeamID, request.AgentType, request.SessionRef)
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, identity); err != nil {
		return nil, err
	}

	var sessionID string
	var contentStatus ContentStatus
	if uploadContext == nil {
		sessionID, contentStatus, err = lockOrCreateSession(ctx, tx, identityOwner, request)
	} else {
		sessionID, contentStatus, err = lockOrCreateTeamSession(ctx, tx, identityOwner, request, *uploadContext)
	}
	if err != nil {
		return nil, err
	}
	responses := make([]PrepareSourceResponse, 0, len(request.Sources))
	for _, sourceRequest := range request.Sources {
		response, err := prepareSource(ctx, tx, sessionID, request.SessionRef, contentStatus, sourceRequest, uploadContext)
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

func resolveTeamUpload(
	ctx context.Context,
	tx *sql.Tx,
	actorID string,
	request PrepareSessionRequest,
) (*teamUploadContext, string, string, error) {
	var teamID sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT team_id FROM users WHERE id = $1`, actorID).Scan(&teamID); err != nil {
		return nil, "", "", err
	}
	if !teamID.Valid || strings.TrimSpace(teamID.String) == "" {
		return nil, "", "", ErrTeamRequired
	}
	cwd := strings.TrimSpace(request.CWD)
	if cwd == "" || !filepath.IsAbs(cwd) {
		return nil, "", ErrorTeamDirectoryUnmapped, nil
	}
	cwd = filepath.Clean(cwd)
	var pathID, pathUserID string
	err := tx.QueryRowContext(ctx, `
		SELECT p.id, p.user_id
		FROM team_sync_paths p
		JOIN users u ON u.id = p.user_id AND u.team_id = p.team_id
		WHERE p.team_id = $1
			AND ($2 = p.normalized_path OR $2 LIKE p.normalized_path || '/%')
		ORDER BY length(p.normalized_path) DESC
		LIMIT 1`, teamID.String, cwd).Scan(&pathID, &pathUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrorTeamDirectoryUnmapped, nil
	}
	if err != nil {
		return nil, "", "", err
	}
	identity := fmt.Sprintf("team:%q|%q|%q", teamID.String, request.AgentType, request.SessionRef)
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, identity); err != nil {
		return nil, "", "", err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT s.user_id, u.team_id
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.agent_type = $1 AND s.session_ref = $2
		FOR UPDATE OF s`, request.AgentType, request.SessionRef)
	if err != nil {
		return nil, "", "", err
	}
	defer rows.Close()
	currentOwners := []string{}
	hasOtherOwner := false
	for rows.Next() {
		var ownerID string
		var ownerTeamID sql.NullString
		if err := rows.Scan(&ownerID, &ownerTeamID); err != nil {
			return nil, "", "", err
		}
		if ownerTeamID.Valid && ownerTeamID.String == teamID.String {
			currentOwners = append(currentOwners, ownerID)
		} else {
			hasOtherOwner = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", "", err
	}
	if err := rows.Close(); err != nil {
		return nil, "", "", err
	}
	if len(currentOwners) > 1 {
		return nil, "", ErrorTeamSessionIdentityConflict, nil
	}
	if len(currentOwners) == 0 && hasOtherOwner {
		return nil, "", ErrorTeamContextChanged, nil
	}
	ownerID := pathUserID
	if len(currentOwners) == 1 {
		ownerID = currentOwners[0]
	}
	return &teamUploadContext{
		TeamID: teamID.String, PathID: pathID, ActorID: actorID,
	}, ownerID, "", nil
}

func commitRejectedPrepare(tx *sql.Tx, request PrepareSessionRequest, errorCode string) ([]PrepareSourceResponse, error) {
	nextAction := "resolve the team upload configuration and retry"
	switch errorCode {
	case ErrorTeamDirectoryUnmapped:
		nextAction = "configure this directory in My Tokens and retry"
	case ErrorTeamSessionIdentityConflict:
		nextAction = "resolve duplicate session ownership before retrying"
	case ErrorTeamContextChanged:
		nextAction = "the existing session owner is outside the current team"
	}
	responses := make([]PrepareSourceResponse, 0, len(request.Sources))
	for _, source := range request.Sources {
		responses = append(responses, PrepareSourceResponse{
			SessionRef: request.SessionRef,
			SourceKey:  strings.TrimSpace(source.SourceKey),
			Action:     PrepareRejected,
			ErrorCode:  errorCode,
			NextAction: nextAction,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return responses, nil
}

func lockOrCreateTeamSession(
	ctx context.Context,
	tx *sql.Tx,
	ownerID string,
	request PrepareSessionRequest,
	upload teamUploadContext,
) (string, ContentStatus, error) {
	var sessionID string
	var contentStatus ContentStatus
	err := tx.QueryRowContext(ctx, `
		SELECT id, content_status
		FROM sessions
		WHERE user_id = $1 AND agent_type = $2 AND session_ref = $3
		FOR UPDATE`, ownerID, request.AgentType, request.SessionRef).Scan(&sessionID, &contentStatus)
	if err == nil {
		return sessionID, contentStatus, updateSessionMetadata(ctx, tx, sessionID, request)
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
			session_ref, user_id, agent_type, parent_session_ref, forked_at, fork_source,
			started_at, last_activity_at, cwd, project_name, summary, content_status,
			team_upload_team_id, team_sync_path_id, team_uploaded_by_user_id
		) VALUES (
			$1, $2, $3, NULLIF($4, ''), $5, NULLIF($6, ''),
			$7, $8, NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''), 'uploading',
			$12, $13, $14
		)
		RETURNING id, content_status`,
		request.SessionRef, ownerID, request.AgentType, request.ParentSessionRef, request.ForkedAt, request.ForkSource,
		startedAt, lastActivityAt, request.CWD, request.ProjectName, request.Summary,
		upload.TeamID, upload.PathID, upload.ActorID,
	).Scan(&sessionID, &contentStatus)
	return sessionID, contentStatus, err
}

func updateSessionMetadata(ctx context.Context, tx *sql.Tx, sessionID string, request PrepareSessionRequest) error {
	lastActivity := request.LastActivityAt
	if lastActivity == nil {
		lastActivity = request.StartedAt
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE sessions
		SET parent_session_ref = COALESCE(NULLIF($1, ''), parent_session_ref),
			forked_at = COALESCE($2::timestamptz, forked_at),
			fork_source = COALESCE(NULLIF($3, ''), fork_source),
			cwd = COALESCE(NULLIF($4, ''), cwd),
			project_name = COALESCE(NULLIF($5, ''), project_name),
			summary = COALESCE(NULLIF($6, ''), summary),
			last_activity_at = CASE WHEN $7::timestamptz IS NULL THEN last_activity_at ELSE GREATEST(last_activity_at, $7) END,
			updated_at = now()
		WHERE id = $8`, request.ParentSessionRef, request.ForkedAt, request.ForkSource,
		request.CWD, request.ProjectName, request.Summary, lastActivity, sessionID)
	return err
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
		return sessionID, contentStatus, updateSessionMetadata(ctx, tx, sessionID, request)
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
			session_ref, user_id, agent_type, parent_session_ref, forked_at, fork_source,
			started_at, last_activity_at, cwd, project_name, summary, content_status
		) VALUES (
			$1, $2, $3, NULLIF($4, ''), $5, NULLIF($6, ''),
			$7, $8, NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''), 'uploading'
		)
		RETURNING id, content_status`,
		request.SessionRef, userID, request.AgentType, request.ParentSessionRef, request.ForkedAt, request.ForkSource,
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
	uploadContext *teamUploadContext,
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
		if err := annotateGenerationUpload(ctx, tx, decision.Generation.ID, uploadContext); err != nil {
			return PrepareSourceResponse{}, err
		}
		response.GenerationID = decision.Generation.ID
		response.GenerationStatus = decision.Generation.Status
		response.ExpectedCursor = decision.Generation.ExpectedCursor
		response.PrefixCheckpointHash = decision.Generation.PrefixCheckpointHash
		response.PrefixCheckpointAlgorithm = decision.Generation.PrefixAlgorithm
	}
	return response, nil
}

func annotateGenerationUpload(ctx context.Context, tx *sql.Tx, generationID string, uploadContext *teamUploadContext) error {
	if uploadContext == nil || generationID == "" {
		return nil
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE session_source_generations
		SET upload_team_id = $2,
			prepared_by_user_id = $3,
			team_sync_path_id = $4
		WHERE id = $1 AND (upload_team_id IS NULL OR upload_team_id = $2)`,
		generationID, uploadContext.TeamID, uploadContext.ActorID, uploadContext.PathID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrTeamContextChanged
	}
	return nil
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
	return retryPostgresConflict(ctx, func() (FinalizeResponse, error) {
		return s.finalizeOnce(ctx, userID, generationID, request)
	})
}

func (s *SyncService) finalizeOnce(ctx context.Context, userID, generationID string, request FinalizeRequest) (FinalizeResponse, error) {
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
		WHERE g.id = $1
			AND (
				(g.upload_team_id IS NULL AND s.user_id = $2)
				OR (
					g.upload_team_id IS NOT NULL
					AND g.upload_team_id = (SELECT team_id FROM users WHERE id = $2)
					AND g.upload_team_id = (SELECT team_id FROM users WHERE id = s.user_id)
				)
			)`, generationID, userID).Scan(&sessionID, &sourceID, &sourceKey)
	if errors.Is(err, sql.ErrNoRows) {
		return FinalizeResponse{}, generationAccessError(ctx, tx, generationID, userID)
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
	if contentStatus != ContentUploading && contentStatus != ContentUploadFailed &&
		contentStatus != ContentAvailable && contentStatus != ContentCleared {
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
		),
			content_status = CASE
				WHEN content_status = 'upload_failed' THEN 'uploading'
				ELSE content_status
			END,
			updated_at = now()
		WHERE id = $1`, sessionID); err != nil {
		return FinalizeResponse{}, err
	}
	if contentStatus == ContentUploadFailed {
		contentStatus = ContentUploading
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

func (s *SyncService) GenerationStatus(ctx context.Context, userID, generationID string) (GenerationStatusResponse, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(generationID) == "" {
		return GenerationStatusResponse{}, ErrInvalidSyncRequest
	}
	var response GenerationStatusResponse
	var activeGeneration bool
	err := s.db.QueryRowContext(ctx, `
		SELECT g.id, sess.session_ref, g.status, sess.content_status, g.expected_cursor,
			COALESCE(src.active_generation_id = g.id, false),
			COALESCE(projection.status, 'pending'),
			COALESCE(projection.content_indexed_cursor, 0),
			COALESCE((
				SELECT last_error
				FROM session_processing_jobs
				WHERE generation_id = g.id AND status = 'dead'
					AND job_type IN ('index_content_chunk', 'rebuild_content_revision')
				ORDER BY created_at DESC
				LIMIT 1
			), '')
		FROM session_source_generations g
		JOIN session_sources src ON src.id = g.source_id
		JOIN sessions sess ON sess.id = src.session_id
		LEFT JOIN LATERAL (
			SELECT status, content_indexed_cursor
			FROM session_content_projection_revisions
			WHERE generation_id = g.id
			ORDER BY CASE status
				WHEN 'active' THEN 0
				WHEN 'validated' THEN 1
				WHEN 'building' THEN 2
				WHEN 'failed' THEN 3
				ELSE 4
			END, created_at DESC
			LIMIT 1
		) projection ON true
		WHERE g.id = $1
			AND (
				(g.upload_team_id IS NULL AND sess.user_id = $2)
				OR (
					g.upload_team_id IS NOT NULL
					AND g.upload_team_id = (SELECT team_id FROM users WHERE id = $2)
					AND g.upload_team_id = (SELECT team_id FROM users WHERE id = sess.user_id)
				)
			)`,
		generationID, userID,
	).Scan(
		&response.GenerationID, &response.SessionRef, &response.GenerationStatus,
		&response.ContentStatus, &response.ExpectedCursor, &activeGeneration,
		&response.ContentProjectionStatus, &response.ContentIndexedCursor, &response.ErrorMessage,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return GenerationStatusResponse{}, generationAccessError(ctx, s.db, generationID, userID)
	}
	if err != nil {
		return GenerationStatusResponse{}, err
	}
	response.ReadyForReports = activeGeneration &&
		response.GenerationStatus == "active" &&
		response.ContentStatus == ContentAvailable &&
		response.ContentProjectionStatus == "active" &&
		response.ContentIndexedCursor >= response.ExpectedCursor
	if !response.ReadyForReports &&
		(response.ContentProjectionStatus == "failed" || response.ErrorMessage != "") {
		response.ErrorCode = "CONTENT_PROJECTION_FAILED"
	} else if !response.ReadyForReports && response.ContentStatus == ContentUploadFailed {
		response.ErrorCode = "SESSION_CONTENT_UPLOAD_FAILED"
	}
	return response, nil
}

func (s *SyncService) Abort(ctx context.Context, userID, generationID string) (AbortResponse, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(generationID) == "" {
		return AbortResponse{}, ErrInvalidSyncRequest
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AbortResponse{}, err
	}
	defer tx.Rollback()
	var sessionID, sourceID, generationStatus string
	var contentStatus ContentStatus
	var activeGenerationID sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT s.id, src.id, g.status, s.content_status, src.active_generation_id
		FROM session_source_generations g
		JOIN session_sources src ON src.id = g.source_id
		JOIN sessions s ON s.id = src.session_id
		WHERE g.id = $1
			AND (
				(g.upload_team_id IS NULL AND s.user_id = $2)
				OR (
					g.upload_team_id IS NOT NULL
					AND g.upload_team_id = (SELECT team_id FROM users WHERE id = $2)
					AND g.upload_team_id = (SELECT team_id FROM users WHERE id = s.user_id)
				)
			)
		FOR UPDATE OF g, src, s`, generationID, userID).Scan(
		&sessionID, &sourceID, &generationStatus, &contentStatus, &activeGenerationID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AbortResponse{}, generationAccessError(ctx, tx, generationID, userID)
	}
	if err != nil {
		return AbortResponse{}, err
	}
	if generationStatus == "active" || generationStatus == "superseded" {
		return AbortResponse{}, ErrFinalizeConflict
	}
	if generationStatus == "staging" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_source_generations
			SET status = 'abandoned', superseded_at = now()
			WHERE id = $1 AND status = 'staging'`, generationID); err != nil {
			return AbortResponse{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_sources
			SET staging_generation_id = NULL, updated_at = now()
			WHERE id = $1 AND staging_generation_id = $2`, sourceID, generationID); err != nil {
			return AbortResponse{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_content_projection_revisions SET status = 'failed'
		WHERE generation_id = $1 AND status IN ('building', 'validated')`, generationID); err != nil {
		return AbortResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_metrics_revisions SET status = 'failed'
		WHERE generation_id = $1 AND status IN ('building', 'validated')`, generationID); err != nil {
		return AbortResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_processing_jobs
		SET status = 'dead', lease_owner = NULL, lease_until = NULL,
			last_error = 'session upload aborted', completed_at = now()
		WHERE generation_id = $1 AND status IN ('pending', 'leased', 'retry_wait')`, generationID); err != nil {
		return AbortResponse{}, err
	}
	rows, err := tx.QueryContext(ctx, `
		UPDATE session_upload_chunks
		SET object_status = CASE WHEN object_status = 'deleted' THEN 'deleted' ELSE 'delete_pending' END,
			content_index_status = CASE WHEN content_index_status = 'indexed' THEN 'indexed' ELSE 'failed' END,
			usage_parse_status = CASE WHEN usage_parse_status = 'parsed' THEN 'parsed' ELSE 'failed' END
		WHERE generation_id = $1
		RETURNING raw_object_key, object_status`, generationID)
	if err != nil {
		return AbortResponse{}, err
	}
	objectKeys := []string{}
	for rows.Next() {
		var key, status string
		if err := rows.Scan(&key, &status); err != nil {
			rows.Close()
			return AbortResponse{}, err
		}
		if status != "deleted" && strings.TrimSpace(key) != "" {
			objectKeys = append(objectKeys, key)
		}
	}
	if err := rows.Close(); err != nil {
		return AbortResponse{}, err
	}
	if !activeGenerationID.Valid {
		contentStatus = ContentUploadFailed
		if _, err := tx.ExecContext(ctx, `
			UPDATE sessions SET content_status = $2, updated_at = now() WHERE id = $1`, sessionID, contentStatus); err != nil {
			return AbortResponse{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return AbortResponse{}, err
	}
	return AbortResponse{
		GenerationID: generationID, Status: "abandoned", ContentStatus: contentStatus,
		DeletedChunks: len(objectKeys), ObjectKeys: objectKeys,
	}, nil
}

func (s *SyncService) MarkAbortObjectsDeleted(ctx context.Context, userID, generationID string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE session_upload_chunks c
		SET object_status = 'deleted'
		FROM session_source_generations g
		JOIN session_sources src ON src.id = g.source_id
		JOIN sessions s ON s.id = src.session_id
		WHERE c.generation_id = g.id AND g.id = $1
			AND (
				(g.upload_team_id IS NULL AND s.user_id = $2)
				OR (
					g.upload_team_id IS NOT NULL
					AND g.upload_team_id = (SELECT team_id FROM users WHERE id = $2)
					AND g.upload_team_id = (SELECT team_id FROM users WHERE id = s.user_id)
				)
			)
			AND g.status = 'abandoned' AND c.object_status = 'delete_pending'`, generationID, userID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return generationAccessError(ctx, s.db, generationID, userID)
	}
	return nil
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
	if err := reportsourcecatalog.EnsureSlice(ctx, tx, sliceID); err != nil {
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
