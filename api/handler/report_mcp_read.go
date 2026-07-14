package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/aidashboard/api/model"
	"github.com/lib/pq"
)

// reportOwner is the unified owner block in §10 return payloads.
// Fields are omitempty so per-scope shapes stay compact.
type reportOwner struct {
	UserID     string `json:"user_id,omitempty"`
	Username   string `json:"username,omitempty"`
	Role       string `json:"role,omitempty"`
	TeamID     string `json:"team_id,omitempty"`
	TeamName   string `json:"team_name,omitempty"`
	LeaderID   string `json:"leader_id,omitempty"`
	LeaderName string `json:"leader_name,omitempty"`
	Scope      string `json:"scope,omitempty"`
}

type userInfo struct {
	ID       string
	Username string
	Role     string
	TeamID   string
}

func loadUserInfoMap(ctx context.Context, db *sql.DB, userIDs []string) (map[string]userInfo, error) {
	if len(userIDs) == 0 {
		return map[string]userInfo{}, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id::text, COALESCE(NULLIF(nickname,''), username), COALESCE(role,''), COALESCE(team_id::text,'')
		FROM users WHERE id::text = ANY($1)`, pq.Array(userIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]userInfo{}
	for rows.Next() {
		var u userInfo
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.TeamID); err != nil {
			return nil, err
		}
		m[u.ID] = u
	}
	return m, rows.Err()
}

func reportRoleLabel(role string) string {
	switch role {
	case "admin":
		return "管理员"
	case "director":
		return "部门负责人"
	case "team_leader":
		return "小组组长"
	case "pm":
		return "PM"
	case "employee":
		return "成员"
	default:
		return role
	}
}

type scopeMember struct {
	UserID               string `json:"user_id"`
	Username             string `json:"username"`
	Role                 string `json:"role"`
	RoleLabel            string `json:"role_label,omitempty"`
	TeamID               string `json:"team_id,omitempty"`
	TeamName             string `json:"team_name,omitempty"`
	IsTeamLeader         bool   `json:"is_team_leader"`
	IsDepartmentDirector bool   `json:"is_department_director"`
	SessionCount         int    `json:"session_count"`
	Active               bool   `json:"active"`
}

type scopeTeam struct {
	TeamID                 string `json:"team_id"`
	TeamName               string `json:"team_name"`
	LeaderID               string `json:"leader_id,omitempty"`
	LeaderName             string `json:"leader_name,omitempty"`
	TeamLeaderName         string `json:"team_leader_name,omitempty"`
	DepartmentDirectorName string `json:"department_director_name,omitempty"`
	MemberCount            int    `json:"member_count"`
	ActiveMemberCount      int    `json:"active_member_count,omitempty"`
}

type scopeContextPayload struct {
	Type             string        `json:"type"`
	TeamID           string        `json:"team_id,omitempty"`
	TeamName         string        `json:"team_name,omitempty"`
	DepartmentID     string        `json:"department_id,omitempty"`
	DepartmentName   string        `json:"department_name,omitempty"`
	Members          []scopeMember `json:"members"`
	Teams            []scopeTeam   `json:"teams,omitempty"`
	TotalMembers     int           `json:"total_members"`
	ActiveMembers    int           `json:"active_members,omitempty"`
	InactiveMembers  int           `json:"inactive_members,omitempty"`
	HasActivityStats bool          `json:"has_activity_stats"`
}

func loadScopeContext(ctx context.Context, db *sql.DB, rs *resolvedScope, activeByUser map[string]int) (scopeContextPayload, error) {
	payload := scopeContextPayload{Members: []scopeMember{}, Teams: []scopeTeam{}}
	if rs == nil {
		return payload, nil
	}
	payload.Type = rs.Type
	payload.TeamID = rs.TeamID
	payload.DepartmentID = rs.DepartmentID
	if rs.Type == "department" {
		// The current product models a department as the teams managed by one
		// director and has no canonical department-name entity. Expose a stable
		// display value so report content never has to infer a name from an ID.
		payload.DepartmentName = "部门"
	}
	payload.HasActivityStats = activeByUser != nil
	if len(rs.UserIDs) == 0 {
		return payload, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT u.id::text,
		       COALESCE(NULLIF(u.nickname,''), u.username),
		       COALESCE(u.role,''),
		       COALESCE(u.team_id::text,''),
		       COALESCE(t.name,''),
		       COALESCE(t.director_user_id::text,''),
		       COALESCE(NULLIF(du.nickname,''), du.username, ''),
		       COALESCE(tl.id::text, ''),
		       COALESCE(NULLIF(tl.nickname,''), tl.username, '')
		FROM users u
		LEFT JOIN teams t ON t.id = u.team_id
		LEFT JOIN users du ON du.id = t.director_user_id
		LEFT JOIN LATERAL (
			SELECT id, nickname, username
			FROM users
			WHERE team_id = t.id AND role = 'team_leader'
			ORDER BY id
			LIMIT 1
		) tl ON true
		WHERE u.id::text = ANY($1)
		ORDER BY COALESCE(t.name,''), COALESCE(NULLIF(u.nickname,''), u.username), u.id`, pq.Array(rs.UserIDs))
	if err != nil {
		return payload, err
	}
	defer rows.Close()

	teamIndex := map[string]int{}
	for rows.Next() {
		var m scopeMember
		var departmentDirectorID, departmentDirectorName, teamLeaderID, teamLeaderName string
		if err := rows.Scan(&m.UserID, &m.Username, &m.Role, &m.TeamID, &m.TeamName, &departmentDirectorID, &departmentDirectorName, &teamLeaderID, &teamLeaderName); err != nil {
			return payload, err
		}
		m.RoleLabel = reportRoleLabel(m.Role)
		m.IsTeamLeader = m.Role == "team_leader"
		m.IsDepartmentDirector = m.Role == "director" || (departmentDirectorID != "" && m.UserID == departmentDirectorID)
		m.SessionCount = activeByUser[m.UserID]
		m.Active = m.SessionCount > 0
		payload.Members = append(payload.Members, m)
		if m.Active {
			payload.ActiveMembers++
		}
		if payload.TeamID != "" && payload.TeamID == m.TeamID && payload.TeamName == "" {
			payload.TeamName = m.TeamName
		}
		if m.TeamID == "" {
			continue
		}
		idx, ok := teamIndex[m.TeamID]
		if !ok {
			idx = len(payload.Teams)
			teamIndex[m.TeamID] = idx
			payload.Teams = append(payload.Teams, scopeTeam{
				TeamID:                 m.TeamID,
				TeamName:               m.TeamName,
				LeaderID:               teamLeaderID,
				LeaderName:             teamLeaderName,
				TeamLeaderName:         teamLeaderName,
				DepartmentDirectorName: departmentDirectorName,
			})
		}
		payload.Teams[idx].MemberCount++
		if m.Active {
			payload.Teams[idx].ActiveMemberCount++
		}
	}
	if err := rows.Err(); err != nil {
		return payload, err
	}
	payload.TotalMembers = len(payload.Members)
	if payload.HasActivityStats {
		payload.InactiveMembers = payload.TotalMembers - payload.ActiveMembers
	}
	return payload, nil
}

type sessionsArgs struct {
	Scope                    reportScope   `json:"scope"`
	Target                   reportTarget  `json:"target,omitempty"`
	DateRange                dateRangeArgs `json:"date_range"`
	UserIDs                  []string      `json:"user_ids,omitempty"`
	SelectedSessionSliceKeys []string      `json:"selected_session_slice_keys,omitempty"`
	Limit                    int           `json:"limit,omitempty"`
	IncludeSummary           bool          `json:"include_summary,omitempty"`
}

