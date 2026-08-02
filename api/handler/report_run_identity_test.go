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
	runID          string
	draft          reportbrief.Draft
	acceptCalls    int
	rejectDetail   string
	rejectCalls    int
	rejectErr      error
	degradedReason string
	degradedErr    error
}

func (s *recordingReportBriefService) RejectInvalid(_ context.Context, _, runID, details string) (reportbrief.Stored, error) {
	s.rejectCalls++
	s.runID = runID
	s.rejectDetail = details
	if s.rejectErr != nil {
		return reportbrief.Stored{}, s.rejectErr
	}
	return reportbrief.Stored{}, reportbrief.ErrInvalid
}

func (s *recordingReportBriefService) Accept(_ context.Context, _, runID string, draft reportbrief.Draft) (reportbrief.Stored, error) {
	s.acceptCalls++
	s.runID = runID
	s.draft = draft
	return reportbrief.Stored{}, reportbrief.ErrInvalid
}

func (*recordingReportBriefService) ValidateForWrite(context.Context, string, string, string, string, string) (reportbrief.Stored, error) {
	return reportbrief.Stored{}, nil
}

func (s *recordingReportBriefService) DegradedWriteReason(context.Context, string, string) (string, error) {
	if s.degradedErr != nil {
		return "", s.degradedErr
	}
	if s.degradedReason == "" {
		return "", reportbrief.ErrNotFound
	}
	return s.degradedReason, nil
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

func TestProjectMemoryTokenBindsUserAndJob(t *testing.T) {
	const (
		secret = "project-memory-test-secret"
		jobRef = "2026-08-01|0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	)
	token, err := MintProjectMemoryJobToken(&model.User{ID: "305", Username: "t03"}, secret, jobRef)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := extractAIHubIdentityWithPolicy(token, secret, false)
	if err != nil {
		t.Fatal(err)
	}
	if identity.UID != 305 || identity.ProjectMemoryJobRef != jobRef || identity.ReportRunID != "" {
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

func TestEmptyBriefArgumentsStayAtArgumentBoundary(t *testing.T) {
	const runID = "958458d9-8e65-489f-8bfc-0de80ff46752"
	for name, rawArgs := range map[string]json.RawMessage{
		"empty arguments":  json.RawMessage(`{}`),
		"blank brief_json": json.RawMessage(`{"brief_json":" "}`),
		"empty brief_json": json.RawMessage(`{"brief_json":"{}"}`),
	} {
		t.Run(name, func(t *testing.T) {
			service := &recordingReportBriefService{}
			handler := &ReportMCPHandler{reportBrief: service, briefEnabled: true}
			request := httptest.NewRequest("POST", "/api/v1/mcp/reports", nil)
			ctx := context.WithValue(request.Context(), userKey, &model.User{ID: "21"})
			ctx = context.WithValue(ctx, reportRunIDKey, runID)
			request = request.WithContext(ctx)

			_, err := handler.toolWriteReportBrief(request, rawArgs)
			if err == nil {
				t.Fatal("expected empty Brief arguments to be rejected")
			}
			if !strings.Contains(err.Error(), "brief_json is required") {
				t.Fatalf("error = %q, want brief_json requirement", err)
			}
			if service.acceptCalls != 0 || service.rejectCalls != 0 {
				t.Fatalf("empty arguments reached semantic validation: accept=%d reject=%d", service.acceptCalls, service.rejectCalls)
			}
		})
	}
}

func TestLegacyBriefFieldsStillReachSemanticValidation(t *testing.T) {
	const runID = "958458d9-8e65-489f-8bfc-0de80ff46752"
	service := &recordingReportBriefService{}
	handler := &ReportMCPHandler{reportBrief: service, briefEnabled: true}
	request := httptest.NewRequest("POST", "/api/v1/mcp/reports", nil)
	ctx := context.WithValue(request.Context(), userKey, &model.User{ID: "21"})
	ctx = context.WithValue(ctx, reportRunIDKey, runID)
	request = request.WithContext(ctx)

	_, err := handler.toolWriteReportBrief(request, json.RawMessage(`{"no_reportable_work":true}`))
	if err == nil {
		t.Fatal("expected recording service to reject the legacy Brief")
	}
	if service.acceptCalls != 1 || !service.draft.NoReportableWork {
		t.Fatalf("legacy Brief did not reach semantic validation: accept=%d draft=%#v", service.acceptCalls, service.draft)
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

	_, err := handler.toolWriteReportBrief(request, json.RawMessage(`{"brief_json":"{\"workstreams\":[,"}`))
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
