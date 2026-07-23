package reportsource

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/sessiondigestv2"
	"github.com/lib/pq"
)

const reportDigestWaitTimeout = 60 * time.Minute

// Serializable report submission can conflict when the same prepared
// selection is attached by concurrent requests. Retry the whole transaction
// so the loser can observe and replay the winner through idempotency.
var reportRunSerializationRetryDelays = []time.Duration{
	50 * time.Millisecond,
	150 * time.Millisecond,
	300 * time.Millisecond,
}

var (
	ErrInvalidIdempotencyKey = errors.New("invalid report run idempotency key")
	ErrIdempotencyKeyReused  = errors.New("report run idempotency key was reused")
)

// RunSubmissionRequest contains the already-authorized, immutable inputs for
// one report run. RequestFingerprintInput must not contain database-derived
// source identities; ActiveDedupeInput contains all non-source execution facts.
type RunSubmissionRequest struct {
	UserID                  string
	ReportType              string
	Period                  Period
	SelectionID             string
	Sources                 []SourceInput
	RequireSources          bool
	BusinessType            string
	AgentID                 string
	ModelID                 string
	IdempotencyKey          string
	RequestFingerprintInput any
	ActiveDedupeInput       any
	InputRef                map[string]any
	ExecutionInput          map[string]any
}

type RunSubmissionResult struct {
	RunID             string
	SelectionID       string
	Replayed          bool
	SourceIdentitySHA string
}

type sourceIdentity struct {
	SliceID              string `json:"session_content_slice_id"`
	SessionID            string `json:"session_id"`
	SourceID             string `json:"source_id"`
	SessionRef           string `json:"session_ref_snapshot"`
	AgentType            string `json:"agent_type"`
	GenerationID         string `json:"source_generation_id"`
	ProjectionRevisionID string `json:"content_projection_revision_id"`
	ContentEpoch         int64  `json:"content_epoch_snapshot"`
	StartCursor          int64  `json:"start_cursor"`
	EndCursor            int64  `json:"end_cursor"`
}

