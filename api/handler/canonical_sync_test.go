package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aidashboard/api/internal/canonicalsync"
	"github.com/aidashboard/api/model"
)

type fakeCanonicalPreparer struct {
	called bool
}

func (fake *fakeCanonicalPreparer) PrepareFamily(_ context.Context, userID string, request canonicalsync.PrepareRequest) ([]canonicalsync.PrepareResult, error) {
	fake.called = true
	return []canonicalsync.PrepareResult{{SessionRef: request.Sessions[0].SessionRef, GenerationID: "generation-1"}}, nil
}

func TestCanonicalPrepareIsIsolatedFromLegacyFormats(t *testing.T) {
	preparer := &fakeCanonicalPreparer{}
	handler := NewCanonicalSyncHandler(preparer)
	body := `{"client_version":"test","sessions":[{"session_ref":"new-client-session","agent_type":"opencode","sources":[{"source_role":"main","source_key":"opencode:new-client-session:main","local_size":3,"prefix_checkpoint_algorithm_version":"sha256-prefix-v1","source_format":"aida_event_v1","ingestion_metadata":{"adapter_version":"v1","usage_capability":"exact"}}]}]}`
	request := requestWithUser(httptest.NewRequest(http.MethodPost, "/api/v1/canonical-session-syncs/prepare", bytes.NewBufferString(body)), &model.User{ID: "42"})
	recorder := httptest.NewRecorder()
	handler.Prepare(recorder, request)
	if recorder.Code != http.StatusOK || !preparer.called || !bytes.Contains(recorder.Body.Bytes(), []byte("generation-1")) {
		t.Fatalf("status=%d called=%v body=%s", recorder.Code, preparer.called, recorder.Body.String())
	}

	preparer.called = false
	legacyBody := bytes.ReplaceAll([]byte(body), []byte("aida_event_v1"), []byte("codex_rollout_v1"))
	request = requestWithUser(httptest.NewRequest(http.MethodPost, "/api/v1/canonical-session-syncs/prepare", bytes.NewReader(legacyBody)), &model.User{ID: "42"})
	recorder = httptest.NewRecorder()
	handler.Prepare(recorder, request)
	if recorder.Code != http.StatusBadRequest || preparer.called {
		t.Fatalf("legacy source reached canonical preparer: status=%d called=%v body=%s", recorder.Code, preparer.called, recorder.Body.String())
	}
}
