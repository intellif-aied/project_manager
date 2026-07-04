package handler

import (
	"database/sql"
	"net/http"

	"github.com/aidashboard/api/model"
	"github.com/go-chi/chi/v5"
)

type FollowHandler struct {
	db *sql.DB
}

func NewFollowHandler(db *sql.DB) *FollowHandler {
	return &FollowHandler{db: db}
}

func (h *FollowHandler) List(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	rows, err := h.db.Query(`
		SELECT user_id, target_type, target_id, created_at
		FROM user_follows
		WHERE user_id = $1
		ORDER BY created_at DESC`, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	follows := []model.UserFollow{}
	for rows.Next() {
		var follow model.UserFollow
		if err := rows.Scan(&follow.UserID, &follow.TargetType, &follow.TargetID, &follow.CreatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		follows = append(follows, follow)
	}
	writeJSON(w, http.StatusOK, follows)
}

func (h *FollowHandler) Followers(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	targetType := r.URL.Query().Get("target_type")
	targetID := r.URL.Query().Get("target_id")
	if !isFollowTargetType(targetType) || targetID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target_type and target_id are required"})
		return
	}
	visible, err := h.targetVisible(u, targetType, targetID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !visible {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "target not found or not visible"})
		return
	}

	rows, err := h.db.Query(`
		SELECT u.id, COALESCE(NULLIF(u.nickname,''), u.username), u.app_role, u.team_id, t.name, f.created_at
		FROM user_follows f
		JOIN users u ON u.id = f.user_id
		LEFT JOIN teams t ON t.id = u.team_id
		WHERE f.target_type = $1 AND f.target_id = $2
		ORDER BY f.created_at ASC`, targetType, targetID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	followers := []model.FollowFollower{}
	for rows.Next() {
		var follower model.FollowFollower
		var teamID, teamName sql.NullString
		if err := rows.Scan(&follower.ID, &follower.Name, &follower.Role, &teamID, &teamName, &follower.FollowedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		follower.TeamID = nullStringPtr(teamID)
		follower.TeamName = nullStringPtr(teamName)
		followers = append(followers, follower)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, followers)
}

func (h *FollowHandler) Follow(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	var req model.FollowRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if !isFollowTargetType(req.TargetType) || req.TargetID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target_type and target_id are required"})
		return
	}
	visible, err := h.targetVisible(u, req.TargetType, req.TargetID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !visible {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "target not found or not visible"})
		return
	}
	if _, err := h.db.Exec(`
		INSERT INTO user_follows (user_id, target_type, target_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, u.ID, req.TargetType, req.TargetID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"favorited":   true,
		"target_type": req.TargetType,
		"target_id":   req.TargetID,
	})
}

func (h *FollowHandler) Unfollow(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	targetType := chi.URLParam(r, "target_type")
	targetID := chi.URLParam(r, "target_id")
	if !isFollowTargetType(targetType) || targetID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid follow target"})
		return
	}
	if _, err := h.db.Exec(`
		DELETE FROM user_follows
		WHERE user_id = $1 AND target_type = $2 AND target_id = $3`, u.ID, targetType, targetID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"favorited":   false,
		"target_type": targetType,
		"target_id":   targetID,
	})
}

func (h *FollowHandler) targetVisible(u *model.User, targetType, targetID string) (bool, error) {
	if targetType == "requirement" {
		return (&RequirementHandler{db: h.db}).canViewRequirement(u, targetID)
	}
	return (&TaskHandler{db: h.db}).canViewTask(u, targetID)
}

func isFollowTargetType(targetType string) bool {
	return targetType == "requirement" || targetType == "task"
}
