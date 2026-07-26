package tokenanalytics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

const rollupSnapshotVersion = "rollup-v2"

var snapshotRetryDelays = []time.Duration{
	50 * time.Millisecond,
	150 * time.Millisecond,
	350 * time.Millisecond,
}

func (s *Service) createSnapshot(ctx context.Context, actor Actor, raw Filters) (Snapshot, error) {
	for attempt := 0; ; attempt++ {
		snapshot, err := s.createSnapshotOnce(ctx, actor, raw)
		if err == nil || !isSerializationConflict(err) {
			return snapshot, err
		}
		if attempt >= len(snapshotRetryDelays) {
			log.Printf("token snapshot serialization retries exhausted scope=%s attempts=%d: %v",
				raw.Scope, attempt+1, err)
			return Snapshot{}, ErrSnapshotBusy
		}

		baseDelay := snapshotRetryDelays[attempt]
		jitter := time.Duration(rand.Int63n(int64(baseDelay/4) + 1))
		timer := time.NewTimer(baseDelay + jitter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Snapshot{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func isSerializationConflict(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && string(pqErr.Code) == "40001"
}

func (s *Service) createSnapshotOnce(ctx context.Context, actor Actor, raw Filters) (Snapshot, error) {
	filters, from, to, err := normalizeFilters(raw)
	if err != nil {
		return Snapshot{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback()

	searchMode := "filtered"
	if filters.Query != "" {
		exact, err := exactRollupSessionExists(ctx, tx, actor, filters)
		if err != nil {
			return Snapshot{}, err
		}
		if exact {
			searchMode = "exact_session_ref"
		}
	} else {
		if _, _, err := buildScope(ctx, tx, actor, filters, "daily"); err != nil {
			return Snapshot{}, err
		}
	}

	rawToken, tokenHash, err := newSnapshotToken()
	if err != nil {
		return Snapshot{}, err
	}
	filtersJSON, _ := json.Marshal(filters)
	snapshot := Snapshot{
		Token: rawToken, Version: rollupSnapshotVersion, Scope: filters.Scope,
		SearchMode: searchMode, Filters: filters,
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO token_query_snapshots (
			token_hash, user_id, scope, search_mode, filters_json,
			metrics_snapshot_at, expires_at, snapshot_version
		) VALUES ($1, $2, $3, $4, $5::jsonb,
			statement_timestamp(), statement_timestamp() + $6::interval, $7)
		RETURNING id::text, metrics_snapshot_at, expires_at`, tokenHash, actor.ID,
		filters.Scope, searchMode, string(filtersJSON), snapshotTTL.String(), rollupSnapshotVersion).Scan(
		&snapshot.ID, &snapshot.MetricsSnapshotAt, &snapshot.ExpiresAt); err != nil {
		return Snapshot{}, err
	}
	if err := materializeRollupReferences(ctx, tx, snapshot.ID, actor, filters, searchMode, from, to); err != nil {
		return Snapshot{}, err
	}
	if err := materializeMembers(ctx, tx, snapshot.ID, actor, filters); err != nil {
		return Snapshot{}, err
	}
	if err := materializeRollupMembers(ctx, tx, snapshot.ID); err != nil {
		return Snapshot{}, err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(version.contribution_count), 0),
			COUNT(*) FILTER (WHERE EXISTS (
				SELECT 1 FROM session_family_token_totals total_row
				WHERE total_row.rollup_version_id = reference.rollup_version_id
					AND total_row.pricing_status = 'pricing_pending'
			))
		FROM token_query_snapshot_rollups reference
		JOIN session_family_rollup_versions version ON version.id = reference.rollup_version_id
		WHERE reference.snapshot_id = $1`, snapshot.ID).Scan(
		&snapshot.RollupCount, &snapshot.ComponentCount, &snapshot.PricingPendingSourceCount); err != nil {
		return Snapshot{}, err
	}
	snapshot.PendingSourceCount, err = countRollupPendingSources(ctx, tx, snapshot.ID, actor, filters, searchMode, from, to)
	if err != nil {
		return Snapshot{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE token_query_snapshots
		SET component_count = $2, rollup_count = $3,
			pending_source_count = $4, pricing_pending_source_count = $5
		WHERE id = $1`, snapshot.ID, snapshot.ComponentCount, snapshot.RollupCount,
		snapshot.PendingSourceCount, snapshot.PricingPendingSourceCount); err != nil {
		return Snapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func materializeRollupMembers(ctx context.Context, tx *sql.Tx, snapshotID string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO token_query_snapshot_members(snapshot_id, user_id, user_display_name)
		SELECT DISTINCT reference.snapshot_id, reference.user_id, reference.user_display_name
		FROM token_query_snapshot_rollups reference
		WHERE reference.snapshot_id = $1
		ON CONFLICT (snapshot_id, user_id) DO NOTHING`, snapshotID)
	return err
}

func exactRollupSessionExists(ctx context.Context, tx *sql.Tx, actor Actor, filters Filters) (bool, error) {
	scope, args, err := buildScope(ctx, tx, actor, filters, "total_row")
	if err != nil {
		return false, err
	}
	where := []string{scope}
	where, args = appendOrganizationFilters(where, args, filters, "total_row")
	args = append(args, filters.Query)
	where = append(where, fmt.Sprintf("matched.session_ref = $%d", len(args)))
	var exact bool
	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM session_family_rollup_versions rollup
			JOIN session_family_memberships membership
				ON membership.family_version_id = rollup.family_version_id
				AND membership.valid_to IS NULL
			JOIN sessions matched ON matched.id = membership.member_session_id
			JOIN session_family_token_totals total_row
				ON total_row.rollup_version_id = rollup.id
			WHERE rollup.status = 'active' AND `+strings.Join(where, " AND ")+`
		)`, args...).Scan(&exact)
	return exact, err
}

func materializeRollupReferences(
	ctx context.Context,
	tx *sql.Tx,
	snapshotID string,
	actor Actor,
	filters Filters,
	searchMode string,
	from, to time.Time,
) error {
	if searchMode == "exact_session_ref" {
		return materializeExactRollupReferences(ctx, tx, snapshotID, actor, filters)
	}
	scope, args, err := buildScope(ctx, tx, actor, filters, "daily")
	if err != nil {
		return err
	}
	where := []string{scope}
	where, args = appendOrganizationFilters(where, args, filters, "daily")
	args = append(args, from.Format("2006-01-02"))
	where = append(where, fmt.Sprintf("daily.activity_date >= $%d::date", len(args)))
	args = append(args, to.Format("2006-01-02"))
	where = append(where, fmt.Sprintf("daily.activity_date <= $%d::date", len(args)))
	if filters.Model != "" {
		args = append(args, filters.Model)
		where = append(where, fmt.Sprintf("daily.canonical_model = $%d", len(args)))
	}
	searchWhere := "true"
	if filters.Query != "" {
		args = append(args, "%"+filters.Query+"%")
		searchWhere = fmt.Sprintf(`EXISTS (
			SELECT 1 FROM session_family_memberships searched_membership
			JOIN sessions searched ON searched.id = searched_membership.member_session_id
			WHERE searched_membership.family_version_id = family.id
				AND searched_membership.valid_to IS NULL
				AND searched.content_status = 'available'
				AND COALESCE(searched.summary, '') ILIKE $%d
		)`, len(args))
	}
	args = append(args, snapshotID)
	snapshotPlaceholder := "$" + strconv.Itoa(len(args))
	_, err = tx.ExecContext(ctx, `
		INSERT INTO token_query_snapshot_rollups (
			snapshot_id, rollup_version_id, family_version_id, root_session_id,
			root_session_ref, agent_type, user_id, user_display_name, summary_snapshot
		)
		SELECT DISTINCT `+snapshotPlaceholder+`::uuid, rollup.id, family.id, root.id,
			root.session_ref, root.agent_type, family.user_id,
			COALESCE(NULLIF(user_row.nickname, ''), NULLIF(user_row.name, ''),
				user_row.username, family.user_id::text),
			CASE WHEN root.content_status = 'available' THEN root.summary END
		FROM session_family_rollup_versions rollup
		JOIN session_family_versions family ON family.id = rollup.family_version_id
		JOIN sessions root ON root.id = rollup.root_session_id
		JOIN users user_row ON user_row.id = family.user_id
		WHERE rollup.status = 'active' AND family.status = 'active'
			AND EXISTS (
				SELECT 1 FROM session_family_daily_usage daily
				WHERE daily.rollup_version_id = rollup.id
					AND `+strings.Join(where, " AND ")+`
			)
			AND `+searchWhere+`
		ON CONFLICT DO NOTHING`, args...)
	return err
}

func materializeExactRollupReferences(
	ctx context.Context,
	tx *sql.Tx,
	snapshotID string,
	actor Actor,
	filters Filters,
) error {
	scope, args, err := buildScope(ctx, tx, actor, filters, "total_row")
	if err != nil {
		return err
	}
	where := []string{scope}
	where, args = appendOrganizationFilters(where, args, filters, "total_row")
	args = append(args, filters.Query)
	queryPlaceholder := "$" + strconv.Itoa(len(args))
	args = append(args, snapshotID)
	snapshotPlaceholder := "$" + strconv.Itoa(len(args))
	_, err = tx.ExecContext(ctx, `
		INSERT INTO token_query_snapshot_rollups (
			snapshot_id, rollup_version_id, family_version_id, root_session_id,
			root_session_ref, agent_type, user_id, user_display_name, summary_snapshot,
			matched_member_session_id, matched_member_session_ref
		)
		SELECT DISTINCT ON (rollup.root_session_id)
			`+snapshotPlaceholder+`::uuid, rollup.id, family.id, root.id,
			root.session_ref, root.agent_type, family.user_id,
			COALESCE(NULLIF(user_row.nickname, ''), NULLIF(user_row.name, ''),
				user_row.username, family.user_id::text),
			CASE WHEN root.content_status = 'available' THEN root.summary END,
			matched.id, matched.session_ref
		FROM session_family_rollup_versions rollup
		JOIN session_family_versions family ON family.id = rollup.family_version_id
		JOIN sessions root ON root.id = rollup.root_session_id
		JOIN users user_row ON user_row.id = family.user_id
		JOIN session_family_memberships membership
			ON membership.family_version_id = family.id AND membership.valid_to IS NULL
		JOIN sessions matched ON matched.id = membership.member_session_id
		WHERE rollup.status = 'active' AND family.status = 'active'
			AND matched.session_ref = `+queryPlaceholder+`
			AND EXISTS (
				SELECT 1 FROM session_family_token_totals total_row
				WHERE total_row.rollup_version_id = rollup.id
					AND `+strings.Join(where, " AND ")+`
			)
		ORDER BY rollup.root_session_id, matched.id
		ON CONFLICT DO NOTHING`, args...)
	return err
}

func countRollupPendingSources(
	ctx context.Context,
	tx *sql.Tx,
	snapshotID string,
	actor Actor,
	filters Filters,
	searchMode string,
	from, to time.Time,
) (int64, error) {
	args := []any{snapshotID}
	appendArg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	chunkWhere := []string{
		"(state.status <> 'ready' OR state.active_usage_parsed_cursor < state.source_high_water_cursor)",
	}
	if searchMode != "exact_session_ref" {
		chunkWhere = append(chunkWhere,
			"(COALESCE(chunk.event_end_at, chunk.event_start_at, chunk.accepted_at) AT TIME ZONE 'Asia/Shanghai')::date >= "+appendArg(from.Format("2006-01-02"))+"::date",
			"(COALESCE(chunk.event_start_at, chunk.event_end_at, chunk.accepted_at) AT TIME ZONE 'Asia/Shanghai')::date <= "+appendArg(to.Format("2006-01-02"))+"::date",
		)
	}
	where := append([]string{}, chunkWhere...)
	currentDepartment := "COALESCE(user_row.department_id, team.department_id)"
	switch filters.Scope {
	case "mine":
		where = append(where, "session.user_id = "+appendArg(actor.ID))
	case "management":
		switch actor.Role {
		case "team_leader":
			if actor.TeamID == nil {
				where = append(where, "false")
			} else {
				where = append(where, "user_row.team_id::text = "+appendArg(*actor.TeamID))
			}
		case "director":
			where = append(where,
				currentDepartment+" IN (SELECT id FROM departments WHERE director_user_id = "+appendArg(actor.ID)+")")
		case "admin":
		default:
			return 0, ErrForbidden
		}
	default:
		return 0, ErrInvalidFilter
	}
	if filters.TeamID != "" {
		where = append(where, "user_row.team_id::text = "+appendArg(filters.TeamID))
	}
	if filters.DepartmentID != "" {
		where = append(where, currentDepartment+"::text = "+appendArg(filters.DepartmentID))
	}
	if filters.UserID != "" {
		where = append(where, "session.user_id::text = "+appendArg(filters.UserID))
	}
	if searchMode == "exact_session_ref" {
		where = append(where, "session.session_ref = "+appendArg(filters.Query))
	} else {
		if filters.Query != "" {
			where = append(where, "session.content_status = 'available'",
				"COALESCE(session.summary, '') ILIKE "+appendArg("%"+filters.Query+"%"))
		}
		if filters.Model != "" {
			where = append(where, "false")
		}
	}
	var count int64
	err := tx.QueryRowContext(ctx, `
		WITH represented_pending AS (
			SELECT DISTINCT source.id AS source_id
			FROM token_query_snapshot_rollups reference
			JOIN session_family_memberships membership
				ON membership.family_version_id = reference.family_version_id
				AND membership.valid_to IS NULL
			JOIN session_sources source ON source.session_id = membership.member_session_id
			JOIN session_source_metrics_states state ON state.source_id = source.id
			JOIN session_upload_chunks chunk ON chunk.generation_id = state.target_generation_id
			WHERE reference.snapshot_id = $1 AND `+strings.Join(chunkWhere, " AND ")+`
		), current_scope_pending AS (
			SELECT DISTINCT source.id AS source_id
			FROM session_sources source
			JOIN sessions session ON session.id = source.session_id
			JOIN users user_row ON user_row.id = session.user_id
			LEFT JOIN teams team ON team.id = user_row.team_id
			JOIN session_source_metrics_states state ON state.source_id = source.id
			JOIN session_upload_chunks chunk ON chunk.generation_id = state.target_generation_id
			WHERE `+strings.Join(where, " AND ")+`
		)
		SELECT COUNT(*) FROM (
			SELECT source_id FROM represented_pending
			UNION
			SELECT source_id FROM current_scope_pending
		) pending_sources`, args...).Scan(&count)
	return count, err
}

func (s *Service) rollupSummary(ctx context.Context, actor Actor, snapshot Snapshot) (Summary, error) {
	where, args, err := s.rollupPredicates(ctx, actor, snapshot, "daily", []any{snapshot.ID}, true, true)
	if err != nil {
		return Summary{}, err
	}
	result := Summary{
		QuerySnapshotToken: snapshot.Token, MetricsSnapshotAt: snapshot.MetricsSnapshotAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt: snapshot.ExpiresAt.UTC().Format(time.RFC3339Nano), SearchMode: snapshot.SearchMode,
		Scope: snapshot.Scope, From: snapshot.Filters.From, To: snapshot.Filters.To,
		PendingSourceCount:        strconv.FormatInt(snapshot.PendingSourceCount, 10),
		PricingPendingSourceCount: strconv.FormatInt(snapshot.PricingPendingSourceCount, 10),
		ComponentCount:            strconv.FormatInt(snapshot.ComponentCount, 10),
		RollupCount:               strconv.FormatInt(snapshot.RollupCount, 10),
	}
	var costUSD, costCNY sql.NullString
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(daily.total_tokens), 0)::text,
			COALESCE(SUM(daily.uncached_input_tokens), 0)::text,
			COALESCE(SUM(daily.cache_read_tokens), 0)::text,
			COALESCE(SUM(daily.cache_write_5m_tokens), 0)::text,
			COALESCE(SUM(daily.cache_write_1h_tokens), 0)::text,
			COALESCE(SUM(daily.output_tokens), 0)::text,
			COUNT(DISTINCT daily.activity_date)::text,
			SUM(daily.estimated_cost_usd)::text, SUM(daily.estimated_cost_cny)::text,
			`+pricingStatusSQL("daily")+`,
			COALESCE(SUM(daily.total_tokens) FILTER (WHERE daily.pricing_status <> 'priced'), 0)::text,
			`+qualityStatusSQL("daily")+`,
			COUNT(DISTINCT reference.root_session_id)
				FILTER (WHERE daily.pricing_status = 'pricing_pending')::text,
			COALESCE(SUM(daily.contribution_count), 0)::text,
			COUNT(DISTINCT reference.root_session_id)::text
		FROM token_query_snapshot_rollups reference
		JOIN session_family_daily_usage daily ON daily.rollup_version_id = reference.rollup_version_id
		WHERE `+strings.Join(where, " AND "), args...).Scan(
		&result.TotalTokens, &result.UncachedInputTokens, &result.CacheReadTokens,
		&result.CacheWrite5mTokens, &result.CacheWrite1hTokens, &result.OutputTokens,
		&result.ActiveDays, &costUSD, &costCNY, &result.PricingStatus,
		&result.UnpricedTokens, &result.QualityStatus, &result.PricingPendingSourceCount,
		&result.ComponentCount, &result.SessionCount)
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

func (s *Service) rollupTrends(ctx context.Context, actor Actor, snapshot Snapshot) (Trends, error) {
	where, args, err := s.rollupPredicates(ctx, actor, snapshot, "daily", []any{snapshot.ID}, true, true)
	if err != nil {
		return Trends{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT daily.activity_date::text, SUM(daily.total_tokens)::text,
			SUM(daily.estimated_cost_cny)::text, `+pricingStatusSQL("daily")+`
		FROM token_query_snapshot_rollups reference
		JOIN session_family_daily_usage daily ON daily.rollup_version_id = reference.rollup_version_id
		WHERE `+strings.Join(where, " AND ")+`
		GROUP BY daily.activity_date ORDER BY daily.activity_date`, args...)
	if err != nil {
		return Trends{}, err
	}
	defer rows.Close()
	result := Trends{QuerySnapshotToken: snapshot.Token, Items: []TrendPoint{}}
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

func (s *Service) rollupRankings(ctx context.Context, actor Actor, snapshot Snapshot, groupBy string) (Rankings, error) {
	where, args, err := s.rollupPredicates(ctx, actor, snapshot, "daily", []any{snapshot.ID}, true, true)
	if err != nil {
		return Rankings{}, err
	}
	result := Rankings{QuerySnapshotToken: snapshot.Token, GroupBy: groupBy, Items: []RankingItem{}}
	var rows *sql.Rows
	if groupBy == "user" {
		rows, err = s.db.QueryContext(ctx, `
			WITH usage AS (
				SELECT daily.user_id, SUM(daily.total_tokens) AS total_tokens,
					COUNT(DISTINCT reference.root_session_id) AS session_count,
					SUM(daily.estimated_cost_cny) AS estimated_cost_cny,
					`+pricingStatusSQL("daily")+` AS pricing_status,
					MAX(daily.activity_date)::text AS last_activity_at
				FROM token_query_snapshot_rollups reference
				JOIN session_family_daily_usage daily ON daily.rollup_version_id = reference.rollup_version_id
				WHERE `+strings.Join(where, " AND ")+`
				GROUP BY daily.user_id
			)
			SELECT member.user_id::text, member.user_display_name,
				COALESCE(usage.total_tokens, 0)::text, COALESCE(usage.session_count, 0)::text,
				usage.estimated_cost_cny::text,
				COALESCE(usage.pricing_status, 'unpriced'), usage.last_activity_at
			FROM token_query_snapshot_members member
			LEFT JOIN usage ON usage.user_id = member.user_id
			WHERE member.snapshot_id = $1
			ORDER BY COALESCE(usage.total_tokens, 0) DESC,
				usage.last_activity_at DESC NULLS LAST, member.user_display_name`, args...)
	} else {
		var key, label, joins string
		switch groupBy {
		case "team":
			key = "COALESCE(daily.team_id_snapshot::text, 'unknown')"
			label = "COALESCE(team.name, '未归属小组')"
			joins = "LEFT JOIN teams team ON team.id = daily.team_id_snapshot"
		case "department":
			key = "COALESCE(daily.department_id_snapshot::text, 'unknown')"
			label = "COALESCE(department.name, '未归属部门')"
			joins = "LEFT JOIN departments department ON department.id = daily.department_id_snapshot"
		case "model":
			key = "COALESCE(NULLIF(daily.canonical_model, ''), 'unknown')"
			label = key
		case "requirement":
			key = "COALESCE(root.requirement_id::text, 'none')"
			label = "COALESCE(requirement.title, '未关联需求')"
			joins = `JOIN sessions root ON root.id = reference.root_session_id
				LEFT JOIN requirements requirement ON requirement.id = root.requirement_id`
		case "task":
			key = "COALESCE(root.task_id::text, 'none')"
			label = "COALESCE(task.title, '未关联任务')"
			joins = `JOIN sessions root ON root.id = reference.root_session_id
				LEFT JOIN tasks task ON task.id = root.task_id`
		default:
			return Rankings{}, ErrInvalidFilter
		}
		query := fmt.Sprintf(`
			SELECT %s, %s, SUM(daily.total_tokens)::text,
				COUNT(DISTINCT reference.root_session_id)::text,
				SUM(daily.estimated_cost_cny)::text, %s, MAX(daily.activity_date)::text
			FROM token_query_snapshot_rollups reference
			JOIN session_family_daily_usage daily ON daily.rollup_version_id = reference.rollup_version_id
			%s
			WHERE %s
			GROUP BY %s, %s
			ORDER BY SUM(daily.total_tokens) DESC, MAX(daily.activity_date) DESC, %s`,
			key, label, pricingStatusSQL("daily"), joins, strings.Join(where, " AND "), key, label, label)
		rows, err = s.db.QueryContext(ctx, query, args...)
	}
	if err != nil {
		return Rankings{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item RankingItem
		var cost, lastActivity sql.NullString
		if err := rows.Scan(&item.Key, &item.Label, &item.TotalTokens, &item.SessionCount, &cost,
			&item.PricingStatus, &lastActivity); err != nil {
			return Rankings{}, err
		}
		item.EstimatedCostCNY = stringPtr(cost)
		item.LastActivityAt = stringPtr(lastActivity)
		item.IsZeroUsage = item.TotalTokens == "0"
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

func (s *Service) rollupSessions(ctx context.Context, actor Actor, snapshot Snapshot, page, pageSize int) (Sessions, error) {
	args := []any{snapshot.ID}
	rangeWhere, args, err := s.rollupPredicates(ctx, actor, snapshot, "daily", args, true, true)
	if err != nil {
		return Sessions{}, err
	}
	lifetimeWhere, args, err := s.rollupPredicates(ctx, actor, snapshot, "lifetime", args, false, true)
	if err != nil {
		return Sessions{}, err
	}
	unavailableWhere, args, err := s.unavailableSessionPredicates(ctx, actor, snapshot, args)
	if err != nil {
		return Sessions{}, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	limitPlaceholder := "$" + strconv.Itoa(len(args)-1)
	offsetPlaceholder := "$" + strconv.Itoa(len(args))
	rows, err := s.db.QueryContext(ctx, `
		WITH range_usage AS (
			SELECT reference.rollup_version_id,
				MIN(daily.activity_date)::text AS activity_from,
				MAX(daily.activity_date)::text AS activity_to,
				array_agg(DISTINCT daily.activity_date::text ORDER BY daily.activity_date::text) AS activity_dates,
				COALESCE(string_agg(DISTINCT NULLIF(daily.canonical_model, ''), ', '
					ORDER BY NULLIF(daily.canonical_model, '')), 'unknown') AS models,
				SUM(daily.total_tokens) AS range_total_tokens,
				SUM(daily.uncached_input_tokens) AS uncached_input_tokens,
				SUM(daily.cache_read_tokens) AS cache_read_tokens,
				SUM(daily.cache_write_5m_tokens) AS cache_write_5m_tokens,
				SUM(daily.cache_write_1h_tokens) AS cache_write_1h_tokens,
				SUM(daily.output_tokens) AS output_tokens,
				SUM(daily.self_total_tokens) AS self_total_tokens,
				SUM(daily.subagent_total_tokens) AS subagent_total_tokens,
				SUM(daily.estimated_cost_cny) AS estimated_cost_cny,
				`+pricingStatusSQL("daily")+` AS pricing_status,
				`+qualityStatusSQL("daily")+` AS quality_status
			FROM token_query_snapshot_rollups reference
			JOIN session_family_daily_usage daily ON daily.rollup_version_id = reference.rollup_version_id
			WHERE `+strings.Join(rangeWhere, " AND ")+`
			GROUP BY reference.rollup_version_id
		), lifetime_usage AS (
			SELECT reference.rollup_version_id,
				SUM(lifetime.total_tokens) AS lifetime_total_tokens
			FROM token_query_snapshot_rollups reference
			JOIN session_family_token_totals lifetime ON lifetime.rollup_version_id = reference.rollup_version_id
			WHERE `+strings.Join(lifetimeWhere, " AND ")+`
			GROUP BY reference.rollup_version_id
		), available_sessions AS (
		SELECT reference.root_session_id::text AS session_id, reference.root_session_ref AS session_ref,
			reference.user_id::text, reference.user_display_name, reference.agent_type,
			'available'::text AS usage_status, reference.summary_snapshot, root.started_at,
			range_usage.activity_from, range_usage.activity_to, range_usage.activity_dates,
			range_usage.models, range_usage.range_total_tokens::text,
			range_usage.uncached_input_tokens::text, range_usage.cache_read_tokens::text,
			range_usage.cache_write_5m_tokens::text, range_usage.cache_write_1h_tokens::text,
			range_usage.output_tokens::text,
			range_usage.self_total_tokens::text, range_usage.subagent_total_tokens::text,
			COALESCE(lifetime_usage.lifetime_total_tokens, 0)::text,
			range_usage.estimated_cost_cny::text, range_usage.pricing_status,
			range_usage.quality_status, family.member_count,
			reference.matched_member_session_id::text,
			reference.matched_member_session_ref
		FROM token_query_snapshot_rollups reference
		JOIN range_usage ON range_usage.rollup_version_id = reference.rollup_version_id
		LEFT JOIN lifetime_usage ON lifetime_usage.rollup_version_id = reference.rollup_version_id
		JOIN session_family_versions family ON family.id = reference.family_version_id
		JOIN sessions root ON root.id = reference.root_session_id
		WHERE reference.snapshot_id = $1
		), unavailable_sessions AS (
			SELECT session.id::text AS session_id, session.session_ref,
				session.user_id::text, COALESCE(NULLIF(user_row.nickname, ''),
					NULLIF(user_row.name, ''), user_row.username, session.user_id::text),
				session.agent_type, 'unavailable'::text AS usage_status, session.summary,
				session.started_at, COALESCE(session.last_activity_at, session.started_at)::date::text,
				COALESCE(session.last_activity_at, session.started_at)::date::text,
				ARRAY[COALESCE(session.last_activity_at, session.started_at)::date::text],
				'unknown'::text, ''::text, ''::text, ''::text, ''::text, ''::text,
				''::text, ''::text, ''::text, ''::text, NULL::text,
				'unavailable'::text, 'unavailable'::text, 1,
				NULL::text, NULL::text
			FROM sessions session
			JOIN users user_row ON user_row.id = session.user_id
			LEFT JOIN teams current_team ON current_team.id = user_row.team_id
			WHERE `+strings.Join(unavailableWhere, " AND ")+`
		), combined_sessions AS (
			SELECT * FROM available_sessions
			UNION ALL
			SELECT * FROM unavailable_sessions
		)
		SELECT combined_sessions.*, COUNT(*) OVER()::int
		FROM combined_sessions
		ORDER BY activity_to DESC, session_ref
		LIMIT `+limitPlaceholder+` OFFSET `+offsetPlaceholder, args...)
	if err != nil {
		return Sessions{}, err
	}
	defer rows.Close()
	result := Sessions{QuerySnapshotToken: snapshot.Token, SearchMode: snapshot.SearchMode,
		Items: []SessionItem{}, Page: page, PageSize: pageSize}
	for rows.Next() {
		var item SessionItem
		var summary, cost, matchedID, matchedRef sql.NullString
		var activityDates pq.StringArray
		var startedAt time.Time
		var total int
		if err := rows.Scan(&item.SessionID, &item.SessionRef, &item.UserID, &item.UserName,
			&item.AgentType, &item.UsageStatus, &summary, &startedAt, &item.ActivityFrom, &item.ActivityTo,
			&activityDates, &item.Model, &item.RangeTotalTokens,
			&item.UncachedInputTokens, &item.CacheReadTokens, &item.CacheWrite5mTokens,
			&item.CacheWrite1hTokens, &item.OutputTokens,
			&item.SelfTotalTokens, &item.SubagentTotalTokens,
			&item.LifetimeTotalTokens, &cost, &item.PricingStatus, &item.QualityStatus,
			&item.MemberCount, &matchedID, &matchedRef, &total); err != nil {
			return Sessions{}, err
		}
		item.FamilyRootSessionRef = item.SessionRef
		item.StartedAt = startedAt.UTC().Format(time.RFC3339Nano)
		item.ActivityDates = []string(activityDates)
		item.SliceCount = len(item.ActivityDates)
		item.FamilyTotalTokens = item.LifetimeTotalTokens
		item.TotalTokens = item.RangeTotalTokens
		item.Summary = stringPtr(summary)
		item.EstimatedCostCNY = stringPtr(cost)
		item.MatchedMemberSessionID = stringPtr(matchedID)
		item.MatchedMemberSessionRef = stringPtr(matchedRef)
		item.IncludedInFamilyTotal = matchedID.Valid && matchedID.String != item.SessionID
		result.Total = total
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

func (s *Service) unavailableSessionPredicates(
	ctx context.Context,
	actor Actor,
	snapshot Snapshot,
	args []any,
) ([]string, []any, error) {
	where := []string{
		`session.content_status = 'available'`,
		`NOT EXISTS (
			SELECT 1 FROM token_query_snapshot_rollups existing
			WHERE existing.snapshot_id = $1 AND existing.root_session_id = session.id
		)`,
	}
	appendArg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	snapshotAt := appendArg(snapshot.MetricsSnapshotAt)
	where = append(where, `EXISTS (
		SELECT 1 FROM session_sources source
		JOIN session_source_generations generation ON generation.id = source.active_generation_id
		WHERE source.session_id = session.id
			AND source.ingestion_metadata->>'usage_capability' = 'unavailable'
			AND generation.status = 'active'
			AND generation.finalized_at IS NOT NULL
			AND generation.finalized_at <= `+snapshotAt+`
	)`)
	switch snapshot.Scope {
	case "mine":
		where = append(where, "session.user_id = "+appendArg(actor.ID))
	case "management":
		switch actor.Role {
		case "team_leader":
			if actor.TeamID == nil {
				where = append(where, "false")
			} else {
				where = append(where, "user_row.team_id::text = "+appendArg(*actor.TeamID))
			}
		case "director":
			rows, err := s.db.QueryContext(ctx, `SELECT id::text FROM departments WHERE director_user_id = $1 ORDER BY id`, actor.ID)
			if err != nil {
				return nil, nil, err
			}
			departments := []string{}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return nil, nil, err
				}
				departments = append(departments, id)
			}
			if err := rows.Close(); err != nil {
				return nil, nil, err
			}
			if len(departments) == 0 {
				where = append(where, "false")
			} else {
				where = append(where, "COALESCE(user_row.department_id, current_team.department_id) = ANY("+
					appendArg(pq.Array(departments))+"::uuid[])")
			}
		case "admin":
		default:
			return nil, nil, ErrForbidden
		}
	default:
		return nil, nil, ErrInvalidFilter
	}
	if snapshot.Filters.TeamID != "" {
		where = append(where, "user_row.team_id::text = "+appendArg(snapshot.Filters.TeamID))
	}
	if snapshot.Filters.DepartmentID != "" {
		where = append(where, "COALESCE(user_row.department_id, current_team.department_id)::text = "+appendArg(snapshot.Filters.DepartmentID))
	}
	if snapshot.Filters.UserID != "" {
		where = append(where, "session.user_id::text = "+appendArg(snapshot.Filters.UserID))
	}
	if snapshot.SearchMode != "exact_session_ref" {
		where = append(where,
			"COALESCE(session.last_activity_at, session.started_at)::date >= "+appendArg(snapshot.Filters.From)+"::date",
			"COALESCE(session.last_activity_at, session.started_at)::date <= "+appendArg(snapshot.Filters.To)+"::date")
	}
	if snapshot.Filters.Model != "" {
		where = append(where, "false")
	}
	if snapshot.Filters.Query != "" {
		query := appendArg("%" + snapshot.Filters.Query + "%")
		where = append(where, "(session.session_ref ILIKE "+query+" OR COALESCE(session.summary, '') ILIKE "+query+")")
	}
	return where, args, nil
}

func (s *Service) rollupPredicates(
	ctx context.Context,
	actor Actor,
	snapshot Snapshot,
	alias string,
	args []any,
	includeDate, includeModel bool,
) ([]string, []any, error) {
	where := []string{"reference.snapshot_id = $1"}
	appendArg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	switch snapshot.Scope {
	case "mine":
		where = append(where, alias+".user_id = "+appendArg(actor.ID))
	case "management":
		switch actor.Role {
		case "team_leader":
			if actor.TeamID == nil {
				where = append(where, "false")
			} else {
				where = append(where, alias+".team_id_snapshot::text = "+appendArg(*actor.TeamID))
			}
		case "director":
			rows, err := s.db.QueryContext(ctx, `SELECT id::text FROM departments WHERE director_user_id = $1 ORDER BY id`, actor.ID)
			if err != nil {
				return nil, nil, err
			}
			departments := []string{}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return nil, nil, err
				}
				departments = append(departments, id)
			}
			if err := rows.Close(); err != nil {
				return nil, nil, err
			}
			if len(departments) == 0 {
				where = append(where, "false")
			} else {
				where = append(where, alias+".department_id_snapshot = ANY("+appendArg(pq.Array(departments))+"::uuid[])")
			}
		case "admin":
		default:
			return nil, nil, ErrForbidden
		}
	default:
		return nil, nil, ErrInvalidFilter
	}
	if snapshot.Filters.TeamID != "" {
		where = append(where, alias+".team_id_snapshot::text = "+appendArg(snapshot.Filters.TeamID))
	}
	if snapshot.Filters.DepartmentID != "" {
		where = append(where, alias+".department_id_snapshot::text = "+appendArg(snapshot.Filters.DepartmentID))
	}
	if snapshot.Filters.UserID != "" {
		where = append(where, alias+".user_id::text = "+appendArg(snapshot.Filters.UserID))
	}
	if snapshot.SearchMode != "exact_session_ref" {
		if includeDate {
			where = append(where,
				alias+".activity_date >= "+appendArg(snapshot.Filters.From)+"::date",
				alias+".activity_date <= "+appendArg(snapshot.Filters.To)+"::date")
		}
		if includeModel && snapshot.Filters.Model != "" {
			where = append(where, alias+".canonical_model = "+appendArg(snapshot.Filters.Model))
		}
	}
	return where, args, nil
}

func pricingStatusSQL(alias string) string {
	return `CASE
		WHEN COUNT(*) = 0 THEN 'unpriced'
		WHEN COUNT(*) FILTER (WHERE ` + alias + `.pricing_status = 'pricing_pending') > 0 THEN 'pricing_pending'
		WHEN COUNT(*) FILTER (WHERE ` + alias + `.pricing_status = 'priced') = COUNT(*) THEN 'priced'
		WHEN COUNT(*) FILTER (WHERE ` + alias + `.pricing_status IN ('priced', 'partially_priced')) > 0 THEN 'partially_priced'
		ELSE 'unpriced'
	END`
}

func qualityStatusSQL(alias string) string {
	return `CASE
		WHEN BOOL_OR(` + alias + `.quality_status = 'conflict') THEN 'conflict'
		WHEN BOOL_OR(` + alias + `.quality_status = 'pending') THEN 'incomplete'
		WHEN BOOL_OR(` + alias + `.quality_status = 'estimated') THEN 'estimated'
		ELSE 'exact'
	END`
}