type sessionItem struct {
	ID                  string         `json:"id"`
	SliceKey            string         `json:"slice_key,omitempty"`
	UserID              string         `json:"user_id"`
	Username            string         `json:"username"`
	Role                string         `json:"role"`
	TeamID              string         `json:"team_id"`
	TeamName            string         `json:"team_name,omitempty"`
	LocalSessionID      string         `json:"local_session_id,omitempty"`
	SessionRef          string         `json:"session_ref"`
	AgentType           string         `json:"agent_type"`
	StartedAt           *time.Time     `json:"started_at"`
	EndedAt             *time.Time     `json:"ended_at,omitempty"`
	ActivityDate        string         `json:"activity_date,omitempty"`
	ActivityStartAt     *time.Time     `json:"activity_start_at,omitempty"`
	ActivityEndAt       *time.Time     `json:"activity_end_at,omitempty"`
	ActivityDates       []string       `json:"activity_dates"`
	Summary             string         `json:"summary"`
	Excerpt             string         `json:"excerpt,omitempty"`
	MessageCount        int            `json:"message_count"`
	SourceEventCount    int            `json:"source_event_count"`
	InputTokens         int64          `json:"input_tokens"`
	OutputTokens        int64          `json:"output_tokens"`
	CacheCreationTokens int64          `json:"cache_creation_tokens"`
	CacheReadTokens     int64          `json:"cache_read_tokens"`
	TotalTokens         int64          `json:"total_tokens"`
	SliceCount          int            `json:"slice_count"`
	SourceHasRawLog     bool           `json:"source_has_raw_log"`
	TokenSliceStrategy  string         `json:"token_slice_strategy"`
	SummaryStrategy     string         `json:"summary_strategy"`
	IsEstimated         bool           `json:"is_estimated"`
	Truncated           bool           `json:"truncated"`
	ToolCallsJSON       map[string]int `json:"tool_calls_json"`
	Tags                []string       `json:"tags"`
	TaskRefs            []string       `json:"task_refs"`
	RequirementRefs     []string       `json:"requirement_refs"`
}

func (h *ReportMCPHandler) toolGetSessions(ctx context.Context, r *http.Request, rawArgs json.RawMessage) (any, error) {
	u, err := requireUser(r)
	if err != nil {
		return nil, err
	}
	var args sessionsArgs
	if err := decodeArguments(rawArgs, &args); err != nil {
		return nil, err
	}
	start, end, err := parseDateRange(args.DateRange)
	if err != nil {
		return nil, err
	}
	rs, err := resolveScope(ctx, h.db, u, args.Scope)
	if err != nil {
		return nil, err
	}
	target, err := resolveTarget(u, args.Target, "", false)
	if err != nil {
		return nil, err
	}
	if err := ensureTargetWithinScope(rs, target); err != nil {
		return nil, err
	}
	visible := rs.UserIDs
	if len(args.UserIDs) > 0 {
		visible = intersectIDs(visible, args.UserIDs)
		if len(visible) == 0 {
			return nil, errForbidden
		}
	}
	selectedSliceKeys, err := normalizeSelectedSessionSliceKeys(args.SelectedSessionSliceKeys)
	if err != nil {
		return nil, mcpErr("INVALID_ARGUMENT", err.Error())
	}
	limit := args.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if len(selectedSliceKeys) > 0 && limit < len(selectedSliceKeys) {
		limit = len(selectedSliceKeys)
	}
	rows, err := h.db.QueryContext(ctx, `
			SELECT s.id::text, sas.user_id::text, COALESCE(NULLIF(u.nickname,''),u.username), COALESCE(u.role,''), COALESCE(u.team_id::text,''), COALESCE(t.name,''),
		       s.session_ref, s.agent_type, s.started_at, s.ended_at,
		       sas.activity_date::text, sas.activity_start_at, sas.activity_end_at,
		       ARRAY[sas.activity_date::text],
		       COALESCE(NULLIF(sas.summary, ''), COALESCE(s.summary,'')),
		       COALESCE(sas.excerpt, ''),
		       COALESCE(sas.message_count, 0)::int,
		       COALESCE(sas.source_event_count, 0)::int,
		       COALESCE(sas.input_tokens, 0),
		       COALESCE(sas.output_tokens, 0),
		       COALESCE(sas.cache_creation_tokens, 0),
		       COALESCE(sas.cache_read_tokens, 0),
		       COALESCE(sas.total_tokens, 0),
		       1::int,
		       sas.source_has_raw_log,
		       sas.token_slice_strategy,
		       sas.summary_strategy,
		       sas.is_estimated
		FROM session_activity_slices sas
			JOIN sessions s ON s.id = sas.session_id
			JOIN users u ON u.id = sas.user_id
			LEFT JOIN teams t ON t.id = u.team_id
			WHERE sas.user_id::text = ANY($3)
			  AND (
			       (COALESCE(cardinality($4::text[]), 0) = 0 AND sas.activity_date >= $1::date AND sas.activity_date <= $2::date)
			       OR (s.id::text || ':' || sas.activity_date::text) = ANY($4::text[])
			  )
			ORDER BY sas.activity_end_at DESC LIMIT $5`, start, end, pq.Array(visible), pq.Array(selectedSliceKeys), limit)
	if err != nil {
		return nil, errMCPInternal
	}
	defer rows.Close()

	sessions := []sessionItem{}
	byDate := map[string]int{}
	byUser := map[string]int{}
	for rows.Next() {
		var it sessionItem
		var startedAt, endedAt, activityStartAt, activityEndAt sql.NullTime
		var activityDates pq.StringArray
		if err := rows.Scan(&it.ID, &it.UserID, &it.Username, &it.Role, &it.TeamID, &it.TeamName,
			&it.SessionRef, &it.AgentType, &startedAt, &endedAt, &it.ActivityDate, &activityStartAt, &activityEndAt, &activityDates,
			&it.Summary, &it.Excerpt, &it.MessageCount, &it.SourceEventCount,
			&it.InputTokens, &it.OutputTokens, &it.CacheCreationTokens, &it.CacheReadTokens, &it.TotalTokens,
			&it.SliceCount, &it.SourceHasRawLog, &it.TokenSliceStrategy, &it.SummaryStrategy, &it.IsEstimated); err != nil {
			return nil, errMCPInternal
		}
		if startedAt.Valid {
			t := startedAt.Time
			it.StartedAt = &t
		}
		if endedAt.Valid {
			t := endedAt.Time
			it.EndedAt = &t
		}
		if activityStartAt.Valid {
			t := activityStartAt.Time
			it.ActivityStartAt = &t
		}
		if activityEndAt.Valid {
			t := activityEndAt.Time
			it.ActivityEndAt = &t
		}
		it.ActivityDates = []string(activityDates)
		it.LocalSessionID = it.SessionRef
		if it.ActivityDate != "" {
			it.SliceKey = it.ID + ":" + it.ActivityDate
		}
		it.Tags = []string{}
		it.TaskRefs = []string{}
		it.RequirementRefs = []string{}
		it.ToolCallsJSON = map[string]int{}
		it.Truncated = len(sessions)+1 == limit
		sessions = append(sessions, it)
		for _, date := range it.ActivityDates {
			byDate[date]++
		}
		byUser[it.UserID]++
	}
	if err := rows.Err(); err != nil {
		return nil, errMCPInternal
	}

	contextScope := *rs
	contextScope.UserIDs = visible
	scopeContext, err := loadScopeContext(ctx, h.db, &contextScope, byUser)
	if err != nil {
		return nil, errMCPInternal
	}

	payload := map[string]any{
		"sessions":      sessions,
		"scope_context": scopeContext,
		"source_state": map[string]any{
			"primary_source":   "sessions",
			"source_mode":      "sessions_only",
			"dependency_ready": true,
			"expected_count":   len(sessions),
			"existing_count":   len(sessions),
			"missing_count":    0,
			"missing_names":    []string{},
		},
	}
	if args.IncludeSummary {
		infoMap, _ := loadUserInfoMap(ctx, h.db, visible)
		payload["summary"] = map[string]any{
			"total":     len(sessions),
			"by_date":   countMapEntries(byDate),
			"by_user":   userCountEntries(byUser, infoMap),
			"truncated": len(sessions) == limit,
		}
	}
	return mcpTextResult(payload), nil
}

type dailyReportsArgs struct {
	Scope          reportScope   `json:"scope"`
	Target         reportTarget  `json:"target,omitempty"`
	DateRange      dateRangeArgs `json:"date_range"`
	ReportScope    string        `json:"report_scope,omitempty"`
	UserIDs        []string      `json:"user_ids,omitempty"`
	IncludeContent *bool         `json:"include_content,omitempty"`
}

type dailyReportItem struct {
	ID                string      `json:"id"`
	ReportScope       string      `json:"report_scope"`
	Date              string      `json:"date"`
	Owner             reportOwner `json:"owner"`
	Content           string      `json:"content,omitempty"`
	ProductStatus     string      `json:"product_status"`
	GenerationMode    string      `json:"generation_mode,omitempty"`
	Edited            bool        `json:"edited"`
	ManagedAgentRunID string      `json:"managed_agent_run_id,omitempty"`
	UpdatedAt         time.Time   `json:"updated_at"`
}

