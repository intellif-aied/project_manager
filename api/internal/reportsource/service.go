package reportsource

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/contentreader"
	"github.com/lib/pq"
)

var (
	ErrInvalidRequest                   = errors.New("invalid report source request")
	ErrSelectionNotFound                = errors.New("report source selection not found")
	ErrSourceUnavailable                = errors.New("report source content is unavailable")
	ErrSelectionConflict                = errors.New("report source selection cannot be attached")
	ErrSelectionMismatch                = errors.New("report source selection does not match the managed report run")
	ErrSourceIncomplete                 = errors.New("report source selection has not been read completely")
	ErrContentItemTooLarge              = errors.New("report source content item exceeds the page limit")
	ErrContentReaderUnavailable         = errors.New("report source content reader is unavailable")
	ErrLargeContextConfirmationRequired = errors.New("large report context confirmation is required")
)

const preparedSelectionTTL = 30 * time.Minute
const reportSourceCursorTTL = time.Hour
const reportSourcePageMaxEvents = 100
const reportSourcePageMaxBytes = 512 << 10

type Service struct {
	db     *sql.DB
	reader ContentReader
	config Config
}

type ContentReader interface {
	Stream(
		context.Context,
		contentreader.Request,
		func(contentreader.Event) error,
	) (contentreader.Result, error)
}

func NewService(database *sql.DB) (*Service, error) {
	return NewServiceWithConfigAndReader(database, nil, DefaultConfig())
}

func NewServiceWithReader(database *sql.DB, reader ContentReader) (*Service, error) {
	return NewServiceWithConfigAndReader(database, reader, DefaultConfig())
}

func NewServiceWithConfig(database *sql.DB, config Config) (*Service, error) {
	return NewServiceWithConfigAndReader(database, nil, config)
}

func NewServiceWithConfigAndReader(database *sql.DB, reader ContentReader, config Config) (*Service, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	normalized, err := config.Normalized()
	if err != nil {
		return nil, err
	}
	return &Service{db: database, reader: reader, config: normalized}, nil
}

type Period struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type SourceInput struct {
	SliceKey string `json:"slice_key"`
}

type SelectionItem struct {
	ID                 string    `json:"id"`
	SessionRef         string    `json:"session_ref"`
	AgentType          string    `json:"agent_type"`
	ActivityStart      time.Time `json:"activity_start_at"`
	ActivityEnd        time.Time `json:"activity_end_at"`
	Summary            string    `json:"summary"`
	ContentEventCount  int64     `json:"content_event_count"`
	ContentStatus      string    `json:"content_status"`
	ContentEpoch       int64     `json:"-"`
	SliceID            string    `json:"-"`
	SessionID          string    `json:"-"`
	SourceID           string    `json:"-"`
	GenerationID       string    `json:"-"`
	ProjectionRevision string    `json:"-"`
	StartCursor        int64     `json:"-"`
	EndCursor          int64     `json:"-"`
}

type Selection struct {
	ID                string          `json:"selection_id"`
	ReportType        string          `json:"report_type"`
	Period            Period          `json:"period"`
	Mode              string          `json:"selection_mode"`
	Status            string          `json:"status"`
	ContentSnapshotAt time.Time       `json:"content_snapshot_at"`
	RequiredReadMode  string          `json:"required_read_mode,omitempty"`
	ContextBytes      int             `json:"context_bytes,omitempty"`
	WarningRequired   bool            `json:"warning_required"`
	WarningCode       string          `json:"warning_code,omitempty"`
	Items             []SelectionItem `json:"items"`
}

type LargeContextConfirmationError struct {
	SelectionID  string
	ContextBytes int
}

func (e *LargeContextConfirmationError) Error() string {
	return ErrLargeContextConfirmationRequired.Error()
}

func (e *LargeContextConfirmationError) Unwrap() error {
	return ErrLargeContextConfirmationRequired
}

type Candidate struct {
	SliceKey           string    `json:"slice_key"`
	SessionRef         string    `json:"session_ref"`
	AgentType          string    `json:"agent_type"`
	Summary            string    `json:"summary"`
	LastActivityAt     time.Time `json:"last_activity_at"`
	ActivityStartAt    time.Time `json:"activity_start_at"`
	ActivityEndAt      time.Time `json:"activity_end_at"`
	CWD                string    `json:"cwd"`
	Models             []string  `json:"models"`
	ContentStatus      string    `json:"content_status"`
	ContentIndexStatus string    `json:"content_index_status"`
	AvailableThroughAt time.Time `json:"available_through_at"`
}

type CandidateQuery struct {
	Query        string
	ActivityFrom *time.Time
	ActivityTo   *time.Time
	Page         int
	PageSize     int
}

