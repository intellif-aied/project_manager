package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/reportsource"
)

type ReportSourceHandler struct {
	service *reportsource.Service
}

func NewReportSourceHandler(service *reportsource.Service) *ReportSourceHandler {
	return &ReportSourceHandler{service: service}
}

func (h *ReportSourceHandler) Capability(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{
		"enabled": h.service != nil,
	})
}

func (h *ReportSourceHandler) ListCandidates(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if h.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "REPORT_SOURCE_UNAVAILABLE", "error": "report source service is unavailable"})
		return
	}
	reportType := strings.TrimSpace(r.URL.Query().Get("report_type"))
	if reportType != reportTypePersonalDaily && reportType != reportTypePersonalWeekly {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REPORT_SOURCE", "error": "personal report_type is required"})
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	activityFrom, activityTo, err := parseCandidateActivityRange(r.URL.Query())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REPORT_SOURCE", "error": err.Error()})
		return
	}
	result, err := h.service.ListCandidates(r.Context(), u.ID, reportsource.CandidateQuery{
		Query: r.URL.Query().Get("q"), ActivityFrom: activityFrom, ActivityTo: activityTo,
		Page: page, PageSize: pageSize,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "REPORT_SOURCE_QUERY_FAILED", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func parseCandidateActivityRange(values url.Values) (*time.Time, *time.Time, error) {
	fromValue := strings.TrimSpace(values.Get("activity_from"))
	toValue := strings.TrimSpace(values.Get("activity_to"))
	activityFrom, err := parseOptionalActivityTime(fromValue, false)
	if err != nil {
		return nil, nil, err
	}
	activityTo, err := parseOptionalActivityTime(toValue, true)
	if err != nil {
		return nil, nil, err
	}
	return activityFrom, activityTo, nil
}

func (h *ReportSourceHandler) CreateSelection(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if h.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "REPORT_SOURCE_UNAVAILABLE", "error": "report source service is unavailable"})
		return
	}
	var request struct {
		ReportType string `json:"report_type"`
		Period     struct {
			Date      string `json:"date,omitempty"`
			WeekStart string `json:"week_start,omitempty"`
			WeekEnd   string `json:"week_end,omitempty"`
		} `json:"period"`
		SelectedSliceKeys []string `json:"selected_slice_keys"`
	}
	if err := readJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REPORT_SOURCE", "error": "invalid request"})
		return
	}
	period, err := reportsource.ReportPeriod(strings.TrimSpace(request.ReportType), request.Period.Date, request.Period.WeekStart, request.Period.WeekEnd)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REPORT_SOURCE", "error": err.Error()})
		return
	}
	inputs := make([]reportsource.SourceInput, 0, len(request.SelectedSliceKeys))
	for _, sliceKey := range request.SelectedSliceKeys {
		if !isValidUUID(strings.TrimSpace(sliceKey)) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REPORT_SOURCE", "error": "invalid slice key"})
			return
		}
		inputs = append(inputs, reportsource.SourceInput{SliceKey: sliceKey})
	}
	selection, err := h.service.CreateExplicit(r.Context(), u.ID, request.ReportType, period, inputs)
	if err != nil {
		writeReportSourceError(w, err)
		return
	}
	selection, err = h.service.PrepareExplicitSelection(
		r.Context(), u.ID, request.ReportType, period, selection.ID,
	)
	if err != nil {
		writeReportSourceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, selection)
}

func parseOptionalActivityTime(value string, endOfDay bool) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return &parsed, nil
	}
	day, err := time.ParseInLocation("2006-01-02", value, time.FixedZone("Asia/Shanghai", 8*60*60))
	if err != nil {
		return nil, errors.New("activity time must be RFC3339 or YYYY-MM-DD")
	}
	if endOfDay {
		day = day.Add(24*time.Hour - time.Nanosecond)
	}
	return &day, nil
}

func writeReportSourceError(w http.ResponseWriter, err error) {
	var largeContextErr *reportsource.LargeContextConfirmationError
	switch {
	case errors.Is(err, reportsource.ErrInvalidRequest):
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REPORT_SOURCE", "error": err.Error()})
	case errors.Is(err, reportsource.ErrSelectionNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "REPORT_SOURCE_SELECTION_NOT_FOUND", "error": "selection not found"})
	case errors.Is(err, reportsource.ErrSourceUnavailable), errors.Is(err, reportsource.ErrSelectionConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"code": "REPORT_SOURCE_UNAVAILABLE", "error": err.Error()})
	case errors.Is(err, reportsource.ErrDigestNotReady):
		writeJSON(w, http.StatusConflict, map[string]string{"code": "REPORT_SOURCE_DIGEST_NOT_READY", "error": "report source digest is not ready; retry later"})
	case errors.Is(err, reportsource.ErrDigestFailed):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "REPORT_SOURCE_DIGEST_FAILED", "error": "report source digest build failed"})
	case errors.Is(err, reportsource.ErrDigestLimitExceeded):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "REPORT_SOURCE_DIGEST_LIMIT_EXCEEDED", "error": "report source digest exceeds the hard limit"})
	case errors.Is(err, reportsource.ErrDigestVersionMismatch):
		writeJSON(w, http.StatusConflict, map[string]string{"code": "REPORT_SOURCE_DIGEST_VERSION_MISMATCH", "error": "report source digest version does not match"})
	case errors.Is(err, reportsource.ErrReadModeMismatch):
		writeJSON(w, http.StatusConflict, map[string]string{"code": "REPORT_SOURCE_READ_MODE_MISMATCH", "error": "report source read mode does not match"})
	case errors.Is(err, reportsource.ErrDigestCorrupt):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "REPORT_SOURCE_DIGEST_FAILED", "error": "report source digest integrity check failed"})
	case errors.As(err, &largeContextErr):
		writeJSON(w, http.StatusOK, map[string]any{
			"status":                     "confirmation_required",
			"code":                       "LARGE_REPORT_CONTEXT_CONFIRMATION_REQUIRED",
			"message":                    "所选会话内容较多，可能消耗较多 Token，部分模型可能无法完整处理。你可以更换模型、减少所选会话，或继续生成。",
			"report_source_selection_id": largeContextErr.SelectionID,
			"context_bytes":              largeContextErr.ContextBytes,
			"warning_required":           true,
			"warning_code":               reportsource.LargeContextWarningCode,
		})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "REPORT_SOURCE_FAILED", "error": err.Error()})
	}
}
