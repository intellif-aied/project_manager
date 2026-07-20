package reportcontext

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/reportsource"
	"github.com/lib/pq"
)

func loadScope(ctx context.Context, tx *sql.Tx, request BuildRequest) (Scope, error) {
	scope := Scope{EffectiveUserID: request.UserID, UserIDs: []string{}, Members: []Actor{}, Teams: []TeamScope{}}
	switch request.ReportType {
	case ReportTypePersonalDaily, ReportTypePersonalWeekly:
		scope.Type = "self"
		member, err := loadUser(ctx, tx, request.Target.UserID)
		if err != nil {
			return Scope{}, err
		}
		scope.UserIDs = []string{member.ID}
		scope.Members = []Actor{member}
	case ReportTypeTeamDaily, ReportTypeTeamWeekly:
		scope.Type = "team"
		scope.TeamID = request.Target.TeamID
		if err := tx.QueryRowContext(ctx, `SELECT name FROM teams WHERE id = $1`, scope.TeamID).Scan(&scope.TeamName); err != nil {
			return Scope{}, mapNoRows(err)
		}
		members, err := loadUsersForTeam(ctx, tx, scope.TeamID)
		if err != nil {
			return Scope{}, err
		}
		scope.Members = members
		for _, member := range members {
			scope.UserIDs = append(scope.UserIDs, member.ID)
		}
		scope.Teams = []TeamScope{{ID: scope.TeamID, Name: scope.TeamName, MemberIDs: append([]string(nil), scope.UserIDs...)}}
	case ReportTypeDepartmentDaily, ReportTypeDepartmentWeekly:
		scope.Type = "department"
		scope.DepartmentID = request.Target.DepartmentID
		if err := tx.QueryRowContext(ctx, `SELECT name FROM departments WHERE id = $1`, scope.DepartmentID).Scan(&scope.DepartmentName); err != nil {
			return Scope{}, mapNoRows(err)
		}
		teams, err := loadTeamsForDepartment(ctx, tx, scope.DepartmentID)
		if err != nil {
			return Scope{}, err
		}
		members, err := loadUsersForDepartment(ctx, tx, scope.DepartmentID)
		if err != nil {
			return Scope{}, err
		}
		for i := range teams {
			for _, member := range members {
				if member.TeamID == teams[i].ID {
					teams[i].MemberIDs = append(teams[i].MemberIDs, member.ID)
				}
			}
		}
		scope.Teams = teams
		scope.Members = members
		for _, member := range members {
			scope.UserIDs = append(scope.UserIDs, member.ID)
		}
	}
	sort.Strings(scope.UserIDs)
	return scope, nil
}

func loadUser(ctx context.Context, tx *sql.Tx, userID string) (Actor, error) {
	var actor Actor
	err := tx.QueryRowContext(ctx, `
		SELECT u.id::text, COALESCE(NULLIF(u.nickname, ''), NULLIF(u.name, ''), u.username),
		       COALESCE(u.team_id::text, ''), COALESCE(t.name, '')
		FROM users u LEFT JOIN teams t ON t.id = u.team_id
		WHERE u.id = $1`, userID).Scan(&actor.ID, &actor.Name, &actor.TeamID, &actor.TeamName)
	return actor, mapNoRows(err)
}

func loadUsersForTeam(ctx context.Context, tx *sql.Tx, teamID string) ([]Actor, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT u.id::text, COALESCE(NULLIF(u.nickname, ''), NULLIF(u.name, ''), u.username),
		       COALESCE(u.team_id::text, ''), COALESCE(t.name, '')
		FROM users u LEFT JOIN teams t ON t.id = u.team_id
		WHERE u.team_id = $1 AND u.status = 'active'
		ORDER BY u.id`, teamID)
	if err != nil {
		return nil, err
	}
	return scanActors(rows)
}

func loadUsersForDepartment(ctx context.Context, tx *sql.Tx, departmentID string) ([]Actor, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT u.id::text, COALESCE(NULLIF(u.nickname, ''), NULLIF(u.name, ''), u.username),
		       COALESCE(u.team_id::text, ''), COALESCE(t.name, '')
		FROM users u LEFT JOIN teams t ON t.id = u.team_id
		WHERE u.status = 'active' AND (u.department_id = $1 OR t.department_id = $1)
		ORDER BY u.id::text`, departmentID)
	if err != nil {
		return nil, err
	}
	return scanActors(rows)
}

