package tokenanalytics

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	snapshotTTL      = 15 * time.Minute
	maxUserSnapshots = 10
)

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func (s *Service) CreateSummary(ctx context.Context, actor Actor, filters Filters) (Summary, error) {
	snapshot, err := s.createSnapshot(ctx, actor, filters)
	if err != nil {
		return Summary{}, err
	}
	return s.summary(ctx, snapshot)
}

func (s *Service) Trends(ctx context.Context, actor Actor, filters Filters, token string) (Trends, error) {
	snapshot, err := s.loadSnapshot(ctx, actor, filters, token)
	if err != nil {
		return Trends{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT activity_date::text, SUM(normalized_total_tokens)::text,
			SUM(estimated_cost_cny) FILTER (WHERE pricing_status = 'priced')::text,
			CASE
				WHEN COUNT(*) FILTER (WHERE pricing_status = 'pricing_pending') > 0 THEN 'pricing_pending'
				WHEN COUNT(*) FILTER (WHERE pricing_status = 'priced') = COUNT(*) THEN 'priced'
				WHEN COUNT(*) FILTER (WHERE pricing_status = 'priced') > 0 THEN 'partially_priced'
				ELSE 'unpriced'
			END
		FROM token_query_snapshot_items
		WHERE snapshot_id = $1
		GROUP BY activity_date
		ORDER BY activity_date`, snapshot.ID)
	if err != nil {
		return Trends{}, err
	}
	defer rows.Close()
	result := Trends{QuerySnapshotToken: token, Items: []TrendPoint{}}
	for rows.Next() {
		var item TrendPoint
		var cost sql.NullString
		if err := rows.Scan(&item.Date, &item.TotalTokens, &cost, &item.PricingStatus); err != nil {
			return Trends{}, err
		}
		item.EstimatedCostCNY = stringPtr(cost)
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

func (s *Service) Rankings(ctx context.Context, actor Actor, filters Filters, token, groupBy string) (Rankings, error) {
	snapshot, err := s.loadSnapshot(ctx, actor, filters, token)
	if err != nil {
		return Rankings{}, err
	}
	groupBy = strings.TrimSpace(groupBy)
	if groupBy == "" {
		groupBy = "user"
	}
	result := Rankings{QuerySnapshotToken: token, GroupBy: groupBy, Items: []RankingItem{}}
	var rows *sql.Rows
	switch groupBy {
	case "user":
		rows, err = s.db.QueryContext(ctx, `
			SELECT member.user_id::text, member.user_display_name,
				COALESCE(SUM(item.normalized_total_tokens), 0)::text,
				SUM(item.estimated_cost_cny) FILTER (WHERE item.pricing_status = 'priced')::text,
				CASE
					WHEN COUNT(item.id) = 0 THEN 'unpriced'
					WHEN COUNT(*) FILTER (WHERE item.pricing_status = 'pricing_pending') > 0 THEN 'pricing_pending'
					WHEN COUNT(*) FILTER (WHERE item.pricing_status = 'priced') = COUNT(item.id) THEN 'priced'
					WHEN COUNT(*) FILTER (WHERE item.pricing_status = 'priced') > 0 THEN 'partially_priced'
					ELSE 'unpriced'
				END,
				MAX(item.occurred_at)::text
			FROM token_query_snapshot_members member
			LEFT JOIN token_query_snapshot_items item
				ON item.snapshot_id = member.snapshot_id AND item.user_id = member.user_id
			WHERE member.snapshot_id = $1
			GROUP BY member.user_id, member.user_display_name
			ORDER BY COALESCE(SUM(item.normalized_total_tokens), 0) DESC,
				MAX(item.occurred_at) DESC NULLS LAST, member.user_display_name`, snapshot.ID)
	case "team":
		rows, err = groupedRankingRows(ctx, s.db, snapshot.ID,
			"COALESCE(team_id_snapshot::text, 'unknown')", "COALESCE(team_name_snapshot, '未归属小组')")
	case "department":
		rows, err = groupedRankingRows(ctx, s.db, snapshot.ID,
			"COALESCE(department_id_snapshot::text, 'unknown')", "COALESCE(department_name_snapshot, '未归属部门')")
	case "model":
		rows, err = groupedRankingRows(ctx, s.db, snapshot.ID,
			"COALESCE(canonical_model, 'unknown')", "COALESCE(canonical_model, 'unknown')")
	default:
		return Rankings{}, ErrInvalidFilter
	}
	if err != nil {
		return Rankings{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item RankingItem
		var cost, lastActivity sql.NullString
		if err := rows.Scan(&item.Key, &item.Label, &item.TotalTokens, &cost, &item.PricingStatus, &lastActivity); err != nil {
			return Rankings{}, err
		}
		item.EstimatedCostCNY = stringPtr(cost)
		item.LastActivityAt = stringPtr(lastActivity)
		item.IsZeroUsage = item.TotalTokens == "0"
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

func groupedRankingRows(ctx context.Context, db *sql.DB, snapshotID, keyExpr, labelExpr string) (*sql.Rows, error) {
	query := fmt.Sprintf(`
		SELECT %s, %s, SUM(normalized_total_tokens)::text,
			SUM(estimated_cost_cny) FILTER (WHERE pricing_status = 'priced')::text,
			CASE
				WHEN COUNT(*) FILTER (WHERE pricing_status = 'pricing_pending') > 0 THEN 'pricing_pending'
				WHEN COUNT(*) FILTER (WHERE pricing_status = 'priced') = COUNT(*) THEN 'priced'
				WHEN COUNT(*) FILTER (WHERE pricing_status = 'priced') > 0 THEN 'partially_priced'
				ELSE 'unpriced'
			END,
			MAX(occurred_at)::text
		FROM token_query_snapshot_items
		WHERE snapshot_id = $1
		GROUP BY %s, %s
		ORDER BY SUM(normalized_total_tokens) DESC, MAX(occurred_at) DESC, %s`,
		keyExpr, labelExpr, keyExpr, labelExpr, labelExpr)
	return db.QueryContext(ctx, query, snapshotID)
}

func (s *Service) Sessions(ctx context.Context, actor Actor, filters Filters, token string, page, pageSize int) (Sessions, error) {
	snapshot, err := s.loadSnapshot(ctx, actor, filters, token)
	if err != nil {
		return Sessions{}, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT session_id) FROM token_query_snapshot_items WHERE snapshot_id = $1`,
		snapshot.ID).Scan(&total); err != nil {
		return Sessions{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT item.session_id::text, item.session_ref, item.user_id::text,
			MAX(item.user_display_name), COALESCE(MAX(item.agent_type), ''),
			CASE WHEN BOOL_OR(session.content_status = 'available') THEN MAX(session.summary) END,
			MIN(item.activity_date)::text, MAX(item.activity_date)::text,
			COALESCE(string_agg(DISTINCT item.canonical_model, ', ' ORDER BY item.canonical_model), 'unknown'),
			SUM(item.normalized_total_tokens)::text,
			SUM(item.estimated_cost_cny) FILTER (WHERE item.pricing_status = 'priced')::text,
			CASE
				WHEN COUNT(*) FILTER (WHERE item.pricing_status = 'pricing_pending') > 0 THEN 'pricing_pending'
				WHEN COUNT(*) FILTER (WHERE item.pricing_status = 'priced') = COUNT(*) THEN 'priced'
				WHEN COUNT(*) FILTER (WHERE item.pricing_status = 'priced') > 0 THEN 'partially_priced'
				ELSE 'unpriced'
			END,
			CASE
				WHEN BOOL_OR(item.quality_status = 'conflict') THEN 'conflict'
				WHEN BOOL_OR(item.quality_status = 'incomplete') THEN 'incomplete'
				WHEN BOOL_OR(item.quality_status = 'estimated' OR item.is_estimated) THEN 'estimated'
				ELSE 'exact'
			END,
			MAX(item.occurred_at)
		FROM token_query_snapshot_items item
		LEFT JOIN sessions session ON session.id = item.session_id
		WHERE item.snapshot_id = $1
		GROUP BY item.session_id, item.session_ref, item.user_id
		ORDER BY MAX(item.occurred_at) DESC, item.session_ref
		LIMIT $2 OFFSET $3`, snapshot.ID, pageSize, (page-1)*pageSize)
	if err != nil {
		return Sessions{}, err
	}
	defer rows.Close()
	result := Sessions{
		QuerySnapshotToken: token,
		SearchMode:         snapshot.SearchMode,
		Items:              []SessionItem{},
		Total:              total,
		Page:               page,
		PageSize:           pageSize,
	}
	for rows.Next() {
		var item SessionItem
		var summary, cost sql.NullString
		var ignoredLastActivity time.Time
		if err := rows.Scan(&item.SessionID, &item.SessionRef, &item.UserID, &item.UserName,
			&item.AgentType, &summary, &item.ActivityFrom, &item.ActivityTo, &item.Model,
			&item.TotalTokens, &cost, &item.PricingStatus, &item.QualityStatus,
			&ignoredLastActivity); err != nil {
			return Sessions{}, err
		}
		item.Summary = stringPtr(summary)
		item.EstimatedCostCNY = stringPtr(cost)
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

func (s *Service) createSnapshot(ctx context.Context, actor Actor, raw Filters) (Snapshot, error) {
	filters, from, to, err := normalizeFilters(raw)
	if err != nil {
		return Snapshot{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback()

	scopeSQL, scopeArgs, err := buildScope(ctx, tx, actor, filters, "component")
	if err != nil {
		return Snapshot{}, err
	}
	baseWhere := []string{
		"component.valid_to IS NULL",
		"revision.status = 'active'",
		"state.active_revision_id = component.revision_id",
		scopeSQL,
	}
	baseArgs := append([]any{}, scopeArgs...)
	baseWhere, baseArgs = appendOrganizationFilters(baseWhere, baseArgs, filters, "component")

	searchMode := "filtered"
	if filters.Query != "" {
		exactWhere := append([]string{}, baseWhere...)
		exactArgs := append([]any{}, baseArgs...)
		exactArgs = append(exactArgs, filters.Query)
		exactWhere = append(exactWhere, fmt.Sprintf("session.session_ref = $%d", len(exactArgs)))
		var exact bool
		err = tx.QueryRowContext(ctx, activeComponentExistsSQL(exactWhere), exactArgs...).Scan(&exact)
		if err != nil {
			return Snapshot{}, err
		}
		if exact {
			searchMode = "exact_session_ref"
		}
	}

	where := append([]string{}, baseWhere...)
	args := append([]any{}, baseArgs...)
	if searchMode != "exact_session_ref" {
		args = append(args, from.Format("2006-01-02"))
		where = append(where, fmt.Sprintf("component.activity_date >= $%d::date", len(args)))
		args = append(args, to.Format("2006-01-02"))
		where = append(where, fmt.Sprintf("component.activity_date <= $%d::date", len(args)))
		if filters.Model != "" {
			args = append(args, filters.Model)
			where = append(where, fmt.Sprintf("COALESCE(alias.canonical_model, component.canonical_model, component.raw_model, '') = $%d", len(args)))
		}
		if filters.Query != "" {
			args = append(args, "%"+filters.Query+"%")
			where = append(where, fmt.Sprintf("session.content_status = 'available' AND COALESCE(session.summary, '') ILIKE $%d", len(args)))
		}
	} else {
		args = append(args, filters.Query)
		where = append(where, fmt.Sprintf("session.session_ref = $%d", len(args)))
	}

	rawToken, tokenHash, err := newSnapshotToken()
	if err != nil {
		return Snapshot{}, err
	}
	filtersJSON, _ := json.Marshal(filters)
	var snapshot Snapshot
	snapshot.Token = rawToken
	snapshot.Scope = filters.Scope
	snapshot.SearchMode = searchMode
	snapshot.Filters = filters
	err = tx.QueryRowContext(ctx, `
		INSERT INTO token_query_snapshots(
			token_hash, user_id, scope, search_mode, filters_json,
			metrics_snapshot_at, expires_at
		) VALUES ($1, $2, $3, $4, $5::jsonb, statement_timestamp(), statement_timestamp() + $6::interval)
		RETURNING id::text, metrics_snapshot_at, expires_at`, tokenHash, actor.ID,
		filters.Scope, searchMode, string(filtersJSON), "15 minutes").Scan(
		&snapshot.ID, &snapshot.MetricsSnapshotAt, &snapshot.ExpiresAt)
	if err != nil {
		return Snapshot{}, err
	}

	if err := materializeMembers(ctx, tx, snapshot.ID, actor, filters); err != nil {
		return Snapshot{}, err
	}
	if err := materializeItems(ctx, tx, snapshot.ID, where, args); err != nil {
		return Snapshot{}, err
	}
	pendingSourceCount, err := countPendingSources(
		ctx, tx, snapshot.ID, actor, filters, searchMode, from, to,
	)
	if err != nil {
		return Snapshot{}, err
	}
	if err := tx.QueryRowContext(ctx, `
		WITH snapshot_stats AS (
			SELECT COUNT(*) AS component_count,
				COUNT(DISTINCT item.session_id) FILTER (WHERE item.pricing_status = 'pricing_pending') AS pricing_pending_sources
			FROM token_query_snapshot_items item
			WHERE item.snapshot_id = $1
		)
		UPDATE token_query_snapshots snapshot
		SET component_count = snapshot_stats.component_count,
			pending_source_count = $2,
			pricing_pending_source_count = snapshot_stats.pricing_pending_sources
		FROM snapshot_stats
		WHERE snapshot.id = $1
		RETURNING snapshot.component_count, snapshot.pending_source_count,
			snapshot.pricing_pending_source_count`, snapshot.ID, pendingSourceCount).Scan(
		&snapshot.ComponentCount, &snapshot.PendingSourceCount, &snapshot.PricingPendingSourceCount); err != nil {
		return Snapshot{}, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM token_query_snapshots WHERE expires_at <= statement_timestamp()`); err != nil {
		return Snapshot{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM token_query_snapshots
		WHERE id IN (
			SELECT id FROM token_query_snapshots
			WHERE user_id = $1
			ORDER BY created_at DESC, id DESC
			OFFSET $2
		)`, actor.ID, maxUserSnapshots); err != nil {
		return Snapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func activeComponentExistsSQL(where []string) string {
	return `
		SELECT EXISTS(
			SELECT 1
			FROM session_usage_components component
			JOIN session_metrics_revisions revision ON revision.id = component.revision_id
			JOIN session_source_metrics_states state ON state.source_id = revision.source_id
			JOIN sessions session ON session.id = component.session_id
			LEFT JOIN model_aliases alias ON alias.provider = component.provider
				AND alias.raw_model_pattern = COALESCE(component.raw_model, component.canonical_model)
				AND alias.status = 'reviewed'
			WHERE ` + strings.Join(where, " AND ") + `
		)`
}

func materializeItems(ctx context.Context, tx *sql.Tx, snapshotID string, where []string, args []any) error {
	args = append(args, snapshotID)
	snapshotPlaceholder := "$" + strconv.Itoa(len(args))
	_, err := tx.ExecContext(ctx, `
		INSERT INTO token_query_snapshot_items (
			snapshot_id, usage_component_id, cost_id, session_id, session_ref, agent_type,
			user_id, user_display_name, user_current_enabled,
			team_id_snapshot, team_name_snapshot, department_id_snapshot, department_name_snapshot,
			activity_date, occurred_at, provider, canonical_model, billing_variant,
			uncached_input_tokens, cache_read_tokens, cache_write_5m_tokens,
			cache_write_1h_tokens, output_tokens, normalized_total_tokens,
			quality_status, is_estimated, pricing_status, estimated_cost_usd, estimated_cost_cny
		)
		SELECT `+snapshotPlaceholder+`, component.id, cost.id, component.session_id, session.session_ref, session.agent_type,
			component.user_id, COALESCE(NULLIF(user_row.nickname, ''), NULLIF(user_row.name, ''), user_row.username, component.user_id::text),
			COALESCE(user_row.local_enabled, false), component.team_id_snapshot, team.name,
			component.department_id_snapshot, department.name, component.activity_date,
			component.occurred_at, component.provider,
			COALESCE(alias.canonical_model, component.canonical_model, component.raw_model), component.billing_variant,
			component.uncached_input_tokens, component.cache_read_tokens,
			component.cache_write_5m_tokens, component.cache_write_1h_tokens,
			component.output_tokens, component.normalized_total_tokens,
			component.quality_status, component.is_estimated,
			COALESCE(cost.pricing_status, 'pricing_pending'), cost.estimated_cost_usd, cost.estimated_cost_cny
		FROM session_usage_components component
		JOIN session_metrics_revisions revision ON revision.id = component.revision_id
		JOIN session_source_metrics_states state ON state.source_id = revision.source_id
		JOIN sessions session ON session.id = component.session_id
		LEFT JOIN users user_row ON user_row.id = component.user_id
		LEFT JOIN teams team ON team.id = component.team_id_snapshot
		LEFT JOIN departments department ON department.id = component.department_id_snapshot
		LEFT JOIN model_aliases alias ON alias.provider = component.provider
			AND alias.raw_model_pattern = COALESCE(component.raw_model, component.canonical_model)
			AND alias.status = 'reviewed'
		LEFT JOIN session_activity_costs cost ON cost.usage_component_id = component.id
			AND cost.calculator_version = 'aida-cost-v1' AND cost.superseded_at IS NULL
		WHERE `+strings.Join(where, " AND "), args...)
	return err
}

func countPendingSources(
	ctx context.Context,
	tx *sql.Tx,
	snapshotID string,
	actor Actor,
	filters Filters,
	searchMode string,
	from time.Time,
	to time.Time,
) (int64, error) {
	args := []any{snapshotID}
	appendArg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	chunkWhere := []string{
		"(state.status <> 'ready' OR state.active_usage_parsed_cursor < state.source_high_water_cursor)",
	}
	if searchMode == "exact_session_ref" {
		chunkWhere = append(chunkWhere, "session.session_ref = "+appendArg(filters.Query))
	} else {
		fromPlaceholder := appendArg(from.Format("2006-01-02"))
		toPlaceholder := appendArg(to.Format("2006-01-02"))
		chunkWhere = append(chunkWhere,
			"COALESCE(chunk.event_end_at, chunk.event_start_at, chunk.accepted_at)::date >= "+fromPlaceholder+"::date",
			"COALESCE(chunk.event_start_at, chunk.event_end_at, chunk.accepted_at)::date <= "+toPlaceholder+"::date",
		)
		if filters.Query != "" {
			chunkWhere = append(chunkWhere,
				"session.content_status = 'available'",
				"COALESCE(session.summary, '') ILIKE "+appendArg("%"+filters.Query+"%"),
			)
		}
	}

	currentWhere := append([]string{}, chunkWhere...)
	currentDepartment := "COALESCE(user_row.department_id, team.department_id)"
	switch filters.Scope {
	case "mine":
		currentWhere = append(currentWhere, "session.user_id = "+appendArg(actor.ID))
	case "management":
		switch actor.Role {
		case "team_leader":
			if actor.TeamID == nil {
				currentWhere = append(currentWhere, "false")
			} else {
				currentWhere = append(currentWhere, "user_row.team_id::text = "+appendArg(*actor.TeamID))
			}
		case "director":
			currentWhere = append(currentWhere,
				currentDepartment+" IN (SELECT id FROM departments WHERE director_user_id = "+appendArg(actor.ID)+")",
			)
		case "admin":
		default:
			return 0, ErrForbidden
		}
	default:
		return 0, ErrInvalidFilter
	}
	if filters.TeamID != "" {
		currentWhere = append(currentWhere, "user_row.team_id::text = "+appendArg(filters.TeamID))
	}
	if filters.DepartmentID != "" {
		currentWhere = append(currentWhere, currentDepartment+"::text = "+appendArg(filters.DepartmentID))
	}
	if filters.UserID != "" {
		currentWhere = append(currentWhere, "session.user_id::text = "+appendArg(filters.UserID))
	}
	if filters.Model != "" {
		// An unparsed source has no trustworthy model dimension yet. Sources with a
		// matching parsed component remain covered by represented_pending below.
		currentWhere = append(currentWhere, "false")
	}

	representedWhere := make([]string, 0, len(chunkWhere))
	for _, condition := range chunkWhere {
		representedWhere = append(representedWhere, strings.ReplaceAll(condition, "session.", "represented_session."))
	}
	query := `
		WITH represented_pending AS (
			SELECT DISTINCT revision.source_id
			FROM token_query_snapshot_items item
			JOIN session_usage_components component ON component.id = item.usage_component_id
			JOIN session_metrics_revisions revision ON revision.id = component.revision_id
			JOIN session_source_metrics_states state ON state.source_id = revision.source_id
			JOIN session_upload_chunks chunk ON chunk.generation_id = state.target_generation_id
			JOIN sessions represented_session ON represented_session.id = item.session_id
			WHERE item.snapshot_id = $1 AND ` + strings.Join(representedWhere, " AND ") + `
		), current_scope_pending AS (
			SELECT DISTINCT source.id AS source_id
			FROM session_sources source
			JOIN sessions session ON session.id = source.session_id
			JOIN users user_row ON user_row.id = session.user_id
			LEFT JOIN teams team ON team.id = user_row.team_id
			JOIN session_source_metrics_states state ON state.source_id = source.id
			JOIN session_upload_chunks chunk ON chunk.generation_id = state.target_generation_id
			WHERE ` + strings.Join(currentWhere, " AND ") + `
		)
		SELECT COUNT(*)
		FROM (
			SELECT source_id FROM represented_pending
			UNION
			SELECT source_id FROM current_scope_pending
		) pending_sources`
	var count int64
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func materializeMembers(ctx context.Context, tx *sql.Tx, snapshotID string, actor Actor, filters Filters) error {
	where := []string{"user_row.local_enabled = true", "user_row.aida_enabled = true"}
	args := []any{snapshotID}
	appendArg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	currentDepartment := "COALESCE(user_row.department_id, team.department_id)"
	switch filters.Scope {
	case "mine":
		where = append(where, "user_row.id = "+appendArg(actor.ID))
	case "management":
		switch actor.Role {
		case "team_leader":
			if actor.TeamID == nil {
				where = append(where, "false")
			} else {
				where = append(where, "user_row.team_id::text = "+appendArg(*actor.TeamID))
			}
		case "director":
			where = append(where, currentDepartment+" IN (SELECT id FROM departments WHERE director_user_id = "+appendArg(actor.ID)+")")
		case "admin":
		default:
			return ErrForbidden
		}
	}
	if filters.TeamID != "" {
		where = append(where, "user_row.team_id::text = "+appendArg(filters.TeamID))
	}
	if filters.DepartmentID != "" {
		where = append(where, currentDepartment+"::text = "+appendArg(filters.DepartmentID))
	}
	if filters.UserID != "" {
		where = append(where, "user_row.id::text = "+appendArg(filters.UserID))
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO token_query_snapshot_members(
			snapshot_id, user_id, user_display_name, team_id, team_name, department_id, department_name
		)
		SELECT $1, user_row.id,
			COALESCE(NULLIF(user_row.nickname, ''), NULLIF(user_row.name, ''), user_row.username, user_row.id::text),
			user_row.team_id, team.name, `+currentDepartment+`, department.name
		FROM users user_row
		LEFT JOIN teams team ON team.id = user_row.team_id
		LEFT JOIN departments department ON department.id = `+currentDepartment+`
		WHERE `+strings.Join(where, " AND ")+`
		ON CONFLICT DO NOTHING`, args...)
	return err
}

func buildScope(ctx context.Context, tx *sql.Tx, actor Actor, filters Filters, alias string) (string, []any, error) {
	switch filters.Scope {
	case "mine":
		if filters.UserID != "" && filters.UserID != strconv.FormatInt(actor.ID, 10) {
			return "", nil, ErrForbidden
		}
		if filters.TeamID != "" || filters.DepartmentID != "" {
			return "", nil, ErrInvalidFilter
		}
		return alias + ".user_id = $1", []any{actor.ID}, nil
	case "management":
		switch actor.Role {
		case "team_leader":
			if actor.TeamID == nil {
				return "false", nil, nil
			}
			if filters.TeamID != "" && filters.TeamID != *actor.TeamID {
				return "", nil, ErrForbidden
			}
			return alias + ".team_id_snapshot::text = $1", []any{*actor.TeamID}, nil
		case "director":
			rows, err := tx.QueryContext(ctx, `SELECT id::text FROM departments WHERE director_user_id = $1 ORDER BY id`, actor.ID)
			if err != nil {
				return "", nil, err
			}
			defer rows.Close()
			departments := []string{}
			owned := map[string]bool{}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					return "", nil, err
				}
				departments = append(departments, id)
				owned[id] = true
			}
			if err := rows.Err(); err != nil {
				return "", nil, err
			}
			if filters.DepartmentID != "" && !owned[filters.DepartmentID] {
				return "", nil, ErrForbidden
			}
			if len(departments) == 0 {
				return "false", nil, nil
			}
			placeholders := make([]string, 0, len(departments))
			args := make([]any, 0, len(departments))
			for index, id := range departments {
				args = append(args, id)
				placeholders = append(placeholders, "$"+strconv.Itoa(index+1)+"::uuid")
			}
			return alias + ".department_id_snapshot IN (" + strings.Join(placeholders, ",") + ")", args, nil
		case "admin":
			return "true", nil, nil
		default:
			return "", nil, ErrForbidden
		}
	default:
		return "", nil, ErrInvalidFilter
	}
}

func appendOrganizationFilters(where []string, args []any, filters Filters, alias string) ([]string, []any) {
	if filters.TeamID != "" {
		args = append(args, filters.TeamID)
		where = append(where, fmt.Sprintf("%s.team_id_snapshot::text = $%d", alias, len(args)))
	}
	if filters.DepartmentID != "" {
		args = append(args, filters.DepartmentID)
		where = append(where, fmt.Sprintf("%s.department_id_snapshot::text = $%d", alias, len(args)))
	}
	if filters.UserID != "" {
		args = append(args, filters.UserID)
		where = append(where, fmt.Sprintf("%s.user_id::text = $%d", alias, len(args)))
	}
	return where, args
}

func (s *Service) loadSnapshot(ctx context.Context, actor Actor, raw Filters, token string) (Snapshot, error) {
	filters, _, _, err := normalizeFilters(raw)
	if err != nil {
		return Snapshot{}, err
	}
	if strings.TrimSpace(token) == "" {
		return Snapshot{}, ErrInvalidFilter
	}
	var snapshot Snapshot
	var storedFilters []byte
	err = s.db.QueryRowContext(ctx, `
		SELECT id::text, scope, search_mode, filters_json, metrics_snapshot_at, expires_at,
			component_count, pending_source_count, pricing_pending_source_count
		FROM token_query_snapshots
		WHERE token_hash = $1 AND user_id = $2`, snapshotTokenHash(token), actor.ID).Scan(
		&snapshot.ID, &snapshot.Scope, &snapshot.SearchMode, &storedFilters,
		&snapshot.MetricsSnapshotAt, &snapshot.ExpiresAt, &snapshot.ComponentCount,
		&snapshot.PendingSourceCount, &snapshot.PricingPendingSourceCount)
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, ErrSnapshotExpired
	}
	if err != nil {
		return Snapshot{}, err
	}
	if !snapshot.ExpiresAt.After(time.Now()) {
		return Snapshot{}, ErrSnapshotExpired
	}
	if err := json.Unmarshal(storedFilters, &snapshot.Filters); err != nil {
		return Snapshot{}, err
	}
	requestedJSON, _ := json.Marshal(filters)
	storedJSON, _ := json.Marshal(snapshot.Filters)
	if string(requestedJSON) != string(storedJSON) {
		return Snapshot{}, ErrSnapshotMismatch
	}
	snapshot.Token = token
	return snapshot, nil
}

