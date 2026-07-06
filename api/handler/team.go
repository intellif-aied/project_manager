package handler

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/model"
)

type TeamHandler struct {
	db *sql.DB
}

func NewTeamHandler(db *sql.DB) *TeamHandler {
	return &TeamHandler{db: db}
}

// Activity returns per-team active counts and idle warnings.
//   - active: had >=1 session on the given date
//   - idle_days: days since last session (NULL -> never)
//   - idle_warnings: idle_days >= 3
func (h *TeamHandler) Activity(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	date := r.URL.Query().Get("date")
	if date == "" {
		date = biztime.Today()
	}

	// Scope teams by role
	teamFilter := ""
	teamArgs := []any{}
	if u.Role == "team_leader" || u.Role == "pm" {
		if u.TeamID == nil {
			writeJSON(w, http.StatusOK, model.TeamActivity{Teams: []model.TeamStat{}, IdleWarnings: []model.IdleWarning{}})
			return
		}
		teamFilter = "WHERE t.id = $1"
		teamArgs = append(teamArgs, *u.TeamID)
	}

	rows, err := h.db.Query(`
		SELECT t.id, t.name FROM teams t
		`+teamFilter+`
		ORDER BY t.name`, teamArgs...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type teamRow struct{ id, name string }
	var teams []teamRow
	for rows.Next() {
		var tr teamRow
		rows.Scan(&tr.id, &tr.name)
		teams = append(teams, tr)
	}
	rows.Close()

	stats := []model.TeamStat{}
	idleWarnings := []model.IdleWarning{}

	for _, t := range teams {
		stat := model.TeamStat{TeamID: t.id, TeamName: t.name}

		memberRows, err := h.db.Query(`
			SELECT u.id, COALESCE(NULLIF(u.nickname,''), u.username),
				EXISTS(SELECT 1 FROM sessions s WHERE s.user_id = u.id AND DATE(s.started_at) = $1) AS active_today,
				(SELECT MAX(DATE(s2.started_at)) FROM sessions s2 WHERE s2.user_id = u.id) AS last_active
			FROM users u
			WHERE u.team_id = $2 AND u.app_role = 'employee'
			ORDER BY COALESCE(NULLIF(u.nickname,''), u.username)`, date, t.id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		for memberRows.Next() {
			var ms model.MemberStat
			var lastActive sql.NullTime
			if err := memberRows.Scan(&ms.UserID, &ms.UserName, &ms.Active, &lastActive); err != nil {
				memberRows.Close()
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			if lastActive.Valid {
				la := lastActive.Time.Format("2006-01-02")
				ms.LastActive = &la
				delta := time.Now().Sub(lastActive.Time).Hours() / 24
				if delta < 0 {
					delta = 0
				}
				ms.IdleDays = int(delta)
			} else {
				ms.IdleDays = 999
			}

			stat.Total++
			if ms.Active {
				stat.Active++
			}
			if ms.IdleDays >= 3 && ms.IdleDays < 999 {
				idleWarnings = append(idleWarnings, model.IdleWarning{
					UserID:   ms.UserID,
					UserName: ms.UserName,
					TeamName: t.name,
					IdleDays: ms.IdleDays,
				})
			} else if ms.IdleDays == 999 {
				idleWarnings = append(idleWarnings, model.IdleWarning{
					UserID:   ms.UserID,
					UserName: ms.UserName,
					TeamName: t.name,
					IdleDays: 999,
				})
			}

			stat.Members = append(stat.Members, ms)
		}
		memberRows.Close()

		stats = append(stats, stat)
	}

	writeJSON(w, http.StatusOK, model.TeamActivity{Teams: stats, IdleWarnings: idleWarnings})
}
