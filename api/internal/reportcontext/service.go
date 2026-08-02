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

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/observability"
	"github.com/aidashboard/api/internal/reportmemory"
	"github.com/aidashboard/api/internal/reportsource"
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
	hints, err := reportmemory.LoadHistoricalHints(ctx, tx, reportmemory.HintRequest{
		UserID:     request.Target.UserID,
		ReportDate: request.Period.Start, Facts: facts,
	})
	if err != nil {
		return nil, err
	}
	if len(hints) == 0 {
		return nil, nil
	}
	result := &ProjectMemoryContext{
		Purpose:      "使用已整理的历史项目名称和别名，帮助归并当天 Evidence Facts。",
		EvidenceRule: "历史提示不是当天事实；不得复制历史成果、状态、指标、日期或结论。",
	}
	for _, hint := range hints {
		converted := HistoricalProjectHint{
			ProjectRef: hint.ProjectRef, CanonicalName: hint.CanonicalName,
			Aliases: hint.Aliases, MatchedFactRefs: hint.MatchedFactRef,
			Confidence:  hint.Confidence,
			Instruction: "仅用于项目命名和归并，不得作为当天成果证据。",
		}
		for _, recent := range hint.RecentContext {
			converted.RecentContext = append(converted.RecentContext, HistoricalHintEntry{
				Date: recent.Date, Overview: recent.Overview, ChildTopics: recent.ChildTopics,
			})
		}
		result.Hints = append(result.Hints, converted)
	}
	return result, nil
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