func (s *Service) CreateReportRun(
	ctx context.Context,
	request RunSubmissionRequest,
) (RunSubmissionResult, error) {
	for attempt := 0; ; attempt++ {
		result, err := s.createReportRunOnce(ctx, request)
		if err == nil || !isSerializationConflict(err) || attempt >= len(reportRunSerializationRetryDelays) {
			return result, err
		}
		timer := time.NewTimer(reportRunSerializationRetryDelays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return RunSubmissionResult{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Service) createReportRunOnce(
	ctx context.Context,
	request RunSubmissionRequest,
) (RunSubmissionResult, error) {
	if s == nil || s.db == nil || strings.TrimSpace(request.UserID) == "" ||
		strings.TrimSpace(request.BusinessType) == "" || strings.TrimSpace(request.AgentID) == "" ||
		strings.TrimSpace(request.IdempotencyKey) == "" || strings.TrimSpace(request.ReportType) == "" {
		return RunSubmissionResult{}, ErrInvalidRequest
	}
	periodStart, periodEnd, err := parsePeriod(request.Period)
	if err != nil {
		return RunSubmissionResult{}, err
	}
	requestFingerprint, err := canonicalSHA256(request.RequestFingerprintInput)
	if err != nil {
		return RunSubmissionResult{}, ErrInvalidRequest
	}
	if existing, found, err := s.findIdempotentRun(
		ctx, request.UserID, request.BusinessType, request.IdempotencyKey, requestFingerprint,
	); err != nil || found {
		return existing, err
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return RunSubmissionResult{}, err
	}
	defer tx.Rollback()

	if existing, found, err := findIdempotentRunTx(
		ctx, tx, request.UserID, request.BusinessType, request.IdempotencyKey, requestFingerprint,
	); err != nil || found {
		return existing, err
	}

	selectionID := strings.TrimSpace(request.SelectionID)
	var selection Selection
	var items []SelectionItem
	selectionMode := "default"
	switch {
	case selectionID != "":
		selection, err = lockPreparedSelection(
			ctx, tx, request.UserID, selectionID, request.ReportType, periodStart, periodEnd,
		)
		if err != nil {
			return RunSubmissionResult{}, err
		}
		items = selection.Items
		selectionMode = selection.Mode
	case len(request.Sources) > 0:
		items, err = resolveExplicitItems(ctx, tx, request.UserID, request.Sources)
		selectionMode = "explicit"
	case request.RequireSources:
		items, err = resolveDefaultItems(ctx, tx, request.UserID, periodStart, periodEnd)
	default:
		items = nil
	}
	if err != nil {
		return RunSubmissionResult{}, err
	}
	items, identities := canonicalizeSelectionItems(items)
	if request.RequireSources && len(identities) == 0 {
		return RunSubmissionResult{}, ErrSourceUnavailable
	}
	sourceIdentitySHA, err := canonicalSHA256(struct {
		Items []sourceIdentity `json:"items"`
	}{Items: identities})
	if err != nil {
		return RunSubmissionResult{}, err
	}
	activeDedupeKey, err := canonicalSHA256(struct {
		Scope             any    `json:"scope"`
		SourceIdentitySHA string `json:"source_identity_set_sha256"`
	}{Scope: request.ActiveDedupeInput, SourceIdentitySHA: sourceIdentitySHA})
	if err != nil {
		return RunSubmissionResult{}, ErrInvalidRequest
	}

	inputRef := cloneAnyMap(request.InputRef)
	executionInput := cloneAnyMap(request.ExecutionInput)
	inputRef["report_source_read_mode"] = ReadModeDigestV2
	inputRef["report_source_digest_version"] = sessiondigestv2.Version
	inputRef["report_source_redaction_version"] = sessiondigestv2.RedactionVersion
	inputRef["report_context_schema_version"] = "report-context/v1"
	executionInput["report_source_read_mode"] = ReadModeDigestV2
	executionInput["report_source_digest_version"] = sessiondigestv2.Version
	executionInput["report_source_redaction_version"] = sessiondigestv2.RedactionVersion
	executionInput["report_context_schema_version"] = "report-context/v1"

	inputJSON, err := json.Marshal(inputRef)
	if err != nil {
		return RunSubmissionResult{}, ErrInvalidRequest
	}
	executionJSON, err := json.Marshal(executionInput)
	if err != nil {
		return RunSubmissionResult{}, ErrInvalidRequest
	}

	var runID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO ai_runs (
			user_id, business_type, runtime_type, agent_id, model_id, status,
			input_ref_json, execution_input_json, execution_stage, stage_updated_at,
			next_attempt_at, digest_wait_deadline_at, idempotency_key,
			active_dedupe_key, source_identity_set_sha256, request_fingerprint
		) VALUES (
			$1, $2, 'managed_session', $3, NULLIF($4, ''), 'pending',
			$5, $6, 'waiting_digest', now(), now(), now() + make_interval(secs => $7),
			$8, $9, $10, $11
		)
		RETURNING id::text`,
		request.UserID, request.BusinessType, request.AgentID, request.ModelID,
		inputJSON, executionJSON, int(reportDigestWaitTimeout.Seconds()), request.IdempotencyKey,
		activeDedupeKey, sourceIdentitySHA, requestFingerprint,
	).Scan(&runID)
	if err != nil {
		if isUniqueViolation(err) {
			_ = tx.Rollback()
			if existing, found, lookupErr := s.findIdempotentRun(
				ctx, request.UserID, request.BusinessType, request.IdempotencyKey, requestFingerprint,
			); lookupErr != nil || found {
				return existing, lookupErr
			}
			return s.findActiveDedupeRun(ctx, request.UserID, request.BusinessType, activeDedupeKey)
		}
		return RunSubmissionResult{}, err
	}

	if request.RequireSources || selectionID != "" || len(request.Sources) > 0 {
		if selectionID == "" {
			selection, err = insertSelection(
				ctx, tx, request.UserID, request.ReportType, periodStart, periodEnd, selectionMode, items,
			)
			if err != nil {
				return RunSubmissionResult{}, err
			}
			selectionID = selection.ID
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE report_source_selections
			SET status = 'attached', attached_run_id = $2, attached_at = now(), expires_at = NULL,
				required_read_mode = CASE WHEN digest_frozen_at IS NULL THEN 'digest_v2' ELSE required_read_mode END
			WHERE id = $1 AND status = 'prepared' AND attached_run_id IS NULL
				AND (digest_frozen_at IS NULL OR digest_version_snapshot = $3)`,
			selectionID, runID, sessiondigestv2.Version,
		)
		if err != nil {
			return RunSubmissionResult{}, err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return RunSubmissionResult{}, ErrSelectionConflict
		}
		inputRef["report_source_selection_id"] = selectionID
		executionInput["report_source_selection_id"] = selectionID
		inputJSON, _ = json.Marshal(inputRef)
		executionJSON, _ = json.Marshal(executionInput)
		if _, err := tx.ExecContext(ctx, `
			UPDATE ai_runs SET input_ref_json = $2, execution_input_json = $3 WHERE id = $1`,
			runID, inputJSON, executionJSON,
		); err != nil {
			return RunSubmissionResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return RunSubmissionResult{}, err
	}
	return RunSubmissionResult{
		RunID: runID, SelectionID: selectionID, SourceIdentitySHA: sourceIdentitySHA,
	}, nil
}

func canonicalizeSelectionItems(items []SelectionItem) ([]SelectionItem, []sourceIdentity) {
	type pair struct {
		item     SelectionItem
		identity sourceIdentity
		key      string
	}
	pairs := make([]pair, 0, len(items))
	for _, item := range items {
		identity := sourceIdentity{
			SliceID: item.SliceID, SessionID: item.SessionID, SourceID: item.SourceID,
			SessionRef: item.SessionRef, AgentType: item.AgentType,
			GenerationID: item.GenerationID, ProjectionRevisionID: item.ProjectionRevision,
			ContentEpoch: item.ContentEpoch, StartCursor: item.StartCursor, EndCursor: item.EndCursor,
		}
		encoded, _ := json.Marshal(identity)
		pairs = append(pairs, pair{item: item, identity: identity, key: string(encoded)})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].key < pairs[j].key })
	canonicalItems := make([]SelectionItem, 0, len(pairs))
	identities := make([]sourceIdentity, 0, len(pairs))
	lastKey := ""
	for _, candidate := range pairs {
		if candidate.key == lastKey {
			continue
		}
		lastKey = candidate.key
		canonicalItems = append(canonicalItems, candidate.item)
		identities = append(identities, candidate.identity)
	}
	return canonicalItems, identities
}