func reportContentIncluded(value *bool) bool {
	return value == nil || *value
}

func (h *ReportMCPHandler) toolGetDailyReports(ctx context.Context, r *http.Request, rawArgs json.RawMessage) (any, error) {
	u, err := requireUser(r)
	if err != nil {
		return nil, err
	}
	var args dailyReportsArgs
	if err := decodeArguments(rawArgs, &args); err != nil {
		return nil, err
	}
	start, end, err := parseDateRange(args.DateRange)
	if err != nil {
		return nil, err
	}
	rs, err := resolveScope(ctx, h.db, u, args.Scope)
	if err != nil {
		return nil, err
	}
	target, err := resolveTarget(u, args.Target, "", false)
	if err != nil {
		return nil, err
	}
	if err := ensureTargetWithinScope(rs, target); err != nil {
		return nil, err
	}
	reportScope := args.ReportScope
	if reportScope == "" {
		reportScope = "personal"
	}
	if reportScope != "personal" && reportScope != "team" && reportScope != "department" {
		return nil, errInvalidScope
	}
	includeContent := reportContentIncluded(args.IncludeContent)
	visible := rs.UserIDs
	if len(args.UserIDs) > 0 {
		visible = intersectIDs(visible, args.UserIDs)
		if len(visible) == 0 {
			return nil, errForbidden
		}
	}
	contextScope := *rs
	contextScope.UserIDs = visible
	scopeContext, err := loadScopeContext(ctx, h.db, &contextScope, nil)
	if err != nil {
		return nil, errMCPInternal
	}

	reports := []dailyReportItem{}
	switch reportScope {
	case "personal":
		rows, err := h.db.QueryContext(ctx, `
			SELECT dr.id::text, dr.user_id::text, COALESCE(NULLIF(u.nickname,''),u.username), COALESCE(u.role,''), COALESCE(u.team_id::text,''), COALESCE(t.name,''),
			       dr.report_date::text, dr.content, COALESCE(dr.generation_mode,'default'), dr.edited, COALESCE(dr.managed_agent_run_id::text,''), dr.updated_at
			FROM daily_reports dr JOIN users u ON u.id = dr.user_id
			LEFT JOIN teams t ON t.id = u.team_id
			WHERE dr.report_date >= $1 AND dr.report_date <= $2 AND dr.user_id = ANY($3)
			  AND dr.status IS NOT NULL
			  AND NULLIF(TRIM(COALESCE(dr.content, '')), '') IS NOT NULL
			ORDER BY dr.report_date DESC`, start, end, pq.Array(visible))
		if err != nil {
			return nil, errMCPInternal
		}
		defer rows.Close()
		for rows.Next() {
			var it dailyReportItem
			if err := rows.Scan(&it.ID, &it.Owner.UserID, &it.Owner.Username, &it.Owner.Role, &it.Owner.TeamID, &it.Owner.TeamName,
				&it.Date, &it.Content, &it.GenerationMode, &it.Edited, &it.ManagedAgentRunID, &it.UpdatedAt); err != nil {
				return nil, errMCPInternal
			}
			it.ReportScope = "personal"
			it.ProductStatus = dailyProductStatus(it.GenerationMode, it.Edited)
			if !includeContent {
				it.Content = ""
			}
			reports = append(reports, it)
		}
		if err := rows.Err(); err != nil {
			return nil, errMCPInternal
		}
	case "team":
		rows, err := h.db.QueryContext(ctx, `
			SELECT tr.id::text, tr.team_id::text, COALESCE(t.name,''), tr.leader_id::text, COALESCE(NULLIF(u.nickname,''),u.username),
			       tr.report_date::text, tr.content, COALESCE(tr.generation_mode,'default'), tr.edited, COALESCE(tr.managed_agent_run_id::text,''), tr.updated_at
			FROM team_reports tr
			LEFT JOIN teams t ON t.id = tr.team_id
			LEFT JOIN users u ON u.id = tr.leader_id
			WHERE tr.report_date >= $1 AND tr.report_date <= $2
			  AND EXISTS (SELECT 1 FROM users vu WHERE vu.team_id = tr.team_id AND vu.id::text = ANY($3))
			  AND tr.status IS NOT NULL
			  AND NULLIF(TRIM(COALESCE(tr.content, '')), '') IS NOT NULL
			ORDER BY tr.report_date DESC`, start, end, pq.Array(visible))
		if err != nil {
			return nil, errMCPInternal
		}
		defer rows.Close()
		for rows.Next() {
			var it dailyReportItem
			if err := rows.Scan(&it.ID, &it.Owner.TeamID, &it.Owner.TeamName, &it.Owner.LeaderID, &it.Owner.LeaderName,
				&it.Date, &it.Content, &it.GenerationMode, &it.Edited, &it.ManagedAgentRunID, &it.UpdatedAt); err != nil {
				return nil, errMCPInternal
			}
			it.ReportScope = "team"
			it.ProductStatus = dailyProductStatus(it.GenerationMode, it.Edited)
			if !includeContent {
				it.Content = ""
			}
			reports = append(reports, it)
		}
		if err := rows.Err(); err != nil {
			return nil, errMCPInternal
		}
	case "department":
		rows, err := h.db.QueryContext(ctx, `
			SELECT dr.id::text, dr.report_date::text, dr.content, COALESCE(dr.generation_mode,'default'), dr.edited, COALESCE(dr.managed_agent_run_id::text,''), dr.updated_at
			FROM department_reports dr
			WHERE dr.report_date >= $1 AND dr.report_date <= $2
			  AND dr.status IS NOT NULL
			  AND NULLIF(TRIM(COALESCE(dr.content, '')), '') IS NOT NULL
			ORDER BY dr.report_date DESC`, start, end)
		if err != nil {
			return nil, errMCPInternal
		}
		defer rows.Close()
		for rows.Next() {
			var it dailyReportItem
			if err := rows.Scan(&it.ID, &it.Date, &it.Content, &it.GenerationMode, &it.Edited, &it.ManagedAgentRunID, &it.UpdatedAt); err != nil {
				return nil, errMCPInternal
			}
			it.ReportScope = "department"
			it.Owner = reportOwner{Scope: "department"}
			it.ProductStatus = dailyProductStatus(it.GenerationMode, it.Edited)
			if !includeContent {
				it.Content = ""
			}
			reports = append(reports, it)
		}
		if err := rows.Err(); err != nil {
			return nil, errMCPInternal
		}
	}

	missing := computeDailyMissing(ctx, h.db, reportScope, visible, start, end, reports)
	payload := map[string]any{
		"reports":       reports,
		"missing":       missing,
		"scope_context": scopeContext,
		"source_state":  reportSourceState(reportScope, "daily", len(reports), len(reports)+len(missing), missing),
		"summary": map[string]int{
			"total_expected": len(reports) + len(missing),
			"total_existing": len(reports),
			"total_missing":  len(missing),
		},
	}
	return mcpTextResult(payload), nil
}

type weeklyReportsArgs struct {
	Scope          reportScope   `json:"scope"`
	Target         reportTarget  `json:"target,omitempty"`
	WeekRange      weekRangeArgs `json:"week_range"`
	ReportScope    string        `json:"report_scope,omitempty"`
	UserIDs        []string      `json:"user_ids,omitempty"`
	IncludeContent *bool         `json:"include_content,omitempty"`
}

type weeklyReportItem struct {
	ID                string      `json:"id"`
	ReportScope       string      `json:"report_scope"`
	WeekStart         string      `json:"week_start"`
	WeekEnd           string      `json:"week_end"`
	Owner             reportOwner `json:"owner"`
	Content           string      `json:"content,omitempty"`
	ProductStatus     string      `json:"product_status"`
	GenerationMode    string      `json:"generation_mode,omitempty"`
	Edited            bool        `json:"edited"`
	ManagedAgentRunID string      `json:"managed_agent_run_id,omitempty"`
	UpdatedAt         time.Time   `json:"updated_at"`
}

