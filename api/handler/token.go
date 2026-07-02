package handler

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aidashboard/api/model"
	"github.com/lib/pq"
)

type TokenHandler struct {
	db *sql.DB
}

func NewTokenHandler(db *sql.DB) *TokenHandler {
	return &TokenHandler{db: db}
}

// Aggregate returns:
//   - total/input/output sums within the period
//   - groups: breakdown by group_by dimension (team|user|requirement|task|model)
//   - series: daily totals within the period
//
// Query: GET /tokens?period=today|week|month|range&from=&to=&group_by=
func (h *TokenHandler) Aggregate(w http.ResponseWriter, r *http.Request) {
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
// default = current month. A single local session can appear multiple times when it
// has activity on multiple dates.
func (h *TokenHandler) ListSessionTokens(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	page, pageSize := parsePagination(r, 20, 100)

	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		now := time.Now()
		firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		from = firstOfMonth.Format("2006-01-02")
		to = now.Format("2006-01-02")
	}

	scope, scopeArgs, _ := buildActivityScope(u, r.URL.Query().Get("scope"))
	args := append([]any{}, scopeArgs...)
	args = append(args, from)
	fromIdx := len(args)
	args = append(args, to)
	toIdx := len(args)

	where := "WHERE sas.activity_date >= $" + strconv.Itoa(fromIdx) + "::date" +
		" AND sas.activity_date <= $" + strconv.Itoa(toIdx) + "::date"
	if scope != "" {
		where += " AND " + scope
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
// team_leader / pm see their team; employee sees only themselves; director sees everything.
func buildTokenScope(u *model.User, requestedScope string) (string, []any, int) {
	if requestedScope == "mine" || u.Role == "employee" {
		return "tu.user_id = $1", []any{u.ID}, 2
	}
	switch u.Role {
	case "team_leader", "pm":
		if u.TeamID == nil {
			return "tu.user_id = $1", []any{u.ID}, 2
		}
		return "tu.user_id IN (SELECT id FROM users WHERE team_id = $1)", []any{*u.TeamID}, 2
	default:
		return "", []any{}, 1
	}
}

func buildTokenScopeForSessionTokens(u *model.User, requestedScope string) (string, []any, int) {
	if requestedScope == "mine" || u.Role == "employee" {
		return "s.user_id = $1", []any{u.ID}, 2
	}
	switch u.Role {
	case "team_leader", "pm":
		if u.TeamID == nil {
			return "s.user_id = $1", []any{u.ID}, 2
		}
		return "s.user_id IN (SELECT id FROM users WHERE team_id = $1)", []any{*u.TeamID}, 2
	default:
		return "", []any{}, 1
	}
}

func buildActivityScope(u *model.User, requestedScope string) (string, []any, int) {
	if requestedScope == "mine" || u.Role == "employee" {
		return "sas.user_id = $1", []any{u.ID}, 2
	}
	switch u.Role {
	case "team_leader", "pm":
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
	today := time.Now().Format("2006-01-02")
	switch period {
	case "today":
		return today, today, nil
	case "week":
		// Week starts Monday in date_trunc('week')
		start := time.Now().AddDate(0, 0, -int(time.Now().Weekday())+1)
		if time.Now().Weekday() == time.Sunday {
			start = time.Now().AddDate(0, 0, -6)
		}
		return start.Format("2006-01-02"), today, nil
	case "month":
		return time.Now().AddDate(0, 0, -time.Now().Day()+1).Format("2006-01-02"), today, nil
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
