package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/aidashboard/api/config"
	projectdb "github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/contentreader"
	"github.com/aidashboard/api/internal/sessionsync"
	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/storage"
	"github.com/go-chi/chi/v5"
)

func TestSessionSyncHTTPEndToEndIntegration(t *testing.T) {
	databaseURL := os.Getenv("AIDA_TEST_DATABASE_URL")
	endpoint := os.Getenv("AIDA_TEST_MINIO_ENDPOINT")
	accessKey := os.Getenv("AIDA_TEST_MINIO_ACCESS_KEY")
	secretKey := os.Getenv("AIDA_TEST_MINIO_SECRET_KEY")
	if databaseURL == "" || endpoint == "" || accessKey == "" || secretKey == "" {
		t.Skip("AIDA_TEST_DATABASE_URL and AIDA_TEST_MINIO_* are required")
	}
	database, err := projectdb.Connect(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := projectdb.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewMinioStorage(&config.Config{
		MinioEndpoint: endpoint, MinioAccessKey: accessKey, MinioSecretKey: secretKey,
		MinioBucket: "aidashboard-v2-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	userID := int64(990020)
	cleanupSessionSyncHTTPUser(database, userID)
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, 'v2-http-e2e-test')`, userID); err != nil {
		t.Fatal(err)
	}
	defer cleanupSessionSyncHTTPUser(database, userID)

	h, err := NewSessionSyncHandler(database, store)
	if err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: fmt.Sprint(userID), Role: "employee"}
	content := []byte("{\"type\":\"message\",\"timestamp\":\"2026-07-14T10:00:00Z\"}\n")
	startedAt := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	preparePayload, _ := json.Marshal(sessionsync.PrepareBatchRequest{
		ClientVersion: "e2e",
		Sessions: []sessionsync.PrepareSessionRequest{{
			SessionRef: "http-e2e", AgentType: "codex", StartedAt: &startedAt, LastActivityAt: &startedAt,
			Sources: []sessionsync.PrepareSourceRequest{{
				SourceRole: "main", SourceKey: "codex:http-e2e:main", LocalSize: int64(len(content)),
				PrefixCheckpointHash: sessionsync.HashBytes(nil), PrefixCheckpointAlgorithmVersion: sessionsync.PrefixCheckpointAlgorithm,
			}},
		}},
	})
	prepareRequest := requestWithUser(httptest.NewRequest(http.MethodPost, "/api/v1/session-syncs/prepare", bytes.NewReader(preparePayload)), user)
	prepareRecorder := httptest.NewRecorder()
	h.Prepare(prepareRecorder, prepareRequest)
	if prepareRecorder.Code != http.StatusOK {
		t.Fatalf("prepare status=%d body=%s", prepareRecorder.Code, prepareRecorder.Body.String())
	}
	var prepareResponse struct {
		Results []sessionsync.PrepareSourceResponse `json:"results"`
	}
	if err := json.Unmarshal(prepareRecorder.Body.Bytes(), &prepareResponse); err != nil || len(prepareResponse.Results) != 1 {
		t.Fatalf("prepare response=%s err=%v", prepareRecorder.Body.String(), err)
	}
	generationID := prepareResponse.Results[0].GenerationID

	var uploadBody bytes.Buffer
	multipartWriter := multipart.NewWriter(&uploadBody)
	metadata, _ := json.Marshal(chunkBatchRequest{Chunks: []chunkUploadRequest{{
		ChunkMetadata: sessionsync.ChunkMetadata{
			StartCursor: 0, EndCursor: int64(len(content)), StartLine: 1, EndLine: 1,
			ContentSHA256: sessionsync.HashBytes(content), EventStartAt: &startedAt, EventEndAt: &startedAt,
		},
		GenerationID: generationID, FileField: "chunk_0", ContentEncoding: "identity", UncompressedSize: int64(len(content)),
	}}})
	if err := multipartWriter.WriteField("metadata", string(metadata)); err != nil {
		t.Fatal(err)
	}
	part, err := multipartWriter.CreateFormFile("chunk_0", "chunk.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatal(err)
	}
	uploadRequest := requestWithUser(httptest.NewRequest(http.MethodPost, "/api/v1/session-chunks/batch", &uploadBody), user)
	uploadRequest.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	uploadRecorder := httptest.NewRecorder()
	h.UploadChunks(uploadRecorder, uploadRequest)
	if uploadRecorder.Code != http.StatusOK || !bytes.Contains(uploadRecorder.Body.Bytes(), []byte(`"status":"accepted"`)) {
		t.Fatalf("upload status=%d body=%s", uploadRecorder.Code, uploadRecorder.Body.String())
	}

	finalizePayload, _ := json.Marshal(sessionsync.FinalizeRequest{
		DeclaredEndCursor: int64(len(content)), PrefixCheckpointHash: sessionsync.HashBytes(content),
		PrefixCheckpointAlgorithmVersion: sessionsync.PrefixCheckpointAlgorithm,
	})
	finalizeRequest := requestWithUser(httptest.NewRequest(http.MethodPost, "/api/v1/session-syncs/"+generationID+"/finalize", bytes.NewReader(finalizePayload)), user)
	finalizeRecorder := httptest.NewRecorder()
	router := chi.NewRouter()
	router.Post("/api/v1/session-syncs/{generationId}/finalize", h.Finalize)
	router.ServeHTTP(finalizeRecorder, finalizeRequest)
	if finalizeRecorder.Code != http.StatusOK || !bytes.Contains(finalizeRecorder.Body.Bytes(), []byte(`"status":"active"`)) {
		t.Fatalf("finalize status=%d body=%s", finalizeRecorder.Code, finalizeRecorder.Body.String())
	}
	processor, err := sessionsync.NewContentProjectionProcessor(database, store)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := database.Query(`
		SELECT id, job_type, session_id, generation_id, chunk_id,
			target_revision_id, content_epoch, payload, attempts, max_attempts,
			COALESCE(lease_owner, ''), COALESCE(lease_until, 'epoch'::timestamptz)
		FROM session_processing_jobs
		WHERE generation_id = $1 AND job_type IN ('index_content_chunk', 'rebuild_content_revision')
		ORDER BY created_at, id`, generationID)
	if err != nil {
		t.Fatal(err)
	}
	var contentJobs []sessionsync.ProcessingJob
	for rows.Next() {
		var job sessionsync.ProcessingJob
		if err := rows.Scan(
			&job.ID, &job.Type, &job.SessionID, &job.GenerationID, &job.ChunkID,
			&job.TargetRevisionID, &job.ContentEpoch, &job.Payload, &job.Attempts,
			&job.MaxAttempts, &job.LeaseOwner, &job.LeaseUntil,
		); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		contentJobs = append(contentJobs, job)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(contentJobs) != 2 {
		t.Fatalf("content jobs=%+v", contentJobs)
	}
	for _, job := range contentJobs {
		if err := processor.Process(context.Background(), job); err != nil {
			t.Fatalf("process %s: %v", job.Type, err)
		}
	}
	statusRequest := requestWithUser(httptest.NewRequest(http.MethodGet, "/api/v1/session-syncs/"+generationID+"/status", nil), user)
	statusRecorder := httptest.NewRecorder()
	statusRouter := chi.NewRouter()
	statusRouter.Get("/api/v1/session-syncs/{generationId}/status", h.Status)
	statusRouter.ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK ||
		!bytes.Contains(statusRecorder.Body.Bytes(), []byte(`"ready_for_reports":true`)) ||
		!bytes.Contains(statusRecorder.Body.Bytes(), []byte(`"content_status":"available"`)) {
		t.Fatalf("status code=%d body=%s", statusRecorder.Code, statusRecorder.Body.String())
	}
	if err := processor.Process(context.Background(), contentJobs[0]); err != nil {
		t.Fatalf("idempotent content replay: %v", err)
	}
	var revisionStatus string
	var indexedCursor int64
	var eventCount, storedEventCount int
	if err := database.QueryRow(`
		SELECT p.status, p.content_indexed_cursor, p.event_count,
			(SELECT COUNT(*) FROM session_content_events e WHERE e.content_projection_revision_id = p.id)
		FROM session_sources src
		JOIN session_content_projection_revisions p ON p.id = src.active_content_projection_revision_id
		WHERE src.active_generation_id = $1`, generationID).Scan(
		&revisionStatus, &indexedCursor, &eventCount, &storedEventCount,
	); err != nil {
		t.Fatal(err)
	}
	if revisionStatus != "active" || indexedCursor != int64(len(content)) || eventCount != 1 || storedEventCount != 1 {
		t.Fatalf("status=%s cursor=%d eventCount=%d stored=%d", revisionStatus, indexedCursor, eventCount, storedEventCount)
	}

	var objectKey string
	if err := database.QueryRow(`SELECT raw_object_key FROM session_upload_chunks WHERE generation_id = $1`, generationID).Scan(&objectKey); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Delete(context.Background(), objectKey) })

	var sessionID string
	if err := database.QueryRow(`
		UPDATE sessions SET summary = 'must not remain readable', raw_log_url = NULL
		WHERE user_id = $1 AND session_ref = 'http-e2e'
		RETURNING id`, userID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	sessionHandler := NewSessionHandler(database, store, nil)
	contentReader, err := contentreader.New(database, store)
	if err != nil {
		t.Fatal(err)
	}
	sessionHandler.ConfigureContentReader(contentReader)
	readRouter := chi.NewRouter()
	readRouter.Get("/api/v1/sessions/{id}", sessionHandler.Get)
	readRouter.Get("/api/v1/sessions/{id}/log", sessionHandler.DownloadLog)
	availableRecorder := httptest.NewRecorder()
	readRouter.ServeHTTP(availableRecorder, requestWithUser(httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sessionID, nil), user))
	if availableRecorder.Code != http.StatusOK ||
		!bytes.Contains(availableRecorder.Body.Bytes(), []byte(`"has_log_content":true`)) {
		t.Fatalf("available get status=%d body=%s", availableRecorder.Code, availableRecorder.Body.String())
	}
	availableDownload := httptest.NewRecorder()
	readRouter.ServeHTTP(availableDownload, requestWithUser(httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sessionID+"/log", nil), user))
	if availableDownload.Code != http.StatusOK || !bytes.Equal(availableDownload.Body.Bytes(), content) {
		t.Fatalf("V2 download status=%d body=%s", availableDownload.Code, availableDownload.Body.String())
	}
	clearRouter := chi.NewRouter()
	clearRouter.Post("/api/v1/sessions/{id}/clear-content", h.ClearContent)
	clearRequest := requestWithUser(httptest.NewRequest(
		http.MethodPost, "/api/v1/sessions/"+sessionID+"/clear-content",
		bytes.NewBufferString(`{"reason":"e2e content clear"}`),
	), user)
	clearRecorder := httptest.NewRecorder()
	clearRouter.ServeHTTP(clearRecorder, clearRequest)
	if clearRecorder.Code != http.StatusAccepted || !bytes.Contains(clearRecorder.Body.Bytes(), []byte(`"content_status":"clearing"`)) {
		t.Fatalf("clear status=%d body=%s", clearRecorder.Code, clearRecorder.Body.String())
	}

	getRecorder := httptest.NewRecorder()
	readRouter.ServeHTTP(getRecorder, requestWithUser(httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sessionID, nil), user))
	if getRecorder.Code != http.StatusOK || bytes.Contains(getRecorder.Body.Bytes(), []byte("must not remain readable")) ||
		!bytes.Contains(getRecorder.Body.Bytes(), []byte(`"content_status":"clearing"`)) {
		t.Fatalf("masked get status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
	downloadRecorder := httptest.NewRecorder()
	readRouter.ServeHTTP(downloadRecorder, requestWithUser(httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sessionID+"/log", nil), user))
	if downloadRecorder.Code != http.StatusGone || !bytes.Contains(downloadRecorder.Body.Bytes(), []byte(`"code":"CONTENT_CLEARED"`)) {
		t.Fatalf("masked download status=%d body=%s", downloadRecorder.Code, downloadRecorder.Body.String())
	}
}

func cleanupSessionSyncHTTPUser(database *sql.DB, userID int64) {
	_, _ = database.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID)
	_, _ = database.Exec(`DELETE FROM users WHERE id = $1`, userID)
}