func (h *ReportMCPHandler) toolGetWeeklyReports(ctx context.Context, r *http.Request, rawArgs json.RawMessage) (any, error) {
	u, err := requireUser(r)
	if err != nil {
		return nil, err
	}
	var args weeklyReportsArgs
	if err := decodeArguments(rawArgs, &args); err != nil {
		return nil, err
	}
	ws, we, err := parseWeekRange(args.WeekRange)
	if err != nil {
		return nil, err
	}
	rs, err := resolveScope(ctx, h.db, u, args.Scope)
	if err != nil {
		return nil, err
	}
	target, err := resolveTarget(u, args.Target, "", false)
	if err != nil {
		return nil, err
	}
	if err := ensureTargetWithinScope(rs, target); err != nil {
		return nil, err
	}
	reportScope := args.ReportScope
	if reportScope == "" {
		reportScope = "personal"
	}
	if reportScope != "personal" && reportScope != "team" && reportScope != "department" {
		return nil, errInvalidScope
	}
	includeContent := reportContentIncluded(args.IncludeContent)
	visible := rs.UserIDs
	if len(args.UserIDs) > 0 {
		visible = intersectIDs(visible, args.UserIDs)
		if len(visible) == 0 {
			return nil, errForbidden
		}
	}
	contextScope := *rs
	contextScope.UserIDs = visible
	scopeContext, err := loadScopeContext(ctx, h.db, &contextScope, nil)
	if err != nil {
		return nil, errMCPInternal
	}

	reports := []weeklyReportItem{}
	switch reportScope {
	case "personal":
		rows, err := h.db.QueryContext(ctx, `
			SELECT r.id::text, r.user_id::text, COALESCE(NULLIF(u.nickname,''),u.username), COALESCE(u.role,''), COALESCE(u.team_id::text,''), COALESCE(t.name,''),
			       r.week_start, r.week_end, r.content, COALESCE(r.generation_mode,'default'), r.edited, COALESCE(r.managed_agent_run_id::text,''), r.updated_at
			FROM personal_weekly_reports r JOIN users u ON u.id = r.user_id
			LEFT JOIN teams t ON t.id = u.team_id
			WHERE r.week_start >= $1 AND r.week_end <= $2 AND r.user_id = ANY($3)
			  AND r.status IS NOT NULL
			  AND NULLIF(TRIM(COALESCE(r.content, '')), '') IS NOT NULL
			ORDER BY r.week_start DESC`, ws, we, pq.Array(visible))
		if err != nil {
			return nil, errMCPInternal
		}
		defer rows.Close()
		for rows.Next() {
			var it weeklyReportItem
			var ws2, we2 time.Time
			if err := rows.Scan(&it.ID, &it.Owner.UserID, &it.Owner.Username, &it.Owner.Role, &it.Owner.TeamID, &it.Owner.TeamName,
				&ws2, &we2, &it.Content, &it.GenerationMode, &it.Edited, &it.ManagedAgentRunID, &it.UpdatedAt); err != nil {
				return nil, errMCPInternal
			}
			it.ReportScope = "personal"
			it.WeekStart = ws2.Format("2006-01-02")
			it.WeekEnd = we2.Format("2006-01-02")
			it.ProductStatus = dailyProductStatus(it.GenerationMode, it.Edited)
			if !includeContent {
				it.Content = ""
			}
			reports = append(reports, it)
		}
		if err := rows.Err(); err != nil {
			return nil, errMCPInternal
		}
	case "team":
		rows, err := h.db.QueryContext(ctx, `
			SELECT r.id::text, r.team_id::text, COALESCE(t.name,''), r.leader_id::text, COALESCE(NULLIF(u.nickname,''),u.username),
			       r.week_start, r.week_end, r.content, COALESCE(r.generation_mode,'default'), r.edited, COALESCE(r.managed_agent_run_id::text,''), r.updated_at
			FROM team_weekly_reports r
			LEFT JOIN teams t ON t.id = r.team_id
			LEFT JOIN users u ON u.id = r.leader_id
			WHERE r.week_start >= $1 AND r.week_end <= $2
			  AND EXISTS (SELECT 1 FROM users vu WHERE vu.team_id = r.team_id AND vu.id::text = ANY($3))
			  AND NULLIF(TRIM(COALESCE(r.content, '')), '') IS NOT NULL
			ORDER BY r.week_start DESC`, ws, we, pq.Array(visible))
		if err != nil {
			return nil, errMCPInternal
		}
		defer rows.Close()
		for rows.Next() {
			var it weeklyReportItem
			var ws2, we2 time.Time
			if err := rows.Scan(&it.ID, &it.Owner.TeamID, &it.Owner.TeamName, &it.Owner.LeaderID, &it.Owner.LeaderName,
				&ws2, &we2, &it.Content, &it.GenerationMode, &it.Edited, &it.ManagedAgentRunID, &it.UpdatedAt); err != nil {
				return nil, errMCPInternal
			}
			it.ReportScope = "team"
			it.WeekStart = ws2.Format("2006-01-02")
			it.WeekEnd = we2.Format("2006-01-02")
			it.ProductStatus = dailyProductStatus(it.GenerationMode, it.Edited)
			if !includeContent {
				it.Content = ""
			}
			reports = append(reports, it)
		}
		if err := rows.Err(); err != nil {
			return nil, errMCPInternal
		}
	case "department":
		rows, err := h.db.QueryContext(ctx, `
			SELECT r.id::text, r.week_start, r.week_end, r.content, COALESCE(r.generation_mode,'default'), r.edited, COALESCE(r.managed_agent_run_id::text,''), r.updated_at
			FROM department_weekly_reports r
			WHERE r.week_start >= $1 AND r.week_end <= $2
			  AND NULLIF(TRIM(COALESCE(r.content, '')), '') IS NOT NULL
			ORDER BY r.week_start DESC`, ws, we)
		if err != nil {
			return nil, errMCPInternal
		}
		defer rows.Close()
		for rows.Next() {
			var it weeklyReportItem
			var ws2, we2 time.Time
			if err := rows.Scan(&it.ID, &ws2, &we2, &it.Content, &it.GenerationMode, &it.Edited, &it.ManagedAgentRunID, &it.UpdatedAt); err != nil {
				return nil, errMCPInternal
			}
			it.ReportScope = "department"
			it.WeekStart = ws2.Format("2006-01-02")
			it.WeekEnd = we2.Format("2006-01-02")
			it.Owner = reportOwner{Scope: "department"}
			it.ProductStatus = dailyProductStatus(it.GenerationMode, it.Edited)
			if !includeContent {
				it.Content = ""
			}
			reports = append(reports, it)
		}
		if err := rows.Err(); err != nil {
			return nil, errMCPInternal
		}
	}
	contextScope.UserIDs = visible
	existingInventory, err := loadWeeklyInventoryExisting(ctx, h.db, reportScope, &contextScope, ws, we)
	if err != nil {
		return nil, errMCPInternal
	}
	expectedInventory, err := loadWeeklyInventoryExpected(ctx, h.db, reportScope, &contextScope, ws, we)
	if err != nil {
		return nil, errMCPInternal
	}
	missing := computeMissing(expectedInventory, existingInventory)
	payload := map[string]any{
		"reports":       reports,
		"missing":       missing,
		"scope_context": scopeContext,
		"source_state":  reportSourceState(reportScope, "weekly", len(existingInventory), len(expectedInventory), missing),
		"summary": map[string]int{
			"total_expected": len(expectedInventory),
			"total_existing": len(existingInventory),
			"total_missing":  len(missing),
		},
	}
	return mcpTextResult(payload), nil
}

type tasksArgs struct {
	Scope              reportScope   `json:"scope"`
	Target             reportTarget  `json:"target,omitempty"`
	DateRange          dateRangeArgs `json:"date_range"`
	Status             []string      `json:"status,omitempty"`
	IncludeRequirement bool          `json:"include_requirement,omitempty"`
}

