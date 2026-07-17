package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
