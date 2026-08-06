package canonicalsync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/sessionsync"
)

const contentParserVersion = "session-content-v2"

type Service struct {
	db            *sql.DB
	releasePolicy ReleasePolicy
}

func NewService(database *sql.DB, releasePolicy ReleasePolicy) (*Service, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &Service{db: database, releasePolicy: releasePolicy}, nil
}

func (s *Service) PrepareFamily(ctx context.Context, userID string, request PrepareRequest) ([]PrepareResult, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("%w: user is required", ErrInvalidRequest)
	}
	if err := ValidatePrepare(request); err != nil {
		return nil, err
	}
	if err := ValidateReleasedPrepare(request, s.releasePolicy); err != nil {
		return nil, err
	}
	normalizePrepare(&request)
	if err := validateFamily(request.Sessions); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	identities := make([]string, 0, len(request.Sessions))
	for _, session := range request.Sessions {
		identities = append(identities, fmt.Sprintf("%q|%q|%q", userID, session.AgentType, session.SessionRef))
	}
	sort.Strings(identities)
	for _, identity := range identities {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, identity); err != nil {
			return nil, err
		}
	}

	type registered struct {
		id     string
		status sessionsync.ContentStatus
	}
	registeredByIdentity := make(map[string]registered, len(request.Sessions))
	for _, session := range request.Sessions {
		id, status, err := registerSession(ctx, tx, userID, session)
		if err != nil {
			return nil, err
		}
		registeredByIdentity[session.AgentType+"\n"+session.SessionRef] = registered{id: id, status: status}
	}
	if err := validateParentsExist(ctx, tx, userID, request.Sessions); err != nil {
		return nil, err
	}

	results := make([]PrepareResult, 0)
	for _, session := range request.Sessions {
		state := registeredByIdentity[session.AgentType+"\n"+session.SessionRef]
		for _, source := range session.Sources {
			result, err := prepareCanonicalSource(ctx, tx, state.id, session.SessionRef, state.status, source)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return results, nil
}

func normalizePrepare(request *PrepareRequest) {
	for i := range request.Sessions {
		s := &request.Sessions[i]
		s.SessionRef, s.AgentType = strings.TrimSpace(s.SessionRef), strings.TrimSpace(s.AgentType)
		s.ParentSessionRef, s.ForkSource = strings.TrimSpace(s.ParentSessionRef), strings.TrimSpace(s.ForkSource)
		s.Summary = strings.ReplaceAll(strings.TrimSpace(s.Summary), "\x00", "\uFFFD")
		for j := range s.Sources {
			s.Sources[j].SourceRole = strings.TrimSpace(s.Sources[j].SourceRole)
			s.Sources[j].SourceKey = strings.TrimSpace(s.Sources[j].SourceKey)
		}
	}
}

func validateFamily(sessions []PrepareSession) error {
	byRef := make(map[string]PrepareSession, len(sessions))
	for _, s := range sessions {
		if old, ok := byRef[s.SessionRef]; ok && old.AgentType != s.AgentType {
			return fmt.Errorf("%w: session_ref is ambiguous across agent types", ErrInvalidRequest)
		}
		byRef[s.SessionRef] = s
		if s.ParentSessionRef == s.SessionRef {
			return fmt.Errorf("%w: session cannot parent itself", ErrInvalidRequest)
		}
	}
	for _, start := range sessions {
		seen := map[string]bool{}
		for current := start; current.ParentSessionRef != ""; {
			if seen[current.SessionRef] {
				return fmt.Errorf("%w: family contains a parent cycle", ErrInvalidRequest)
			}
			seen[current.SessionRef] = true
			next, ok := byRef[current.ParentSessionRef]
			if !ok || next.AgentType != current.AgentType {
				return fmt.Errorf("%w: every parent must be registered in the same family", ErrInvalidRequest)
			}
			current = next
		}
	}
	return nil
}

func registerSession(ctx context.Context, tx *sql.Tx, userID string, request PrepareSession) (string, sessionsync.ContentStatus, error) {
	var id string
	var status sessionsync.ContentStatus
	var storedParent, storedForkSource sql.NullString
	var storedForkedAt sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT id,content_status,parent_session_ref,forked_at,fork_source FROM sessions WHERE user_id=$1 AND agent_type=$2 AND session_ref=$3 FOR UPDATE`, userID, request.AgentType, request.SessionRef).Scan(&id, &status, &storedParent, &storedForkedAt, &storedForkSource)
	if err == nil {
		if storedParent.String != request.ParentSessionRef || storedForkSource.String != request.ForkSource || !sameOptionalTime(storedForkedAt, request.ForkedAt) {
			return "", "", fmt.Errorf("%w: existing session family metadata is immutable", ErrInvalidRequest)
		}
		last := request.LastActivityAt
		if last == nil {
			last = request.StartedAt
		}
		_, err = tx.ExecContext(ctx, `UPDATE sessions SET cwd=COALESCE(NULLIF($1,''),cwd),project_name=COALESCE(NULLIF($2,''),project_name),repository_key=COALESCE(NULLIF($3,''),repository_key),summary=COALESCE(NULLIF($4,''),summary),last_activity_at=CASE WHEN $5::timestamptz IS NULL THEN last_activity_at ELSE GREATEST(last_activity_at,$5) END,updated_at=now() WHERE id=$6`, request.CWD, request.ProjectName, request.RepositoryKey, request.Summary, last, id)
		return id, status, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	now := time.Now().UTC()
	started := now
	if request.StartedAt != nil && !request.StartedAt.IsZero() {
		started = request.StartedAt.UTC()
	}
	last := started
	if request.LastActivityAt != nil && !request.LastActivityAt.IsZero() {
		last = request.LastActivityAt.UTC()
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO sessions (session_ref,user_id,agent_type,parent_session_ref,forked_at,fork_source,started_at,last_activity_at,cwd,project_name,repository_key,summary,content_status) VALUES ($1,$2,$3,NULLIF($4,''),$5,NULLIF($6,''),$7,$8,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),'uploading') RETURNING id,content_status`, request.SessionRef, userID, request.AgentType, request.ParentSessionRef, request.ForkedAt, request.ForkSource, started, last, request.CWD, request.ProjectName, request.RepositoryKey, request.Summary).Scan(&id, &status)
	return id, status, err
}

