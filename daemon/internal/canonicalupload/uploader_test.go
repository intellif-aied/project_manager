package canonicalupload_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aidashboard/daemon/internal/canonicalupload"
	"github.com/aidashboard/daemon/internal/sessionadapter"
)

func TestPrepareFamilyUsesCanonicalOnlyEndpoint(t *testing.T) {
	var request struct {
		Sessions []struct {
			SessionRef       string `json:"session_ref"`
			ParentSessionRef string `json:"parent_session_ref"`
			Sources          []struct {
				SourceFormat string `json:"source_format"`
				SourceKey    string `json:"source_key"`
			} `json:"sources"`
		} `json:"sessions"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/canonical-session-syncs/prepare" || r.Header.Get("Authorization") != "Bearer token" {
			http.Error(w, "wrong canonical request", http.StatusBadRequest)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"session_ref":"child","source_key":"opencode:child:main","generation_id":"generation-1","action":"rebuild_required"}]}`))
	}))
	defer server.Close()

	directory := t.TempDir()
	childPath := filepath.Join(directory, "child.jsonl")
	if err := os.WriteFile(childPath, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	parent := sessionadapter.MaterializedSession{Descriptor: sessionadapter.Descriptor{
		ClientType: "opencode", NativeSessionRef: "parent",
	}}
	child := sessionadapter.MaterializedSession{Descriptor: sessionadapter.Descriptor{
		ClientType: "opencode", NativeSessionRef: "child", ParentRef: "parent",
	}, SourceFormat: "aida_event_v1", CanonicalPath: childPath, AdapterVersion: "opencode-v1", UsageCapability: sessionadapter.UsageExact}

	uploader, err := canonicalupload.New(canonicalupload.Config{
		BaseURL: server.URL, Token: "token", ClientVersion: "test-cli", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := uploader.PrepareFamily(context.Background(), []sessionadapter.MaterializedSession{child, parent})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared) != 1 || prepared[0].GenerationID != "generation-1" {
		t.Fatalf("prepared=%+v", prepared)
	}
	if len(request.Sessions) != 2 || request.Sessions[0].SessionRef != "child" || request.Sessions[0].ParentSessionRef != "parent" ||
		len(request.Sessions[0].Sources) != 1 || request.Sessions[0].Sources[0].SourceFormat != "aida_event_v1" ||
		request.Sessions[1].SessionRef != "parent" || len(request.Sessions[1].Sources) != 0 {
		t.Fatalf("request=%+v", request)
	}
}

func TestUploadFamilyUsesCanonicalPrepareThenSharedChunkAndFinalize(t *testing.T) {
	content := []byte("{\"schema\":\"aida.session.event.v1\",\"event_id\":\"e\",\"timestamp\":\"2026-07-21T01:02:03Z\",\"type\":\"message\",\"payload\":{}}\n")
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/canonical-session-syncs/prepare":
			_, _ = w.Write([]byte(`{"results":[{"session_ref":"s","source_key":"workbuddy:s:main","generation_id":"g","generation_status":"staging","expected_cursor":0,"prefix_checkpoint_hash":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","action":"rebuild_required"}]}`))
		case "/session-chunks/batch":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			metadata := r.FormValue("metadata")
			if !strings.Contains(metadata, `"generation_id":"g"`) {
				t.Errorf("metadata=%s", metadata)
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`{"results":[{"status":"accepted","acked_cursor":%d}]}`, len(content))))
		case "/session-syncs/g/finalize":
			_, _ = w.Write([]byte(`{"status":"active"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	uploader, err := canonicalupload.New(canonicalupload.Config{BaseURL: server.URL, Token: "token", ClientVersion: "test", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := uploader.UploadFamily(context.Background(), []sessionadapter.MaterializedSession{{Descriptor: sessionadapter.Descriptor{ClientType: "workbuddy", NativeSessionRef: "s"}, CanonicalPath: path, SourceFormat: "aida_event_v1", AdapterVersion: "v1", UsageCapability: sessionadapter.UsageExact}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].UploadedChunks != 1 || !result[0].Finalized {
		t.Fatalf("result=%+v", result)
	}
	if strings.Join(calls, ",") != "/canonical-session-syncs/prepare,/session-chunks/batch,/session-syncs/g/finalize" {
		t.Fatalf("calls=%v", calls)
	}
}
