package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aidashboard/api/internal/tokenanalytics"
	"github.com/aidashboard/api/model"
)

func TestTokenAnalyticsCapabilityReflectsServiceAvailabilityAndRole(t *testing.T) {
	tests := []struct {
		name             string
		user             *model.User
		available        bool
		wantStatus       int
		wantEnabled      bool
		wantManage       bool
		wantManagePrices bool
	}{
		{name: "unauthorized", wantStatus: http.StatusUnauthorized},
		{name: "unavailable", user: &model.User{ID: "10", Role: "admin"}, wantStatus: http.StatusOK},
		{name: "employee", user: &model.User{ID: "11", Role: "employee"}, available: true, wantStatus: http.StatusOK, wantEnabled: true},
		{name: "leader", user: &model.User{ID: "12", Role: "team_leader"}, available: true, wantStatus: http.StatusOK, wantEnabled: true, wantManage: true},
		{name: "admin", user: &model.User{ID: "13", Role: "admin"}, available: true, wantStatus: http.StatusOK, wantEnabled: true, wantManage: true, wantManagePrices: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var service *tokenanalytics.Service
			if test.available {
				service = tokenanalytics.NewService(nil)
			}
			handler := NewTokenAnalyticsHandler(service)
			req := httptest.NewRequest(http.MethodGet, "/token-analytics/capability", nil)
			if test.user != nil {
				req = requestWithUser(req, test.user)
			}
			recorder := httptest.NewRecorder()
			handler.Capability(recorder, req)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if recorder.Code != http.StatusOK {
				return
			}
			var response struct {
				Enabled          bool `json:"enabled"`
				CanManage        bool `json:"can_manage"`
				CanManagePricing bool `json:"can_manage_pricing"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Enabled != test.wantEnabled || response.CanManage != test.wantManage ||
				response.CanManagePricing != test.wantManagePrices {
				t.Fatalf("capability=%+v", response)
			}
		})
	}
}

func TestTokenAnalyticsUnavailableEndpointReturnsServiceUnavailable(t *testing.T) {
	handler := NewTokenAnalyticsHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/token-analytics/summary?scope=mine&from=2026-07-01&to=2026-07-07", nil)
	req = requestWithUser(req, &model.User{ID: "21", Role: "employee"})
	recorder := httptest.NewRecorder()

	handler.Summary(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestTokenAnalyticsSnapshotErrorsHaveStableCodes(t *testing.T) {
	tests := []struct {
		err      error
		status   int
		wantCode string
	}{
		{err: tokenanalytics.ErrSnapshotMismatch, status: http.StatusBadRequest, wantCode: "QUERY_SNAPSHOT_MISMATCH"},
		{err: tokenanalytics.ErrSnapshotExpired, status: http.StatusGone, wantCode: "QUERY_SNAPSHOT_EXPIRED"},
		{err: tokenanalytics.ErrForbidden, status: http.StatusForbidden},
		{err: tokenanalytics.ErrInvalidFilter, status: http.StatusBadRequest},
		{err: errors.New("database unavailable"), status: http.StatusInternalServerError},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		writeTokenAnalyticsError(recorder, test.err)
		if recorder.Code != test.status {
			t.Fatalf("error=%v status=%d body=%s", test.err, recorder.Code, recorder.Body.String())
		}
		if test.wantCode == "" {
			continue
		}
		var response struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Code != test.wantCode {
			t.Fatalf("error=%v code=%q", test.err, response.Code)
		}
	}
}

func TestPricingManagementRequiresAdmin(t *testing.T) {
	tests := []struct {
		name   string
		user   *model.User
		status int
	}{
		{name: "unauthorized", status: http.StatusUnauthorized},
		{name: "employee", user: &model.User{ID: "31", Role: "employee"}, status: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewPricingAdminHandler(nil, nil)
			req := httptest.NewRequest(http.MethodGet, "/admin/price-books", nil)
			if test.user != nil {
				req = requestWithUser(req, test.user)
			}
			recorder := httptest.NewRecorder()
			handler.ListPriceBooks(recorder, req)
			if recorder.Code != test.status {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
