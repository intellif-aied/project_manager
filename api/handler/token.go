package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/tokenanalytics"
	"github.com/aidashboard/api/model"
	"github.com/lib/pq"
)

type TokenHandler struct {
	db        *sql.DB
	analytics tokenAnalyticsService
}

type tokenAnalyticsService interface {
	CreateSummary(context.Context, tokenanalytics.Actor, tokenanalytics.Filters) (tokenanalytics.Summary, error)
	Trends(context.Context, tokenanalytics.Actor, tokenanalytics.Filters, string) (tokenanalytics.Trends, error)
	Rankings(context.Context, tokenanalytics.Actor, tokenanalytics.Filters, string, string) (tokenanalytics.Rankings, error)
	Sessions(context.Context, tokenanalytics.Actor, tokenanalytics.Filters, string, int, int) (tokenanalytics.Sessions, error)
}

func NewTokenHandler(db *sql.DB) *TokenHandler {
	return &TokenHandler{db: db, analytics: tokenanalytics.NewService(db)}
}

func (h *TokenHandler) Aggregate(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	actor, err := legacyTokenActor(u)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "week"
	}
	groupBy := r.URL.Query().Get("group_by")
	if groupBy == "" {
		groupBy = "model"
	}
	startDate, endDate, err := resolvePeriod(period, r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	filters := tokenanalytics.Filters{
		Scope: legacyTokenScope(u, r.URL.Query().Get("scope")), From: startDate, To: endDate,
	}
	summary, err := h.analytics.CreateSummary(r.Context(), actor, filters)
	if err != nil {
		writeTokenAnalyticsError(w, err)
		return
	}
	trends, err := h.analytics.Trends(r.Context(), actor, filters, summary.QuerySnapshotToken)
	if err != nil {
		writeTokenAnalyticsError(w, err)
		return
	}
	groups := []model.TokenGroup{}
	if groupBy == "team" || groupBy == "user" || groupBy == "model" ||
		groupBy == "requirement" || groupBy == "task" {
		rankings, rankErr := h.analytics.Rankings(r.Context(), actor, filters,
			summary.QuerySnapshotToken, groupBy)
		if rankErr != nil {
			writeTokenAnalyticsError(w, rankErr)
			return
		}
		total := parseTokenInt(summary.TotalTokens)
		for _, ranking := range rankings.Items {
			value := parseTokenInt(ranking.TotalTokens)
			percent := 0.0
			if total > 0 {
				percent = float64(value) / float64(total) * 100
			}
			groups = append(groups, model.TokenGroup{Key: ranking.Key, Label: ranking.Label, Value: value, Percent: percent})
		}
	}
	series := make([]model.TokenPoint, 0, len(trends.Items))
	for _, point := range trends.Items {
		series = append(series, model.TokenPoint{Date: point.Date, Value: parseTokenInt(point.TotalTokens)})
	}
	writeJSON(w, http.StatusOK, model.TokenAggregation{
		Total: parseTokenInt(summary.TotalTokens), InputSum: parseTokenInt(summary.UncachedInputTokens),
		OutputSum:        parseTokenInt(summary.OutputTokens),
		CacheCreationSum: parseTokenInt(summary.CacheWrite5mTokens) + parseTokenInt(summary.CacheWrite1hTokens),
		CacheReadSum:     parseTokenInt(summary.CacheReadTokens), Groups: groups, Series: series,
		Period: period, GroupBy: groupBy,
	})
}

