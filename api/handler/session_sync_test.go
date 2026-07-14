package handler

import (
	"bytes"
	"compress/gzip"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/model"
)

func TestSessionSyncPrepareAvailableToAuthenticatedUser(t *testing.T) {
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	handler, err := NewSessionSyncHandler(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session-syncs/prepare", bytes.NewBufferString(`{"sessions":[]}`))
	request = requestWithUser(request, &model.User{ID: "42", Role: "employee"})
	recorder := httptest.NewRecorder()
	handler.Prepare(recorder, request)
	if recorder.Code != http.StatusBadRequest || !bytes.Contains(recorder.Body.Bytes(), []byte("INVALID_PREPARE")) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSessionSyncRequiresAuthentication(t *testing.T) {
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	handler, err := NewSessionSyncHandler(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session-syncs/prepare", bytes.NewBufferString(`{"sessions":[]}`))
	recorder := httptest.NewRecorder()
	handler.Prepare(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSessionSyncEnabledUploadRequiresObjectStorage(t *testing.T) {
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	handler, err := NewSessionSyncHandler(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session-chunks/batch", http.NoBody)
	request = requestWithUser(request, &model.User{ID: "42", Role: "employee"})
	recorder := httptest.NewRecorder()
	handler.UploadChunks(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || !bytes.Contains(recorder.Body.Bytes(), []byte("OBJECT_STORAGE_UNAVAILABLE")) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestValidatedChunkReaderStreamsGzip(t *testing.T) {
	content := []byte("{\"event\":1}\n")
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	file := writeTemporaryMultipartFile(t, compressed.Bytes())
	reader, closeReader, err := validatedChunkReader(file, &multipart.FileHeader{Size: int64(compressed.Len())}, chunkUploadRequest{
		ContentEncoding: "gzip", UncompressedSize: int64(len(content)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeReader()
	decompressed, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(decompressed, content) {
		t.Fatalf("decompressed=%q err=%v", decompressed, err)
	}
}

func TestValidatedChunkReaderRejectsIdentitySizeMismatch(t *testing.T) {
	file := writeTemporaryMultipartFile(t, []byte("abc"))
	_, _, err := validatedChunkReader(file, &multipart.FileHeader{Size: 3}, chunkUploadRequest{
		ContentEncoding: "identity", UncompressedSize: 2,
	})
	if err == nil {
		t.Fatal("identity size mismatch was accepted")
	}
}

func writeTemporaryMultipartFile(t *testing.T, content []byte) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "chunk-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	return file
}
