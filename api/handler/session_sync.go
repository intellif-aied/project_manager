package handler

import (
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/aidashboard/api/internal/sessionsync"
	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/storage"
	"github.com/go-chi/chi/v5"
)

const (
	maxSessionSyncRequestBytes      = 256 << 20
	maxSessionSyncMetadataBytes     = 1 << 20
	maxSessionSyncCompressedChunk   = 16 << 20
	maxSessionSyncUncompressedChunk = 64 << 20
	maxSessionSyncCompressionRatio  = 100
)

type SessionSyncHandler struct {
	service   *sessionsync.SyncService
	lifecycle *sessionsync.ContentLifecycleService
	acceptor  *sessionsync.ChunkAcceptor
}

func NewSessionSyncHandler(
	database *sql.DB,
	store *storage.MinioStorage,
) (*SessionSyncHandler, error) {
	service, err := sessionsync.NewSyncService(database)
	if err != nil {
		return nil, err
	}
	lifecycle, err := sessionsync.NewContentLifecycleService(database)
	if err != nil {
		return nil, err
	}
	var acceptor *sessionsync.ChunkAcceptor
	if store != nil {
		repository, err := sessionsync.NewPostgresChunkRepository(database)
		if err != nil {
			return nil, err
		}
		acceptor, err = sessionsync.NewChunkAcceptor(repository, store)
		if err != nil {
			return nil, err
		}
	}
	return &SessionSyncHandler{service: service, lifecycle: lifecycle, acceptor: acceptor}, nil
}

func (h *SessionSyncHandler) Prepare(w http.ResponseWriter, r *http.Request) {
	u, ok := h.authorize(w, r)
	if !ok {
		return
	}
	var request sessionsync.PrepareBatchRequest
	if err := decodeLimitedJSON(w, r, &request); err != nil {
		return
	}
	if len(request.Sessions) == 0 || len(request.Sessions) > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_PREPARE", "error": "sessions must contain 1 to 100 items"})
		return
	}

	results := make([]sessionsync.PrepareSourceResponse, 0)
	for _, session := range request.Sessions {
		prepared, err := h.service.Prepare(r.Context(), u.ID, session)
		if err != nil {
			writeSessionSyncError(w, err)
			return
		}
		results = append(results, prepared...)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

type chunkBatchRequest struct {
	Chunks []chunkUploadRequest `json:"chunks"`
}

type chunkUploadRequest struct {
	sessionsync.ChunkMetadata
	GenerationID     string `json:"generation_id"`
	FileField        string `json:"file_field"`
	ContentEncoding  string `json:"content_encoding,omitempty"`
	UncompressedSize int64  `json:"uncompressed_size"`
}

type chunkUploadResponse struct {
	Status             string `json:"status"`
	GenerationID       string `json:"generation_id"`
	ContentSHA256      string `json:"content_sha256"`
	AckedCursor        int64  `json:"acked_cursor"`
	ExpectedCursor     int64  `json:"expected_cursor"`
	ContentIndexStatus string `json:"content_index_status,omitempty"`
	UsageParseStatus   string `json:"usage_parse_status,omitempty"`
	ErrorCode          string `json:"error_code,omitempty"`
	NextAction         string `json:"next_action,omitempty"`
}

func (h *SessionSyncHandler) UploadChunks(w http.ResponseWriter, r *http.Request) {
	u, ok := h.authorize(w, r)
	if !ok {
		return
	}
	if h.acceptor == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "OBJECT_STORAGE_UNAVAILABLE", "error": "incremental session storage is not configured"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSessionSyncRequestBytes)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_CHUNK", "error": "invalid multipart request: " + err.Error()})
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	metadata := r.FormValue("metadata")
	if len(metadata) == 0 || len(metadata) > maxSessionSyncMetadataBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_CHUNK", "error": "missing or oversized metadata"})
		return
	}
	var request chunkBatchRequest
	if err := json.Unmarshal([]byte(metadata), &request); err != nil || len(request.Chunks) == 0 || len(request.Chunks) > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_CHUNK", "error": "metadata must contain 1 to 100 chunks"})
		return
	}

	responses := make([]chunkUploadResponse, 0, len(request.Chunks))
	for index, chunk := range request.Chunks {
		if chunk.FileField == "" {
			chunk.FileField = fmt.Sprintf("chunk_%d", index)
		}
		file, header, err := r.FormFile(chunk.FileField)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_CHUNK", "error": "missing file field " + chunk.FileField})
			return
		}
		reader, closeReader, err := validatedChunkReader(file, header, chunk)
		if err != nil {
			file.Close()
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_CHUNK", "error": err.Error()})
			return
		}
		decision, acceptErr := h.acceptor.Accept(r.Context(), sessionsync.AcceptChunkRequest{
			UserID:       u.ID,
			GenerationID: strings.TrimSpace(chunk.GenerationID),
			Chunk:        chunk.ChunkMetadata,
			ContentSize:  chunk.UncompressedSize,
			Content:      reader,
		})
		closeReader()
		file.Close()
		if acceptErr != nil {
			writeSessionSyncError(w, acceptErr)
			return
		}
		status := string(decision.Status)
		if decision.ErrorCode == sessionsync.ErrorContentCleared {
			status = "content_cleared"
		}
		response := chunkUploadResponse{
			Status: status, GenerationID: chunk.GenerationID, ContentSHA256: chunk.ContentSHA256,
			AckedCursor: decision.AckedCursor, ExpectedCursor: decision.ExpectedCursor,
			ErrorCode: decision.ErrorCode, NextAction: decision.NextAction,
		}
		if decision.Status == sessionsync.ChunkAccepted {
			response.ContentIndexStatus = "pending"
			response.UsageParseStatus = "pending"
		}
		responses = append(responses, response)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": responses})
}