func (h *TokenHandler) ListSessionTokens(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	actor, err := legacyTokenActor(u)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	page, pageSize := parsePagination(r, 20, 100)
	from, to := legacyTokenDateRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	filters := tokenanalytics.Filters{Scope: legacyTokenScope(u, r.URL.Query().Get("scope")), From: from, To: to}
	snapshotToken := strings.TrimSpace(r.URL.Query().Get("query_snapshot_token"))
	if snapshotToken == "" {
		summary, err := h.analytics.CreateSummary(r.Context(), actor, filters)
		if err != nil {
			writeTokenAnalyticsError(w, err)
			return
		}
		snapshotToken = summary.QuerySnapshotToken
	}
	sessions, err := h.analytics.Sessions(r.Context(), actor, filters,
		snapshotToken, page, pageSize)
	if errors.Is(err, tokenanalytics.ErrSnapshotExpired) {
		summary, refreshErr := h.analytics.CreateSummary(r.Context(), actor, filters)
		if refreshErr != nil {
			writeTokenAnalyticsError(w, refreshErr)
			return
		}
		snapshotToken = summary.QuerySnapshotToken
		sessions, err = h.analytics.Sessions(r.Context(), actor, filters,
			snapshotToken, page, pageSize)
	}
	if err != nil {
		writeTokenAnalyticsError(w, err)
		return
	}
	items := make([]model.SessionTokens, 0, len(sessions.Items))
	for _, item := range sessions.Items {
		startedAt, _ := time.Parse(time.RFC3339Nano, item.StartedAt)
		models := []string{}
		for _, value := range strings.Split(item.Model, ",") {
			if value = strings.TrimSpace(value); value != "" {
				models = append(models, value)
			}
		}
		if len(models) == 0 {
			models = []string{"unknown"}
		}
		current := model.SessionTokens{
			SessionID: item.SessionID, LocalSessionID: item.SessionRef, SessionRef: item.SessionRef,
			UserID: item.UserID, UserName: item.UserName, AgentType: item.AgentType,
			Models: models, Summary: item.Summary, StartedAt: startedAt,
			ActivityDate: item.ActivityFrom, ActivityDates: item.ActivityDates, SliceCount: item.SliceCount,
			IsEstimated: item.QualityStatus != "exact", TokenSliceStrategy: "family_rollup_v2",
			InputTokens: parseTokenInt(item.UncachedInputTokens), OutputTokens: parseTokenInt(item.OutputTokens),
			CacheCreationTokens: parseTokenInt(item.CacheWrite5mTokens) + parseTokenInt(item.CacheWrite1hTokens),
			CacheReadTokens:     parseTokenInt(item.CacheReadTokens), TotalTokens: parseTokenInt(item.RangeTotalTokens),
			FamilyRootSessionRef: item.FamilyRootSessionRef,
			SelfTotalTokens:      parseTokenInt(item.SelfTotalTokens),
			SubagentTotalTokens:  parseTokenInt(item.SubagentTotalTokens),
			FamilyTotalTokens:    parseTokenInt(item.FamilyTotalTokens),
			LifetimeTotalTokens:  parseTokenInt(item.LifetimeTotalTokens),
			RangeTotalTokens:     parseTokenInt(item.RangeTotalTokens), MemberCount: item.MemberCount,
		}
		items = append(items, current)
	}
	writeJSON(w, http.StatusOK, model.PaginatedSessionTokens{
		Items: items, Total: sessions.Total, Page: sessions.Page, PageSize: sessions.PageSize,
		QuerySnapshotToken: snapshotToken,
	})
}

func legacyTokenActor(user *model.User) (tokenanalytics.Actor, error) {
	id, err := strconv.ParseInt(user.ID, 10, 64)
	if err != nil || id <= 0 {
		return tokenanalytics.Actor{}, errors.New("invalid local user identity")
	}
	return tokenanalytics.Actor{ID: id, Role: user.Role, TeamID: user.TeamID}, nil
}

func legacyTokenScope(user *model.User, requested string) string {
	if requested == "mine" || user.Role == "employee" || user.Role == "pm" {
		return "mine"
	}
	return "management"
}

func parseTokenInt(value string) int64 {
	result, _ := strconv.ParseInt(value, 10, 64)
	return result
}

func legacyTokenDateRange(from, to string) (string, string) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" {
		from = "1970-01-01"
	}
	if to == "" {
		to = "9999-12-31"
	}
	return from, to
}