func scanActors(rows *sql.Rows) ([]Actor, error) {
	defer rows.Close()
	actors := []Actor{}
	for rows.Next() {
		var actor Actor
		if err := rows.Scan(&actor.ID, &actor.Name, &actor.TeamID, &actor.TeamName); err != nil {
			return nil, err
		}
		actors = append(actors, actor)
	}
	return actors, rows.Err()
}

func loadTeamsForDepartment(ctx context.Context, tx *sql.Tx, departmentID string) ([]TeamScope, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id::text, name FROM teams WHERE department_id = $1 ORDER BY id`, departmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	teams := []TeamScope{}
	for rows.Next() {
		var team TeamScope
		team.MemberIDs = []string{}
		if err := rows.Scan(&team.ID, &team.Name); err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

type reportRow struct {
	ID        string
	Owner     Actor
	TeamID    string
	TeamName  string
	Period    reportsource.Period
	Content   string
	UpdatedAt time.Time
}

func loadReportsAndCoverage(ctx context.Context, tx *sql.Tx, request BuildRequest, scope Scope) ([]SourceReport, []CoverageItem, []SourceIssue, error) {
	switch request.ReportType {
	case ReportTypePersonalDaily:
		return []SourceReport{}, []CoverageItem{}, []SourceIssue{}, nil
	case ReportTypePersonalWeekly:
		rows, err := queryPersonalDailyReports(ctx, tx, scope.UserIDs, request.Period.Start, request.Period.End)
		if err != nil {
			return nil, nil, nil, err
		}
		return collectUncoveredReports(rows, ReportTypePersonalDaily, request.Period.End)
	case ReportTypeTeamDaily:
		rows, err := queryPersonalDailyReports(ctx, tx, scope.UserIDs, request.Period.Start, request.Period.End)
		if err != nil {
			return nil, nil, nil, err
		}
		return collectMemberCoverage(scope.Members, rows, ReportTypePersonalDaily, request.Period.End)
	case ReportTypeTeamWeekly:
		rows, err := queryPersonalWeeklyReports(ctx, tx, scope.UserIDs, request.Period.Start)
		if err != nil {
			return nil, nil, nil, err
		}
		return collectMemberCoverage(scope.Members, rows, ReportTypePersonalWeekly, request.Period.End)
	case ReportTypeDepartmentDaily, ReportTypeDepartmentWeekly:
		return loadDepartmentReports(ctx, tx, request, scope)
	default:
		return nil, nil, nil, ErrInvalidRequest
	}
}

func loadDepartmentReports(ctx context.Context, tx *sql.Tx, request BuildRequest, scope Scope) ([]SourceReport, []CoverageItem, []SourceIssue, error) {
	teamIDs := make([]string, 0, len(scope.Teams))
	teamSet := map[string]bool{}
	for _, team := range scope.Teams {
		teamIDs = append(teamIDs, team.ID)
		teamSet[team.ID] = true
	}
	directMembers := []Actor{}
	directIDs := []string{}
	for _, member := range scope.Members {
		if member.TeamID == "" || !teamSet[member.TeamID] {
			directMembers = append(directMembers, member)
			directIDs = append(directIDs, member.ID)
		}
	}

	var teamRows, memberRows []reportRow
	var err error
	teamReportType := ReportTypeTeamDaily
	memberReportType := ReportTypePersonalDaily
	if request.ReportType == ReportTypeDepartmentDaily {
		teamRows, err = queryTeamDailyReports(ctx, tx, teamIDs, request.Period.Start)
		if err == nil {
			memberRows, err = queryPersonalDailyReports(ctx, tx, directIDs, request.Period.Start, request.Period.End)
		}
	} else {
		teamReportType = ReportTypeTeamWeekly
		memberReportType = ReportTypePersonalWeekly
		teamRows, err = queryTeamWeeklyReports(ctx, tx, teamIDs, request.Period.Start)
		if err == nil {
			memberRows, err = queryPersonalWeeklyReports(ctx, tx, directIDs, request.Period.Start)
		}
	}
	if err != nil {
		return nil, nil, nil, err
	}

	reports1, coverage1, issues1, err := collectTeamCoverage(scope.Teams, teamRows, teamReportType, request.Period.End)
	if err != nil {
		return nil, nil, nil, err
	}
	reports2, coverage2, issues2, err := collectMemberCoverage(directMembers, memberRows, memberReportType, request.Period.End)
	if err != nil {
		return nil, nil, nil, err
	}
	reports := append(reports1, reports2...)
	coverage := append(coverage1, coverage2...)
	issues := append(issues1, issues2...)
	sort.Slice(reports, func(i, j int) bool { return reportKey(reports[i]) < reportKey(reports[j]) })
	return reports, coverage, issues, nil
}

func collectUncoveredReports(rows []reportRow, reportType, expectedEnd string) ([]SourceReport, []CoverageItem, []SourceIssue, error) {
	seen := map[string]bool{}
	reports := []SourceReport{}
	issues := []SourceIssue{}
	for _, row := range rows {
		key := row.Owner.ID + ":" + row.Period.Start
		if seen[key] {
			return nil, nil, nil, fmt.Errorf("%w: %s %s", ErrDuplicate, reportType, key)
		}
		seen[key] = true
		if reason := reportInvalidReason(row, reportType, expectedEnd); reason != "" {
			issues = append(issues, invalidIssue("member", row.Owner.ID, row.Owner.Name, row.ID, reason))
			continue
		}
		reports = append(reports, sourceReport(row, reportType))
	}
	sort.Slice(reports, func(i, j int) bool { return reportKey(reports[i]) < reportKey(reports[j]) })
	return reports, []CoverageItem{}, issues, nil
}

func collectMemberCoverage(members []Actor, rows []reportRow, reportType, expectedEnd string) ([]SourceReport, []CoverageItem, []SourceIssue, error) {
	byOwner := map[string][]reportRow{}
	for _, row := range rows {
		byOwner[row.Owner.ID] = append(byOwner[row.Owner.ID], row)
	}
	reports := []SourceReport{}
	coverage := []CoverageItem{}
	issues := []SourceIssue{}
	for _, member := range members {
		item := CoverageItem{Type: "member", ID: member.ID, Name: member.Name, ExpectedReportType: reportType}
		matched := byOwner[member.ID]
		switch len(matched) {
		case 0:
			item.SourceStatus = "missing"
			issues = append(issues, missingIssue("member", member.ID, member.Name))
		case 1:
			row := matched[0]
			item.ReportID = row.ID
			if reason := reportInvalidReason(row, reportType, expectedEnd); reason != "" {
				item.SourceStatus = "invalid"
				item.InvalidReason = reason
				issues = append(issues, invalidIssue("member", member.ID, member.Name, row.ID, reason))
			} else {
				item.SourceStatus = "available"
				reports = append(reports, sourceReport(row, reportType))
			}
		default:
			return nil, nil, nil, fmt.Errorf("%w: %s member %s", ErrDuplicate, reportType, member.ID)
		}
		coverage = append(coverage, item)
	}
	sort.Slice(reports, func(i, j int) bool { return reportKey(reports[i]) < reportKey(reports[j]) })
	return reports, coverage, issues, nil
}

func collectTeamCoverage(teams []TeamScope, rows []reportRow, reportType, expectedEnd string) ([]SourceReport, []CoverageItem, []SourceIssue, error) {
	byTeam := map[string][]reportRow{}
	for _, row := range rows {
		byTeam[row.TeamID] = append(byTeam[row.TeamID], row)
	}
	reports := []SourceReport{}
	coverage := []CoverageItem{}
	issues := []SourceIssue{}
	for _, team := range teams {
		item := CoverageItem{Type: "team", ID: team.ID, Name: team.Name, MemberIDs: team.MemberIDs, ExpectedReportType: reportType}
		matched := byTeam[team.ID]
		switch len(matched) {
		case 0:
			item.SourceStatus = "missing"
			issues = append(issues, missingIssue("team", team.ID, team.Name))
		case 1:
			row := matched[0]
			item.ReportID = row.ID
			if reason := reportInvalidReason(row, reportType, expectedEnd); reason != "" {
				item.SourceStatus = "invalid"
				item.InvalidReason = reason
				issues = append(issues, invalidIssue("team", team.ID, team.Name, row.ID, reason))
			} else {
				item.SourceStatus = "available"
				reports = append(reports, sourceReport(row, reportType))
			}
		default:
			return nil, nil, nil, fmt.Errorf("%w: %s team %s", ErrDuplicate, reportType, team.ID)
		}
		coverage = append(coverage, item)
	}
	return reports, coverage, issues, nil
}

func reportInvalidReason(row reportRow, reportType, expectedEnd string) string {
	if strings.TrimSpace(row.Content) == "" {
		return "empty_content"
	}
	if strings.HasSuffix(reportType, "weekly") && row.Period.End != expectedEnd {
		return "period_mismatch"
	}
	return ""
}

func sourceReport(row reportRow, reportType string) SourceReport {
	return SourceReport{ID: row.ID, ReportType: reportType, Owner: row.Owner, TeamID: row.TeamID, TeamName: row.TeamName, Period: row.Period, Content: row.Content, UpdatedAt: biztime.FormatRFC3339(row.UpdatedAt)}
}

func reportKey(report SourceReport) string {
	return report.ReportType + ":" + report.TeamID + ":" + report.Owner.ID + ":" + report.Period.Start
}

func missingIssue(sourceType, sourceID, sourceName string) SourceIssue {
	return SourceIssue{Type: "missing", SourceType: sourceType, SourceID: sourceID, SourceName: sourceName, Reason: "report_not_found"}
}

func invalidIssue(sourceType, sourceID, sourceName, reportID, reason string) SourceIssue {
	return SourceIssue{Type: "invalid", SourceType: sourceType, SourceID: sourceID, SourceName: sourceName, ReportID: reportID, Reason: reason}
}

func queryPersonalDailyReports(ctx context.Context, tx *sql.Tx, userIDs []string, start, end string) ([]reportRow, error) {
	if len(userIDs) == 0 {
		return []reportRow{}, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT r.id::text, r.user_id::text,
		       COALESCE(NULLIF(u.nickname, ''), NULLIF(u.name, ''), u.username),
		       COALESCE(u.team_id::text, ''), COALESCE(t.name, ''),
		       r.report_date::text, r.content, r.updated_at
		FROM daily_reports r
		JOIN users u ON u.id = r.user_id LEFT JOIN teams t ON t.id = u.team_id
		WHERE r.user_id::text = ANY($1) AND r.report_date BETWEEN $2::date AND $3::date
		ORDER BY r.user_id, r.report_date, r.id`, pq.Array(userIDs), start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []reportRow{}
	for rows.Next() {
		var row reportRow
		if err := rows.Scan(&row.ID, &row.Owner.ID, &row.Owner.Name, &row.Owner.TeamID, &row.Owner.TeamName, &row.Period.Start, &row.Content, &row.UpdatedAt); err != nil {
			return nil, err
		}
		row.Period.End = row.Period.Start
		result = append(result, row)
	}
	return result, rows.Err()
}

