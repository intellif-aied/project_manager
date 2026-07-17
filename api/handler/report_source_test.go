package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/internal/reportsource"
	"github.com/aidashboard/api/model"
)

func TestReportSourceCapabilityUsesServiceAvailability(t *testing.T) {
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service, err := reportsource.NewService(database)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewReportSourceHandler(service)

	for _, test := range []struct {
		name   string
		userID string
	}{
		{name: "first user", userID: "42"},
		{name: "second user", userID: "43"},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/report-source-capability", nil)
			req = req.WithContext(context.WithValue(req.Context(), userKey, &model.User{ID: test.userID}))
			response := httptest.NewRecorder()
			handler.Capability(response, req)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var payload map[string]bool
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if !payload["enabled"] {
				t.Fatal("report source selection must be available to every authenticated user")
			}
		})
	}
}

func TestReportSourceCapabilityRequiresAuthentication(t *testing.T) {
	handler := NewReportSourceHandler(nil)
	response := httptest.NewRecorder()
	handler.Capability(response, httptest.NewRequest(http.MethodGet, "/report-source-capability", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestParseCandidateActivityRangeDoesNotTreatReportPeriodAsActivityFilter(t *testing.T) {
	from, to, err := parseCandidateActivityRange(url.Values{
		"period_start": {"2026-07-17"},
		"period_end":   {"2026-07-17"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if from != nil || to != nil {
		t.Fatalf("report period unexpectedly became activity filter: %v..%v", from, to)
	}
}

func TestParseCandidateActivityRangePrefersExplicitActivityRange(t *testing.T) {
	from, to, err := parseCandidateActivityRange(url.Values{
		"period_start":  {"2026-07-17"},
		"period_end":    {"2026-07-17"},
		"activity_from": {"2026-07-15"},
		"activity_to":   {"2026-07-16"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantFrom := time.Date(2026, 7, 15, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	wantTo := time.Date(2026, 7, 16, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60)).Add(24*time.Hour - time.Nanosecond)
	if from == nil || !from.Equal(wantFrom) || to == nil || !to.Equal(wantTo) {
		t.Fatalf("range = %v..%v, want %v..%v", from, to, wantFrom, wantTo)
	}
}

func TestWriteReportSourceErrorReturnsRetryableLargeContextWarning(t *testing.T) {
	response := httptest.NewRecorder()
	writeReportSourceError(response, &reportsource.LargeContextConfirmationError{
		SelectionID:  "selection-large",
		ContextBytes: reportsource.LargeContextWarningBytes + 1,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "confirmation_required" ||
		payload["code"] != "LARGE_REPORT_CONTEXT_CONFIRMATION_REQUIRED" ||
		payload["warning_code"] != reportsource.LargeContextWarningCode ||
		payload["report_source_selection_id"] != "selection-large" ||
		payload["warning_required"] != true {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	message, _ := payload["message"].(string)
	if message == "" || strings.Contains(message, "Digest") || strings.Contains(message, "清洗") {
		t.Fatalf("warning exposed internal details: %q", message)
	}
}
