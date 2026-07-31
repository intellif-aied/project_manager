package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aidashboard/api/internal/autodailyreport"
	"github.com/aidashboard/api/model"
)

type fakeAutoDailyReportConfigService struct {
	config     autodailyreport.Config
	err        error
	enabled    *bool
	operatorID string
}

func (f *fakeAutoDailyReportConfigService) GetConfig(context.Context) (autodailyreport.Config, error) {
	return f.config, f.err
}

func (f *fakeAutoDailyReportConfigService) SetEnabled(_ context.Context, enabled bool, operatorID string) (autodailyreport.Config, error) {
	f.enabled = &enabled
	f.operatorID = operatorID
	return f.config, f.err
}

func TestAutoDailyReportAdminGetConfig(t *testing.T) {
	updatedAt := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	service := &fakeAutoDailyReportConfigService{config: autodailyreport.Config{
		Enabled: false, UpdatedAt: updatedAt, QuietPeriodSeconds: 120,
	}}
	handler := NewAutoDailyReportAdminHandler(service)
	recorder := httptest.NewRecorder()
	handler.GetConfig(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"quiet_period_seconds":120`)) {
		t.Fatalf("unexpected response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAutoDailyReportAdminUpdateRecordsOperator(t *testing.T) {
	service := &fakeAutoDailyReportConfigService{config: autodailyreport.Config{Enabled: true, QuietPeriodSeconds: 120}}
	handler := NewAutoDailyReportAdminHandler(service)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(`{"enabled":true}`))
	request = request.WithContext(context.WithValue(request.Context(), userKey, &model.User{ID: "42", Role: "admin"}))
	handler.UpdateConfig(recorder, request)
	if recorder.Code != http.StatusOK || service.enabled == nil || !*service.enabled || service.operatorID != "42" {
		t.Fatalf("update was not applied: status=%d enabled=%v operator=%q", recorder.Code, service.enabled, service.operatorID)
	}
}

func TestAutoDailyReportAdminUpdateRejectsMissingEnabled(t *testing.T) {
	handler := NewAutoDailyReportAdminHandler(&fakeAutoDailyReportConfigService{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString(`{}`))
	request = request.WithContext(context.WithValue(request.Context(), userKey, &model.User{ID: "42", Role: "admin"}))
	handler.UpdateConfig(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", recorder.Code)
	}
}

func TestAutoDailyReportAdminReportsStorageFailure(t *testing.T) {
	handler := NewAutoDailyReportAdminHandler(&fakeAutoDailyReportConfigService{err: errors.New("database unavailable")})
	recorder := httptest.NewRecorder()
	handler.GetConfig(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", recorder.Code)
	}
}