func validatedChunkReader(
	file multipart.File,
	header *multipart.FileHeader,
	request chunkUploadRequest,
) (io.Reader, func(), error) {
	if request.UncompressedSize <= 0 || request.UncompressedSize > maxSessionSyncUncompressedChunk ||
		header.Size <= 0 || header.Size > maxSessionSyncCompressedChunk {
		return nil, func() {}, errors.New("chunk size exceeds configured limits")
	}
	encoding := strings.ToLower(strings.TrimSpace(request.ContentEncoding))
	if encoding == "" || encoding == "identity" {
		if header.Size != request.UncompressedSize {
			return nil, func() {}, errors.New("identity chunk size does not match uncompressed_size")
		}
		return file, func() {}, nil
	}
	if encoding != "gzip" {
		return nil, func() {}, errors.New("unsupported chunk content_encoding")
	}
	if request.UncompressedSize > header.Size*maxSessionSyncCompressionRatio {
		return nil, func() {}, errors.New("chunk compression ratio exceeds configured limit")
	}
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, func() {}, errors.New("invalid gzip chunk")
	}
	return gzipReader, func() { _ = gzipReader.Close() }, nil
}

func (h *SessionSyncHandler) Finalize(w http.ResponseWriter, r *http.Request) {
	u, ok := h.authorize(w, r)
	if !ok {
		return
	}
	generationID := chi.URLParam(r, "generationId")
	if !isValidUUID(generationID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_GENERATION", "error": "invalid generation id"})
		return
	}
	var request sessionsync.FinalizeRequest
	if err := decodeLimitedJSON(w, r, &request); err != nil {
		return
	}
	response, err := h.service.Finalize(r.Context(), u.ID, generationID, request)
	if err != nil {
		writeSessionSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *SessionSyncHandler) ClearContent(w http.ResponseWriter, r *http.Request) {
	u, ok := h.authorize(w, r)
	if !ok {
		return
	}
	sessionID := chi.URLParam(r, "id")
	if !isValidUUID(sessionID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_SESSION", "error": "invalid session id"})
		return
	}
	var request struct {
		Reason string `json:"reason"`
	}
	if err := decodeLimitedJSON(w, r, &request); err != nil {
		return
	}
	response, err := h.lifecycle.RequestClear(r.Context(), u.ID, sessionID, request.Reason)
	if err != nil {
		writeSessionSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, response)
}

func (h *SessionSyncHandler) RestoreContent(w http.ResponseWriter, r *http.Request) {
	u, ok := h.authorize(w, r)
	if !ok {
		return
	}
	sessionID := chi.URLParam(r, "id")
	if !isValidUUID(sessionID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_SESSION", "error": "invalid session id"})
		return
	}
	response, err := h.lifecycle.RequestRestore(r.Context(), u.ID, sessionID)
	if err != nil {
		writeSessionSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, response)
}

func (h *SessionSyncHandler) authorize(w http.ResponseWriter, r *http.Request) (*model.User, bool) {
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return nil, false
	}
	return u, true
}

func decodeLimitedJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxSessionSyncMetadataBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "error": err.Error()})
		return err
	}
	return nil
}

func writeSessionSyncError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sessionsync.ErrGenerationNotFound), errors.Is(err, sessionsync.ErrSessionNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "NOT_FOUND", "error": err.Error()})
	case errors.Is(err, sessionsync.ErrInvalidSyncRequest):
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "error": err.Error()})
	case errors.Is(err, sessionsync.ErrSourceKeyConflict), errors.Is(err, sessionsync.ErrFinalizeConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"code": "SOURCE_CONFLICT", "error": err.Error()})
	case errors.Is(err, sessionsync.ErrContentTransition), errors.Is(err, sessionsync.ErrContentLifecyclePending):
		writeJSON(w, http.StatusConflict, map[string]string{"code": "CONTENT_STATE_CONFLICT", "error": err.Error()})
	case errors.Is(err, sessionsync.ErrIncrementalSourceNeeded):
		writeJSON(w, http.StatusConflict, map[string]string{"code": "INCREMENTAL_SOURCE_REQUIRED", "error": err.Error()})
	case errors.Is(err, sessionsync.ErrLegacyContentMigration):
		writeJSON(w, http.StatusConflict, map[string]string{"code": "LEGACY_CONTENT_MIGRATION_REQUIRED", "error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "SESSION_SYNC_FAILED", "error": err.Error()})
	}
}