// Aggregate returns:
//   - total/input/output sums within the period
//   - groups: breakdown by group_by dimension (team|user|requirement|task|model)
//   - series: daily totals within the period
//
// Query: GET /tokens?period=today|week|month|range&from=&to=&group_by=
func (h *TokenHandler) aggregateLegacy(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "week"
	}
	groupBy := r.URL.Query().Get("group_by")
	if groupBy == "" {
		groupBy = "model"
	}

	startDate, endDate, err := resolvePeriod(period, r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Build base WHERE with role scoping + activity period
	scope, args, argIdx := buildActivityScope(u, r.URL.Query().Get("scope"))
	args = append(args, startDate)
	startIdx := argIdx
	argIdx++
	args = append(args, endDate)
	endIdx := argIdx
	argIdx++

	where := "WHERE sas.activity_date >= $" + strconv.Itoa(startIdx) + "::date AND sas.activity_date <= $" + strconv.Itoa(endIdx) + "::date"
	if scope != "" {
		where += " AND " + scope
	}

	// Totals
	var total, inputSum, outputSum, cacheCreateSum, cacheReadSum int64
	err = h.db.QueryRow(`
		SELECT COALESCE(SUM(sas.total_tokens),0),
		       COALESCE(SUM(sas.input_tokens),0),
		       COALESCE(SUM(sas.output_tokens),0),
		       COALESCE(SUM(sas.cache_creation_tokens),0),
		       COALESCE(SUM(sas.cache_read_tokens),0)
		FROM session_activity_slices sas
		`+where, args...).Scan(&total, &inputSum, &outputSum, &cacheCreateSum, &cacheReadSum)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Groups
	groups, err := h.queryActivityGroups(where, args, groupBy, total)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Series (daily)
	series, err := h.queryActivitySeries(where, args)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, model.TokenAggregation{
		Total:            total,
		InputSum:         inputSum,
		OutputSum:        outputSum,
		CacheCreationSum: cacheCreateSum,
		CacheReadSum:     cacheReadSum,
		Groups:           groups,
		Series:           series,
		Period:           period,
		GroupBy:          groupBy,
	})
}

