package reportcontext

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/observability"
	"github.com/aidashboard/api/internal/reportmemory"
	"github.com/aidashboard/api/internal/reportsource"
	"github.com/lib/pq"
)

const SchemaVersion = "report-context/v1"

var (
	ErrInvalidRequest = errors.New("invalid report context request")
	ErrNotFound       = errors.New("report context not found")
	ErrIncomplete     = errors.New("report context source is incomplete")
	ErrDuplicate      = errors.New("duplicate report context source")
)

type sourceReader interface {
	ReadAttachedSelection(ctx context.Context, userID, selectionID, runID, reportType string, period reportsource.Period, pageCursor string) (reportsource.ContentPage, error)
}

type Service struct {
	db     *sql.DB
	source sourceReader
}

func NewService(db *sql.DB, source *reportsource.Service) *Service {
	return &Service{db: db, source: source}
}

type StoredContext struct {
	Payload []byte
	Hash    string
	Bytes   int
}

// Build prepares and freezes all platform-defined facts for one report run.
// A successful context is immutable: repeated calls return the stored payload
// instead of reading current business tables again.
func (s *Service) Build(ctx context.Context, request BuildRequest) (StoredContext, error) {
	if err := request.validate(); err != nil || s == nil || s.db == nil {
		return StoredContext{}, ErrInvalidRequest
	}
	if stored, err := s.Get(ctx, request.UserID, request.RunID); err == nil {
		return stored, nil
	} else if !errors.Is(err, ErrNotFound) {
		return StoredContext{}, err
	}

	sessions, legacyDigest, sourceMode, err := s.loadSessions(ctx, request)
	if err != nil {
		return StoredContext{}, err
	}
	if request.EnableWorkspaceMemory && request.SourceSelectionID != "" {
		if _, err := s.observeSelectionWorkspaces(ctx, request.UserID, request.SourceSelectionID); err != nil {
			log.Printf("observe report workspaces failed for run %s: %v", request.RunID, err)
		}
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: false})
	if err != nil {
		return StoredContext{}, err
	}
	defer tx.Rollback()

	if err := verifyRun(ctx, tx, request.UserID, request.RunID); err != nil {
		return StoredContext{}, err
	}
	assembled, err := assemble(ctx, tx, request)
	if err != nil {
		return StoredContext{}, err
	}
	assembled.Sessions = sessions
	assembled.SourceState = sourceStateFor(assembled, sourceMode)
	if len(sessions) > 0 {
		assembled.SourceState.Mode = sessions[0].Mode
	}
	assembled.Sources = Sources{SessionDigest: legacyDigest}
	assembled, err = projectPayloadForRepresentation(assembled, request.Representation, request.IncludeWorkThreads)
	if err != nil {
		return StoredContext{}, err
	}
	if assembled.WorkEvidence != nil && request.SourceSelectionID != "" {
		if err := storeFactSources(ctx, tx, request.RunID, assembled.WorkEvidence); err != nil {
			return StoredContext{}, err
		}
	}
	if request.EnableMemoryShadow && assembled.WorkEvidence != nil {
		memoryContext, err := loadProjectMemoryContext(ctx, tx, request, assembled.WorkEvidence)
		if err != nil {
			log.Printf("load Project Memory hints failed for run %s: %v", request.RunID, err)
		} else if memoryContext != nil {
			assembled.ProjectMemoryContext = memoryContext
			assembled.ContinuityContext = nil
		}
	}

	encoded, err := json.Marshal(assembled)
	if err != nil {
		return StoredContext{}, err
	}
	payload, err := canonicalJSON(encoded)
	if err != nil {
		return StoredContext{}, err
	}
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	result, err := tx.ExecContext(ctx, `
		INSERT INTO report_run_contexts (
			run_id, schema_version, source_selection_id, context_hash, context_payload, context_bytes
		) VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5::jsonb, $6)
		ON CONFLICT (run_id) DO NOTHING`,
		request.RunID, SchemaVersion, request.SourceSelectionID, hash, payload, len(payload))
	if err != nil {
		return StoredContext{}, fmt.Errorf("store report context: %w", err)
	}
	written, err := result.RowsAffected()
	if err != nil {
		return StoredContext{}, err
	}
	if err := tx.Commit(); err != nil {
		return StoredContext{}, err
	}
	if written == 0 {
		return s.Get(ctx, request.UserID, request.RunID)
	}
	observability.ObservePayload("context", len(payload))
	return StoredContext{Payload: payload, Hash: hash, Bytes: len(payload)}, nil
}

const workspaceObservationAttempts = 3