func sameOptionalTime(stored sql.NullTime, requested *time.Time) bool {
	if !stored.Valid {
		return requested == nil || requested.IsZero()
	}
	return requested != nil && !requested.IsZero() && stored.Time.UTC().Equal(requested.UTC())
}

func validateParentsExist(ctx context.Context, tx *sql.Tx, userID string, family []PrepareSession) error {
	for _, s := range family {
		if s.ParentSessionRef == "" {
			continue
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE user_id=$1 AND agent_type=$2 AND session_ref=$3)`, userID, s.AgentType, s.ParentSessionRef).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%w: parent session %q is not registered", ErrInvalidRequest, s.ParentSessionRef)
		}
	}
	return nil
}

type sourceState struct {
	id, sourceKey, sourceFormat string
	activeID, stagingID         sql.NullString
	metadata                    []byte
}

func prepareCanonicalSource(ctx context.Context, tx *sql.Tx, sessionID, sessionRef string, contentStatus sessionsync.ContentStatus, request PrepareSource) (PrepareResult, error) {
	metadata, err := json.Marshal(request.IngestionMetadata)
	if err != nil {
		return PrepareResult{}, err
	}
	var source sourceState
	err = tx.QueryRowContext(ctx, `SELECT id,source_key,source_format,ingestion_metadata,active_generation_id,staging_generation_id FROM session_sources WHERE session_id=$1 AND source_role=$2 FOR UPDATE`, sessionID, request.SourceRole).Scan(&source.id, &source.sourceKey, &source.sourceFormat, &source.metadata, &source.activeID, &source.stagingID)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `INSERT INTO session_sources(session_id,source_role,source_key,source_format,ingestion_metadata) VALUES($1,$2,$3,$4,$5) RETURNING id,source_key,source_format,ingestion_metadata,active_generation_id,staging_generation_id`, sessionID, request.SourceRole, request.SourceKey, request.SourceFormat, metadata).Scan(&source.id, &source.sourceKey, &source.sourceFormat, &source.metadata, &source.activeID, &source.stagingID)
	}
	if err != nil {
		return PrepareResult{}, err
	}
	if source.sourceKey != request.SourceKey || source.sourceFormat != request.SourceFormat {
		return PrepareResult{}, fmt.Errorf("%w: source identity or format conflicts with existing source role", ErrInvalidRequest)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE session_sources SET ingestion_metadata=$1,updated_at=now() WHERE id=$2`, metadata, source.id); err != nil {
		return PrepareResult{}, err
	}
	active, err := loadGeneration(ctx, tx, source.activeID)
	if err != nil {
		return PrepareResult{}, err
	}
	decision := sessionsync.DecidePrepare(sessionsync.PrepareState{ContentStatus: contentStatus, ActiveGeneration: active}, sessionsync.PrepareInput{LocalSize: request.LocalSize, PrefixCheckpointHash: request.PrefixCheckpointHash, PrefixAlgorithm: request.PrefixCheckpointAlgorithmVersion})
	if decision.Action == sessionsync.PrepareRebuildRequired {
		decision.Generation, err = ensureGeneration(ctx, tx, source, request)
		if err != nil {
			return PrepareResult{}, err
		}
	} else if decision.Generation != nil {
		_, err = tx.ExecContext(ctx, `UPDATE session_source_generations SET source_size=$1 WHERE id=$2`, request.LocalSize, decision.Generation.ID)
		if err != nil {
			return PrepareResult{}, err
		}
	}
	r := PrepareResult{SessionRef: sessionRef, SourceKey: request.SourceKey, ContentStatus: string(contentStatus), Action: string(decision.Action), ErrorCode: decision.ErrorCode, NextAction: decision.NextAction}
	if decision.Generation != nil {
		r.GenerationID = decision.Generation.ID
		r.GenerationStatus = decision.Generation.Status
		r.ExpectedCursor = decision.Generation.ExpectedCursor
		r.PrefixCheckpointHash = decision.Generation.PrefixCheckpointHash
		r.PrefixCheckpointAlgorithm = decision.Generation.PrefixAlgorithm
	}
	return r, nil
}

