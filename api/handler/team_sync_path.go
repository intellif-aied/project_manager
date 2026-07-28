package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/aidashboard/api/model"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

type TeamSyncPathHandler struct {
	db *sql.DB
}

type teamSyncPath struct {
	ID             string    `json:"id"`
	NormalizedPath string    `json:"normalized_path"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func NewTeamSyncPathHandler(database *sql.DB) *TeamSyncPathHandler {
	return &TeamSyncPathHandler{db: database}
}

func (h *TeamSyncPathHandler) List(w http.ResponseWriter, r *http.Request) {
	u, ok := teamSyncPathUser(w, r)
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, normalized_path, created_at, updated_at
		FROM team_sync_paths
		WHERE team_id = $1 AND user_id = $2
		ORDER BY normalized_path`, *u.TeamID, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "TEAM_SYNC_PATH_LIST_FAILED", "error": err.Error()})
		return
	}
	defer rows.Close()
	items := []teamSyncPath{}
	for rows.Next() {
		var item teamSyncPath
		if err := rows.Scan(&item.ID, &item.NormalizedPath, &item.CreatedAt, &item.UpdatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "TEAM_SYNC_PATH_LIST_FAILED", "error": err.Error()})
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "TEAM_SYNC_PATH_LIST_FAILED", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *TeamSyncPathHandler) Create(w http.ResponseWriter, r *http.Request) {
	u, ok := teamSyncPathUser(w, r)
	if !ok {
		return
	}
	path, ok := decodeTeamSyncPath(w, r)
	if !ok {
		return
	}
	item, err := h.save(r.Context(), u, "", path)
	if err != nil {
		writeTeamSyncPathError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *TeamSyncPathHandler) Update(w http.ResponseWriter, r *http.Request) {
	u, ok := teamSyncPathUser(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if !isValidUUID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_TEAM_DIRECTORY", "error": "invalid path id"})
		return
	}
	path, ok := decodeTeamSyncPath(w, r)
	if !ok {
		return
	}
	item, err := h.save(r.Context(), u, id, path)
	if err != nil {
		writeTeamSyncPathError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *TeamSyncPathHandler) Delete(w http.ResponseWriter, r *http.Request) {
	u, ok := teamSyncPathUser(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if !isValidUUID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_TEAM_DIRECTORY", "error": "invalid path id"})
		return
	}
	result, err := h.db.ExecContext(r.Context(), `
		DELETE FROM team_sync_paths
		WHERE id = $1 AND team_id = $2 AND user_id = $3`, id, *u.TeamID, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "TEAM_SYNC_PATH_DELETE_FAILED", "error": err.Error()})
		return
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "TEAM_SYNC_PATH_NOT_FOUND", "error": "team sync path not found"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var (
	errTeamSyncPathConflict = errors.New("team sync path conflicts with another member")
	errTeamSyncPathNotFound = errors.New("team sync path not found")
)

func (h *TeamSyncPathHandler) save(ctx context.Context, user *model.User, id, normalizedPath string) (teamSyncPath, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return teamSyncPath{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "team-sync-path:"+*user.TeamID); err != nil {
		return teamSyncPath{}, err
	}
	var conflict bool
	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM team_sync_paths
			WHERE team_id = $1 AND user_id <> $2
				AND ($3 = normalized_path OR $3 LIKE normalized_path || '/%' OR normalized_path LIKE $3 || '/%')
				AND (NULLIF($4, '') IS NULL OR id <> NULLIF($4, '')::uuid)
		)`, *user.TeamID, user.ID, normalizedPath, id).Scan(&conflict)
	if err != nil {
		return teamSyncPath{}, err
	}
	if conflict {
		return teamSyncPath{}, errTeamSyncPathConflict
	}
	var item teamSyncPath
	if id == "" {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO team_sync_paths (team_id, user_id, normalized_path)
			VALUES ($1, $2, $3)
			RETURNING id, normalized_path, created_at, updated_at`,
			*user.TeamID, user.ID, normalizedPath,
		).Scan(&item.ID, &item.NormalizedPath, &item.CreatedAt, &item.UpdatedAt)
	} else {
		err = tx.QueryRowContext(ctx, `
			UPDATE team_sync_paths
			SET normalized_path = $1, updated_at = now()
			WHERE id = $2 AND team_id = $3 AND user_id = $4
			RETURNING id, normalized_path, created_at, updated_at`,
			normalizedPath, id, *user.TeamID, user.ID,
		).Scan(&item.ID, &item.NormalizedPath, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return teamSyncPath{}, errTeamSyncPathNotFound
		}
	}
	if err != nil {
		var databaseError *pq.Error
		if errors.As(err, &databaseError) && databaseError.Constraint == "team_sync_paths_team_id_normalized_path_key" {
			return teamSyncPath{}, errTeamSyncPathConflict
		}
		return teamSyncPath{}, err
	}
	if err := tx.Commit(); err != nil {
		return teamSyncPath{}, err
	}
	return item, nil
}

func teamSyncPathUser(w http.ResponseWriter, r *http.Request) (*model.User, bool) {
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return nil, false
	}
	if u.TeamID == nil || strings.TrimSpace(*u.TeamID) == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"code": "TEAM_REQUIRED", "error": "current user does not belong to a team"})
		return nil, false
	}
	return u, true
}

func decodeTeamSyncPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	var request struct {
		Path string `json:"path"`
	}
	if err := decodeLimitedJSON(w, r, &request); err != nil {
		return "", false
	}
	value := strings.TrimSpace(request.Path)
	if value == "" || strings.ContainsRune(value, '\x00') || !filepath.IsAbs(value) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_TEAM_DIRECTORY", "error": "path must be an absolute directory"})
		return "", false
	}
	value = filepath.Clean(value)
	if value == "/" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_TEAM_DIRECTORY", "error": "filesystem root cannot be configured"})
		return "", false
	}
	return value, true
}

func writeTeamSyncPathError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errTeamSyncPathConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"code": "TEAM_DIRECTORY_CONFLICT", "error": err.Error()})
	case errors.Is(err, errTeamSyncPathNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "TEAM_SYNC_PATH_NOT_FOUND", "error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "TEAM_SYNC_PATH_SAVE_FAILED", "error": err.Error()})
	}
}
