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

	"github.com/lib/pq"
)

var (
	ErrInvalidRequest      = errors.New("invalid report source request")
	ErrSelectionNotFound   = errors.New("report source selection not found")
	ErrSourceUnavailable   = errors.New("report source content is unavailable")
	ErrSelectionConflict   = errors.New("report source selection cannot be attached")
	ErrSelectionMismatch   = errors.New("report source selection does not match the managed report run")
	ErrSourceIncomplete    = errors.New("report source selection has not been read completely")
	ErrContentItemTooLarge = errors.New("report source content item exceeds the page limit")
)

const preparedSelectionTTL = 30 * time.Minute
const reportSourceCursorTTL = time.Hour
const reportSourcePageMaxEvents = 100
const reportSourcePageMaxBytes = 512 << 10

type Service struct {
	db *sql.DB
}

func NewService(database *sql.DB) (*Service, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &Service{db: database}, nil
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
	Items             []SelectionItem `json:"items"`
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
	TotalTokens        int64     `json:"total_tokens"`
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
	SourceMode      string        `json:"source_mode"`
	SelectionID     string        `json:"selection_id"`
	ContentSnapshot time.Time     `json:"content_snapshot_at"`
	Completeness    string        `json:"completeness"`
	ReturnedCount   int           `json:"returned_item_count"`
	ReturnedEvents  int           `json:"returned_event_count"`
	HasMore         bool          `json:"has_more"`
	NextCursor      *string       `json:"next_cursor"`
	Items           []ContentItem `json:"items"`
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
		WITH all_candidates AS (
			SELECT sl.id::text AS slice_key, s.session_ref, s.agent_type,
				COALESCE((
					SELECT NULLIF(btrim(e2.summary), '')
					FROM session_content_events e2
					WHERE e2.content_projection_revision_id = rev.id
						AND e2.source_start_cursor >= sl.start_cursor
						AND e2.source_end_cursor <= sl.end_cursor
						AND NULLIF(btrim(e2.summary), '') IS NOT NULL
						AND e2.event_type NOT IN ('response_item.custom_tool_call', 'response_item.function_call')
					ORDER BY CASE e2.event_type
						WHEN 'event_msg.user_message' THEN 0
						WHEN 'event_msg.agent_message' THEN 1
						WHEN 'response_item.message' THEN 2
						ELSE 3
					END, e2.source_start_cursor, e2.id
					LIMIT 1
				), 'Session 增量内容（' || COUNT(*)::text || ' 条记录）') AS summary,
				MAX(e.occurred_at) AS last_activity_at,
				MIN(e.occurred_at) AS activity_start_at,
				MAX(e.occurred_at) AS activity_end_at,
				COALESCE(s.cwd, '') AS cwd, COALESCE(s.models, '{}') AS models,
				s.content_status, MAX(e.occurred_at) AS available_through_at,
				COALESCE((
					SELECT SUM(uc.normalized_total_tokens)
					FROM session_usage_components uc
					JOIN session_upload_chunks ch ON ch.id = uc.chunk_id
					WHERE uc.valid_to IS NULL AND ch.generation_id = sl.generation_id
						AND ch.start_cursor >= sl.start_cursor AND ch.end_cursor <= sl.end_cursor
				), 0) AS total_tokens
			FROM session_content_slices sl
			JOIN sessions s ON s.id = sl.session_id
			JOIN session_sources src ON src.id = sl.source_id AND src.session_id = s.id
			JOIN session_content_projection_revisions rev
				ON rev.id = src.active_content_projection_revision_id
				AND rev.generation_id = sl.generation_id AND rev.status = 'active'
			JOIN session_content_events e
				ON e.content_projection_revision_id = rev.id
				AND e.source_start_cursor >= sl.start_cursor
				AND e.source_end_cursor <= sl.end_cursor
			WHERE s.user_id = $1 AND s.content_status = 'available'
				AND rev.content_indexed_cursor >= sl.end_cursor
			GROUP BY sl.id, s.id, rev.id
		), candidates AS (
			SELECT * FROM all_candidates
			WHERE ($2 = '%%' OR lower(session_ref) LIKE $2 OR lower(summary) LIKE $2)
				AND ($3::timestamptz IS NULL OR activity_end_at >= $3)
				AND ($4::timestamptz IS NULL OR activity_start_at <= $4)
		)`
	var total int
	if err := s.db.QueryRowContext(ctx, candidateCTE+` SELECT COUNT(*) FROM candidates`, userID, search, from, to).Scan(&total); err != nil {
		return CandidatePage{}, err
	}
	rows, err := s.db.QueryContext(ctx, candidateCTE+`
		SELECT slice_key, session_ref, agent_type, summary, last_activity_at, activity_start_at,
			activity_end_at, cwd, models, content_status, available_through_at, total_tokens
		FROM candidates
		ORDER BY last_activity_at DESC, session_ref ASC
		LIMIT $5 OFFSET $6`, userID, search, from, to, query.PageSize, (query.Page-1)*query.PageSize)
	if err != nil {
		return CandidatePage{}, err
	}
	defer rows.Close()
	items := make([]Candidate, 0, query.PageSize)
	for rows.Next() {
		var item Candidate
		var models pq.StringArray
		if err := rows.Scan(&item.SliceKey, &item.SessionRef, &item.AgentType, &item.Summary, &item.LastActivityAt,
			&item.ActivityStartAt, &item.ActivityEndAt, &item.CWD, &models,
			&item.ContentStatus, &item.AvailableThroughAt, &item.TotalTokens); err != nil {
			return CandidatePage{}, err
		}
		item.Models = []string(models)
		item.ContentIndexStatus = "ready"
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return CandidatePage{}, err
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

func (s *Service) CreateAttachedRun(
	ctx context.Context,
	userID, reportType string,
	period Period,
	selectionID, businessType, agentID, modelID string,
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
	if inputRef == nil {
		inputRef = map[string]any{}
	}
	inputRef["report_source_selection_id"] = selection.ID
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
	var mode, status string
	var snapshotAt time.Time
	var storedStart, storedEnd time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT selection_mode, status, content_snapshot_at, period_start, period_end
		FROM report_source_selections
		WHERE id = $1 AND user_id = $2 AND attached_run_id = $3`, selectionID, userID, runID).Scan(
		&mode, &status, &snapshotAt, &storedStart, &storedEnd,
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
		item := items[nextOffset]
		cursor := item.StartCursor
		if nextOffset == itemOffset && nextEventCursor > cursor {
			cursor = nextEventCursor
		}
		rows, err := tx.QueryContext(ctx, `
			SELECT source_start_cursor, source_end_cursor, occurred_at, event_type,
				COALESCE(summary, ''), COALESCE(excerpt, ''), COALESCE(content_payload, '{}'::jsonb)
			FROM session_content_events
			WHERE content_projection_revision_id = $1
				AND source_start_cursor >= $2 AND source_end_cursor <= $3
			ORDER BY source_start_cursor, source_end_cursor`, item.ProjectionRevision, cursor, item.EndCursor)
		if err != nil {
			return ContentPage{}, err
		}
		contentItem := ContentItem{
			SessionRef: item.SessionRef, AgentType: item.AgentType,
			ActivityStartAt: item.ActivityStart, ActivityEndAt: item.ActivityEnd,
			Summary: item.Summary, ContentQuality: "exact", ContentEventCount: item.ContentEventCount,
			Events: []ContentEvent{},
		}
		itemComplete := true
		for rows.Next() {
			var event ContentEvent
			var eventStart, eventEnd int64
			if err := rows.Scan(&eventStart, &eventEnd, &event.OccurredAt, &event.EventType,
				&event.Summary, &event.Excerpt, &event.Payload); err != nil {
				rows.Close()
				return ContentPage{}, err
			}
			event.Payload = redactReportUsageMetrics(event.Payload)
			encoded, err := json.Marshal(event)
			if err != nil {
				rows.Close()
				return ContentPage{}, err
			}
			if len(encoded) > reportSourcePageMaxBytes {
				rows.Close()
				return ContentPage{}, ErrContentItemTooLarge
			}
			if page.ReturnedEvents >= reportSourcePageMaxEvents || pageBytes+len(encoded) > reportSourcePageMaxBytes {
				itemComplete = false
				nextCursorValue = eventStart
				break
			}
			contentItem.Events = append(contentItem.Events, event)
			page.ReturnedEvents++
			pageBytes += len(encoded)
			nextCursorValue = eventEnd
		}
		if err := rows.Close(); err != nil {
			return ContentPage{}, err
		}
		if len(contentItem.Events) > 0 {
			page.Items = append(page.Items, contentItem)
		}
		if !itemComplete {
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
			SET read_completed_at = COALESCE(read_completed_at, now())
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

func loadInternalSelectionItems(ctx context.Context, tx *sql.Tx, selectionID string) ([]SelectionItem, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id::text, session_id::text, session_ref_snapshot, agent_type, source_id::text,
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
		if err := rows.Scan(&item.ID, &item.SessionID, &item.SessionRef, &item.AgentType,
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
			SELECT s.id::text, s.session_ref, s.agent_type, src.id::text, g.id::text, rev.id::text,
				sl.start_cursor, sl.end_cursor, MIN(e.occurred_at), MAX(e.occurred_at),
				COALESCE((
					SELECT NULLIF(btrim(e2.summary), '') FROM session_content_events e2
					WHERE e2.content_projection_revision_id = rev.id
						AND e2.source_start_cursor >= sl.start_cursor AND e2.source_end_cursor <= sl.end_cursor
						AND NULLIF(btrim(e2.summary), '') IS NOT NULL
						AND e2.event_type NOT IN ('response_item.custom_tool_call', 'response_item.function_call')
					ORDER BY CASE e2.event_type
						WHEN 'event_msg.user_message' THEN 0
						WHEN 'event_msg.agent_message' THEN 1
						WHEN 'response_item.message' THEN 2
						ELSE 3
					END, e2.source_start_cursor, e2.id LIMIT 1
				), 'Session 增量内容（' || COUNT(*)::text || ' 条记录）'), s.content_status, s.content_epoch, COUNT(*)
			FROM sessions s
			JOIN session_sources src ON src.session_id = s.id
			JOIN session_content_slices sl ON sl.id = $2 AND sl.session_id = s.id AND sl.source_id = src.id
			JOIN session_source_generations g ON g.id = sl.generation_id AND g.status = 'active'
			JOIN session_content_projection_revisions rev
				ON rev.id = src.active_content_projection_revision_id AND rev.generation_id = g.id AND rev.status = 'active'
			JOIN session_content_events e
				ON e.content_projection_revision_id = rev.id
				AND e.source_start_cursor >= sl.start_cursor AND e.source_end_cursor <= sl.end_cursor
			WHERE s.user_id = $1 AND s.content_status = 'available'
				AND rev.content_indexed_cursor >= sl.end_cursor
			GROUP BY s.id, src.id, g.id, rev.id, sl.id`, userID, input.SliceKey).Scan(
			&item.SessionID, &item.SessionRef, &item.AgentType, &item.SourceID, &item.GenerationID,
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
		WITH complete_slices AS (
			SELECT s.id::text AS session_id, s.session_ref, s.agent_type,
				src.id::text AS source_id, g.id::text AS generation_id, rev.id::text AS revision_id,
				sl.start_cursor, sl.end_cursor, MIN(e.occurred_at) AS activity_start_at,
				MAX(e.occurred_at) AS activity_end_at,
				COALESCE((
					SELECT NULLIF(btrim(e2.summary), '')
					FROM session_content_events e2
					WHERE e2.content_projection_revision_id = rev.id
						AND e2.source_start_cursor >= sl.start_cursor
						AND e2.source_end_cursor <= sl.end_cursor
						AND NULLIF(btrim(e2.summary), '') IS NOT NULL
						AND e2.event_type NOT IN ('response_item.custom_tool_call', 'response_item.function_call')
					ORDER BY CASE e2.event_type
						WHEN 'event_msg.user_message' THEN 0
						WHEN 'event_msg.agent_message' THEN 1
						WHEN 'response_item.message' THEN 2
						ELSE 3
					END, e2.source_start_cursor, e2.id
					LIMIT 1
				), 'Session 增量内容（' || COUNT(*)::text || ' 条记录）') AS summary,
				s.content_status, s.content_epoch, COUNT(*) AS content_event_count
			FROM sessions s
			JOIN session_sources src ON src.session_id = s.id
			JOIN session_source_generations g
				ON g.id = src.active_generation_id AND g.status = 'active'
			JOIN session_content_slices sl
				ON sl.session_id = s.id AND sl.source_id = src.id AND sl.generation_id = g.id
			JOIN session_content_projection_revisions rev
				ON rev.id = src.active_content_projection_revision_id
				AND rev.generation_id = g.id AND rev.status = 'active'
			JOIN session_content_events e
				ON e.content_projection_revision_id = rev.id
				AND e.source_start_cursor >= sl.start_cursor
				AND e.source_end_cursor <= sl.end_cursor
			WHERE s.user_id = $1 AND s.content_status = 'available'
				AND rev.content_indexed_cursor >= sl.end_cursor
			GROUP BY s.id, src.id, g.id, rev.id, sl.id
		)
		SELECT session_id, session_ref, agent_type, source_id, generation_id, revision_id,
			start_cursor, end_cursor, activity_start_at, activity_end_at, summary,
			content_status, content_epoch, content_event_count
		FROM complete_slices
		WHERE (activity_end_at AT TIME ZONE 'Asia/Shanghai')::date >= $2::date
			AND (activity_start_at AT TIME ZONE 'Asia/Shanghai')::date <= $3::date
		ORDER BY activity_start_at, session_ref, start_cursor`,
		userID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SelectionItem{}
	for rows.Next() {
		var item SelectionItem
		if err := rows.Scan(&item.SessionID, &item.SessionRef, &item.AgentType, &item.SourceID,
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
				selection_id, session_id, session_ref_snapshot, agent_type, source_id,
				source_generation_id, content_projection_revision_id, start_cursor, end_cursor,
				activity_start_at, activity_end_at, summary_snapshot, content_status_snapshot,
				content_epoch_snapshot, content_event_count
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NULLIF($12, ''), $13, $14, $15)
			RETURNING id::text`, selection.ID, item.SessionID, item.SessionRef, item.AgentType,
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
			content_snapshot_at, expires_at
		FROM report_source_selections
		WHERE id = $1 AND user_id = $2 FOR UPDATE`, selectionID, userID).Scan(
		&selection.ID, &selection.ReportType, &start, &end, &selection.Mode, &selection.Status,
		&selection.ContentSnapshotAt, &expires,
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
	if err := validateSelectionSourcesAvailable(ctx, tx, selection.ID); err != nil {
		return Selection{}, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id::text, session_id::text, session_ref_snapshot, agent_type, source_id::text,
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
		if err := rows.Scan(&item.ID, &item.SessionID, &item.SessionRef, &item.AgentType,
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
	var status string
	var readCompletedAt sql.NullTime
	var storedStart, storedEnd time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT status, period_start, period_end, read_completed_at
		FROM report_source_selections
		WHERE id = $1 AND user_id = $2 AND attached_run_id = $3`, selectionID, userID, runID).Scan(
		&status, &storedStart, &storedEnd, &readCompletedAt,
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
	if !readCompletedAt.Valid {
		return ErrSourceIncomplete
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