type CandidatePage struct {
	Items    []Candidate `json:"items"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
	Total    int         `json:"total"`
}

type ContentEvent struct {
	OccurredAt time.Time       `json:"occurred_at"`
	EventType  string          `json:"event_type"`
	Summary    string          `json:"summary,omitempty"`
	Excerpt    string          `json:"excerpt,omitempty"`
	Payload    json.RawMessage `json:"payload"`
}

type ContentItem struct {
	SessionRef        string         `json:"session_ref"`
	AgentType         string         `json:"agent_type"`
	ActivityStartAt   time.Time      `json:"activity_start_at"`
	ActivityEndAt     time.Time      `json:"activity_end_at"`
	Summary           string         `json:"summary,omitempty"`
	ContentQuality    string         `json:"content_quality"`
	ContentEventCount int64          `json:"content_event_count"`
	Events            []ContentEvent `json:"events"`
}

type ContentPage struct {
	SourceMode      string          `json:"source_mode"`
	SelectionID     string          `json:"selection_id"`
	ContentSnapshot time.Time       `json:"content_snapshot_at"`
	Completeness    string          `json:"completeness"`
	ReturnedCount   int             `json:"returned_item_count"`
	ReturnedEvents  int             `json:"returned_event_count"`
	HasMore         bool            `json:"has_more"`
	NextCursor      *string         `json:"next_cursor"`
	Items           []ContentItem   `json:"items"`
	FrozenPayload   json.RawMessage `json:"-"`
}

func (s *Service) ListCandidates(ctx context.Context, userID string, query CandidateQuery) (CandidatePage, error) {
	if strings.TrimSpace(userID) == "" {
		return CandidatePage{}, ErrInvalidRequest
	}
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 || query.PageSize > 100 {
		query.PageSize = 20
	}
	search := "%" + strings.ToLower(strings.TrimSpace(query.Query)) + "%"
	var from, to any
	if query.ActivityFrom != nil {
		from = query.ActivityFrom.UTC()
	}
	if query.ActivityTo != nil {
		to = query.ActivityTo.UTC()
	}
	const candidateCTE = `
		WITH valid_sources AS MATERIALIZED (
			SELECT s.id AS session_id, s.user_id, s.session_ref, s.agent_type,
				COALESCE(s.cwd, '') AS cwd, COALESCE(s.models, '{}') AS models,
				s.content_status, s.content_epoch, src.id AS source_id,
				revision.generation_id, revision.id AS revision_id
			FROM sessions s
			JOIN session_sources src
				ON src.session_id = s.id AND src.active_content_projection_revision_id IS NOT NULL
			JOIN session_content_projection_revisions revision
				ON revision.id = src.active_content_projection_revision_id
				AND revision.status = 'active'
			WHERE s.user_id = $1 AND s.content_status = 'available'
		), candidates AS MATERIALIZED (
			SELECT catalog.slice_id::text AS slice_key, valid.session_ref, valid.agent_type,
				catalog.summary, catalog.activity_start_at, catalog.activity_end_at,
				valid.cwd, valid.models, valid.content_status
			FROM report_source_slice_catalog catalog
			JOIN valid_sources valid
				ON valid.session_id = catalog.session_id
				AND valid.user_id = catalog.user_id
				AND valid.source_id = catalog.source_id
				AND valid.generation_id = catalog.generation_id
				AND valid.revision_id = catalog.content_projection_revision_id
				AND valid.content_epoch = catalog.content_epoch
			WHERE catalog.user_id = $1 AND catalog.status = 'ready'
				AND ($3::timestamptz IS NULL OR catalog.activity_end_at >= $3)
				AND ($4::timestamptz IS NULL OR catalog.activity_start_at <= $4)
				AND ($2 = '%%' OR lower(valid.session_ref) LIKE $2 OR lower(catalog.summary) LIKE $2)
		)`
	var total int
	rows, err := s.db.QueryContext(ctx, candidateCTE+`,
		paged AS (
			SELECT * FROM candidates
			ORDER BY activity_end_at DESC, session_ref ASC, slice_key ASC
			LIMIT $5 OFFSET $6
		)
		SELECT p.slice_key, p.session_ref, p.agent_type, p.summary,
			p.activity_end_at AS last_activity_at, p.activity_start_at,
			p.activity_end_at, p.cwd, p.models, p.content_status, p.activity_end_at AS available_through_at,
			(SELECT COUNT(*) FROM candidates) AS total_count
		FROM paged p
		ORDER BY p.activity_end_at DESC, p.session_ref ASC, p.slice_key ASC`, userID, search, from, to, query.PageSize, (query.Page-1)*query.PageSize)
	if err != nil {
		return CandidatePage{}, err
	}
	defer rows.Close()
	items := make([]Candidate, 0, query.PageSize)
	for rows.Next() {
		var item Candidate
		var models pq.StringArray
		var totalCount int
		if err := rows.Scan(&item.SliceKey, &item.SessionRef, &item.AgentType, &item.Summary, &item.LastActivityAt,
			&item.ActivityStartAt, &item.ActivityEndAt, &item.CWD, &models,
			&item.ContentStatus, &item.AvailableThroughAt, &totalCount); err != nil {
			return CandidatePage{}, err
		}
		if totalCount > total {
			total = totalCount
		}
		item.Models = []string(models)
		item.ContentIndexStatus = "ready"
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return CandidatePage{}, err
	}
	if len(items) == 0 {
		if err := s.db.QueryRowContext(ctx, candidateCTE+` SELECT COUNT(*) FROM candidates`, userID, search, from, to).Scan(&total); err != nil {
			return CandidatePage{}, err
		}
	}
	return CandidatePage{Items: items, Page: query.Page, PageSize: query.PageSize, Total: total}, nil
}

func (s *Service) CreateExplicit(
	ctx context.Context,
	userID, reportType string,
	period Period,
	inputs []SourceInput,
) (Selection, error) {
	if !validPersonalReportType(reportType) || strings.TrimSpace(userID) == "" || len(inputs) == 0 {
		return Selection{}, ErrInvalidRequest
	}
	periodStart, periodEnd, err := parsePeriod(period)
	if err != nil {
		return Selection{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Selection{}, err
	}
	defer tx.Rollback()
	items, err := resolveExplicitItems(ctx, tx, userID, inputs)
	if err != nil {
		return Selection{}, err
	}
	selection, err := insertSelection(ctx, tx, userID, reportType, periodStart, periodEnd, "explicit", items)
	if err != nil {
		return Selection{}, err
	}
	if err := tx.Commit(); err != nil {
		return Selection{}, err
	}
	return selection, nil
}

// PrepareExplicitSelection freezes the exact representation that a managed
// report Agent will read. It is called by the selection API so the UI can make
// a warning decision from the real context size rather than raw Session size
// or a token estimate. The selection remains prepared and can be attached once.
func (s *Service) PrepareExplicitSelection(
	ctx context.Context,
	userID, reportType string,
	period Period,
	selectionID string,
) (Selection, error) {
	if !validPersonalReportType(reportType) || strings.TrimSpace(userID) == "" ||
		strings.TrimSpace(selectionID) == "" {
		return Selection{}, ErrInvalidRequest
	}
	periodStart, periodEnd, err := parsePeriod(period)
	if err != nil {
		return Selection{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Selection{}, err
	}
	defer tx.Rollback()
	selection, err := lockPreparedSelection(
		ctx, tx, userID, selectionID, reportType, periodStart, periodEnd,
	)
	if err != nil {
		return Selection{}, err
	}
	if selection.Mode != "explicit" {
		return Selection{}, ErrSelectionConflict
	}
	if err := validateSelectionSourcesAvailable(ctx, tx, selection.ID); err != nil {
		return Selection{}, err
	}
	if err := s.ensureSelectionContextFrozen(ctx, tx, &selection, userID); err != nil {
		return Selection{}, err
	}
	if err := tx.Commit(); err != nil {
		return Selection{}, err
	}
	return selection, nil
}

func (s *Service) CreateAttachedRun(
	ctx context.Context,
	userID, reportType string,
	period Period,
	selectionID, businessType, agentID, modelID string,
	largeContextConfirmed bool,
	inputRef map[string]any,
) (string, Selection, error) {
	if !validPersonalReportType(reportType) || strings.TrimSpace(userID) == "" {
		return "", Selection{}, ErrInvalidRequest
	}
	periodStart, periodEnd, err := parsePeriod(period)
	if err != nil {
		return "", Selection{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", Selection{}, err
	}
	defer tx.Rollback()
	var selection Selection
	if strings.TrimSpace(selectionID) == "" {
		items, err := resolveDefaultItems(ctx, tx, userID, periodStart, periodEnd)
		if err != nil {
			return "", Selection{}, err
		}
		selection, err = insertSelection(ctx, tx, userID, reportType, periodStart, periodEnd, "default", items)
		if err != nil {
			return "", Selection{}, err
		}
	} else {
		selection, err = lockPreparedSelection(ctx, tx, userID, selectionID, reportType, periodStart, periodEnd)
		if err != nil {
			return "", Selection{}, err
		}
	}
	if err := validateSelectionSourcesAvailable(ctx, tx, selection.ID); err != nil {
		return "", Selection{}, err
	}
	if err := s.ensureSelectionContextFrozen(ctx, tx, &selection, userID); err != nil {
		return "", Selection{}, err
	}
	if selection.WarningRequired && !largeContextConfirmed {
		// Keep the exact frozen prepared selection for the confirmation retry.
		// No ai_run, credential, or managed Agent Session has been created yet.
		if err := tx.Commit(); err != nil {
			return "", Selection{}, err
		}
		return "", selection, &LargeContextConfirmationError{
			SelectionID:  selection.ID,
			ContextBytes: selection.ContextBytes,
		}
	}
	requiredReadMode := selection.RequiredReadMode
	if inputRef == nil {
		inputRef = map[string]any{}
	}
	inputRef["report_source_selection_id"] = selection.ID
	inputRef["report_source_read_mode"] = requiredReadMode
	if requiredReadMode == ReadModeDigestV1 || requiredReadMode == ReadModeDigestV2 {
		inputRef["report_source_digest_version"] = s.config.DigestVersion
		inputRef["report_source_redaction_version"] = s.config.RedactionVersion
	}
	inputJSON, err := json.Marshal(inputRef)
	if err != nil {
		return "", Selection{}, err
	}
	var runID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO ai_runs (
			user_id, business_type, runtime_type, agent_id, model_id,
			status, input_ref_json, started_at
		) VALUES ($1, $2, 'managed_session', $3, NULLIF($4, ''), 'pending', $5, now())
		RETURNING id::text`, userID, businessType, agentID, modelID, inputJSON).Scan(&runID)
	if err != nil {
		return "", Selection{}, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE report_source_selections
		SET status = 'attached', attached_run_id = $2, attached_at = now(), expires_at = NULL
		WHERE id = $1 AND status = 'prepared' AND attached_run_id IS NULL`, selection.ID, runID)
	if err != nil {
		return "", Selection{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return "", Selection{}, ErrSelectionConflict
	}
	selection.Status = "attached"
	if err := tx.Commit(); err != nil {
		return "", Selection{}, err
	}
	return runID, selection, nil
}

func (s *Service) ensureSelectionContextFrozen(
	ctx context.Context,
	tx *sql.Tx,
	selection *Selection,
	userID string,
) error {
	if selection == nil {
		return ErrSelectionConflict
	}
	requiredReadMode := s.config.RequiredReadMode(userID)
	needsFreeze := selection.RequiredReadMode != requiredReadMode
	if requiredReadMode == ReadModeFull {
		// A newly inserted selection also starts with required_read_mode=full,
		// so run the idempotent full-mode freeze to clear any stale snapshots.
		needsFreeze = true
	} else if selection.ContextBytes <= 0 {
		needsFreeze = true
	}
	if needsFreeze {
		if err := s.freezeSelectionForRun(ctx, tx, *selection, requiredReadMode); err != nil {
			return err
		}
	}
	var contextBytes int
	var storedMode string
	err := tx.QueryRowContext(ctx, `
		SELECT required_read_mode, COALESCE(selection_digest_bytes, 0)
		FROM report_source_selections
		WHERE id = $1 AND status = 'prepared'`, selection.ID,
	).Scan(&storedMode, &contextBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSelectionConflict
	}
	if err != nil {
		return err
	}
	if storedMode != requiredReadMode ||
		((storedMode == ReadModeDigestV1 || storedMode == ReadModeDigestV2) && contextBytes <= 0) {
		return ErrDigestCorrupt
	}
	selection.RequiredReadMode = storedMode
	selection.ContextBytes = contextBytes
	applySelectionContextWarning(selection)
	return nil
}

func applySelectionContextWarning(selection *Selection) {
	if selection == nil {
		return
	}
	selection.WarningRequired = selection.ContextBytes > LargeContextWarningBytes
	selection.WarningCode = ""
	if selection.WarningRequired {
		selection.WarningCode = LargeContextWarningCode
	}
}

func (s *Service) ReadAttachedSelection(
	ctx context.Context,
	userID, selectionID, runID, reportType string,
	period Period,
	pageCursor string,
) (ContentPage, error) {
	periodStart, periodEnd, err := parsePeriod(period)
	if err != nil || !validPersonalReportType(reportType) || strings.TrimSpace(runID) == "" {
		return ContentPage{}, ErrSelectionMismatch
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: false})
	if err != nil {
		return ContentPage{}, err
	}
	defer tx.Rollback()
	var mode, status, requiredReadMode string
	var snapshotAt time.Time
	var storedStart, storedEnd time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT selection_mode, status, required_read_mode, content_snapshot_at, period_start, period_end
		FROM report_source_selections
		WHERE id = $1 AND user_id = $2 AND attached_run_id = $3`, selectionID, userID, runID).Scan(
		&mode, &status, &requiredReadMode, &snapshotAt, &storedStart, &storedEnd,
	)
	if errors.Is(err, sql.ErrNoRows) || status != "attached" {
		return ContentPage{}, ErrSelectionMismatch
	}
	if err != nil {
		return ContentPage{}, err
	}
	var runReportType string
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(input_ref_json->>'report_type', '') FROM ai_runs
		WHERE id = $1 AND user_id = $2 AND business_type = 'report_agent_run'`, runID, userID).Scan(&runReportType); err != nil {
		return ContentPage{}, ErrSelectionMismatch
	}
	if runReportType != reportType || !sameDate(storedStart, periodStart) || !sameDate(storedEnd, periodEnd) {
		return ContentPage{}, ErrSelectionMismatch
	}
	if err := validateSelectionSourcesAvailable(ctx, tx, selectionID); err != nil {
		return ContentPage{}, err
	}
	if requiredReadMode == ReadModeDigestV1 || requiredReadMode == ReadModeDigestV2 {
		if strings.TrimSpace(pageCursor) != "" {
			return ContentPage{}, ErrReadModeMismatch
		}
		if requiredReadMode == ReadModeDigestV2 {
			return s.readFrozenDigestV2Selection(ctx, tx, userID, selectionID, runID)
		}
		return s.readFrozenDigestSelection(ctx, tx, userID, selectionID, runID)
	}
	if requiredReadMode != ReadModeFull {
		return ContentPage{}, ErrReadModeMismatch
	}
	if s.reader == nil {
		return ContentPage{}, ErrContentReaderUnavailable
	}

	itemOffset := 0
	nextEventCursor := int64(0)
	if strings.TrimSpace(pageCursor) != "" {
		err := tx.QueryRowContext(ctx, `
			SELECT item_offset, next_event_cursor FROM report_source_page_cursors
			WHERE id = $1 AND selection_id = $2 AND user_id = $3 AND expires_at > now()`,
			pageCursor, selectionID, userID).Scan(&itemOffset, &nextEventCursor)
		if errors.Is(err, sql.ErrNoRows) {
			return ContentPage{}, ErrSelectionMismatch
		}
		if err != nil {
			return ContentPage{}, err
		}
	}
	items, err := loadInternalSelectionItems(ctx, tx, selectionID)
	if err != nil {
		return ContentPage{}, err
	}
	page := ContentPage{
		SourceMode: mode, SelectionID: selectionID, ContentSnapshot: snapshotAt,
		Completeness: "complete", Items: []ContentItem{},
	}
	pageBytes := 0
	nextOffset := itemOffset
	nextCursorValue := nextEventCursor
	for nextOffset < len(items) {
		if page.ReturnedEvents >= reportSourcePageMaxEvents {
			break
		}
		item := items[nextOffset]
		cursor := item.StartCursor
		if nextOffset == itemOffset && nextEventCursor > cursor {
			cursor = nextEventCursor
		}
		if cursor > item.EndCursor {
			return ContentPage{}, ErrSelectionMismatch
		}
		if cursor == item.EndCursor {
			nextOffset++
			nextCursorValue = 0
			continue
		}
		plan, err := planContentRange(
			ctx, tx, item.ProjectionRevision, cursor, item.EndCursor,
			reportSourcePageMaxEvents-page.ReturnedEvents,
		)
		if err != nil {
			return ContentPage{}, err
		}
		contentItem := ContentItem{
			SessionRef: item.SessionRef, AgentType: item.AgentType,
			ActivityStartAt: item.ActivityStart, ActivityEndAt: item.ActivityEnd,
			Summary: item.Summary, ContentQuality: "exact", ContentEventCount: item.ContentEventCount,
			Events: []ContentEvent{},
		}
		pageOverflow := false
		overflowCursor := int64(0)
		readResult, err := s.reader.Stream(ctx, contentreader.Request{
			RevisionID:     item.ProjectionRevision,
			StartCursor:    cursor,
			EndCursor:      plan.EndCursor,
			ValidationMode: contentreader.ValidationIndexedRange,
		}, func(source contentreader.Event) error {
			event := ContentEvent{
				OccurredAt: source.OccurredAt,
				EventType:  source.EventType,
				Summary:    source.Summary,
				Excerpt:    source.Excerpt,
				Payload:    redactReportUsageMetrics(source.Payload),
			}
			encoded, err := json.Marshal(event)
			if err != nil {
				return err
			}
			if len(encoded) > reportSourcePageMaxBytes {
				return ErrContentItemTooLarge
			}
			if pageOverflow {
				return nil
			}
			if pageBytes+len(encoded) > reportSourcePageMaxBytes {
				pageOverflow = true
				overflowCursor = source.SourceStartCursor
				return nil
			}
			contentItem.Events = append(contentItem.Events, event)
			page.ReturnedEvents++
			pageBytes += len(encoded)
			nextCursorValue = source.SourceEndCursor
			return nil
		})
		if err != nil {
			return ContentPage{}, err
		}
		if readResult.EventCount != int64(plan.EventCount) {
			return ContentPage{}, ErrSourceUnavailable
		}
		if len(contentItem.Events) > 0 {
			page.Items = append(page.Items, contentItem)
		}
		if pageOverflow {
			nextCursorValue = overflowCursor
			break
		}
		if plan.HasMore {
			break
		}
		nextOffset++
		nextCursorValue = 0
	}
	page.ReturnedCount = len(page.Items)
	if nextOffset < len(items) {
		var cursorID string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO report_source_page_cursors (
				selection_id, user_id, item_offset, next_event_cursor, expires_at
			) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (selection_id, user_id, item_offset, next_event_cursor)
			DO UPDATE SET expires_at = GREATEST(
				report_source_page_cursors.expires_at,
				EXCLUDED.expires_at
			)
			RETURNING id::text`,
			selectionID, userID, nextOffset, nextCursorValue, time.Now().UTC().Add(reportSourceCursorTTL),
		).Scan(&cursorID); err != nil {
			return ContentPage{}, err
		}
		page.HasMore = true
		page.Completeness = "partial"
		page.NextCursor = &cursorID
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE report_source_selections
			SET read_completed_at = COALESCE(read_completed_at, now()), read_completed_mode = 'full'
			WHERE id = $1 AND user_id = $2 AND attached_run_id = $3 AND status = 'attached'`,
			selectionID, userID, runID,
		); err != nil {
			return ContentPage{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ContentPage{}, err
	}
	return page, nil
}

type contentRangePlan struct {
	EndCursor  int64
	EventCount int
	HasMore    bool
}

func planContentRange(
	ctx context.Context,
	tx *sql.Tx,
	revisionID string,
	startCursor, itemEndCursor int64,
	maxEvents int,
) (contentRangePlan, error) {
	if tx == nil || strings.TrimSpace(revisionID) == "" || startCursor < 0 ||
		itemEndCursor <= startCursor || maxEvents <= 0 {
		return contentRangePlan{}, ErrInvalidRequest
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT source_start_cursor, source_end_cursor
		FROM session_content_events
		WHERE content_projection_revision_id = $1
			AND source_start_cursor >= $2 AND source_end_cursor <= $3
		ORDER BY source_start_cursor, source_end_cursor, id
		LIMIT $4`, revisionID, startCursor, itemEndCursor, maxEvents+1)
	if err != nil {
		return contentRangePlan{}, err
	}
	defer rows.Close()
	type boundary struct {
		start int64
		end   int64
	}
	boundaries := make([]boundary, 0, maxEvents+1)
	for rows.Next() {
		var current boundary
		if err := rows.Scan(&current.start, &current.end); err != nil {
			return contentRangePlan{}, err
		}
		if current.start < startCursor || current.end <= current.start || current.end > itemEndCursor {
			return contentRangePlan{}, ErrSourceUnavailable
		}
		boundaries = append(boundaries, current)
	}
	if err := rows.Err(); err != nil {
		return contentRangePlan{}, err
	}
	plan := contentRangePlan{EndCursor: itemEndCursor, EventCount: len(boundaries)}
	if len(boundaries) > maxEvents {
		plan.EventCount = maxEvents
		plan.EndCursor = boundaries[maxEvents-1].end
		plan.HasMore = true
	}
	return plan, nil
}