type taskItem struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Status      string       `json:"status"`
	Progress    int          `json:"progress"`
	Assignee    reportOwner  `json:"assignee"`
	Requirement *taskReqLink `json:"requirement,omitempty"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type taskReqLink struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func dashboardMyItemIDs(ctx context.Context, db *sql.DB, userID, targetType string) ([]string, error) {
	refs, err := loadDashboardMyItemRefs(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, ref := range refs {
		if ref.TargetType == targetType {
			ids = append(ids, ref.TargetID)
		}
	}
	return ids, nil
}

func (h *ReportMCPHandler) toolGetTasks(ctx context.Context, r *http.Request, rawArgs json.RawMessage) (any, error) {
	u, err := requireUser(r)
	if err != nil {
		return nil, err
	}
	var args tasksArgs
	if err := decodeArguments(rawArgs, &args); err != nil {
		return nil, err
	}
	start, end, err := parseDateRange(args.DateRange)
	if err != nil {
		return nil, err
	}
	rs, err := resolveScope(ctx, h.db, u, args.Scope)
	if err != nil {
		return nil, err
	}
	target, err := resolveTarget(u, args.Target, "", false)
	if err != nil {
		return nil, err
	}
	if err := ensureTargetWithinScope(rs, target); err != nil {
		return nil, err
	}
	visible := rs.UserIDs
	statuses := args.Status
	if len(statuses) == 0 {
		statuses = []string{"todo", "in_progress", "done", "blocked"}
	}
	visibilityClause := `EXISTS (
			SELECT 1 FROM task_responsibles tr
			WHERE tr.task_id = t.id AND tr.user_id = ANY($3)
		  )`
	queryArgs := []any{start, end, pq.Array(visible), pq.Array(statuses)}
	if rs.Type == "self" {
		myTaskIDs, err := dashboardMyItemIDs(ctx, h.db, u.ID, "task")
		if err != nil {
			return nil, errMCPInternal
		}
		visibilityClause = "t.id::text = ANY($3)"
		queryArgs[2] = pq.Array(myTaskIDs)
	}

	query := fmt.Sprintf(`
		SELECT t.id::text, t.title, t.status, COALESCE(t.progress,0),
		       COALESCE(MIN(tr_all.user_id::text), ''),
		       COALESCE(
		           NULLIF(string_agg(DISTINCT COALESCE(NULLIF(ru.nickname,''), ru.username), '、') FILTER (WHERE ru.id IS NOT NULL), ''),
		           ''
		       ),
		       r.id::text, COALESCE(r.title,''), t.updated_at
		FROM tasks t
		LEFT JOIN requirements r ON r.id = t.requirement_id
		LEFT JOIN task_responsibles tr_all ON tr_all.task_id = t.id
		LEFT JOIN users ru ON ru.id = tr_all.user_id
		WHERE t.updated_at >= $1 AND t.updated_at < ($2::date + 1)
		  AND %s
		  AND t.status = ANY($4)
		GROUP BY t.id, t.title, t.status, t.progress, r.id, r.title, t.updated_at
		ORDER BY t.updated_at DESC LIMIT 200`, visibilityClause)
	rows, err := h.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, errMCPInternal
	}
	defer rows.Close()

	tasks := []taskItem{}
	summary := map[string]int{"total": 0, "blocked": 0, "done": 0, "in_progress": 0}
	for rows.Next() {
		var it taskItem
		var reqID, reqTitle string
		if err := rows.Scan(&it.ID, &it.Title, &it.Status, &it.Progress, &it.Assignee.UserID, &it.Assignee.Username, &reqID, &reqTitle, &it.UpdatedAt); err != nil {
			return nil, errMCPInternal
		}
		if args.IncludeRequirement && reqID != "" {
			it.Requirement = &taskReqLink{ID: reqID, Title: reqTitle}
		}
		tasks = append(tasks, it)
		summary["total"]++
		switch it.Status {
		case "blocked":
			summary["blocked"]++
		case "done":
			summary["done"]++
		case "in_progress":
			summary["in_progress"]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, errMCPInternal
	}
	payload := map[string]any{"tasks": tasks, "summary": summary}
	return mcpTextResult(payload), nil
}

type requirementsArgs struct {
	Scope        reportScope   `json:"scope"`
	Target       reportTarget  `json:"target,omitempty"`
	DateRange    dateRangeArgs `json:"date_range"`
	IncludeTasks bool          `json:"include_tasks,omitempty"`
	IncludeRisks bool          `json:"include_risks,omitempty"`
}

type reqItem struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"`
	Status    string      `json:"status"`
	Owner     reportOwner `json:"owner"`
	Teams     []string    `json:"teams"`
	Risks     []any       `json:"risks,omitempty"`
	UpdatedAt time.Time   `json:"updated_at"`
}

func (h *ReportMCPHandler) toolGetRequirements(ctx context.Context, r *http.Request, rawArgs json.RawMessage) (any, error) {
	u, err := requireUser(r)
	if err != nil {
		return nil, err
	}
	var args requirementsArgs
	if err := decodeArguments(rawArgs, &args); err != nil {
		return nil, err
	}
	start, end, err := parseDateRange(args.DateRange)
	if err != nil {
		return nil, err
	}
	rs, err := resolveScope(ctx, h.db, u, args.Scope)
	if err != nil {
		return nil, err
	}
	target, err := resolveTarget(u, args.Target, "", false)
	if err != nil {
		return nil, err
	}
	if err := ensureTargetWithinScope(rs, target); err != nil {
		return nil, err
	}
	visible := rs.UserIDs
	visibilityClause := `(r.creator_id = ANY($3) OR EXISTS (
		       SELECT 1 FROM requirement_teams rt JOIN users u2 ON u2.team_id = rt.team_id
		       WHERE rt.requirement_id = r.id AND u2.id = ANY($3)))`
	queryArgs := []any{start, end, pq.Array(visible)}
	if rs.Type == "self" {
		myRequirementIDs, err := dashboardMyItemIDs(ctx, h.db, u.ID, "requirement")
		if err != nil {
			return nil, errMCPInternal
		}
		visibilityClause = "r.id::text = ANY($3)"
		queryArgs[2] = pq.Array(myRequirementIDs)
	}
	query := fmt.Sprintf(`
		SELECT r.id::text, r.title, r.status, COALESCE(r.creator_id::text,''), COALESCE(NULLIF(u.nickname,''),u.username),
		       r.updated_at
		FROM requirements r
		LEFT JOIN users u ON u.id = r.creator_id
		WHERE r.updated_at >= $1 AND r.updated_at < ($2::date + 1)
		  AND %s
		ORDER BY r.updated_at DESC LIMIT 200`, visibilityClause)
	rows, err := h.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, errMCPInternal
	}
	defer rows.Close()

	reqs := []reqItem{}
	riskCount := 0
	for rows.Next() {
		var it reqItem
		if err := rows.Scan(&it.ID, &it.Title, &it.Status, &it.Owner.UserID, &it.Owner.Username, &it.UpdatedAt); err != nil {
			return nil, errMCPInternal
		}
		it.Teams = []string{}
		if args.IncludeRisks {
			it.Risks = []any{}
		}
		reqs = append(reqs, it)
	}
	if err := rows.Err(); err != nil {
		return nil, errMCPInternal
	}
	payload := map[string]any{
		"requirements": reqs,
		"summary":      map[string]int{"total": len(reqs), "risk_count": riskCount},
	}
	return mcpTextResult(payload), nil
}

type existingReportArgs struct {
	ReportType string       `json:"report_type"`
	Period     periodArgs   `json:"period"`
	Target     reportTarget `json:"target,omitempty"`
}

func (h *ReportMCPHandler) toolGetExistingReport(ctx context.Context, r *http.Request, rawArgs json.RawMessage) (any, error) {
	u, err := requireUser(r)
	if err != nil {
		return nil, err
	}
	var args existingReportArgs
	if err := decodeArguments(rawArgs, &args); err != nil {
		return nil, err
	}
	if err := validateReportType(args.ReportType); err != nil {
		return nil, err
	}
	date, ws, we, err := resolveReportPeriod(args.ReportType, args.Period)
	if err != nil {
		return nil, err
	}
	target, err := resolveTarget(u, args.Target, args.ReportType, false)
	if err != nil {
		return nil, err
	}
	rs, err := resolveScope(ctx, h.db, u, reportScope{Type: scopeTypeForReportTarget(u, args.ReportType, target)})
	if err != nil {
		return nil, err
	}
	if err := ensureTargetWithinScope(rs, target); err != nil {
		return nil, err
	}

	snapshot, err := loadReportSnapshot(ctx, h.db, args.ReportType, date, ws, we, target)
	if err != nil {
		return nil, errMCPInternal
	}
	lastRun, err := loadLastAIRunForTarget(ctx, h.db, args.ReportType, date, ws, we, target, snapshot)
	if err != nil {
		return nil, errMCPInternal
	}
	productStatus := computeProductStatus(snapshot, lastRun)

	if snapshot == nil {
		return mcpTextResult(map[string]any{"report": nil, "product_status": productStatus}), nil
	}
	report := map[string]any{
		"id":                   snapshot.ID,
		"report_type":          args.ReportType,
		"generation_mode":      snapshot.GenerationMode,
		"edited":               snapshot.Edited,
		"managed_agent_run_id": snapshot.ManagedAgentRunID,
		"updated_at":           snapshot.UpdatedAt,
	}
	if date != "" {
		report["period"] = map[string]string{"date": date}
	} else {
		report["period"] = map[string]string{"week_start": ws, "week_end": we}
	}
	if snapshot.Content != "" {
		report["content"] = snapshot.Content
	}
	return mcpTextResult(map[string]any{"report": report, "product_status": productStatus}), nil
}

