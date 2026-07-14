package handler

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/aidashboard/api/model"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

type DepartmentHandler struct{ db *sql.DB }

func NewDepartmentHandler(db *sql.DB) *DepartmentHandler { return &DepartmentHandler{db: db} }

func (h *DepartmentHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(`
		SELECT d.id::text, d.name, d.director_user_id::text,
			COALESCE(NULLIF(u.nickname,''), u.username), d.created_at, d.updated_at,
			COALESCE(array_agg(DISTINCT t.id::text) FILTER (WHERE t.id IS NOT NULL), '{}'),
			COALESCE(array_agg(DISTINCT pm.id::text) FILTER (WHERE pm.id IS NOT NULL), '{}')
		FROM departments d
		LEFT JOIN users u ON u.id = d.director_user_id
		LEFT JOIN teams t ON t.department_id = d.id
		LEFT JOIN users pm ON pm.department_id = d.id AND pm.app_role = 'pm'
		GROUP BY d.id, u.nickname, u.username ORDER BY d.name`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []model.Department{}
	for rows.Next() {
		var item model.Department
		var directorID, directorName sql.NullString
		if err := rows.Scan(&item.ID, &item.Name, &directorID, &directorName, &item.CreatedAt, &item.UpdatedAt, pq.Array(&item.TeamIDs), pq.Array(&item.PMUserIDs)); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		item.DirectorUserID = nullStringPtr(directorID)
		item.DirectorName = nullStringPtr(directorName)
		items = append(items, item)
	}
	writeJSON(w, 200, items)
}

func (h *DepartmentHandler) Create(w http.ResponseWriter, r *http.Request) {
	h.save(w, r, "")
}

func (h *DepartmentHandler) Update(w http.ResponseWriter, r *http.Request) {
	h.save(w, r, chi.URLParam(r, "id"))
}

func (h *DepartmentHandler) save(w http.ResponseWriter, r *http.Request, id string) {
	var req model.AdminSaveDepartmentRequest
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeJSON(w, 400, map[string]string{"error": "name is required"})
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()
	if id == "" {
		err = tx.QueryRow(`INSERT INTO departments(name, director_user_id) VALUES($1, NULLIF($2,'')::bigint) RETURNING id::text`, strings.TrimSpace(req.Name), departmentStringValue(req.DirectorUserID)).Scan(&id)
	} else {
		_, err = tx.Exec(`UPDATE departments SET name=$1, director_user_id=NULLIF($2,'')::bigint, updated_at=now() WHERE id=$3`, strings.TrimSpace(req.Name), departmentStringValue(req.DirectorUserID), id)
	}
	if err == nil {
		_, err = tx.Exec(`UPDATE teams SET department_id=NULL, director_user_id=NULL WHERE department_id=$1`, id)
	}
	if err == nil {
		_, err = tx.Exec(`UPDATE users SET department_id=NULL WHERE department_id=$1 AND app_role IN ('pm','director')`, id)
	}
	if err == nil && len(req.TeamIDs) > 0 {
		_, err = tx.Exec(`UPDATE teams SET department_id=$1, director_user_id=NULLIF($2,'')::bigint WHERE id::text=ANY($3)`, id, departmentStringValue(req.DirectorUserID), pq.Array(req.TeamIDs))
	}
	if err == nil && len(req.PMUserIDs) > 0 {
		_, err = tx.Exec(`UPDATE users SET department_id=$1 WHERE app_role='pm' AND id::text=ANY($2)`, id, pq.Array(req.PMUserIDs))
	}
	if err == nil && req.DirectorUserID != nil {
		_, err = tx.Exec(`UPDATE users SET department_id=$1 WHERE id::text=$2 AND app_role='director'`, id, *req.DirectorUserID)
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err = tx.Commit(); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"id": id})
}

func departmentStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
