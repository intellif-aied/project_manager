package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIncrementalUploadRetriesResponseLossAndResumes(t *testing.T) {
	content := []byte(
		"{\"type\":\"user\",\"sessionId\":\"sync-client\",\"timestamp\":\"2026-07-14T01:00:00Z\"}\n" +
			"{\"type\":\"assistant\",\"sessionId\":\"sync-client\",\"timestamp\":\"2026-07-14T01:01:00Z\"}\n",
	)
	serverState := &fakeSessionSyncServer{generationID: "11111111-1111-4111-8111-111111111111", failFirstChunkResponse: true}
	server := httptest.NewServer(http.HandlerFunc(serverState.serveHTTP))
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	sourcePath := filepath.Join(home, "session.jsonl")
	if err := os.WriteFile(sourcePath, content, 0600); err != nil {
		t.Fatal(err)
	}
	startedAt := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(time.Minute)
	session := &SessionInfo{
		SessionRef: "sync-client", AgentType: "codex", FilePath: sourcePath,
		StartedAt: startedAt, EndedAt: endedAt, Cwd: "/workspace/project", ProjectDir: "project",
		Summary: "session summary",
	}
	cfg := &Config{APIURL: server.URL, Token: "test-token"}
	results, err := uploadSessionGroupIncremental(cfg, []sessionWithFile{{info: session, filePath: sourcePath}}, session.SessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "uploaded" || results[0].UploadedChunks != 1 {
		t.Fatalf("results=%+v", results)
	}
	serverState.mu.Lock()
	if serverState.chunkRequests != 2 || serverState.acceptedChunks != 1 || serverState.finalizeRequests != 1 ||
		serverState.prepareSummary != session.Summary || string(serverState.content) != string(content) {
		t.Fatalf("server state: chunk_requests=%d accepted_chunks=%d finalize_requests=%d content_bytes=%d",
			serverState.chunkRequests, serverState.acceptedChunks, serverState.finalizeRequests, len(serverState.content))
	}
	serverState.mu.Unlock()

	stateInfo, err := os.Stat(uploadStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if stateInfo.Mode().Perm() != 0600 {
		t.Fatalf("upload state mode=%o", stateInfo.Mode().Perm())
	}
	states, err := loadUploadStates()
	if err != nil {
		t.Fatal(err)
	}
	if len(states.Sources) != 1 {
		t.Fatalf("states=%+v", states)
	}
	for _, state := range states.Sources {
		if state.LastAckedCursor != int64(len(content)) || state.LastAckedLine != 2 || state.PrefixCheckpointHash != hashTestBytes(content) {
			t.Fatalf("state=%+v", state)
		}
	}

	results, err = uploadSessionGroupIncremental(cfg, []sessionWithFile{{info: session, filePath: sourcePath}}, session.SessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "unchanged" || results[0].UploadedChunks != 0 {
		t.Fatalf("second results=%+v", results)
	}
	serverState.mu.Lock()
	if serverState.chunkRequests != 2 || serverState.finalizeRequests != 1 {
		t.Fatalf("second upload sent data: chunks=%d finalize=%d", serverState.chunkRequests, serverState.finalizeRequests)
	}
	serverState.mu.Unlock()

	newContent := []byte("{\"type\":\"user\",\"sessionId\":\"sync-client\",\"timestamp\":\"2026-07-14T01:02:00Z\"}\n")
	if err := os.WriteFile(sourcePath, append(append([]byte(nil), content...), newContent...), 0600); err != nil {
		t.Fatal(err)
	}
	results, err = uploadSessionGroupIncremental(cfg, []sessionWithFile{{info: session, filePath: sourcePath}}, session.SessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "uploaded" || results[0].UploadedChunks != 1 {
		t.Fatalf("incremental results=%+v", results)
	}
	serverState.mu.Lock()
	defer serverState.mu.Unlock()
	if serverState.acceptedChunks != 2 || serverState.finalizeRequests != 2 || string(serverState.content) != string(append(content, newContent...)) {
		t.Fatalf("incremental server state: accepted=%d finalize=%d content=%d",
			serverState.acceptedChunks, serverState.finalizeRequests, len(serverState.content))
	}
}

func TestIncrementalUploadFallsBackOnlyForExplicitDisabledCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"SESSION_SYNC_NOT_ENABLED","error":"not found"}`))
	}))
	defer server.Close()
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "session.jsonl")
	_ = os.WriteFile(path, []byte("{}\n"), 0600)
	session := &SessionInfo{SessionRef: "disabled", AgentType: "codex", FilePath: path}
	_, err := uploadSessionGroupIncremental(&Config{APIURL: server.URL, Token: "token"}, []sessionWithFile{{info: session, filePath: path}}, session.SessionRef)
	if !errors.Is(err, errSessionSyncNotEnabled) {
		t.Fatalf("err=%v", err)
	}
}

func TestIncrementalUploadFinalizesSnapshotWhenSourceGrowsDuringUpload(t *testing.T) {
	initial := []byte("{\"type\":\"user\",\"sessionId\":\"growing\",\"timestamp\":\"2026-07-15T01:00:00Z\"}\n")
	appended := []byte("{\"type\":\"assistant\",\"sessionId\":\"growing\",\"timestamp\":\"2026-07-15T01:01:00Z\"}\n")
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "growing.jsonl")
	if err := os.WriteFile(path, initial, 0600); err != nil {
		t.Fatal(err)
	}
	serverState := &fakeSessionSyncServer{generationID: "22222222-2222-4222-8222-222222222222"}
	serverState.afterFirstAccepted = func() {
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		if _, err := file.Write(appended); err != nil {
			t.Error(err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(serverState.serveHTTP))
	defer server.Close()
	session := &SessionInfo{SessionRef: "growing", AgentType: "codex", FilePath: path}
	cfg := &Config{APIURL: server.URL, Token: "test-token"}

	results, err := uploadSessionGroupIncremental(cfg, []sessionWithFile{{info: session, filePath: path}}, session.SessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].UploadedChunks != 1 {
		t.Fatalf("first results=%+v", results)
	}
	serverState.mu.Lock()
	if serverState.finalizeRequests != 1 || string(serverState.content) != string(initial) {
		serverState.mu.Unlock()
		t.Fatalf("first snapshot finalize=%d content=%d", serverState.finalizeRequests, len(serverState.content))
	}
	serverState.mu.Unlock()

	results, err = uploadSessionGroupIncremental(cfg, []sessionWithFile{{info: session, filePath: path}}, session.SessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].UploadedChunks != 1 {
		t.Fatalf("second results=%+v", results)
	}
	serverState.mu.Lock()
	defer serverState.mu.Unlock()
	if serverState.finalizeRequests != 2 || string(serverState.content) != string(append(initial, appended...)) {
		t.Fatalf("second snapshot finalize=%d content=%d", serverState.finalizeRequests, len(serverState.content))
	}
}

func TestIncrementalUploadFinalizesCompletePrefixBeforeIncompleteTail(t *testing.T) {
	complete := []byte("{\"type\":\"user\",\"sessionId\":\"tail\",\"timestamp\":\"2026-07-15T01:00:00Z\"}\n")
	incomplete := []byte("{\"type\":\"assistant\",\"sessionId\":\"tail\"")
	remainder := []byte(",\"timestamp\":\"2026-07-15T01:01:00Z\"}\n")
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "tail.jsonl")
	if err := os.WriteFile(path, append(append([]byte{}, complete...), incomplete...), 0600); err != nil {
		t.Fatal(err)
	}
	serverState := &fakeSessionSyncServer{generationID: "33333333-3333-4333-8333-333333333333"}
	server := httptest.NewServer(http.HandlerFunc(serverState.serveHTTP))
	defer server.Close()
	session := &SessionInfo{SessionRef: "tail", AgentType: "codex", FilePath: path}
	cfg := &Config{APIURL: server.URL, Token: "test-token"}

	results, err := uploadSessionGroupIncremental(cfg, []sessionWithFile{{info: session, filePath: path}}, session.SessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].UploadedChunks != 1 || !results[0].PendingTail {
		t.Fatalf("first results=%+v", results)
	}
	serverState.mu.Lock()
	if serverState.finalizeRequests != 1 || string(serverState.content) != string(complete) {
		serverState.mu.Unlock()
		t.Fatalf("first prefix finalize=%d content=%d", serverState.finalizeRequests, len(serverState.content))
	}
	serverState.mu.Unlock()

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(remainder); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	results, err = uploadSessionGroupIncremental(cfg, []sessionWithFile{{info: session, filePath: path}}, session.SessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].UploadedChunks != 1 || results[0].PendingTail {
		t.Fatalf("second results=%+v", results)
	}
	serverState.mu.Lock()
	defer serverState.mu.Unlock()
	want := append(append([]byte{}, complete...), append(incomplete, remainder...)...)
	if serverState.finalizeRequests != 2 || string(serverState.content) != string(want) {
		t.Fatalf("second prefix finalize=%d content=%d", serverState.finalizeRequests, len(serverState.content))
	}
}

type fakeSessionSyncServer struct {
	mu                     sync.Mutex
	generationID           string
	content                []byte
	active                 bool
	chunkRequests          int
	acceptedChunks         int
	finalizeRequests       int
	prepareSummary         string
	failFirstChunkResponse bool
	afterFirstAccepted     func()
}

func (s *fakeSessionSyncServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer test-token" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.URL.Path == "/session-syncs/prepare":
		var request prepareBatchRequest
		if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Sessions) != 1 || len(request.Sessions[0].Sources) != 1 {
			http.Error(w, "invalid prepare", http.StatusBadRequest)
			return
		}
		s.prepareSummary = request.Sessions[0].Summary
		source := request.Sessions[0].Sources[0]
		action := "rebuild_required"
		status := "staging"
		if s.active && source.LocalSize >= int64(len(s.content)) && source.PrefixCheckpointHash == hashTestBytes(s.content) {
			action = "append"
			if source.LocalSize == int64(len(s.content)) {
				action = "unchanged"
			}
			status = "active"
		}
		writeTestJSON(w, map[string]any{"results": []map[string]any{{
			"session_ref": request.Sessions[0].SessionRef, "source_key": source.SourceKey,
			"generation_id": s.generationID, "generation_status": status,
			"expected_cursor": len(s.content), "prefix_checkpoint_hash": hashTestBytes(s.content),
			"prefix_checkpoint_algorithm_version": prefixCheckpointAlgorithm,
			"content_status":                      "available", "action": action,
		}}})
	case r.URL.Path == "/session-chunks/batch":
		s.chunkRequests++
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var metadata struct {
			Chunks []struct {
				StartCursor   int64  `json:"start_cursor"`
				EndCursor     int64  `json:"end_cursor"`
				ContentSHA256 string `json:"content_sha256"`
			} `json:"chunks"`
		}
		if json.Unmarshal([]byte(r.FormValue("metadata")), &metadata) != nil || len(metadata.Chunks) != 1 {
			http.Error(w, "invalid metadata", http.StatusBadRequest)
			return
		}
		file, _, err := r.FormFile("chunk_0")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		chunkContent, _ := io.ReadAll(file)
		file.Close()
		chunk := metadata.Chunks[0]
		responseStatus := "accepted"
		if chunk.StartCursor == int64(len(s.content)) && chunk.EndCursor == chunk.StartCursor+int64(len(chunkContent)) && chunk.ContentSHA256 == hashTestBytes(chunkContent) {
			s.content = append(s.content, chunkContent...)
			s.acceptedChunks++
			if s.acceptedChunks == 1 && s.afterFirstAccepted != nil {
				s.afterFirstAccepted()
			}
		} else if chunk.EndCursor <= int64(len(s.content)) && chunk.ContentSHA256 == hashTestBytes(chunkContent) {
			responseStatus = "duplicate"
		} else {
			http.Error(w, "cursor conflict", http.StatusConflict)
			return
		}
		if s.failFirstChunkResponse {
			s.failFirstChunkResponse = false
			http.Error(w, "simulated response loss", http.StatusInternalServerError)
			return
		}
		writeTestJSON(w, map[string]any{"results": []map[string]any{{
			"status": responseStatus, "acked_cursor": chunk.EndCursor, "expected_cursor": len(s.content),
		}}})
	case strings.HasPrefix(r.URL.Path, "/session-syncs/") && strings.HasSuffix(r.URL.Path, "/finalize"):
		s.finalizeRequests++
		var request struct {
			DeclaredEndCursor    int64  `json:"declared_end_cursor"`
			PrefixCheckpointHash string `json:"prefix_checkpoint_hash"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.DeclaredEndCursor != int64(len(s.content)) || request.PrefixCheckpointHash != hashTestBytes(s.content) {
			http.Error(w, "invalid finalize", http.StatusConflict)
			return
		}
		s.active = true
		writeTestJSON(w, map[string]any{"status": "active"})
	default:
		http.NotFound(w, r)
	}
}

func writeTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func hashTestBytes(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}
