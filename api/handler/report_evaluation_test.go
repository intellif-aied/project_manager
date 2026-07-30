package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aidashboard/api/internal/reporteval"
	"github.com/aidashboard/api/internal/reportsource"
	"github.com/aidashboard/api/model"
	"github.com/go-chi/chi/v5"
)

type evaluationSourceStub struct {
	userID string
}

func (stub *evaluationSourceStub) CreateExplicit(
	_ context.Context, userID, _ string, _ reportsource.Period, _ []reportsource.SourceInput,
) (reportsource.Selection, error) {
	stub.userID = userID
	return reportsource.Selection{Items: []reportsource.SelectionItem{{AgentType: "codex"}}}, nil
}

type evaluationFreezerStub struct {
	source reporteval.SourceEvidence
}

func (stub evaluationFreezerStub) Freeze(context.Context, reportsource.Selection) (reporteval.SourceEvidence, error) {
	return stub.source, nil
}

type evaluationArtifactsStub struct {
	userID    string
	artifacts reporteval.RunArtifactEnvelope
}

func (stub *evaluationArtifactsStub) Load(_ context.Context, userID, _ string) (reporteval.RunArtifactEnvelope, error) {
	stub.userID = userID
	return stub.artifacts, nil
}

func newEvaluationHandlerForTest(t *testing.T) (*ReportEvaluationHandler, *evaluationSourceStub, *evaluationArtifactsStub) {
	t.Helper()
	source := reporteval.SourceEvidence{
		SchemaVersion: reporteval.SourceSchemaVersion, SourceIdentitySHA256: strings.Repeat("a", 64),
		RedactionVersion: reporteval.EvidenceRedactionVersion,
		Items: []reporteval.SourceEvidenceItem{{EvidenceSourceID: "source-001", AgentType: "codex", Events: []reporteval.SourceEvidenceEvent{{
			EvidenceRef: "source-001/event-000001", OccurredAt: time.Now().UTC(), EventType: "assistant", Payload: json.RawMessage(`{}`),
		}}}},
	}
	manifest := json.RawMessage(`{"pipeline_profile":"digest_context_brief_final"}`)
	var value any
	_ = json.Unmarshal(manifest, &value)
	variantHash, _ := reporteval.CanonicalSHA256(value)
	sources := &evaluationSourceStub{}
	artifacts := &evaluationArtifactsStub{artifacts: reporteval.RunArtifactEnvelope{
		SchemaVersion: reporteval.RunArtifactsSchemaVersion, RunID: "33333333-3333-4333-8333-333333333333",
		Status: "succeeded", CreatedAt: time.Now().UTC(), SourceIdentitySHA256: strings.Repeat("a", 64),
		VariantManifest: manifest, VariantSHA256: variantHash,
	}}
	handler, err := NewReportEvaluationHandler(
		"test", "revision-a", "isolated-test-a", sources, evaluationFreezerStub{source: source}, artifacts,
	)
	if err != nil {
		t.Fatal(err)
	}
	return handler, sources, artifacts
}

func withEvaluationUser(request *http.Request) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), userKey, &model.User{ID: "305"}))
}

func TestNewReportEvaluationHandlerRejectsProduction(t *testing.T) {
	_, sources, artifacts := newEvaluationHandlerForTest(t)
	if _, err := NewReportEvaluationHandler(
		"production", "revision-a", "production-a", sources, evaluationFreezerStub{}, artifacts,
	); err == nil {
		t.Fatal("expected production handler to be rejected")
	}
}

func TestReportEvaluationRuntimeReturnsServerAttestation(t *testing.T) {
	handler, _, _ := newEvaluationHandlerForTest(t)
	request := withEvaluationUser(httptest.NewRequest(http.MethodGet, "/api/v1/evaluation/runtime", nil))
	response := httptest.NewRecorder()
	handler.Runtime(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"environment":"test"`) || !strings.Contains(response.Body.String(), `"instance_id":"isolated-test-a"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestReportEvaluationFreezeSourceUsesAuthenticatedUser(t *testing.T) {
	handler, sources, _ := newEvaluationHandlerForTest(t)
	request := withEvaluationUser(httptest.NewRequest(http.MethodPost, "/api/v1/evaluation/sources/freeze", strings.NewReader(`{
		"report_type":"personal_daily","report_date":"2026-07-30",
		"selected_session_slice_keys":["11111111-1111-4111-8111-111111111111"]
	}`)))
	response := httptest.NewRecorder()
	handler.FreezeSource(response, request)
	if response.Code != http.StatusCreated || sources.userID != "305" || !strings.Contains(response.Body.String(), reporteval.SourceSchemaVersion) {
		t.Fatalf("response = %d %s user=%s", response.Code, response.Body.String(), sources.userID)
	}
}

func TestReportEvaluationArtifactsAreScopedToAuthenticatedUser(t *testing.T) {
	handler, _, artifacts := newEvaluationHandlerForTest(t)
	request := withEvaluationUser(httptest.NewRequest(http.MethodGet, "/api/v1/evaluation/runs/id/artifacts", nil))
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("runId", "33333333-3333-4333-8333-333333333333")
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
	response := httptest.NewRecorder()
	handler.RunArtifacts(response, request)
	if response.Code != http.StatusOK || artifacts.userID != "305" {
		t.Fatalf("response = %d %s user=%s", response.Code, response.Body.String(), artifacts.userID)
	}
}
