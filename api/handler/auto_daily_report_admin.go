package handler

import (
	"context"
	"net/http"

	"github.com/aidashboard/api/internal/autodailyreport"
)

type AutoDailyReportAdminHandler struct {
	service autoDailyReportConfigService
}

type autoDailyReportConfigService interface {
	GetConfig(context.Context) (autodailyreport.Config, error)
	SetEnabled(context.Context, bool, string) (autodailyreport.Config, error)
}

func NewAutoDailyReportAdminHandler(service autoDailyReportConfigService) *AutoDailyReportAdminHandler {
	return &AutoDailyReportAdminHandler{service: service}
}

func (h *AutoDailyReportAdminHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "auto daily report service is unavailable"})
		return
	}
	config, err := h.service.GetConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (h *AutoDailyReportAdminHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "auto daily report service is unavailable"})
		return
	}
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if err := readJSON(r, &request); err != nil || request.Enabled == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "enabled is required"})
		return
	}
	user := getUser(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	config, err := h.service.SetEnabled(r.Context(), *request.Enabled, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, config)
}
