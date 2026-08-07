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
	"sort"
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
	if request.EnableWorkspaceMemory && assembled.WorkEvidence != nil && request.SourceSelectionID != "" {
		workspaceContext, err := loadWorkspaceContext(ctx, tx, request.RunID, request.SourceSelectionID)
		if err != nil {
			log.Printf("load report workspace context failed for run %s: %v", request.RunID, err)
		} else if workspaceContext != nil {
			assembled.WorkspaceContext = workspaceContext
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

func loadWorkspaceContext(ctx context.Context, tx *sql.Tx, runID, selectionID string) (*WorkspaceContext, error) {
	if tx == nil || strings.TrimSpace(runID) == "" || strings.TrimSpace(selectionID) == "" {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT source.fact_ref, evidence.workspace_id::text, MIN(evidence.observed_from)
		FROM report_run_fact_sources source
		JOIN report_source_selections selection
		  ON selection.id = $2::uuid AND selection.attached_run_id = source.run_id
		JOIN report_source_selection_items item
		  ON item.selection_id = selection.id AND item.session_ref_snapshot = source.session_ref
		JOIN report_workspace_evidence evidence
		  ON evidence.source_session_id = item.session_id
		 AND evidence.content_projection_revision_id = item.content_projection_revision_id
		 AND evidence.start_cursor = item.start_cursor
		 AND evidence.end_cursor = item.end_cursor
		WHERE source.run_id = $1
		GROUP BY source.fact_ref, evidence.workspace_id
		ORDER BY MIN(evidence.observed_from), evidence.workspace_id, source.fact_ref`, runID, selectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type workspaceGroup struct {
		workspaceID string
		firstSeen   time.Time
		factRefs    map[string]struct{}
	}
	groupsByID := make(map[string]*workspaceGroup)
	for rows.Next() {
		var factRef, workspaceID string
		var observedFrom time.Time
		if err := rows.Scan(&factRef, &workspaceID, &observedFrom); err != nil {
			return nil, err
		}
		factRef = strings.TrimSpace(factRef)
		workspaceID = strings.TrimSpace(workspaceID)
		if factRef == "" || workspaceID == "" {
			continue
		}
		group := groupsByID[workspaceID]
		if group == nil {
			group = &workspaceGroup{workspaceID: workspaceID, firstSeen: observedFrom, factRefs: make(map[string]struct{})}
			groupsByID[workspaceID] = group
		} else if observedFrom.Before(group.firstSeen) {
			group.firstSeen = observedFrom
		}
		group.factRefs[factRef] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(groupsByID) < 2 {
		return nil, nil
	}
	groups := make([]*workspaceGroup, 0, len(groupsByID))
	for _, group := range groupsByID {
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool {
		if !groups[i].firstSeen.Equal(groups[j].firstSeen) {
			return groups[i].firstSeen.Before(groups[j].firstSeen)
		}
		return groups[i].workspaceID < groups[j].workspaceID
	})
	result := &WorkspaceContext{
		Purpose:      "保留当天事实的工作空间边界，帮助区分同时推进的不同工作；workspace_ref 不是项目名称或成果证据。",
		GroupingRule: "不同 workspace_ref 的 Facts 默认分开；相同技术词（例如 MCP）不能作为跨工作空间合并依据。同一 workspace_ref 默认只生成一个 Workstream，只有当天 Facts 明确出现不同项目名时才拆分。组内若只有一个明确项目或平台名，未命名模块归入该父级；否则采用最短且重复出现的明确名称。当天 Facts 或 project_memory_context 明确指向同一项目时，仍允许跨工作空间归并。",
		Groups:       make([]WorkspaceFactGroup, 0, len(groups)),
	}
	for index, group := range groups {
		factRefs := make([]string, 0, len(group.factRefs))
		for factRef := range group.factRefs {
			factRefs = append(factRefs, factRef)
		}
		sort.Strings(factRefs)
		result.Groups = append(result.Groups, WorkspaceFactGroup{
			WorkspaceRef: fmt.Sprintf("workspace-%03d", index+1),
			FactRefs:     factRefs,
		})
	}
	return result, nil
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
		GroupingRule: "project_memory_context 不是成果证据或强制归属。workstream_cues 是该项目历史上确认过的稳定模块或工作对象，只用于识别父项目。semantic_fact_refs 与名称、别名或 workstream_cues 匹配时，当天没有冲突项目或目标可优先采用 canonical_name，并把具体工作保留为子项；workspace_fact_refs 只能作为辅助。新项目或不确定内容不得被强行归入历史项目。",
	}
	for _, hint := range hints {
		instruction := "这是根据名称或别名相似性召回的历史背景，不是项目归属结论。结合当天 Facts 自行判断是否采用；冲突、不确定或已切换项目时忽略。历史名称不得作为成果证据。"
		if hint.CandidateOnly {
			instruction = "这是近期 Project Memory 中未与当天 Fact 匹配的背景候选。通常应忽略；只有当天 Facts 自身明确给出该项目名称或归属时才可参考。不得为了使用候选而合并工作。"
		}
		converted := HistoricalProjectHint{
			ProjectRef: hint.ProjectRef, CanonicalName: hint.CanonicalName,
			Aliases:          hint.Aliases,
			WorkstreamCues:   hint.WorkstreamCues,
			SemanticFactRefs: hint.SemanticFactRef, WorkspaceFactRefs: hint.WorkspaceFactRef,
			Confidence: hint.Confidence, MatchBasis: hint.MatchBasis,
			CandidateOnly: hint.CandidateOnly, Instruction: instruction,
		}
		switch hint.MatchBasis {
		case "workspace_semantic":
			converted.Instruction = "semantic_fact_refs 通过当天名称、别名或稳定 workstream_cues 锚定该历史项目，workspace_fact_refs 仅表示同工作空间候选。这是高可信的父项目命名参考，但不是当天成果证据；无冲突时优先采用 canonical_name，并把具体工作保留为子成果。"
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
