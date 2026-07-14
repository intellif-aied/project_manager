package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/aidashboard/api/internal/tokenanalytics"
)

type TokenAnalyticsHandler struct {
	service *tokenanalytics.Service
}

func NewTokenAnalyticsHandler(service *tokenanalytics.Service) *TokenAnalyticsHandler {
	return &TokenAnalyticsHandler{service: service}
}

func (h *TokenAnalyticsHandler) Capability(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	enabled := h.service != nil
	canManage := enabled && (user.Role == "team_leader" || user.Role == "director" || user.Role == "admin")
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":            enabled,
		"can_manage":         canManage,
		"can_manage_pricing": enabled && user.Role == "admin",
	})
}

func (h *TokenAnalyticsHandler) Summary(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	result, err := h.service.CreateSummary(r.Context(), actor, tokenAnalyticsFilters(r))
	if err != nil {
		writeTokenAnalyticsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *TokenAnalyticsHandler) Trends(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	result, err := h.service.Trends(r.Context(), actor, tokenAnalyticsFilters(r), r.URL.Query().Get("query_snapshot_token"))
	if err != nil {
		writeTokenAnalyticsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *TokenAnalyticsHandler) Rankings(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	result, err := h.service.Rankings(r.Context(), actor, tokenAnalyticsFilters(r),
		r.URL.Query().Get("query_snapshot_token"), r.URL.Query().Get("group_by"))
	if err != nil {
		writeTokenAnalyticsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *TokenAnalyticsHandler) Sessions(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	page, pageSize := parsePagination(r, 20, 100)
	result, err := h.service.Sessions(r.Context(), actor, tokenAnalyticsFilters(r),
		r.URL.Query().Get("query_snapshot_token"), page, pageSize)
	if err != nil {
		writeTokenAnalyticsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *TokenAnalyticsHandler) actor(w http.ResponseWriter, r *http.Request) (tokenanalytics.Actor, bool) {
	user := getUser(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return tokenanalytics.Actor{}, false
	}
	if h.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "token analytics is unavailable"})
		return tokenanalytics.Actor{}, false
	}
	userID, err := strconv.ParseInt(user.ID, 10, 64)
	if err != nil || userID <= 0 {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid local user identity"})
		return tokenanalytics.Actor{}, false
	}
	return tokenanalytics.Actor{ID: userID, Role: user.Role, TeamID: user.TeamID}, true
}

func tokenAnalyticsFilters(r *http.Request) tokenanalytics.Filters {
	query := r.URL.Query()
	return tokenanalytics.Filters{
		Scope:        query.Get("scope"),
		From:         query.Get("from"),
		To:           query.Get("to"),
		TeamID:       query.Get("team_id"),
		DepartmentID: query.Get("department_id"),
		UserID:       query.Get("user_id"),
		Model:        query.Get("model"),
		Query:        query.Get("q"),
	}
}

func writeTokenAnalyticsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tokenanalytics.ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient permissions"})
	case errors.Is(err, tokenanalytics.ErrInvalidFilter):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid token analytics filters"})
	case errors.Is(err, tokenanalytics.ErrSnapshotMismatch):
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"code":  "QUERY_SNAPSHOT_MISMATCH",
			"error": "query snapshot does not match filters",
		})
	case errors.Is(err, tokenanalytics.ErrSnapshotExpired):
		writeJSON(w, http.StatusGone, map[string]string{
			"code":  "QUERY_SNAPSHOT_EXPIRED",
			"error": "query snapshot expired; restart from summary",
		})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}
