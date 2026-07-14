package handler

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/model"
	"github.com/go-chi/chi/v5"
)

type memberScope struct{ departmentID, teamID string }

func (h *ReportHandler) resolveMemberScope(w http.ResponseWriter, r *http.Request, u *model.User) (memberScope, bool) {
	if u == nil {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return memberScope{}, false
	}
	switch u.Role {
	case "team_leader":
		if u.TeamID == nil {
			writeJSON(w, 403, map[string]string{"error": "access denied"})
			return memberScope{}, false
		}
		return memberScope{teamID: *u.TeamID}, true
	case "director":
		var id string
		if err := h.db.QueryRowContext(r.Context(), `SELECT id::text FROM departments WHERE director_user_id=$1`, u.ID).Scan(&id); err != nil {
			writeJSON(w, 403, map[string]string{"error": "department not configured"})
			return memberScope{}, false
		}
		return memberScope{departmentID: id}, true
	case "admin":
		id := strings.TrimSpace(r.URL.Query().Get("department_id"))
		if id == "" {
			writeJSON(w, 400, map[string]string{"error": "department_id is required"})
			return memberScope{}, false
		}
		return memberScope{departmentID: id}, true
	default:
		writeJSON(w, 403, map[string]string{"error": "access denied"})
		return memberScope{}, false
	}
}

func memberScopeWhere(scope memberScope) (string, any) {
	if scope.teamID != "" {
		return "u.team_id::text = $2", scope.teamID
	}
	return "COALESCE(u.department_id, t.department_id)::text = $2", scope.departmentID
}

func (h *ReportHandler) ListMemberDailyReports(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	scope, ok := h.resolveMemberScope(w, r, u)
	if !ok {
		return
	}
	date := r.URL.Query().Get("date")
	if date == "" {
		date = biztime.Today()
	}
	where, arg := memberScopeWhere(scope)
	h.listMemberReports(w, `
		SELECT u.id::text, COALESCE(NULLIF(u.nickname,''),u.username), u.app_role,
			COALESCE(u.department_id,t.department_id)::text, COALESCE(d.name,''), u.team_id::text, COALESCE(t.name,''),
			dr.id::text, dr.status, COALESCE(dr.saved_at,dr.updated_at),
			LEFT(COALESCE(NULLIF(dr.submitted_content,''),dr.content,''),160)
		FROM users u LEFT JOIN teams t ON t.id=u.team_id
		LEFT JOIN departments d ON d.id=COALESCE(u.department_id,t.department_id)
		LEFT JOIN daily_reports dr ON dr.user_id=u.id AND dr.report_date=$1 AND NULLIF(TRIM(COALESCE(NULLIF(dr.submitted_content,''),dr.content,'')),'') IS NOT NULL
		WHERE `+where+` AND u.local_enabled=true ORDER BY CASE u.app_role WHEN 'director' THEN 1 WHEN 'pm' THEN 2 WHEN 'team_leader' THEN 3 ELSE 4 END, COALESCE(NULLIF(u.nickname,''),u.username)`, date, arg)
}

func (h *ReportHandler) ListMemberWeeklyReports(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	scope, ok := h.resolveMemberScope(w, r, u)
	if !ok {
		return
	}
	weekStart := r.URL.Query().Get("week_start")
	if weekStart == "" {
		writeJSON(w, 400, map[string]string{"error": "week_start is required"})
		return
	}
	where, arg := memberScopeWhere(scope)
	h.listMemberReports(w, `
		SELECT u.id::text, COALESCE(NULLIF(u.nickname,''),u.username), u.app_role,
			COALESCE(u.department_id,t.department_id)::text, COALESCE(d.name,''), u.team_id::text, COALESCE(t.name,''),
			pwr.id::text, pwr.status, COALESCE(pwr.saved_at,pwr.updated_at), LEFT(COALESCE(NULLIF(pwr.submitted_content,''),pwr.content,''),160)
		FROM users u LEFT JOIN teams t ON t.id=u.team_id
		LEFT JOIN departments d ON d.id=COALESCE(u.department_id,t.department_id)
		LEFT JOIN personal_weekly_reports pwr ON pwr.user_id=u.id AND pwr.week_start=$1 AND NULLIF(TRIM(COALESCE(NULLIF(pwr.submitted_content,''),pwr.content,'')),'') IS NOT NULL
		WHERE `+where+` AND u.local_enabled=true ORDER BY CASE u.app_role WHEN 'director' THEN 1 WHEN 'pm' THEN 2 WHEN 'team_leader' THEN 3 ELSE 4 END, COALESCE(NULLIF(u.nickname,''),u.username)`, weekStart, arg)
}

func (h *ReportHandler) listMemberReports(w http.ResponseWriter, query string, args ...any) {
	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []model.MemberPersonalReport{}
	for rows.Next() {
		var x model.MemberPersonalReport
		var did, tid, rid, status, preview sql.NullString
		var saved sql.NullTime
		if err := rows.Scan(&x.UserID, &x.UserName, &x.Role, &did, &x.DepartmentName, &tid, &x.TeamName, &rid, &status, &saved, &preview); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		x.DepartmentID = nullStringPtr(did)
		x.TeamID = nullStringPtr(tid)
		x.ReportID = nullStringPtr(rid)
		x.HasReport = rid.Valid
		if status.Valid {
			x.Status = status.String
		}
		x.SavedAt = nullTimePtr(saved)
		if preview.Valid {
			x.ContentPreview = preview.String
		}
		items = append(items, x)
	}
	writeJSON(w, 200, items)
}

func (h *ReportHandler) canAccessMemberUser(ctx context.Context, viewer *model.User, targetID string) bool {
	if viewer.ID == targetID || viewer.Role == "admin" {
		return true
	}
	var ok bool
	switch viewer.Role {
	case "team_leader":
		if viewer.TeamID != nil {
			_ = h.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND team_id=$2)`, targetID, *viewer.TeamID).Scan(&ok)
		}
	case "director":
		_ = h.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u LEFT JOIN teams t ON t.id=u.team_id JOIN departments d ON d.id=COALESCE(u.department_id,t.department_id) WHERE u.id=$1 AND d.director_user_id=$2)`, targetID, viewer.ID).Scan(&ok)
	}
	return ok
}

func (h *ReportHandler) GetMemberDailyReport(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	id := chi.URLParam(r, "id")
	var target string
	if err := h.db.QueryRow(`SELECT user_id::text FROM daily_reports WHERE id=$1`, id).Scan(&target); err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	if !h.canAccessMemberUser(r.Context(), u, target) {
		writeJSON(w, 403, map[string]string{"error": "access denied"})
		return
	}
	h.Get(w, r)
}

func (h *ReportHandler) GetMemberWeeklyReport(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	id := chi.URLParam(r, "id")
	report, err := h.getPersonalWeeklyReportByID(id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	if !h.canAccessMemberUser(r.Context(), u, report.UserID) {
		writeJSON(w, 403, map[string]string{"error": "access denied"})
		return
	}
	writeJSON(w, 200, report)
}