// observeSelectionWorkspaces keeps optional identity materialization outside
// the Report Context transaction. PostgreSQL aborts a transaction after a
// serialization/deadlock error, so swallowing that error inside Build would
// make the mandatory context write fail as well.
func (s *Service) observeSelectionWorkspaces(ctx context.Context, userID, selectionID string) (reportmemory.WorkspaceEvidenceStats, error) {
	var stats reportmemory.WorkspaceEvidenceStats
	var lastErr error
	for attempt := 0; attempt < workspaceObservationAttempts; attempt++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return stats, err
		}
		stats, err = reportmemory.ObserveSelectionWorkspaces(ctx, tx, userID, selectionID)
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
		if err == nil {
			return stats, nil
		}
		lastErr = err
		if !retryableWorkspaceObservationError(err) || attempt+1 == workspaceObservationAttempts {
			break
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return stats, ctx.Err()
		case <-timer.C:
		}
	}
	return stats, lastErr
}

func retryableWorkspaceObservationError(err error) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return false
	}
	switch string(pqErr.Code) {
	case "40001", "40P01", "23505":
		return true
	default:
		return false
	}
}

func storeFactSources(ctx context.Context, tx *sql.Tx, runID string, evidence *WorkEvidence) error {
	if tx == nil || evidence == nil {
		return nil
	}
	for _, fact := range evidence.Facts {
		for _, sourceRef := range fact.SourceRefs {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO report_run_fact_sources (run_id, fact_ref, session_ref)
				VALUES ($1, $2, $3)
				ON CONFLICT (run_id, fact_ref, session_ref) DO NOTHING`,
				runID, fact.FactRef, sourceRef); err != nil {
				return fmt.Errorf("store report fact source: %w", err)
			}
		}
	}
	return nil
}

// BuildPersonal remains as a compatibility wrapper while all callers move to
// Build. It preserves the existing personal-report entry point.
func (s *Service) BuildPersonal(ctx context.Context, userID, runID, selectionID, reportType string, period reportsource.Period, target any) (StoredContext, error) {
	typedTarget, err := targetFromAny(target)
	if err != nil {
		return StoredContext{}, err
	}
	return s.Build(ctx, BuildRequest{
		UserID:            userID,
		RunID:             runID,
		ReportType:        reportType,
		Period:            period,
		Timezone:          biztime.Zone,
		TriggerSource:     "manual",
		Target:            typedTarget,
		SourceSelectionID: selectionID,
	})
}

func (s *Service) loadSessions(ctx context.Context, request BuildRequest) ([]SessionSource, json.RawMessage, string, error) {
	if request.SourceSelectionID == "" {
		return []SessionSource{}, nil, sourceModeForReport(request.ReportType, false), nil
	}
	if s.source == nil {
		return nil, nil, "", ErrIncomplete
	}
	page, err := s.source.ReadAttachedSelection(ctx, request.UserID, request.SourceSelectionID, request.RunID, request.ReportType, request.Period, "")
	if err != nil {
		return nil, nil, "", err
	}
	digest, digestMode, err := normalizeFrozenPayload(page.FrozenPayload)
	if err != nil {
		return nil, nil, "", err
	}
	return []SessionSource{{SelectionID: request.SourceSelectionID, Mode: digestMode, Digest: digest}}, digest, sourceModeForReport(request.ReportType, true), nil
}

func (s *Service) Get(ctx context.Context, userID, runID string) (StoredContext, error) {
	if s == nil || s.db == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(runID) == "" {
		return StoredContext{}, ErrInvalidRequest
	}
	var payload []byte
	var hash string
	var size int
	err := s.db.QueryRowContext(ctx, `
		SELECT c.context_payload, c.context_hash, c.context_bytes
		FROM report_run_contexts c
		JOIN ai_runs r ON r.id = c.run_id
		WHERE c.run_id = $1 AND r.user_id = $2 AND r.business_type = 'report_agent_run'`,
		runID, userID).Scan(&payload, &hash, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredContext{}, ErrNotFound
	}
	if err != nil {
		return StoredContext{}, err
	}
	normalized, err := canonicalJSON(payload)
	if err != nil || len(normalized) == 0 {
		return StoredContext{}, ErrIncomplete
	}
	return StoredContext{Payload: normalized, Hash: hash, Bytes: size}, nil
}

func loadProjectMemoryContext(ctx context.Context, tx *sql.Tx, request BuildRequest, evidence *WorkEvidence) (*ProjectMemoryContext, error) {
	goalsByThread := make(map[string]string, len(evidence.Threads))
	for _, thread := range evidence.Threads {
		if goal := strings.TrimSpace(thread.Goal); goal != "" {
			goalsByThread[thread.ThreadRef] = goal
		}
	}
	facts := make([]reportmemory.FactInput, 0, len(evidence.Facts))
	for _, fact := range evidence.Facts {
		goals := make([]string, 0, len(fact.ThreadRefs))
		for _, threadRef := range fact.ThreadRefs {
			if goal := goalsByThread[threadRef]; goal != "" {
				goals = append(goals, goal)
			}
		}
		facts = append(facts, reportmemory.FactInput{
			FactRef: fact.FactRef, Text: fact.Text, ThreadGoals: goals,
		})
	}
	hintRunID := ""
	if request.EnableWorkspaceMemory {
		hintRunID = request.RunID
	}
	hints, err := reportmemory.LoadHistoricalHints(ctx, tx, reportmemory.HintRequest{
		UserID: request.Target.UserID, RunID: hintRunID,
		ReportDate: request.Period.Start, Facts: facts,
	})
	if err != nil {
		return nil, err
	}
	if len(hints) == 0 {
		return nil, nil
	}
	return projectMemoryContextFromHints(hints), nil
}

func projectMemoryContextFromHints(hints []reportmemory.HistoricalProjectHint) *ProjectMemoryContext {
	result := &ProjectMemoryContext{
		Purpose:      "提供用户近期项目名称和别名作为可选背景，帮助理解当天工作；是否关联由当天 Facts 决定。",
		EvidenceRule: "历史提示不是当天事实；不得复制历史成果、状态、指标、日期或结论。",
		GroupingRule: "project_memory_context 不是成果证据或强制归属。match_basis=workspace_semantic 且 candidate_only=false 时，semantic_fact_refs 是项目语义锚点，workspace_fact_refs 只是同工作空间候选；当天 Facts 没有出现冲突项目或目标时，可用 canonical_name 归并锚点及兼容的别名和子能力。其他 Hint 仍是弱参考；两个相似项目不得因历史背景被合并。",
	}
	for _, hint := range hints {
		instruction := "这是根据名称或别名相似性召回的历史背景，不是项目归属结论。结合当天 Facts 自行判断是否采用；冲突、不确定或已切换项目时忽略。历史名称不得作为成果证据。"
		if hint.CandidateOnly {
			instruction = "这是近期 Project Memory 中未与当天 Fact 匹配的背景候选。通常应忽略；只有当天 Facts 自身明确给出该项目名称或归属时才可参考。不得为了使用候选而合并工作。"
		}
		converted := HistoricalProjectHint{
			ProjectRef: hint.ProjectRef, CanonicalName: hint.CanonicalName,
			Aliases:          hint.Aliases,
			SemanticFactRefs: hint.SemanticFactRef, WorkspaceFactRefs: hint.WorkspaceFactRef,
			Confidence: hint.Confidence, MatchBasis: hint.MatchBasis,
			CandidateOnly: hint.CandidateOnly, Instruction: instruction,
		}
		switch hint.MatchBasis {
		case "workspace_semantic":
			converted.Instruction = "semantic_fact_refs 通过当天名称/别名语义锚定该历史项目，workspace_fact_refs 仅表示同工作空间候选。这是高可信的父项目命名参考，但不是当天成果证据；锚点应采用 canonical_name，工作空间候选只有在当天目标兼容且无其他项目冲突时才归入，并保留具体工作为子成果。"
		case "workspace":
			converted.Instruction = "这是由当天 Fact 所在 Workspace 召回的历史项目弱参考，只用于辅助项目命名与归并。当天事实兼容时可采用；出现新项目名称、目标冲突或无法确认时忽略。历史内容不得作为当天成果。"
		}
		result.Hints = append(result.Hints, converted)
	}
	return result
}

func normalizeFrozenPayload(raw json.RawMessage) (json.RawMessage, string, error) {
	normalized, err := normalizeJSON(raw)
	if err != nil || len(normalized) == 0 || bytes.Equal(normalized, []byte("null")) {
		return nil, "", ErrIncomplete
	}
	var object map[string]any
	if err := json.Unmarshal(normalized, &object); err != nil || len(object) == 0 {
		return nil, "", ErrIncomplete
	}
	mode, _ := object["content_mode"].(string)
	if mode != reportsource.ReadModeDigestV1 && mode != reportsource.ReadModeDigestV2 {
		return nil, "", ErrIncomplete
	}
	return json.RawMessage(normalized), mode, nil
}

func normalizeJSON(raw []byte) ([]byte, error) {
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, raw); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func canonicalJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
