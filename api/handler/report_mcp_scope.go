package handler

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/aidashboard/api/model"
)

// reportScope is the read permission boundary (doc §3.3 / §8).
type reportScope struct {
	Type         string   `json:"type"` // self | team | department | all
	TeamID       string   `json:"team_id,omitempty"`
	DepartmentID string   `json:"department_id,omitempty"`
	UserIDs      []string `json:"user_ids,omitempty"`
}

// reportTarget narrows a read or pinpoints a writeback target (doc §3.3.1).
type reportTarget struct {
	Type         string `json:"type"` // self | user | team | department
	UserID       string `json:"user_id,omitempty"`
	TeamID       string `json:"team_id,omitempty"`
	DepartmentID string `json:"department_id,omitempty"`
}

// resolvedScope is the post-validation scope: a concrete set of user IDs that the
// caller is allowed to read, plus concrete team/department identifiers when the
// scope is team/department. SQL queries consume visibleUserIDs.
type resolvedScope struct {
	Type         string
	UserIDs      []string
	TeamID       string
	DepartmentID string
}

var allowedScopesByRole = map[string][]string{
	"employee":    {"self"},
	"pm":          {"self"},
	"team_leader": {"self", "team"},
	"director":    {"self", "department"},
	"admin":       {"self", "team", "department", "all"},
}

var _ = containsString // reused from permissions.go
// resolveScope validates and converges the Agent-supplied scope against the
// current user's role. It never enlarges permissions. On violation it returns
// FORBIDDEN. The db handle is required to enumerate visible users for
// team/department/all scopes.
func resolveScope(ctx context.Context, db *sql.DB, u *model.User, in reportScope) (*resolvedScope, error) {
	if u == nil {
		return nil, errUnauthorized
	}
	scopeType := in.Type
	if scopeType == "" {
		scopeType = "self"
	}
	allowed, ok := allowedScopesByRole[u.Role]
	if !ok || !containsString(allowed, scopeType) {
		return nil, errForbidden
	}
	rs := &resolvedScope{Type: scopeType}

	switch scopeType {
	case "self":
		rs.UserIDs = []string{u.ID}
	case "team":
		// TL: own team (enforced). Admin: optional team_id, else all teams.
		if u.Role == "team_leader" {
			if u.TeamID == nil {
				return nil, errForbidden
			}
			rs.TeamID = *u.TeamID
		} else if u.Role == "admin" {
			if in.TeamID != "" {
				rs.TeamID = in.TeamID
			}
		}
		ids, err := userIDsForTeam(ctx, db, rs.TeamID)
		if err != nil {
			return nil, errMCPInternal
		}
		rs.UserIDs = ids
	case "department":
		// Director: own managed department. Admin: optional department_id.
		if u.Role == "director" {
			if u.DepartmentID == nil || *u.DepartmentID == "" {
				return nil, errForbidden
			}
			rs.DepartmentID = *u.DepartmentID
		} else if u.Role == "admin" {
			if in.DepartmentID != "" {
				rs.DepartmentID = in.DepartmentID
			}
		}
		ids, err := userIDsForDepartment(ctx, db, rs.DepartmentID)
		if err != nil {
			return nil, errMCPInternal
		}
		rs.UserIDs = ids
	case "all":
		// Admin only.
		ids, err := allUserIDs(ctx, db)
		if err != nil {
			return nil, errMCPInternal
		}
		rs.UserIDs = ids
	}

	// user_ids can only narrow the range.
	if len(in.UserIDs) > 0 {
		allowed := stringSet(rs.UserIDs)
		narrowed := make([]string, 0, len(in.UserIDs))
		for _, id := range in.UserIDs {
			if allowed[id] {
				narrowed = append(narrowed, id)
			}
		}
		if len(narrowed) == 0 {
			return nil, errForbidden
		}
		rs.UserIDs = narrowed
	}
	return rs, nil
}