type reportInventoryArgs struct {
	Scope       reportScope   `json:"scope"`
	Target      reportTarget  `json:"target,omitempty"`
	ReportScope string        `json:"report_scope"`
	ReportKind  string        `json:"report_kind"`
	DateRange   dateRangeArgs `json:"date_range,omitempty"`
	WeekRange   weekRangeArgs `json:"week_range,omitempty"`
}

func (h *ReportMCPHandler) toolGetReportInventory(ctx context.Context, r *http.Request, rawArgs json.RawMessage) (any, error) {
	u, err := requireUser(r)
	if err != nil {
		return nil, err
	}
	var args reportInventoryArgs
	if err := decodeArguments(rawArgs, &args); err != nil {
		return nil, err
	}
	if args.ReportScope != "personal" && args.ReportScope != "team" && args.ReportScope != "department" {
		return nil, errInvalidScope
	}
	if args.ReportKind != "daily" && args.ReportKind != "weekly" {
		return nil, errInvalidPeriod
	}
	rs, err := resolveScope(ctx, h.db, u, args.Scope)
	if err != nil {
		return nil, err
	}
	target, err := resolveTarget(u, args.Target, "", false)
	if err != nil {
		return nil, err
	}
	if err := ensureTargetWithinScope(rs, target); err != nil {
		return nil, err
	}
	scopeContext, err := loadScopeContext(ctx, h.db, rs, nil)
	if err != nil {
		return nil, errMCPInternal
	}

	if args.ReportKind == "daily" {
		start, end, err := parseDateRange(args.DateRange)
		if err != nil {
			return nil, err
		}
		existing, err := loadDailyInventoryExisting(ctx, h.db, args.ReportScope, rs, start, end)
		if err != nil {
			return nil, errMCPInternal
		}
		expected, err := loadDailyInventoryExpected(ctx, h.db, args.ReportScope, rs, start, end)
		if err != nil {
			return nil, errMCPInternal
		}
		missing := computeMissing(expected, existing)
		return mcpTextResult(map[string]any{
			"inventory": map[string]any{
				"expected": expected,
				"existing": existing,
				"missing":  missing,
			},
			"scope_context": scopeContext,
			"source_state":  reportSourceState(args.ReportScope, "daily", len(existing), len(expected), missing),
			"summary": map[string]int{
				"total_expected": len(expected),
				"total_existing": len(existing),
				"total_missing":  len(missing),
			},
		}), nil
	}

	ws, we, err := parseWeekRange(args.WeekRange)
	if err != nil {
		return nil, err
	}
	existing, err := loadWeeklyInventoryExisting(ctx, h.db, args.ReportScope, rs, ws, we)
	if err != nil {
		return nil, errMCPInternal
	}
	expected, err := loadWeeklyInventoryExpected(ctx, h.db, args.ReportScope, rs, ws, we)
	if err != nil {
		return nil, errMCPInternal
	}
	missing := computeMissing(expected, existing)
	return mcpTextResult(map[string]any{
		"inventory": map[string]any{
			"expected": expected,
			"existing": existing,
			"missing":  missing,
		},
		"scope_context": scopeContext,
		"source_state":  reportSourceState(args.ReportScope, "weekly", len(existing), len(expected), missing),
		"summary": map[string]int{
			"total_expected": len(expected),
			"total_existing": len(existing),
			"total_missing":  len(missing),
		},
	}), nil
}

// reportSnapshot is the unified read view for §3.6 product_status computation.
type reportSnapshot struct {
	ID                string
	Content           string
	GenerationMode    string
	Edited            bool
	ManagedAgentRunID string
	UpdatedAt         time.Time
}

type aiRunSnapshot struct {
	ID        string
	Status    string
	CreatedAt time.Time
}

func loadReportSnapshot(ctx context.Context, db *sql.DB, reportType, date, ws, we string, target reportTarget) (*reportSnapshot, error) {
	switch reportType {
	case reportTypePersonalDaily:
		return loadSnapshotRow(ctx, db, `SELECT id::text, content, COALESCE(generation_mode,'default'), edited, COALESCE(managed_agent_run_id::text,''), updated_at
			FROM daily_reports
			WHERE user_id = $1 AND report_date = $2
			  AND status IS NOT NULL
			  AND NULLIF(TRIM(COALESCE(content, '')), '') IS NOT NULL`, target.UserID, date)
	case reportTypePersonalWeekly:
		return loadSnapshotRow(ctx, db, `SELECT id::text, content, COALESCE(generation_mode,'default'), edited, COALESCE(managed_agent_run_id::text,''), updated_at
			FROM personal_weekly_reports
			WHERE user_id = $1 AND week_start = $2 AND week_end = $3
			  AND status IS NOT NULL
			  AND NULLIF(TRIM(COALESCE(content, '')), '') IS NOT NULL`, target.UserID, ws, we)
	case reportTypeTeamDaily:
		return loadSnapshotRow(ctx, db, `SELECT id::text, content, COALESCE(generation_mode,'default'), edited, COALESCE(managed_agent_run_id::text,''), updated_at
			FROM team_reports
			WHERE team_id = $1 AND report_date = $2
			  AND status IS NOT NULL
			  AND NULLIF(TRIM(COALESCE(content, '')), '') IS NOT NULL`, target.TeamID, date)
	case reportTypeTeamWeekly:
		return loadSnapshotRow(ctx, db, `SELECT id::text, content, COALESCE(generation_mode,'default'), edited, COALESCE(managed_agent_run_id::text,''), updated_at
			FROM team_weekly_reports
			WHERE team_id = $1 AND week_start = $2 AND week_end = $3
			  AND NULLIF(TRIM(COALESCE(content, '')), '') IS NOT NULL`, target.TeamID, ws, we)
	case reportTypeDepartmentDaily:
		return loadSnapshotRow(ctx, db, `SELECT id::text, content, COALESCE(generation_mode,'default'), edited, COALESCE(managed_agent_run_id::text,''), updated_at
			FROM department_reports
			WHERE report_date = $1
			  AND status IS NOT NULL
			  AND NULLIF(TRIM(COALESCE(content, '')), '') IS NOT NULL`, date)
	case reportTypeDepartmentWeekly:
		return loadSnapshotRow(ctx, db, `SELECT id::text, content, COALESCE(generation_mode,'default'), edited, COALESCE(managed_agent_run_id::text,''), updated_at
			FROM department_weekly_reports
			WHERE week_start = $1 AND week_end = $2
			  AND NULLIF(TRIM(COALESCE(content, '')), '') IS NOT NULL`, ws, we)
	}
	return nil, nil
}