func (s *Service) summary(ctx context.Context, snapshot Snapshot) (Summary, error) {
	result := Summary{
		QuerySnapshotToken:        snapshot.Token,
		MetricsSnapshotAt:         snapshot.MetricsSnapshotAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt:                 snapshot.ExpiresAt.UTC().Format(time.RFC3339Nano),
		SearchMode:                snapshot.SearchMode,
		Scope:                     snapshot.Scope,
		From:                      snapshot.Filters.From,
		To:                        snapshot.Filters.To,
		PendingSourceCount:        strconv.FormatInt(snapshot.PendingSourceCount, 10),
		PricingPendingSourceCount: strconv.FormatInt(snapshot.PricingPendingSourceCount, 10),
		ComponentCount:            strconv.FormatInt(snapshot.ComponentCount, 10),
	}
	var costUSD, costCNY sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(normalized_total_tokens), 0)::text,
			COALESCE(SUM(uncached_input_tokens), 0)::text,
			COALESCE(SUM(cache_read_tokens), 0)::text,
			COALESCE(SUM(cache_write_5m_tokens), 0)::text,
			COALESCE(SUM(cache_write_1h_tokens), 0)::text,
			COALESCE(SUM(output_tokens), 0)::text,
			COUNT(DISTINCT activity_date)::text,
			SUM(estimated_cost_usd) FILTER (WHERE pricing_status = 'priced')::text,
			SUM(estimated_cost_cny) FILTER (WHERE pricing_status = 'priced')::text,
			CASE
				WHEN COUNT(*) = 0 THEN 'unpriced'
				WHEN COUNT(*) FILTER (WHERE pricing_status = 'pricing_pending') > 0 THEN 'pricing_pending'
				WHEN COUNT(*) FILTER (WHERE pricing_status = 'priced') = COUNT(*) THEN 'priced'
				WHEN COUNT(*) FILTER (WHERE pricing_status = 'priced') > 0 THEN 'partially_priced'
				ELSE 'unpriced'
			END,
			COALESCE(SUM(normalized_total_tokens) FILTER (WHERE pricing_status <> 'priced'), 0)::text,
			CASE
				WHEN BOOL_OR(quality_status = 'conflict') THEN 'conflict'
				WHEN BOOL_OR(quality_status = 'incomplete') THEN 'incomplete'
				WHEN BOOL_OR(quality_status = 'estimated' OR is_estimated) THEN 'estimated'
				ELSE 'exact'
			END
		FROM token_query_snapshot_items
		WHERE snapshot_id = $1`, snapshot.ID).Scan(
		&result.TotalTokens, &result.UncachedInputTokens, &result.CacheReadTokens,
		&result.CacheWrite5mTokens, &result.CacheWrite1hTokens, &result.OutputTokens,
		&result.ActiveDays, &costUSD, &costCNY, &result.PricingStatus,
		&result.UnpricedTokens, &result.QualityStatus)
	if err != nil {
		return Summary{}, err
	}
	result.EstimatedCostUSD = stringPtr(costUSD)
	result.EstimatedCostCNY = stringPtr(costCNY)
	result.DataFreshness = "ready"
	if snapshot.PendingSourceCount > 0 {
		result.DataFreshness = "pending"
	}
	return result, nil
}

func normalizeFilters(raw Filters) (Filters, time.Time, time.Time, error) {
	filters := Filters{
		Scope:        strings.TrimSpace(raw.Scope),
		From:         strings.TrimSpace(raw.From),
		To:           strings.TrimSpace(raw.To),
		TeamID:       strings.TrimSpace(raw.TeamID),
		DepartmentID: strings.TrimSpace(raw.DepartmentID),
		UserID:       strings.TrimSpace(raw.UserID),
		Model:        strings.TrimSpace(raw.Model),
		Query:        strings.TrimSpace(raw.Query),
	}
	if filters.Scope != "mine" && filters.Scope != "management" {
		return Filters{}, time.Time{}, time.Time{}, ErrInvalidFilter
	}
	from, err := time.Parse("2006-01-02", filters.From)
	if err != nil {
		return Filters{}, time.Time{}, time.Time{}, ErrInvalidFilter
	}
	to, err := time.Parse("2006-01-02", filters.To)
	if err != nil || to.Before(from) {
		return Filters{}, time.Time{}, time.Time{}, ErrInvalidFilter
	}
	if filters.UserID != "" {
		if _, err := strconv.ParseInt(filters.UserID, 10, 64); err != nil {
			return Filters{}, time.Time{}, time.Time{}, ErrInvalidFilter
		}
	}
	return filters, from, to, nil
}

func newSnapshotToken() (string, string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(buffer)
	return raw, snapshotTokenHash(raw), nil
}

func snapshotTokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}