func canonicalSHA256(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func cloneAnyMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input)+4)
	for key, value := range input {
		output[key] = value
	}
	return output
}

func (s *Service) findIdempotentRun(
	ctx context.Context,
	userID, businessType, idempotencyKey, requestFingerprint string,
) (RunSubmissionResult, bool, error) {
	return findIdempotentRunRow(
		s.db.QueryRowContext(ctx, `
			SELECT r.id::text, r.request_fingerprint,
				COALESCE(sel.id::text, ''), r.source_identity_set_sha256
			FROM ai_runs r
			LEFT JOIN report_source_selections sel ON sel.attached_run_id = r.id
			WHERE r.user_id = $1 AND r.business_type = $2 AND r.idempotency_key = $3`,
			userID, businessType, idempotencyKey),
		requestFingerprint,
	)
}

func findIdempotentRunTx(
	ctx context.Context,
	tx *sql.Tx,
	userID, businessType, idempotencyKey, requestFingerprint string,
) (RunSubmissionResult, bool, error) {
	return findIdempotentRunRow(
		tx.QueryRowContext(ctx, `
			SELECT r.id::text, r.request_fingerprint,
				COALESCE(sel.id::text, ''), r.source_identity_set_sha256
			FROM ai_runs r
			LEFT JOIN report_source_selections sel ON sel.attached_run_id = r.id
			WHERE r.user_id = $1 AND r.business_type = $2 AND r.idempotency_key = $3
			FOR UPDATE OF r`, userID, businessType, idempotencyKey),
		requestFingerprint,
	)
}

type rowScanner interface {
	Scan(...any) error
}

func findIdempotentRunRow(
	row rowScanner,
	requestFingerprint string,
) (RunSubmissionResult, bool, error) {
	var result RunSubmissionResult
	var storedFingerprint string
	if err := row.Scan(
		&result.RunID, &storedFingerprint, &result.SelectionID, &result.SourceIdentitySHA,
	); errors.Is(err, sql.ErrNoRows) {
		return RunSubmissionResult{}, false, nil
	} else if err != nil {
		return RunSubmissionResult{}, false, err
	}
	if storedFingerprint != requestFingerprint {
		return RunSubmissionResult{}, true, ErrIdempotencyKeyReused
	}
	result.Replayed = true
	return result, true, nil
}

func (s *Service) findActiveDedupeRun(
	ctx context.Context,
	userID, businessType, activeDedupeKey string,
) (RunSubmissionResult, error) {
	var result RunSubmissionResult
	err := s.db.QueryRowContext(ctx, `
		SELECT r.id::text, COALESCE(sel.id::text, ''), r.source_identity_set_sha256
		FROM ai_runs r
		LEFT JOIN report_source_selections sel ON sel.attached_run_id = r.id
		WHERE r.user_id = $1 AND r.business_type = $2 AND r.active_dedupe_key = $3
			AND r.status IN ('pending', 'running')`,
		userID, businessType, activeDedupeKey,
	).Scan(&result.RunID, &result.SelectionID, &result.SourceIdentitySHA)
	if err != nil {
		return RunSubmissionResult{}, err
	}
	result.Replayed = true
	return result, nil
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func isSerializationConflict(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "40001"
}