// ListSessionTokens returns per-activity-slice token breakdown for the requesting user
// (or their team / whole org depending on role). Filters: ?from=&to= (YYYY-MM-DD),
// no date filter is applied when both bounds are omitted. A single local session can
// appear multiple times when it has activity on multiple dates.
func (h *TokenHandler) listSessionTokensLegacy(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	page, pageSize := parsePagination(r, 20, 100)

	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	scope, scopeArgs, _ := buildActivityScope(u, r.URL.Query().Get("scope"))
	args := append([]any{}, scopeArgs...)
	whereParts := []string{}
	if scope != "" {
		whereParts = append(whereParts, scope)
	}
	if from != "" {
		args = append(args, from)
		whereParts = append(whereParts, "sas.activity_date >= $"+strconv.Itoa(len(args))+"::date")
	}
	if to != "" {
		args = append(args, to)
		whereParts = append(whereParts, "sas.activity_date <= $"+strconv.Itoa(len(args))+"::date")
	}

	where := ""
	if len(whereParts) > 0 {
		where = "WHERE " + strings.Join(whereParts, " AND ")
	}

	var total int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM session_activity_slices sas "+where, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	q := `
		SELECT s.id, s.session_ref, s.user_id, COALESCE(COALESCE(NULLIF(u.nickname,''), u.username), ''), s.agent_type,
		       CASE
		         WHEN array_length(sas.models, 1) > 0 THEN sas.models
		         ELSE ARRAY[COALESCE(NULLIF(sas.model, ''), NULLIF(s.model, ''), 'unknown')]
		       END,
		       COALESCE(NULLIF(sas.summary, ''), NULLIF(sas.excerpt, ''), s.summary),
		       s.started_at,
		       sas.activity_date::text,
		       sas.activity_start_at,
		       sas.activity_end_at,
		       ARRAY[sas.activity_date::text],
		       1::int,
		       sas.source_has_raw_log,
		       sas.is_estimated,
		       sas.token_slice_strategy,
		       COALESCE(sas.input_tokens, 0),
		       COALESCE(sas.output_tokens, 0),
		       COALESCE(sas.cache_creation_tokens, 0),
		       COALESCE(sas.cache_read_tokens, 0),
		       COALESCE(sas.total_tokens, 0)
		FROM session_activity_slices sas
		JOIN sessions s ON s.id = sas.session_id
		LEFT JOIN users u ON u.id = s.user_id
		` + where + `
		ORDER BY sas.activity_end_at DESC, sas.session_id DESC, sas.activity_date DESC
		LIMIT $` + strconv.Itoa(len(args)+1) + ` OFFSET $` + strconv.Itoa(len(args)+2)
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(q, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	out := []model.SessionTokens{}
	for rows.Next() {
		var s model.SessionTokens
		var models pq.StringArray
		var activityDates pq.StringArray
		var summary sql.NullString
		var activityStart, activityEnd sql.NullTime
		if err := rows.Scan(&s.SessionID, &s.SessionRef, &s.UserID, &s.UserName, &s.AgentType, &models,
			&summary, &s.StartedAt, &s.ActivityDate, &activityStart, &activityEnd, &activityDates, &s.SliceCount,
			&s.SourceHasRawLog, &s.IsEstimated, &s.TokenSliceStrategy, &s.InputTokens, &s.OutputTokens,
			&s.CacheCreationTokens, &s.CacheReadTokens, &s.TotalTokens); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.LocalSessionID = s.SessionRef
		if s.ActivityDate != "" {
			s.SliceKey = s.SessionID + ":" + s.ActivityDate
		}
		s.Summary = nullStringPtr(summary)
		s.Models = []string(models)
		if s.Models == nil {
			s.Models = []string{}
		}
		if activityStart.Valid {
			t := activityStart.Time
			s.ActivityStartAt = &t
		}
		if activityEnd.Valid {
			t := activityEnd.Time
			s.ActivityEndAt = &t
		}
		s.ActivityDates = []string(activityDates)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, model.PaginatedSessionTokens{
		Items:    out,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// buildTokenScope returns the role-scoped WHERE fragment for the current user.
// team_leader sees their team; employee and pm see only themselves; director/admin see everything.
func buildTokenScope(u *model.User, requestedScope string) (string, []any, int) {
	if requestedScope == "mine" || u.Role == "employee" || u.Role == "pm" {
		return "tu.user_id = $1", []any{u.ID}, 2
	}
	switch u.Role {
	case "team_leader":
		if u.TeamID == nil {
			return "tu.user_id = $1", []any{u.ID}, 2
		}
		return "tu.user_id IN (SELECT id FROM users WHERE team_id = $1)", []any{*u.TeamID}, 2
	default:
		return "", []any{}, 1
	}
}

func buildTokenScopeForSessionTokens(u *model.User, requestedScope string) (string, []any, int) {
	if requestedScope == "mine" || u.Role == "employee" || u.Role == "pm" {
		return "s.user_id = $1", []any{u.ID}, 2
	}
	switch u.Role {
	case "team_leader":
		if u.TeamID == nil {
			return "s.user_id = $1", []any{u.ID}, 2
		}
		return "s.user_id IN (SELECT id FROM users WHERE team_id = $1)", []any{*u.TeamID}, 2
	default:
		return "", []any{}, 1
	}
}

func buildActivityScope(u *model.User, requestedScope string) (string, []any, int) {
	if requestedScope == "mine" || u.Role == "employee" || u.Role == "pm" {
		return "sas.user_id = $1", []any{u.ID}, 2
	}
	switch u.Role {
	case "team_leader":
		if u.TeamID == nil {
			return "sas.user_id = $1", []any{u.ID}, 2
		}
		return "sas.user_id IN (SELECT id FROM users WHERE team_id = $1)", []any{*u.TeamID}, 2
	default:
		return "", []any{}, 1
	}
}

func (h *TokenHandler) queryActivityGroups(where string, args []any, groupBy string, total int64) ([]model.TokenGroup, error) {
	var groupExpr, labelExpr, extraJoins string
	switch groupBy {
	case "team":
		extraJoins = "LEFT JOIN users u ON u.id = sas.user_id LEFT JOIN teams tm ON tm.id = u.team_id"
		groupExpr = "COALESCE(tm.id::text, 'none')"
		labelExpr = "COALESCE(tm.name, '未分配团队')"
	case "user":
		extraJoins = "LEFT JOIN users u ON u.id = sas.user_id"
		groupExpr = "sas.user_id::text"
		labelExpr = "COALESCE(COALESCE(NULLIF(u.nickname,''), u.username), '未知')"
	case "requirement":
		extraJoins = "LEFT JOIN requirements r ON r.id = sas.requirement_id"
		groupExpr = "COALESCE(sas.requirement_id::text, 'none')"
		labelExpr = "COALESCE(r.title, '未关联需求')"
	case "task":
		extraJoins = "LEFT JOIN tasks t ON t.id = sas.task_id"
		groupExpr = "COALESCE(sas.task_id::text, 'none')"
		labelExpr = "COALESCE(t.title, '未关联任务')"
	case "model":
		fallthrough
	default:
		groupBy = "model"
		groupExpr = "COALESCE(NULLIF(sas.model, ''), 'unknown')"
		labelExpr = "COALESCE(NULLIF(sas.model, ''), 'unknown')"
	}

	q := fmt.Sprintf(`
		SELECT %s as key, %s as label, COALESCE(SUM(sas.total_tokens),0) as value
		FROM session_activity_slices sas
		%s
		%s
		GROUP BY %s, %s
		ORDER BY value DESC`, groupExpr, labelExpr, extraJoins, where, groupExpr, labelExpr)

	rows, err := h.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := []model.TokenGroup{}
	for rows.Next() {
		var g model.TokenGroup
		if err := rows.Scan(&g.Key, &g.Label, &g.Value); err != nil {
			return nil, err
		}
		if total > 0 {
			g.Percent = float64(g.Value) * 100.0 / float64(total)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (h *TokenHandler) queryActivitySeries(where string, args []any) ([]model.TokenPoint, error) {
	q := fmt.Sprintf(`
		SELECT sas.activity_date as d, COALESCE(SUM(sas.total_tokens),0) as v
		FROM session_activity_slices sas
		%s
		GROUP BY d
		ORDER BY d`, where)

	rows, err := h.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pts := []model.TokenPoint{}
	for rows.Next() {
		var d time.Time
		var v int64
		if err := rows.Scan(&d, &v); err != nil {
			return nil, err
		}
		pts = append(pts, model.TokenPoint{Date: d.Format("2006-01-02"), Value: v})
	}
	return pts, rows.Err()
}

func (h *TokenHandler) queryGroups(where string, args []any, groupBy string, total int64) ([]model.TokenGroup, error) {
	var groupExpr, labelExpr, extraJoins string
	switch groupBy {
	case "team":
		extraJoins = "LEFT JOIN users u ON u.id = tu.user_id LEFT JOIN teams tm ON tm.id = u.team_id"
		groupExpr = "COALESCE(tm.id::text, 'none')"
		labelExpr = "COALESCE(tm.name, '未分配团队')"
	case "user":
		extraJoins = "LEFT JOIN users u ON u.id = tu.user_id"
		groupExpr = "tu.user_id::text"
		labelExpr = "COALESCE(COALESCE(NULLIF(u.nickname,''), u.username), '未知')"
	case "requirement":
		extraJoins = "LEFT JOIN requirements r ON r.id = tu.requirement_id"
		groupExpr = "COALESCE(tu.requirement_id::text, 'none')"
		labelExpr = "COALESCE(r.title, '未关联需求')"
	case "task":
		extraJoins = "LEFT JOIN tasks t ON t.id = tu.task_id"
		groupExpr = "COALESCE(tu.task_id::text, 'none')"
		labelExpr = "COALESCE(t.title, '未关联任务')"
	case "model":
		fallthrough
	default:
		groupBy = "model"
		groupExpr = "tu.model"
		labelExpr = "COALESCE(NULLIF(tu.model, ''), 'unknown')"
	}

	q := fmt.Sprintf(`
		SELECT %s as key, %s as label, COALESCE(SUM(tu.total_tokens),0) as value
		FROM token_usage tu
		%s
		%s
		GROUP BY %s, %s
		ORDER BY value DESC`, groupExpr, labelExpr, extraJoins, where, groupExpr, labelExpr)

	rows, err := h.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := []model.TokenGroup{}
	for rows.Next() {
		var g model.TokenGroup
		if err := rows.Scan(&g.Key, &g.Label, &g.Value); err != nil {
			return nil, err
		}
		if total > 0 {
			g.Percent = float64(g.Value) * 100.0 / float64(total)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (h *TokenHandler) querySeries(where string, args []any) ([]model.TokenPoint, error) {
	q := fmt.Sprintf(`
		SELECT DATE(tu.recorded_at) as d, COALESCE(SUM(tu.total_tokens),0) as v
		FROM token_usage tu
		%s
		GROUP BY d
		ORDER BY d`, where)

	rows, err := h.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pts := []model.TokenPoint{}
	for rows.Next() {
		var d time.Time
		var v int64
		if err := rows.Scan(&d, &v); err != nil {
			return nil, err
		}
		pts = append(pts, model.TokenPoint{Date: d.Format("2006-01-02"), Value: v})
	}
	return pts, rows.Err()
}

func resolvePeriod(period, from, to string) (string, string, error) {
	return resolvePeriodAt(period, from, to, biztime.Now())
}

func resolvePeriodAt(period, from, to string, now time.Time) (string, string, error) {
	today := biztime.Date(now)
	switch period {
	case "today":
		return today, today, nil
	case "week":
		return biztime.WeekStart(now).Format("2006-01-02"), today, nil
	case "month":
		return biztime.MonthStart(now).Format("2006-01-02"), today, nil
	case "range":
		if from == "" || to == "" {
			return "", "", fmt.Errorf("range period requires from and to")
		}
		return from, to, nil
	default:
		return "", "", fmt.Errorf("invalid period: %s", period)
	}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