func loadInternalSelectionItems(ctx context.Context, tx *sql.Tx, selectionID string) ([]SelectionItem, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id::text, COALESCE(session_content_slice_id::text, ''), session_id::text, session_ref_snapshot, agent_type, source_id::text,
			source_generation_id::text, content_projection_revision_id::text, start_cursor,
			end_cursor, activity_start_at, activity_end_at, COALESCE(summary_snapshot, ''),
			content_status_snapshot, content_epoch_snapshot, content_event_count
		FROM report_source_selection_items WHERE selection_id = $1 ORDER BY created_at, id`, selectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SelectionItem{}
	for rows.Next() {
		var item SelectionItem
		if err := rows.Scan(&item.ID, &item.SliceID, &item.SessionID, &item.SessionRef, &item.AgentType,
			&item.SourceID, &item.GenerationID, &item.ProjectionRevision, &item.StartCursor,
			&item.EndCursor, &item.ActivityStart, &item.ActivityEnd, &item.Summary,
			&item.ContentStatus, &item.ContentEpoch, &item.ContentEventCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func validPersonalReportType(reportType string) bool {
	return reportType == "personal_daily" || reportType == "personal_weekly"
}

func parsePeriod(period Period) (time.Time, time.Time, error) {
	start, err := time.Parse("2006-01-02", strings.TrimSpace(period.Start))
	if err != nil {
		return time.Time{}, time.Time{}, ErrInvalidRequest
	}
	end, err := time.Parse("2006-01-02", strings.TrimSpace(period.End))
	if err != nil || end.Before(start) {
		return time.Time{}, time.Time{}, ErrInvalidRequest
	}
	return start, end, nil
}

func resolveExplicitItems(ctx context.Context, tx *sql.Tx, userID string, inputs []SourceInput) ([]SelectionItem, error) {
	resolved := make([]SelectionItem, 0, len(inputs))
	for _, input := range inputs {
		input.SliceKey = strings.TrimSpace(input.SliceKey)
		if input.SliceKey == "" {
			return nil, ErrInvalidRequest
		}
		var item SelectionItem
		err := tx.QueryRowContext(ctx, `
			SELECT catalog.slice_id::text, s.id::text, s.session_ref, s.agent_type,
				catalog.source_id::text, catalog.generation_id::text,
				catalog.content_projection_revision_id::text,
				catalog.start_cursor, catalog.end_cursor, catalog.activity_start_at,
				catalog.activity_end_at, catalog.summary, s.content_status,
				s.content_epoch, catalog.event_count
			FROM report_source_slice_catalog catalog
			JOIN sessions s
				ON s.id = catalog.session_id AND s.user_id = catalog.user_id
			JOIN session_sources source
				ON source.id = catalog.source_id AND source.session_id = s.id
				AND source.active_generation_id = catalog.generation_id
				AND source.active_content_projection_revision_id = catalog.content_projection_revision_id
			JOIN session_source_generations generation
				ON generation.id = catalog.generation_id AND generation.status = 'active'
			JOIN session_content_projection_revisions revision
				ON revision.id = catalog.content_projection_revision_id
				AND revision.generation_id = generation.id AND revision.status = 'active'
			WHERE catalog.user_id = $1 AND catalog.slice_id = $2
				AND catalog.status = 'ready'
				AND s.content_status = 'available'
				AND s.content_epoch = catalog.content_epoch`, userID, input.SliceKey).Scan(
			&item.SliceID, &item.SessionID, &item.SessionRef, &item.AgentType, &item.SourceID, &item.GenerationID,
			&item.ProjectionRevision, &item.StartCursor, &item.EndCursor, &item.ActivityStart,
			&item.ActivityEnd, &item.Summary, &item.ContentStatus, &item.ContentEpoch,
			&item.ContentEventCount,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSourceUnavailable
		}
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, item)
	}
	return resolved, nil
}

func resolveDefaultItems(ctx context.Context, tx *sql.Tx, userID string, start, end time.Time) ([]SelectionItem, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT catalog.slice_id::text, s.id::text, s.session_ref, s.agent_type,
			catalog.source_id::text, catalog.generation_id::text,
			catalog.content_projection_revision_id::text,
			catalog.start_cursor, catalog.end_cursor, catalog.activity_start_at,
			catalog.activity_end_at, catalog.summary, s.content_status,
			s.content_epoch, catalog.event_count
		FROM report_source_slice_catalog catalog
		JOIN sessions s
			ON s.id = catalog.session_id AND s.user_id = catalog.user_id
		JOIN session_sources source
			ON source.id = catalog.source_id AND source.session_id = s.id
			AND source.active_generation_id = catalog.generation_id
			AND source.active_content_projection_revision_id = catalog.content_projection_revision_id
		JOIN session_source_generations generation
			ON generation.id = catalog.generation_id AND generation.status = 'active'
		JOIN session_content_projection_revisions revision
			ON revision.id = catalog.content_projection_revision_id
			AND revision.generation_id = generation.id AND revision.status = 'active'
		WHERE catalog.user_id = $1 AND catalog.status = 'ready'
			AND s.content_status = 'available'
			AND s.content_epoch = catalog.content_epoch
			AND (catalog.activity_end_at AT TIME ZONE 'Asia/Shanghai')::date >= $2::date
			AND (catalog.activity_start_at AT TIME ZONE 'Asia/Shanghai')::date <= $3::date
		ORDER BY catalog.activity_start_at, s.session_ref, catalog.start_cursor, catalog.slice_id`,
		userID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SelectionItem{}
	for rows.Next() {
		var item SelectionItem
		if err := rows.Scan(&item.SliceID, &item.SessionID, &item.SessionRef, &item.AgentType, &item.SourceID,
			&item.GenerationID, &item.ProjectionRevision, &item.StartCursor, &item.EndCursor,
			&item.ActivityStart, &item.ActivityEnd, &item.Summary, &item.ContentStatus, &item.ContentEpoch,
			&item.ContentEventCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

var reportUsageMetricKeys = map[string]struct{}{
	"usage":                       {},
	"token_usage":                 {},
	"total_token_usage":           {},
	"last_token_usage":            {},
	"input_tokens":                {},
	"output_tokens":               {},
	"cached_input_tokens":         {},
	"cache_read_input_tokens":     {},
	"cache_creation_input_tokens": {},
	"cache_read_tokens":           {},
	"cache_creation_tokens":       {},
	"reasoning_output_tokens":     {},
	"total_tokens":                {},
}

func redactReportUsageMetrics(payload json.RawMessage) json.RawMessage {
	if len(payload) == 0 {
		return json.RawMessage(`{}`)
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return json.RawMessage(`{}`)
	}
	redacted, changed := redactReportUsageValue(value)
	if !changed {
		return payload
	}
	encoded, err := json.Marshal(redacted)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}

func redactReportUsageValue(value any) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		changed := false
		for key, item := range typed {
			if _, remove := reportUsageMetricKeys[strings.ToLower(strings.TrimSpace(key))]; remove {
				delete(typed, key)
				changed = true
				continue
			}
			redacted, itemChanged := redactReportUsageValue(item)
			if itemChanged {
				typed[key] = redacted
				changed = true
			}
		}
		return typed, changed
	case []any:
		changed := false
		for index, item := range typed {
			redacted, itemChanged := redactReportUsageValue(item)
			if itemChanged {
				typed[index] = redacted
				changed = true
			}
		}
		return typed, changed
	default:
		return value, false
	}
}

func mergeItems(items []SelectionItem) []SelectionItem {
	sort.Slice(items, func(i, j int) bool {
		if items[i].SourceID == items[j].SourceID {
			return items[i].StartCursor < items[j].StartCursor
		}
		return items[i].SourceID < items[j].SourceID
	})
	merged := make([]SelectionItem, 0, len(items))
	for _, item := range items {
		last := len(merged) - 1
		if last >= 0 && merged[last].SourceID == item.SourceID && item.StartCursor <= merged[last].EndCursor {
			if item.EndCursor > merged[last].EndCursor {
				merged[last].EndCursor = item.EndCursor
			}
			if item.ActivityStart.Before(merged[last].ActivityStart) {
				merged[last].ActivityStart = item.ActivityStart
			}
			if item.ActivityEnd.After(merged[last].ActivityEnd) {
				merged[last].ActivityEnd = item.ActivityEnd
			}
			merged[last].ContentEventCount += item.ContentEventCount
			continue
		}
		merged = append(merged, item)
	}
	return merged
}

func insertSelection(
	ctx context.Context,
	tx *sql.Tx,
	userID, reportType string,
	periodStart, periodEnd time.Time,
	mode string,
	items []SelectionItem,
) (Selection, error) {
	selection := Selection{
		ReportType: reportType, Period: Period{Start: periodStart.Format("2006-01-02"), End: periodEnd.Format("2006-01-02")},
		Mode: mode, Status: "prepared", Items: items,
	}
	expiresAt := time.Now().UTC().Add(preparedSelectionTTL)
	err := tx.QueryRowContext(ctx, `
		INSERT INTO report_source_selections (
			user_id, report_type, period_start, period_end, selection_mode, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text, content_snapshot_at`, userID, reportType, periodStart, periodEnd, mode, expiresAt,
	).Scan(&selection.ID, &selection.ContentSnapshotAt)
	if err != nil {
		return Selection{}, err
	}
	for i := range selection.Items {
		item := &selection.Items[i]
		err := tx.QueryRowContext(ctx, `
			INSERT INTO report_source_selection_items (
				selection_id, session_content_slice_id, session_id, session_ref_snapshot, agent_type, source_id,
				source_generation_id, content_projection_revision_id, start_cursor, end_cursor,
				activity_start_at, activity_end_at, summary_snapshot, content_status_snapshot,
				content_epoch_snapshot, content_event_count
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NULLIF($13, ''), $14, $15, $16)
			RETURNING id::text`, selection.ID, item.SliceID, item.SessionID, item.SessionRef, item.AgentType,
			item.SourceID, item.GenerationID, item.ProjectionRevision, item.StartCursor, item.EndCursor,
			item.ActivityStart, item.ActivityEnd, item.Summary, item.ContentStatus, item.ContentEpoch,
			item.ContentEventCount,
		).Scan(&item.ID)
		if err != nil {
			return Selection{}, err
		}
	}
	return selection, nil
}

func lockPreparedSelection(
	ctx context.Context,
	tx *sql.Tx,
	userID, selectionID, reportType string,
	periodStart, periodEnd time.Time,
) (Selection, error) {
	var selection Selection
	var start, end time.Time
	var expires sql.NullTime
	err := tx.QueryRowContext(ctx, `
		SELECT id::text, report_type, period_start, period_end, selection_mode, status,
			content_snapshot_at, expires_at, required_read_mode,
			COALESCE(selection_digest_bytes, 0)
		FROM report_source_selections
		WHERE id = $1 AND user_id = $2 FOR UPDATE`, selectionID, userID).Scan(
		&selection.ID, &selection.ReportType, &start, &end, &selection.Mode, &selection.Status,
		&selection.ContentSnapshotAt, &expires, &selection.RequiredReadMode,
		&selection.ContextBytes,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Selection{}, ErrSelectionNotFound
	}
	if err != nil {
		return Selection{}, err
	}
	if selection.Status != "prepared" || selection.ReportType != reportType ||
		!sameDate(start, periodStart) || !sameDate(end, periodEnd) ||
		(expires.Valid && !expires.Time.After(time.Now())) {
		return Selection{}, ErrSelectionConflict
	}
	selection.Period = Period{Start: start.Format("2006-01-02"), End: end.Format("2006-01-02")}
	rows, err := tx.QueryContext(ctx, `
		SELECT id::text, COALESCE(session_content_slice_id::text, ''), session_id::text, session_ref_snapshot, agent_type, source_id::text,
			source_generation_id::text, content_projection_revision_id::text, start_cursor,
			end_cursor, activity_start_at, activity_end_at, COALESCE(summary_snapshot, ''),
			content_status_snapshot, content_epoch_snapshot, content_event_count
		FROM report_source_selection_items WHERE selection_id = $1 ORDER BY created_at, id`, selection.ID)
	if err != nil {
		return Selection{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item SelectionItem
		if err := rows.Scan(&item.ID, &item.SliceID, &item.SessionID, &item.SessionRef, &item.AgentType,
			&item.SourceID, &item.GenerationID, &item.ProjectionRevision, &item.StartCursor,
			&item.EndCursor, &item.ActivityStart, &item.ActivityEnd, &item.Summary,
			&item.ContentStatus, &item.ContentEpoch, &item.ContentEventCount); err != nil {
			return Selection{}, err
		}
		selection.Items = append(selection.Items, item)
	}
	if err := rows.Err(); err != nil {
		return Selection{}, err
	}
	if selection.Mode == "explicit" && len(selection.Items) == 0 {
		return Selection{}, ErrSelectionConflict
	}
	return selection, nil
}

// ValidateAttachedSelectionTx keeps source availability stable until the caller
// commits or rolls back its own report write transaction.
func (s *Service) ValidateAttachedSelectionTx(
	ctx context.Context,
	tx *sql.Tx,
	userID, selectionID, runID, reportType string,
	period Period,
) error {
	if s == nil || tx == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(selectionID) == "" ||
		strings.TrimSpace(runID) == "" || !validPersonalReportType(reportType) {
		return ErrSelectionMismatch
	}
	periodStart, periodEnd, err := parsePeriod(period)
	if err != nil {
		return ErrSelectionMismatch
	}
	var status, requiredReadMode string
	var readCompletedMode sql.NullString
	var readCompletedAt sql.NullTime
	var storedStart, storedEnd time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT status, period_start, period_end, required_read_mode,
			read_completed_mode, read_completed_at
		FROM report_source_selections
		WHERE id = $1 AND user_id = $2 AND attached_run_id = $3`, selectionID, userID, runID).Scan(
		&status, &storedStart, &storedEnd, &requiredReadMode, &readCompletedMode, &readCompletedAt,
	)
	if errors.Is(err, sql.ErrNoRows) || status != "attached" {
		return ErrSelectionMismatch
	}
	if err != nil {
		return err
	}
	var runReportType string
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(input_ref_json->>'report_type', '')
		FROM ai_runs
		WHERE id = $1 AND user_id = $2 AND business_type = 'report_agent_run'`,
		runID, userID).Scan(&runReportType); err != nil {
		return ErrSelectionMismatch
	}
	if runReportType != reportType || !sameDate(storedStart, periodStart) || !sameDate(storedEnd, periodEnd) {
		return ErrSelectionMismatch
	}
	if !readCompletedAt.Valid || !readCompletedMode.Valid || readCompletedMode.String != requiredReadMode {
		return ErrSourceIncomplete
	}
	if requiredReadMode == ReadModeDigestV1 {
		if err := s.validateFrozenDigestSelectionTx(ctx, tx, userID, selectionID, runID); err != nil {
			return err
		}
	} else if requiredReadMode == ReadModeDigestV2 {
		if err := s.validateFrozenDigestV2SelectionTx(ctx, tx, userID, selectionID, runID); err != nil {
			return err
		}
	}
	return validateSelectionSourcesAvailable(ctx, tx, selectionID)
}

func validateSelectionSourcesAvailable(ctx context.Context, tx *sql.Tx, selectionID string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT s.content_status, s.content_epoch, i.content_epoch_snapshot
		FROM report_source_selection_items i
		JOIN sessions s ON s.id = i.session_id
		WHERE i.selection_id = $1
		ORDER BY s.id
		FOR SHARE OF s`, selectionID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var currentEpoch, snapshotEpoch int64
		if err := rows.Scan(&status, &currentEpoch, &snapshotEpoch); err != nil {
			return err
		}
		if status != "available" || currentEpoch != snapshotEpoch {
			return ErrSourceUnavailable
		}
	}
	return rows.Err()
}

func sameDate(a, b time.Time) bool {
	return a.Format("2006-01-02") == b.Format("2006-01-02")
}

func ReportPeriod(reportType, date, weekStart, weekEnd string) (Period, error) {
	switch reportType {
	case "personal_daily":
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return Period{}, ErrInvalidRequest
		}
		return Period{Start: date, End: date}, nil
	case "personal_weekly":
		period := Period{Start: weekStart, End: weekEnd}
		_, _, err := parsePeriod(period)
		return period, err
	default:
		return Period{}, fmt.Errorf("%w: report type", ErrInvalidRequest)
	}
}