func loadSnapshotRow(ctx context.Context, db *sql.DB, query string, args ...any) (*reportSnapshot, error) {
	row := db.QueryRowContext(ctx, query, args...)
	var s reportSnapshot
	err := row.Scan(&s.ID, &s.Content, &s.GenerationMode, &s.Edited, &s.ManagedAgentRunID, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// loadLastAIRunForTarget returns the most recent ai_run for the given target.
// For team/department missing reports, ai_runs has no target column, so a failed
// run cannot be attributed safely and must not affect product_status.
func loadLastAIRunForTarget(ctx context.Context, db *sql.DB, reportType, date, ws, we string, target reportTarget, snapshot *reportSnapshot) (*aiRunSnapshot, error) {
	if snapshot == nil && target.UserID == "" {
		return nil, nil
	}
	query := `
		SELECT id::text, status, created_at
		FROM ai_runs
		WHERE business_type = $1`
	args := []any{reportType}
	if snapshot != nil && snapshot.ID != "" {
		query += ` AND (business_id = $2::uuid OR business_id IS NULL)`
		args = append(args, snapshot.ID)
	} else if target.UserID != "" {
		query += ` AND user_id = $2`
		args = append(args, target.UserID)
	}
	query += ` ORDER BY created_at DESC LIMIT 1`
	row := db.QueryRowContext(ctx, query, args...)
	var r aiRunSnapshot
	err := row.Scan(&r.ID, &r.Status, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// computeProductStatus implements §3.6 logic.
func computeProductStatus(report *reportSnapshot, lastRun *aiRunSnapshot) string {
	if report == nil {
		if lastRun != nil && lastRun.Status == "failed" {
			return "generation_failed"
		}
		return "missing"
	}
	if report.GenerationMode == "managed_agent" && !report.Edited {
		return "ai_generated"
	}
	if report.GenerationMode == "managed_agent" && report.Edited {
		return "modified"
	}
	return "manual"
}

func dailyProductStatus(generationMode string, edited bool) string {
	if generationMode == "managed_agent" && !edited {
		return "ai_generated"
	}
	if generationMode == "managed_agent" && edited {
		return "modified"
	}
	return "manual"
}

func ensureTargetWithinScope(rs *resolvedScope, target reportTarget) error {
	if rs == nil {
		return nil
	}
	if target.Type == "user" && target.UserID != "" && !stringSet(rs.UserIDs)[target.UserID] {
		return errForbidden
	}
	if target.Type == "team" && rs.TeamID != "" && target.TeamID != "" && target.TeamID != rs.TeamID {
		return errForbidden
	}
	if target.Type == "department" && rs.DepartmentID != "" && target.DepartmentID != "" && target.DepartmentID != rs.DepartmentID {
		return errForbidden
	}
	return nil
}

func scopeTypeForReportTarget(u *model.User, reportType string, target reportTarget) string {
	if target.Type == "user" {
		if u != nil && target.UserID == u.ID {
			return "self"
		}
		if u != nil && u.Role == "admin" {
			return "all"
		}
		if u != nil && u.Role == "team_leader" {
			return "team"
		}
		return "department"
	}
	switch reportType {
	case reportTypeTeamDaily, reportTypeTeamWeekly:
		return "team"
	case reportTypeDepartmentDaily, reportTypeDepartmentWeekly:
		if u != nil && u.Role == "admin" {
			return "all"
		}
		return "department"
	}
	return "self"
}

func intersectIDs(visible, requested []string) []string {
	set := stringSet(visible)
	out := make([]string, 0, len(requested))
	for _, id := range requested {
		if set[id] {
			out = append(out, id)
		}
	}
	return out
}

func countMapEntries(m map[string]int) []map[string]any {
	out := make([]map[string]any, 0, len(m))
	for k, v := range m {
		out = append(out, map[string]any{"date": k, "count": v})
	}
	return out
}

func userCountEntries(m map[string]int, infoMap map[string]userInfo) []map[string]any {
	out := make([]map[string]any, 0, len(m))
	for uid, count := range m {
		entry := map[string]any{"user_id": uid, "count": count}
		if info, ok := infoMap[uid]; ok {
			entry["username"] = info.Username
		}
		out = append(out, entry)
	}
	return out
}

func dailyReportDateKey(value string) string {
	if len(value) >= len("2006-01-02") {
		candidate := value[:len("2006-01-02")]
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			return candidate
		}
	}
	return value
}

func computeDailyMissing(ctx context.Context, db *sql.DB, reportScope string, visible []string, start, end string, reports []dailyReportItem) []map[string]any {
	if reportScope != "personal" {
		return []map[string]any{}
	}
	existing := map[string]bool{}
	for _, r := range reports {
		if r.Owner.UserID == "" {
			continue
		}
		existing[r.Owner.UserID+"|"+dailyReportDateKey(r.Date)] = true
	}
	infoMap, _ := loadUserInfoMap(ctx, db, visible)
	missing := []map[string]any{}
	startT, _ := time.Parse("2006-01-02", start)
	endT, _ := time.Parse("2006-01-02", end)
	for d := startT; !d.After(endT); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		for _, uid := range visible {
			if existing[uid+"|"+dateStr] {
				continue
			}
			entry := map[string]any{
				"date":       dateStr,
				"owner_type": "user",
				"owner_id":   uid,
				"reason":     "missing_report",
			}
			if info, ok := infoMap[uid]; ok {
				entry["username"] = info.Username
			}
			missing = append(missing, entry)
		}
	}
	return missing
}

func loadDailyInventoryExisting(ctx context.Context, db *sql.DB, reportScope string, rs *resolvedScope, start, end string) ([]map[string]any, error) {
	switch reportScope {
	case "personal":
		rows, err := db.QueryContext(ctx, `
			SELECT id::text, user_id::text, report_date::text, COALESCE(generation_mode,'default'), edited
			FROM daily_reports
			WHERE report_date >= $1 AND report_date <= $2 AND user_id = ANY($3)
			  AND status IS NOT NULL
			  AND NULLIF(TRIM(COALESCE(content, '')), '') IS NOT NULL`, start, end, pq.Array(rs.UserIDs))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, uid, date, mode string
			var edited bool
			if err := rows.Scan(&id, &uid, &date, &mode, &edited); err != nil {
				return nil, err
			}
			out = append(out, map[string]any{
				"owner_type":     "user",
				"owner_id":       uid,
				"date":           date,
				"report_id":      id,
				"product_status": dailyProductStatus(mode, edited),
			})
		}
		return out, rows.Err()
	case "team":
		rows, err := db.QueryContext(ctx, `
			SELECT id::text, team_id::text, report_date::text, COALESCE(generation_mode,'default'), edited
			FROM team_reports
			WHERE report_date >= $1 AND report_date <= $2
			  AND EXISTS (SELECT 1 FROM users vu WHERE vu.team_id = team_reports.team_id AND vu.id::text = ANY($3))
			  AND status IS NOT NULL
			  AND NULLIF(TRIM(COALESCE(content, '')), '') IS NOT NULL`, start, end, pq.Array(rs.UserIDs))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, tid, date, mode string
			var edited bool
			if err := rows.Scan(&id, &tid, &date, &mode, &edited); err != nil {
				return nil, err
			}
			out = append(out, map[string]any{
				"owner_type":     "team",
				"owner_id":       tid,
				"date":           date,
				"report_id":      id,
				"product_status": dailyProductStatus(mode, edited),
			})
		}
		return out, rows.Err()
	case "department":
		rows, err := db.QueryContext(ctx, `
			SELECT id::text, report_date::text, COALESCE(generation_mode,'default'), edited
			FROM department_reports
			WHERE report_date >= $1 AND report_date <= $2
			  AND status IS NOT NULL
			  AND NULLIF(TRIM(COALESCE(content, '')), '') IS NOT NULL`, start, end)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, date, mode string
			var edited bool
			if err := rows.Scan(&id, &date, &mode, &edited); err != nil {
				return nil, err
			}
			out = append(out, map[string]any{
				"owner_type":     "department",
				"owner_id":       "",
				"date":           date,
				"report_id":      id,
				"product_status": dailyProductStatus(mode, edited),
			})
		}
		return out, rows.Err()
	}
	return nil, nil
}

func loadDailyInventoryExpected(ctx context.Context, db *sql.DB, reportScope string, rs *resolvedScope, start, end string) ([]map[string]any, error) {
	startT, _ := time.Parse("2006-01-02", start)
	endT, _ := time.Parse("2006-01-02", end)
	dates := []string{}
	for d := startT; !d.After(endT); d = d.AddDate(0, 0, 1) {
		dates = append(dates, d.Format("2006-01-02"))
	}
	switch reportScope {
	case "personal":
		infoMap, err := loadUserInfoMap(ctx, db, rs.UserIDs)
		if err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, uid := range rs.UserIDs {
			entry := map[string]any{
				"owner_type": "user",
				"owner_id":   uid,
				"dates":      dates,
			}
			if info, ok := infoMap[uid]; ok {
				entry["username"] = info.Username
			}
			out = append(out, entry)
		}
		return out, nil
	case "team":
		teams, err := loadInventoryTeams(ctx, db, rs)
		if err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, team := range teams {
			out = append(out, map[string]any{
				"owner_type": "team",
				"owner_id":   team.ID,
				"team_name":  team.Name,
				"dates":      dates,
			})
		}
		return out, nil
	case "department":
		return []map[string]any{{
			"owner_type":      "department",
			"owner_id":        rs.DepartmentID,
			"department_id":   rs.DepartmentID,
			"department_name": "部门",
			"dates":           dates,
		}}, nil
	}
	return []map[string]any{}, nil
}