func loadGeneration(ctx context.Context, tx *sql.Tx, id sql.NullString) (*sessionsync.GenerationCheckpoint, error) {
	if !id.Valid {
		return nil, nil
	}
	g := new(sessionsync.GenerationCheckpoint)
	err := tx.QueryRowContext(ctx, `SELECT id,status,expected_cursor,prefix_checkpoint_hash,prefix_checkpoint_algorithm_version FROM session_source_generations WHERE id=$1`, id.String).Scan(&g.ID, &g.Status, &g.ExpectedCursor, &g.PrefixCheckpointHash, &g.PrefixAlgorithm)
	return g, err
}

func ensureGeneration(ctx context.Context, tx *sql.Tx, source sourceState, request PrepareSource) (*sessionsync.GenerationCheckpoint, error) {
	if source.stagingID.Valid {
		if _, err := tx.ExecContext(ctx, `UPDATE session_source_generations SET status='abandoned',superseded_at=now() WHERE id=$1 AND status='staging'`, source.stagingID.String); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE session_content_projection_revisions SET status='failed' WHERE generation_id=$1 AND status='building'`, source.stagingID.String); err != nil {
			return nil, err
		}
	}
	g := new(sessionsync.GenerationCheckpoint)
	err := tx.QueryRowContext(ctx, `INSERT INTO session_source_generations(source_id,status,expected_cursor,prefix_checkpoint_hash,prefix_checkpoint_algorithm_version,source_size) VALUES($1,'staging',0,$2,$3,$4) RETURNING id,status,expected_cursor,prefix_checkpoint_hash,prefix_checkpoint_algorithm_version`, source.id, sessionsync.HashBytes(nil), sessionsync.PrefixCheckpointAlgorithm, request.LocalSize).Scan(&g.ID, &g.Status, &g.ExpectedCursor, &g.PrefixCheckpointHash, &g.PrefixAlgorithm)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE session_sources SET staging_generation_id=$1,updated_at=now() WHERE id=$2`, g.ID, source.id); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO session_content_projection_revisions(generation_id,content_parser_version,status,source_high_water_cursor) VALUES($1,$2,'building',0)`, g.ID, contentParserVersion)
	return g, err
}
