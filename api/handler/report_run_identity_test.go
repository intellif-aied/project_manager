package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aidashboard/api/internal/reportbrief"
	"github.com/aidashboard/api/model"
)

type recordingReportBriefService struct {
	runID        string
	draft        reportbrief.Draft
	rejectDetail string
	rejectErr    error
}

func (s *recordingReportBriefService) RejectInvalid(_ context.Context, _, runID, details string) (reportbrief.Stored, error) {
	s.runID = runID
	s.rejectDetail = details
	if s.rejectErr != nil {
		return reportbrief.Stored{}, s.rejectErr
	}
	return reportbrief.Stored{}, reportbrief.ErrInvalid
}

func (s *recordingReportBriefService) Accept(_ context.Context, _, runID string, draft reportbrief.Draft) (reportbrief.Stored, error) {
	s.runID = runID
	s.draft = draft
	return reportbrief.Stored{}, reportbrief.ErrInvalid
}

func (*recordingReportBriefService) ValidateForWrite(context.Context, string, string, string, string, string) (reportbrief.Stored, error) {
	return reportbrief.Stored{}, nil
}

func TestReportRunTokenBindsUserAndRun(t *testing.T) {
	const (
		secret = "report-run-test-secret"
		runID  = "958458d9-8e65-489f-8bfc-0de80ff46752"
	)
	token, err := MintReportRunToken(&model.User{ID: "21", Username: "user-21"}, secret, runID)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := extractAIHubIdentityWithPolicy(token, secret, false)
	if err != nil {
		t.Fatal(err)
	}
	if identity.UID != 21 || identity.ReportRunID != runID {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestResolveReportRunIDUsesBoundRunWhenToolArgumentsAreEmpty(t *testing.T) {
	const runID = "958458d9-8e65-489f-8bfc-0de80ff46752"
	request := httptest.NewRequest("POST", "/api/v1/mcp/reports", nil)
	request = request.WithContext(context.WithValue(request.Context(), reportRunIDKey, runID))
	got, err := resolveReportRunID(request, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != runID {
		t.Fatalf("run_id = %q, want %q", got, runID)
	}
}

func TestResolveReportRunIDRejectsArgumentMismatch(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/v1/mcp/reports", nil)
	request = request.WithContext(context.WithValue(request.Context(), reportRunIDKey, "bound-run"))
	if _, err := resolveReportRunID(request, "different-run"); err == nil {
		t.Fatal("expected bound run mismatch")
	}
}

func TestEmptyBriefArgumentsReachBoundRunValidation(t *testing.T) {
	const runID = "958458d9-8e65-489f-8bfc-0de80ff46752"
	service := &recordingReportBriefService{}
	handler := &ReportMCPHandler{reportBrief: service, briefEnabled: true}
	request := httptest.NewRequest("POST", "/api/v1/mcp/reports", nil)
	ctx := context.WithValue(request.Context(), userKey, &model.User{ID: "21"})
	ctx = context.WithValue(ctx, reportRunIDKey, runID)
	request = request.WithContext(ctx)

	if _, err := handler.toolWriteReportBrief(request, json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected empty Brief to be rejected by Report Brief validation")
	}
	if service.runID != runID {
		t.Fatalf("validated run_id = %q, want %q", service.runID, runID)
	}
	if len(service.draft.Workstreams) != 0 || service.draft.NoReportableWork {
		t.Fatalf("unexpected empty draft normalization: %#v", service.draft)
	}
}

func TestMalformedBriefJSONCountsAgainstBoundRun(t *testing.T) {
	const runID = "958458d9-8e65-489f-8bfc-0de80ff46752"
	service := &recordingReportBriefService{}
	handler := &ReportMCPHandler{reportBrief: service, briefEnabled: true}
	request := httptest.NewRequest("POST", "/api/v1/mcp/reports", nil)
	ctx := context.WithValue(request.Context(), userKey, &model.User{ID: "21"})
	ctx = context.WithValue(ctx, reportRunIDKey, runID)
	request = request.WithContext(ctx)

	_, err := handler.toolWriteReportBrief(request, json.RawMessage(`{"brief_json":"{"}`))
	if err == nil {
		t.Fatal("expected malformed brief_json to be rejected")
	}
	if service.runID != runID || !strings.Contains(service.rejectDetail, "valid Report Brief JSON") {
		t.Fatalf("rejection run=%q detail=%q", service.runID, service.rejectDetail)
	}
}

func TestWriteServeErrorMakesReportRetryCodeVisible(t *testing.T) {
	handler := &ReportMCPHandler{}
	recorder := httptest.NewRecorder()
	handler.writeServeError(recorder, json.RawMessage(`1`),
		mcpErr("REPORT_BRIEF_RETRY_EXHAUSTED", "correct the source Brief"))

	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"message":"REPORT_BRIEF_RETRY_EXHAUSTED: correct the source Brief"`)) {
		t.Fatalf("response does not expose stable error code: %s", recorder.Body.String())
	}
}
