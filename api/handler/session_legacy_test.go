package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLegacySessionBatchUploadDisabled(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/batch", nil)
	response := httptest.NewRecorder()

	LegacySessionBatchUploadDisabled(response, request)

	if response.Code != http.StatusUpgradeRequired {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusUpgradeRequired)
	}
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "CLI_UPGRADE_REQUIRED" {
		t.Fatalf("code=%q", body["code"])
	}
}