func loadWeeklyInventoryExisting(ctx context.Context, db *sql.DB, reportScope string, rs *resolvedScope, ws, we string) ([]map[string]any, error) {
	switch reportScope {
	case "personal":
		rows, err := db.QueryContext(ctx, `
			SELECT id::text, user_id::text, week_start::text, COALESCE(generation_mode,'default'), edited
			FROM personal_weekly_reports
			WHERE week_start >= $1 AND week_end <= $2 AND user_id = ANY($3)
			  AND status IS NOT NULL
			  AND NULLIF(TRIM(COALESCE(content, '')), '') IS NOT NULL`, ws, we, pq.Array(rs.UserIDs))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, uid, ws2, mode string
			var edited bool
			if err := rows.Scan(&id, &uid, &ws2, &mode, &edited); err != nil {
				return nil, err
			}
			out = append(out, map[string]any{
				"owner_type":     "user",
				"owner_id":       uid,
				"week_start":     ws2,
				"report_id":      id,
				"product_status": dailyProductStatus(mode, edited),
			})
		}
		return out, rows.Err()
	case "team":
		rows, err := db.QueryContext(ctx, `
			SELECT id::text, team_id::text, week_start::text, COALESCE(generation_mode,'default'), edited
			FROM team_weekly_reports
			WHERE week_start >= $1 AND week_end <= $2
			  AND EXISTS (SELECT 1 FROM users vu WHERE vu.team_id = team_weekly_reports.team_id AND vu.id::text = ANY($3))
			  AND NULLIF(TRIM(COALESCE(content, '')), '') IS NOT NULL`, ws, we, pq.Array(rs.UserIDs))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, tid, ws2, mode string
			var edited bool
			if err := rows.Scan(&id, &tid, &ws2, &mode, &edited); err != nil {
				return nil, err
			}
			out = append(out, map[string]any{
				"owner_type":     "team",
				"owner_id":       tid,
				"week_start":     ws2,
				"report_id":      id,
				"product_status": dailyProductStatus(mode, edited),
			})
		}
		return out, rows.Err()
	case "department":
		rows, err := db.QueryContext(ctx, `
			SELECT id::text, week_start::text, COALESCE(generation_mode,'default'), edited
			FROM department_weekly_reports
			WHERE week_start >= $1 AND week_end <= $2
			  AND NULLIF(TRIM(COALESCE(content, '')), '') IS NOT NULL`, ws, we)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, ws2, mode string
			var edited bool
			if err := rows.Scan(&id, &ws2, &mode, &edited); err != nil {
				return nil, err
			}
			out = append(out, map[string]any{
				"owner_type":     "department",
				"owner_id":       "",
				"week_start":     ws2,
				"report_id":      id,
				"product_status": dailyProductStatus(mode, edited),
			})
		}
		return out, rows.Err()
	}
	return nil, nil
}

func loadWeeklyInventoryExpected(ctx context.Context, db *sql.DB, reportScope string, rs *resolvedScope, ws, we string) ([]map[string]any, error) {
	switch reportScope {
	case "personal":
		infoMap, err := loadUserInfoMap(ctx, db, rs.UserIDs)
		if err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, uid := range rs.UserIDs {
			entry := map[string]any{
				"owner_type": "user",
				"owner_id":   uid,
				"week_start": ws,
				"week_end":   we,
			}
			if info, ok := infoMap[uid]; ok {
				entry["username"] = info.Username
			}
			out = append(out, entry)
		}
		return out, nil
	case "team":
		teams, err := loadInventoryTeams(ctx, db, rs)
		if err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, team := range teams {
			out = append(out, map[string]any{
				"owner_type": "team",
				"owner_id":   team.ID,
				"team_name":  team.Name,
				"week_start": ws,
				"week_end":   we,
			})
		}
		return out, nil
	case "department":
		return []map[string]any{{
			"owner_type":      "department",
			"owner_id":        rs.DepartmentID,
			"department_id":   rs.DepartmentID,
			"department_name": "部门",
			"week_start":      ws,
			"week_end":        we,
		}}, nil
	}
	return []map[string]any{}, nil
}

type inventoryTeam struct {
	ID   string
	Name string
}

func loadInventoryTeams(ctx context.Context, db *sql.DB, rs *resolvedScope) ([]inventoryTeam, error) {
	query := `SELECT id::text, name FROM teams`
	args := []any{}
	if rs != nil && rs.TeamID != "" {
		query += ` WHERE id = $1`
		args = append(args, rs.TeamID)
	} else if rs != nil && rs.Type == "department" && rs.DepartmentID != "" {
		query += ` WHERE director_user_id = $1`
		args = append(args, rs.DepartmentID)
	}
	query += ` ORDER BY name`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	teams := []inventoryTeam{}
	for rows.Next() {
		var team inventoryTeam
		if err := rows.Scan(&team.ID, &team.Name); err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

func computeMissing(expected, existing []map[string]any) []map[string]any {
	existKeys := map[string]bool{}
	for _, e := range existing {
		key := fmt.Sprintf("%v|%s", e["owner_id"], inventoryPeriodKey(e["date"]))
		if e["week_start"] != nil {
			key = fmt.Sprintf("%v|%s", e["owner_id"], inventoryPeriodKey(e["week_start"]))
		}
		existKeys[key] = true
	}
	missing := []map[string]any{}
	for _, exp := range expected {
		dates, _ := exp["dates"].([]string)
		if dates == nil {
			key := fmt.Sprintf("%v|%s", exp["owner_id"], inventoryPeriodKey(exp["date"]))
			if exp["week_start"] != nil {
				key = fmt.Sprintf("%v|%s", exp["owner_id"], inventoryPeriodKey(exp["week_start"]))
			}
			if !existKeys[key] {
				missing = append(missing, exp)
			}
			continue
		}
		uid, _ := exp["owner_id"].(string)
		for _, d := range dates {
			if existKeys[uid+"|"+d] {
				continue
			}
			entry := map[string]any{
				"owner_type": exp["owner_type"],
				"owner_id":   uid,
				"date":       d,
			}
			copyInventoryOwnerMetadata(entry, exp)
			missing = append(missing, entry)
		}
	}
	return missing
}

func reportSourceState(reportScope, reportKind string, existingCount, expectedCount int, missing []map[string]any) map[string]any {
	sourceMode := "reports_only"
	switch {
	case existingCount == 0 && expectedCount > 0:
		sourceMode = "insufficient"
	case len(missing) > 0:
		sourceMode = "mixed"
	}
	return map[string]any{
		"primary_source":   reportPrimarySource(reportScope, reportKind),
		"source_mode":      sourceMode,
		"dependency_ready": len(missing) == 0,
		"expected_count":   expectedCount,
		"existing_count":   existingCount,
		"missing_count":    len(missing),
		"missing_names":    missingDisplayNames(missing),
	}
}

func reportPrimarySource(reportScope, reportKind string) string {
	switch reportScope {
	case "personal":
		if reportKind == "weekly" {
			return "personal_weekly_reports"
		}
		return "personal_daily_reports"
	case "team":
		if reportKind == "weekly" {
			return "team_weekly_reports"
		}
		return "team_daily_reports"
	case "department":
		if reportKind == "weekly" {
			return "department_weekly_reports"
		}
		return "department_daily_reports"
	default:
		return reportKind + "_reports"
	}
}

func missingDisplayNames(missing []map[string]any) []string {
	names := []string{}
	seen := map[string]bool{}
	for _, item := range missing {
		name := displayNameFromInventoryItem(item)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func displayNameFromInventoryItem(item map[string]any) string {
	for _, key := range []string{"username", "team_name", "department_name"} {
		if value, ok := item[key].(string); ok && value != "" {
			return value
		}
	}
	if value, ok := item["owner_type"].(string); ok && value == "department" {
		return "部门"
	}
	return ""
}

func copyInventoryOwnerMetadata(dst, src map[string]any) {
	for _, key := range []string{"username", "team_name", "department_id", "department_name"} {
		if value, ok := src[key]; ok {
			dst[key] = value
		}
	}
}

func inventoryPeriodKey(value any) string {
	switch v := value.(type) {
	case time.Time:
		return v.Format("2006-01-02")
	case string:
		return dailyReportDateKey(v)
	default:
		return fmt.Sprintf("%v", value)
	}
}