// resolveTarget validates a writeback/read target against the caller's role and
// returns the concrete identifiers that reportStore should use. write=true
// applies the writeback permission matrix (doc §3.5.2); write=false applies the
// read-side target rules (doc §3.3.1).
func resolveTarget(u *model.User, in reportTarget, reportType string, write bool) (reportTarget, error) {
	if u == nil {
		return reportTarget{}, errUnauthorized
	}
	t := in
	if t.Type == "" {
		t.Type = "self"
	}

	switch t.Type {
	case "self":
		// Resolve implicit "self" first, then apply the same role boundary as an explicit target.
		// Otherwise an employee can ask for report_type=team_daily with target=self and bypass
		// the team/department write matrix through resolveSelfTarget.
		target := resolveSelfTarget(u, reportType)
		if err := validateSelfTargetAccess(u, target, reportType); err != nil {
			return reportTarget{}, err
		}
		return target, nil
	case "user":
		if t.UserID == "" {
			return reportTarget{}, errInvalidTarget
		}
		// Write rules: only Admin may write another user's personal reports.
		if write {
			if reportType == "personal_daily" || reportType == "personal_weekly" {
				if u.Role != "admin" && t.UserID != u.ID {
					return reportTarget{}, errForbidden
				}
			}
		}
		// Read rules: Admin any; Director within department; TL within team; employee/PM only self.
		switch u.Role {
		case "admin":
			// ok
		case "director":
			if t.UserID != u.ID {
				// membership must be checked at SQL time against department users; defer.
			}
		case "team_leader":
			if t.UserID != u.ID && u.TeamID == nil {
				return reportTarget{}, errForbidden
			}
		default:
			if t.UserID != u.ID {
				return reportTarget{}, errForbidden
			}
		}
		return t, nil
	case "team":
		if t.TeamID == "" {
			if u.Role == "team_leader" && u.TeamID != nil {
				t.TeamID = *u.TeamID
			} else {
				return reportTarget{}, errInvalidTarget
			}
		}
		if write {
			// team_daily / team_weekly: TL writes own team; Admin any. Director cannot.
			if u.Role == "team_leader" {
				if u.TeamID == nil || t.TeamID != *u.TeamID {
					return reportTarget{}, errForbidden
				}
			} else if u.Role == "admin" {
				// ok
			} else {
				return reportTarget{}, errForbidden
			}
		} else {
			// Read: TL own team; Director within department (defer membership); Admin any.
			if u.Role == "team_leader" {
				if u.TeamID == nil || t.TeamID != *u.TeamID {
					return reportTarget{}, errForbidden
				}
			} else if u.Role == "employee" || u.Role == "pm" {
				return reportTarget{}, errForbidden
			}
		}
		return t, nil
	case "department":
		if t.DepartmentID == "" {
			if u.Role == "director" && u.DepartmentID != nil {
				t.DepartmentID = *u.DepartmentID
			} else {
				return reportTarget{}, errInvalidTarget
			}
		}
		if write {
			// department_daily / department_weekly: Director writes own department; Admin any.
			if u.Role == "director" {
				if u.DepartmentID == nil || t.DepartmentID != *u.DepartmentID {
					return reportTarget{}, errForbidden
				}
			} else if u.Role == "admin" {
				// ok
			} else {
				return reportTarget{}, errForbidden
			}
		} else {
			if u.Role == "employee" || u.Role == "pm" || u.Role == "team_leader" {
				return reportTarget{}, errForbidden
			}
		}
		return t, nil
	}
	return reportTarget{}, errInvalidTarget
}

func validateSelfTargetAccess(u *model.User, target reportTarget, reportType string) error {
	switch reportType {
	case "personal_daily", "personal_weekly":
		if target.UserID == "" {
			return errInvalidTarget
		}
		return nil
	case "team_daily", "team_weekly":
		if target.TeamID == "" {
			return nil
		}
		if u.Role == "team_leader" && u.TeamID != nil && target.TeamID == *u.TeamID {
			return nil
		}
		return errForbidden
	case "department_daily", "department_weekly":
		if target.DepartmentID == "" {
			return errForbidden
		}
		if u.Role == "director" && u.DepartmentID != nil && target.DepartmentID == *u.DepartmentID {
			return nil
		}
		return errForbidden
	}
	return nil
}

func resolveSelfTarget(u *model.User, reportType string) reportTarget {
	switch reportType {
	case "personal_daily", "personal_weekly":
		return reportTarget{Type: "self", UserID: u.ID}
	case "team_daily", "team_weekly":
		teamID := ""
		if u.TeamID != nil {
			teamID = *u.TeamID
		}
		return reportTarget{Type: "team", TeamID: teamID}
	case "department_daily", "department_weekly":
		departmentID := ""
		if u.DepartmentID != nil {
			departmentID = *u.DepartmentID
		}
		return reportTarget{Type: "department", DepartmentID: departmentID}
	}
	return reportTarget{Type: "self"}
}

func userIDsForTeam(ctx context.Context, db *sql.DB, teamID string) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("db unavailable")
	}
	if teamID == "" {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id::text FROM users WHERE team_id = $1`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func userIDsForDepartment(ctx context.Context, db *sql.DB, departmentID string) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("db unavailable")
	}
	rows, err := db.QueryContext(ctx, `
		SELECT u.id::text
		FROM users u
		LEFT JOIN teams t ON t.id = u.team_id
		JOIN departments d ON d.id = COALESCE(u.department_id, t.department_id)
		WHERE d.id = $1`, departmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func allUserIDs(ctx context.Context, db *sql.DB) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("db unavailable")
	}
	rows, err := db.QueryContext(ctx, `SELECT id::text FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func stringSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, item := range items {
		m[item] = true
	}
	return m
}