func queryPersonalWeeklyReports(ctx context.Context, tx *sql.Tx, userIDs []string, weekStart string) ([]reportRow, error) {
	if len(userIDs) == 0 {
		return []reportRow{}, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT r.id::text, r.user_id::text,
		       COALESCE(NULLIF(u.nickname, ''), NULLIF(u.name, ''), u.username),
		       COALESCE(u.team_id::text, ''), COALESCE(t.name, ''),
		       r.week_start::text, r.week_end::text, r.content, r.updated_at
		FROM personal_weekly_reports r
		JOIN users u ON u.id = r.user_id LEFT JOIN teams t ON t.id = u.team_id
		WHERE r.user_id::text = ANY($1) AND r.week_start = $2::date
		ORDER BY r.user_id, r.id`, pq.Array(userIDs), weekStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []reportRow{}
	for rows.Next() {
		var row reportRow
		if err := rows.Scan(&row.ID, &row.Owner.ID, &row.Owner.Name, &row.Owner.TeamID, &row.Owner.TeamName, &row.Period.Start, &row.Period.End, &row.Content, &row.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func queryTeamDailyReports(ctx context.Context, tx *sql.Tx, teamIDs []string, date string) ([]reportRow, error) {
	if len(teamIDs) == 0 {
		return []reportRow{}, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT r.id::text, r.team_id::text, t.name, r.report_date::text, r.content, r.updated_at
		FROM team_reports r JOIN teams t ON t.id = r.team_id
		WHERE r.team_id::text = ANY($1) AND r.report_date = $2::date
		ORDER BY r.team_id, r.id`, pq.Array(teamIDs), date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []reportRow{}
	for rows.Next() {
		var row reportRow
		if err := rows.Scan(&row.ID, &row.TeamID, &row.TeamName, &row.Period.Start, &row.Content, &row.UpdatedAt); err != nil {
			return nil, err
		}
		row.Period.End = row.Period.Start
		result = append(result, row)
	}
	return result, rows.Err()
}

func queryTeamWeeklyReports(ctx context.Context, tx *sql.Tx, teamIDs []string, weekStart string) ([]reportRow, error) {
	if len(teamIDs) == 0 {
		return []reportRow{}, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT r.id::text, r.team_id::text, t.name, r.week_start::text, r.week_end::text, r.content, r.updated_at
		FROM team_weekly_reports r JOIN teams t ON t.id = r.team_id
		WHERE r.team_id::text = ANY($1) AND r.week_start = $2::date
		ORDER BY r.team_id, r.id`, pq.Array(teamIDs), weekStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []reportRow{}
	for rows.Next() {
		var row reportRow
		if err := rows.Scan(&row.ID, &row.TeamID, &row.TeamName, &row.Period.Start, &row.Period.End, &row.Content, &row.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func loadRequirements(ctx context.Context, tx *sql.Tx, request BuildRequest, scope Scope, startUTC, endExclusiveUTC time.Time) ([]Requirement, error) {
	clause := ""
	args := []any{startUTC, endExclusiveUTC}
	switch request.ReportType {
	case ReportTypePersonalDaily, ReportTypePersonalWeekly:
		clause = `(r.creator_id::text = $3 OR EXISTS (SELECT 1 FROM requirement_responsibles rr WHERE rr.requirement_id = r.id AND rr.user_id::text = $3) OR EXISTS (SELECT 1 FROM user_follows f WHERE f.user_id::text = $3 AND f.target_type = 'requirement' AND f.target_id = r.id))`
		args = append(args, request.Target.UserID)
	case ReportTypeTeamDaily, ReportTypeTeamWeekly:
		clause = `(EXISTS (SELECT 1 FROM requirement_teams rt WHERE rt.requirement_id = r.id AND rt.team_id::text = $3) OR (NOT EXISTS (SELECT 1 FROM requirement_teams rt_any WHERE rt_any.requirement_id = r.id) AND EXISTS (SELECT 1 FROM requirement_responsibles rr WHERE rr.requirement_id = r.id AND rr.user_id::text = ANY($4))))`
		args = append(args, request.Target.TeamID, pq.Array(scope.UserIDs))
	case ReportTypeDepartmentDaily, ReportTypeDepartmentWeekly:
		teamIDs := make([]string, 0, len(scope.Teams))
		for _, team := range scope.Teams {
			teamIDs = append(teamIDs, team.ID)
		}
		clause = `(EXISTS (SELECT 1 FROM requirement_teams rt WHERE rt.requirement_id = r.id AND rt.team_id::text = ANY($3)) OR EXISTS (SELECT 1 FROM requirement_responsibles rr WHERE rr.requirement_id = r.id AND rr.user_id::text = ANY($4)))`
		args = append(args, pq.Array(teamIDs), pq.Array(scope.UserIDs))
	default:
		return nil, ErrInvalidRequest
	}
	query := fmt.Sprintf(`
		SELECT r.id::text, r.title, r.description, r.status, r.priority, r.progress,
		       COALESCE(r.deadline::text, ''), r.creator_id::text,
		       COALESCE(NULLIF(c.nickname, ''), NULLIF(c.name, ''), c.username),
		       COALESCE(c.team_id::text, ''), COALESCE(ct.name, ''),
		       COALESCE((SELECT jsonb_agg(jsonb_build_object('id', ru.id::text, 'name', COALESCE(NULLIF(ru.nickname, ''), NULLIF(ru.name, ''), ru.username), 'team_id', COALESCE(ru.team_id::text, ''), 'team_name', COALESCE(rut.name, '')) ORDER BY ru.id) FROM requirement_responsibles rr JOIN users ru ON ru.id = rr.user_id LEFT JOIN teams rut ON rut.id = ru.team_id WHERE rr.requirement_id = r.id), '[]'::jsonb),
		       ARRAY(SELECT rt.team_id::text FROM requirement_teams rt WHERE rt.requirement_id = r.id ORDER BY rt.team_id),
		       r.updated_at
		FROM requirements r
		JOIN users c ON c.id = r.creator_id LEFT JOIN teams ct ON ct.id = c.team_id
		WHERE r.updated_at >= $1 AND r.updated_at < $2 AND %s
		ORDER BY r.updated_at, r.id`, clause)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Requirement{}
	for rows.Next() {
		var item Requirement
		var responsibleJSON []byte
		var teamIDs pq.StringArray
		var updated time.Time
		if err := rows.Scan(&item.ID, &item.Title, &item.Description, &item.Status, &item.Priority, &item.Progress, &item.Deadline, &item.Creator.ID, &item.Creator.Name, &item.Creator.TeamID, &item.Creator.TeamName, &responsibleJSON, &teamIDs, &updated); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(responsibleJSON, &item.Responsibles); err != nil {
			return nil, err
		}
		if item.Responsibles == nil {
			item.Responsibles = []Actor{}
		}
		item.TeamIDs = []string(teamIDs)
		item.UpdatedAt = biztime.FormatRFC3339(updated)
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadTasks(ctx context.Context, tx *sql.Tx, request BuildRequest, scope Scope, startUTC, endExclusiveUTC time.Time) ([]Task, error) {
	clause := ""
	args := []any{startUTC, endExclusiveUTC}
	switch request.ReportType {
	case ReportTypePersonalDaily, ReportTypePersonalWeekly:
		clause = `(t.creator_id::text = $3 OR EXISTS (SELECT 1 FROM task_responsibles tr WHERE tr.task_id = t.id AND tr.user_id::text = $3) OR EXISTS (SELECT 1 FROM user_follows f WHERE f.user_id::text = $3 AND f.target_type = 'task' AND f.target_id = t.id))`
		args = append(args, request.Target.UserID)
	case ReportTypeTeamDaily, ReportTypeTeamWeekly, ReportTypeDepartmentDaily, ReportTypeDepartmentWeekly:
		clause = `EXISTS (SELECT 1 FROM task_responsibles tr WHERE tr.task_id = t.id AND tr.user_id::text = ANY($3))`
		args = append(args, pq.Array(scope.UserIDs))
	default:
		return nil, ErrInvalidRequest
	}
	query := fmt.Sprintf(`
		SELECT t.id::text, t.requirement_id::text, r.title, t.title, t.status, t.priority, t.progress,
		       COALESCE(t.due_date::text, ''), COALESCE(t.creator_id::text, ''),
		       COALESCE(NULLIF(c.nickname, ''), NULLIF(c.name, ''), c.username, ''),
		       COALESCE(c.team_id::text, ''), COALESCE(ct.name, ''),
		       COALESCE((SELECT jsonb_agg(jsonb_build_object('id', ru.id::text, 'name', COALESCE(NULLIF(ru.nickname, ''), NULLIF(ru.name, ''), ru.username), 'team_id', COALESCE(ru.team_id::text, ''), 'team_name', COALESCE(rut.name, '')) ORDER BY ru.id) FROM task_responsibles tr JOIN users ru ON ru.id = tr.user_id LEFT JOIN teams rut ON rut.id = ru.team_id WHERE tr.task_id = t.id), '[]'::jsonb),
		       t.updated_at
		FROM tasks t JOIN requirements r ON r.id = t.requirement_id
		LEFT JOIN users c ON c.id = t.creator_id LEFT JOIN teams ct ON ct.id = c.team_id
		WHERE t.updated_at >= $1 AND t.updated_at < $2 AND %s
		ORDER BY t.updated_at, t.id`, clause)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Task{}
	for rows.Next() {
		var item Task
		var responsibleJSON []byte
		var updated time.Time
		if err := rows.Scan(&item.ID, &item.RequirementID, &item.RequirementTitle, &item.Title, &item.Status, &item.Priority, &item.Progress, &item.DueDate, &item.Creator.ID, &item.Creator.Name, &item.Creator.TeamID, &item.Creator.TeamName, &responsibleJSON, &updated); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(responsibleJSON, &item.Responsibles); err != nil {
			return nil, err
		}
		if item.Responsibles == nil {
			item.Responsibles = []Actor{}
		}
		item.UpdatedAt = biztime.FormatRFC3339(updated)
		result = append(result, item)
	}
	return result, rows.Err()
}

func mapNoRows(err error) error {
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	return err
}
