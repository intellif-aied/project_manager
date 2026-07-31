package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	uploadStateFileName        = "upload-state.json"
	uploadStateVersion         = 2
	prefixCheckpointAlgorithm  = "sha256-prefix-v1"
	defaultSyncChunkEvents     = 500
	defaultSyncChunkBytes      = 500 << 20
	defaultSyncMaxLineBytes    = 500 << 20
	sessionChunkRequestTimeout = 30 * time.Minute
)

var errSessionSyncNotEnabled = errors.New("incremental session sync is not enabled")

var (
	sessionReadinessTimeout      = 60 * time.Second
	sessionReadinessPollInterval = time.Second
)

type uploadStateFile struct {
	Version int                         `json:"version"`
	Sources map[string]localUploadState `json:"sources"`
}

type localUploadState struct {
	SessionRef                       string    `json:"session_ref"`
	AgentType                        string    `json:"agent_type"`
	SourceKey                        string    `json:"source_key"`
	GenerationID                     string    `json:"generation_id"`
	LastAckedCursor                  int64     `json:"last_acked_cursor"`
	LastAckedLine                    int64     `json:"last_acked_line"`
	PrefixCheckpointHash             string    `json:"prefix_checkpoint_hash"`
	PrefixCheckpointAlgorithmVersion string    `json:"prefix_checkpoint_algorithm_version"`
	LastAckedChunkHash               string    `json:"last_acked_chunk_hash,omitempty"`
	LastPrepareAt                    time.Time `json:"last_prepare_at"`
	LastUploadAt                     time.Time `json:"last_upload_at,omitempty"`
}

type prepareBatchRequest struct {
	ClientVersion string                  `json:"client_version"`
	UploadMode    string                  `json:"upload_mode,omitempty"`
	Sessions      []prepareSessionRequest `json:"sessions"`
}

type prepareSessionRequest struct {
	SessionRef       string                 `json:"session_ref"`
	AgentType        string                 `json:"agent_type"`
	ParentSessionRef string                 `json:"parent_session_ref,omitempty"`
	ForkedAt         *time.Time             `json:"forked_at,omitempty"`
	ForkSource       string                 `json:"fork_source,omitempty"`
	Summary          string                 `json:"summary,omitempty"`
	StartedAt        *time.Time             `json:"started_at,omitempty"`
	LastActivityAt   *time.Time             `json:"last_activity_at,omitempty"`
	CWD              string                 `json:"cwd,omitempty"`
	ProjectName      string                 `json:"project_name,omitempty"`
	Sources          []prepareSourceRequest `json:"sources"`
}

type prepareSourceRequest struct {
	SourceRole                       string `json:"source_role"`
	SourceKey                        string `json:"source_key"`
	LocalSize                        int64  `json:"local_size"`
	PrefixCheckpointHash             string `json:"prefix_checkpoint_hash"`
	PrefixCheckpointAlgorithmVersion string `json:"prefix_checkpoint_algorithm_version"`
}

type prepareSourceResult struct {
	SessionRef                string `json:"session_ref"`
	SourceKey                 string `json:"source_key"`
	GenerationID              string `json:"generation_id"`
	GenerationStatus          string `json:"generation_status"`
	ExpectedCursor            int64  `json:"expected_cursor"`
	PrefixCheckpointHash      string `json:"prefix_checkpoint_hash"`
	PrefixCheckpointAlgorithm string `json:"prefix_checkpoint_algorithm_version"`
	ContentStatus             string `json:"content_status"`
	Action                    string `json:"action"`
	ErrorCode                 string `json:"error_code"`
	NextAction                string `json:"next_action"`
}

type incrementalUploadResult struct {
	SessionRef      string
	CWD             string
	GenerationID    string
	Status          string
	UploadedChunks  int
	PendingTail     bool
	ContentStatus   string
	ReadyForReports bool
	ErrorCode       string
}

func uploadSessionGroupIncremental(cfg *Config, items []sessionWithFile, parentSessionRef string) ([]incrementalUploadResult, error) {
	return uploadSessionGroupIncrementalWithMode(cfg, items, parentSessionRef, uploadModePersonal)
}

func uploadSessionGroupIncrementalWithMode(cfg *Config, items []sessionWithFile, parentSessionRef, uploadMode string) ([]incrementalUploadResult, error) {
	states, err := loadUploadStates()
	if err != nil {
		return nil, err
	}
	results := make([]incrementalUploadResult, 0, len(items))
	for index, item := range items {
		parentRef := strings.TrimSpace(item.info.ParentSessionRef)
		if parentRef == "" && index > 0 {
			parentRef = parentSessionRef
		}
		result, err := uploadSessionSourceIncrementalWithMode(cfg, states, item, parentRef, uploadMode)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func uploadSessionSourceIncremental(
	cfg *Config,
	states *uploadStateFile,
	item sessionWithFile,
	parentSessionRef string,
) (result incrementalUploadResult, returnErr error) {
	return uploadSessionSourceIncrementalWithMode(cfg, states, item, parentSessionRef, uploadModePersonal)
}

func uploadSessionSourceIncrementalWithMode(
	cfg *Config,
	states *uploadStateFile,
	item sessionWithFile,
	parentSessionRef string,
	uploadMode string,
) (result incrementalUploadResult, returnErr error) {
	session := item.info
	if session == nil || session.SessionRef == "" || item.filePath == "" {
		return incrementalUploadResult{}, errors.New("session metadata and source file are required")
	}
	fileInfo, err := os.Stat(item.filePath)
	if err != nil {
		return incrementalUploadResult{}, err
	}
	agentType := normalizedAgentType(session.AgentType)
	if uploadMode != uploadModeTeam {
		if err := preflightSessionSource(item.filePath); err != nil {
			return incrementalUploadResult{}, err
		}
	}
	sourceKey := fmt.Sprintf("%s:%s:main", agentType, session.SessionRef)
	stateKey := uploadStateKeyForMode(uploadMode, agentType, session.SessionRef, sourceKey)
	state := states.Sources[stateKey]
	prepared, err := prepareSessionSourceWithMode(cfg, session, item.filePath, parentSessionRef, sourceKey, fileInfo.Size(), state, uploadMode)
	if err != nil {
		return incrementalUploadResult{}, err
	}
	result = incrementalUploadResult{
		SessionRef: session.SessionRef, CWD: session.Cwd, GenerationID: prepared.GenerationID,
		Status: prepared.Action, ContentStatus: prepared.ContentStatus, ErrorCode: prepared.ErrorCode,
	}
	if prepared.Action == "content_cleared" {
		return result, nil
	}
	if prepared.Action == "rejected" {
		if prepared.ErrorCode == "TEAM_DIRECTORY_UNMAPPED" {
			result.Status = "unmapped"
			return result, nil
		}
		if uploadMode == uploadModeTeam && isNonRetryableTeamPrepareError(prepared.ErrorCode) {
			result.Status = "blocked"
			return result, nil
		}
		return result, fmt.Errorf("prepare rejected: %s %s", prepared.ErrorCode, prepared.NextAction)
	}
	if prepared.GenerationID == "" || prepared.ExpectedCursor < 0 || prepared.ExpectedCursor > fileInfo.Size() {
		return incrementalUploadResult{}, errors.New("server returned an invalid generation checkpoint")
	}
	abortOnFailure := prepared.GenerationStatus == "staging"
	defer func() {
		if returnErr == nil || !abortOnFailure {
			return
		}
		if abortErr := abortSessionSource(cfg, prepared.GenerationID); abortErr != nil {
			returnErr = fmt.Errorf("%w; staging cleanup failed: %v", returnErr, abortErr)
			return
		}
		delete(states.Sources, stateKey)
		_ = saveUploadStates(states)
	}()
	if uploadMode == uploadModeTeam {
		if err := preflightSessionSource(item.filePath); err != nil {
			return incrementalUploadResult{}, err
		}
	}

	file, err := os.Open(item.filePath)
	if err != nil {
		return incrementalUploadResult{}, err
	}
	defer file.Close()
	prefixHasher, ackedLines, err := initializeLocalPrefix(file, prepared.ExpectedCursor)
	if err != nil {
		return incrementalUploadResult{}, err
	}
	localPrefixHash := hex.EncodeToString(prefixHasher.Sum(nil))
	if localPrefixHash != prepared.PrefixCheckpointHash {
		return incrementalUploadResult{}, errors.New("local prefix changed after prepare; run upload again to rebuild the source")
	}
	state = localUploadState{
		SessionRef: session.SessionRef, AgentType: agentType, SourceKey: sourceKey,
		GenerationID: prepared.GenerationID, LastAckedCursor: prepared.ExpectedCursor,
		LastAckedLine: ackedLines, PrefixCheckpointHash: localPrefixHash,
		PrefixCheckpointAlgorithmVersion: prefixCheckpointAlgorithm,
		LastPrepareAt:                    time.Now().UTC(),
	}
	states.Sources[stateKey] = state
	if err := saveUploadStates(states); err != nil {
		return incrementalUploadResult{}, err
	}

	if _, err := file.Seek(prepared.ExpectedCursor, io.SeekStart); err != nil {
		return incrementalUploadResult{}, err
	}
	remaining := fileInfo.Size() - prepared.ExpectedCursor
	pendingTail, err := streamSessionJSONLChunks(
		io.LimitReader(file, remaining),
		prepared.ExpectedCursor,
		ackedLines+1,
		sessionChunkLimits{
			MaxEvents: defaultSyncChunkEvents, MaxChunkBytes: defaultSyncChunkBytes, MaxLineBytes: defaultSyncMaxLineBytes,
		},
		func(chunk localSessionChunk) error {
			if err := uploadIncrementalChunk(cfg, prepared.GenerationID, chunk); err != nil {
				return err
			}
			if _, err := prefixHasher.Write(chunk.Content); err != nil {
				return err
			}
			state.LastAckedCursor = chunk.EndCursor
			state.LastAckedLine = chunk.EndLine
			state.LastAckedChunkHash = chunk.ContentSHA256
			state.PrefixCheckpointHash = hex.EncodeToString(prefixHasher.Sum(nil))
			state.LastUploadAt = time.Now().UTC()
			states.Sources[stateKey] = state
			if err := saveUploadStates(states); err != nil {
				return err
			}
			result.UploadedChunks++
			return nil
		},
	)
	if err != nil {
		return incrementalUploadResult{}, err
	}
	result.PendingTail = pendingTail
	if result.UploadedChunks > 0 {
		result.Status = "uploaded"
	} else if prepared.Action == "unchanged" {
		result.Status = "unchanged"
	}

	needsFinalize := result.UploadedChunks > 0 || prepared.GenerationStatus == "staging" || prepared.Action == "rebuild_required" || prepared.Action == "restore"
	completePrefixUploaded := result.UploadedChunks > 0 && state.LastAckedCursor > prepared.ExpectedCursor
	completeSnapshotUploaded := !pendingTail && state.LastAckedCursor == fileInfo.Size()
	if needsFinalize && (completePrefixUploaded || completeSnapshotUploaded) {
		if err := finalizeSessionSource(cfg, prepared.GenerationID, state.LastAckedCursor, state.PrefixCheckpointHash); err != nil {
			return incrementalUploadResult{}, err
		}
		if result.Status == "rebuild_required" {
			result.Status = "uploaded"
		}
	}
	abortOnFailure = false
	readiness, err := waitForGenerationReadiness(cfg, prepared.GenerationID)
	if err != nil {
		return incrementalUploadResult{}, err
	}
	result.ContentStatus = readiness.ContentStatus
	result.ReadyForReports = readiness.ReadyForReports
	result.ErrorCode = readiness.ErrorCode
	switch {
	case readiness.ErrorCode != "":
		result.Status = "failed"
	case readiness.ReadyForReports:
		if prepared.Action == "unchanged" && result.UploadedChunks == 0 {
			result.Status = "unchanged"
		} else {
			result.Status = "ready"
		}
	default:
		result.Status = "processing"
	}
	return result, nil
}

func isNonRetryableTeamPrepareError(errorCode string) bool {
	switch strings.TrimSpace(errorCode) {
	case "TEAM_CONTEXT_CHANGED", "TEAM_SESSION_IDENTITY_CONFLICT":
		return true
	default:
		return false
	}
}

func prepareSessionSource(
	cfg *Config,
	session *SessionInfo,
	sourcePath string,
	parentSessionRef, sourceKey string,
	localSize int64,
	state localUploadState,
) (prepareSourceResult, error) {
	return prepareSessionSourceWithMode(cfg, session, sourcePath, parentSessionRef, sourceKey, localSize, state, uploadModePersonal)
}

func prepareSessionSourceWithMode(
	cfg *Config,
	session *SessionInfo,
	sourcePath string,
	parentSessionRef, sourceKey string,
	localSize int64,
	state localUploadState,
	uploadMode string,
) (prepareSourceResult, error) {
	prefixHash := ""
	if state.SourceKey == sourceKey && state.LastAckedCursor >= 0 && state.PrefixCheckpointAlgorithmVersion == prefixCheckpointAlgorithm {
		prefixHash = state.PrefixCheckpointHash
	}
	for attempt := 0; attempt < 3; attempt++ {
		requestUploadMode := ""
		if uploadMode == uploadModeTeam {
			requestUploadMode = uploadModeTeam
		}
		request := prepareBatchRequest{
			ClientVersion: Version,
			UploadMode:    requestUploadMode,
			Sessions: []prepareSessionRequest{{
				SessionRef: session.SessionRef, AgentType: normalizedAgentType(session.AgentType),
				ParentSessionRef: parentSessionRef, ForkedAt: timePointer(session.ForkedAt), ForkSource: session.ForkSource,
				Summary: session.Summary, StartedAt: timePointer(session.StartedAt),
				LastActivityAt: timePointer(session.LastActiveAt()), CWD: session.Cwd,
				ProjectName: sessionProjectDisplay(session),
				Sources: []prepareSourceRequest{{
					SourceRole: "main", SourceKey: sourceKey, LocalSize: localSize,
					PrefixCheckpointHash: prefixHash, PrefixCheckpointAlgorithmVersion: prefixCheckpointAlgorithm,
				}},
			}},
		}
		payload, _ := json.Marshal(request)
		status, body, err := doSessionSyncRequest(cfg, http.MethodPost, "/session-syncs/prepare", "application/json", bytes.NewReader(payload), defaultRequestTimeout)
		if err != nil {
			return prepareSourceResult{}, err
		}
		if status == http.StatusNotFound && sessionSyncErrorCode(body) == "SESSION_SYNC_NOT_ENABLED" {
			return prepareSourceResult{}, errSessionSyncNotEnabled
		}
		if status >= 300 {
			return prepareSourceResult{}, fmt.Errorf("prepare HTTP %d: %s", status, truncate(string(body), 300))
		}
		var response struct {
			Results []prepareSourceResult `json:"results"`
		}
		if err := json.Unmarshal(body, &response); err != nil || len(response.Results) != 1 {
			return prepareSourceResult{}, errors.New("invalid prepare response")
		}
		prepared := response.Results[0]
		if prepared.Action == "rejected" && prepared.ErrorCode == "INVALID_CHECKPOINT" && prepared.ExpectedCursor > 0 {
			hashValue, _, err := hashLocalPrefix(sourcePath, prepared.ExpectedCursor)
			if err != nil {
				return prepareSourceResult{}, err
			}
			prefixHash = hashValue
			continue
		}
		return prepared, nil
	}
	return prepareSourceResult{}, errors.New("prepare checkpoint did not converge")
}

func preflightSessionSource(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = streamSessionJSONLChunks(file, 0, 1, sessionChunkLimits{
		MaxEvents: defaultSyncChunkEvents, MaxChunkBytes: defaultSyncChunkBytes, MaxLineBytes: defaultSyncMaxLineBytes,
	}, func(localSessionChunk) error { return nil })
	if err != nil {
		return fmt.Errorf("session source preflight failed before prepare: %w", err)
	}
	return nil
}

func uploadIncrementalChunk(cfg *Config, generationID string, chunk localSessionChunk) error {
	eventStart, eventEnd := localChunkEventRange(chunk.Content)
	metadata := map[string]any{"chunks": []map[string]any{{
		"generation_id": generationID, "file_field": "chunk_0", "content_encoding": "identity",
		"uncompressed_size": len(chunk.Content), "start_cursor": chunk.StartCursor, "end_cursor": chunk.EndCursor,
		"start_line": chunk.StartLine, "end_line": chunk.EndLine, "content_sha256": chunk.ContentSHA256,
		"event_start_at": eventStart, "event_end_at": eventEnd,
	}}}
	metadataJSON, _ := json.Marshal(metadata)
	for attempt := 0; attempt < 3; attempt++ {
		body, contentType := streamChunkMultipartBody(metadataJSON, chunk.Content)
		status, responseBody, requestErr := doSessionSyncRequest(
			cfg, http.MethodPost, "/session-chunks/batch", contentType, body, sessionChunkRequestTimeout,
		)
		_ = body.Close()
		if requestErr != nil {
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
				continue
			}
			return requestErr
		}
		if status >= 500 && attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
			continue
		}
		if status >= 300 {
			return fmt.Errorf("chunk HTTP %d: %s", status, truncate(string(responseBody), 300))
		}
		var response struct {
			Results []struct {
				Status         string `json:"status"`
				AckedCursor    int64  `json:"acked_cursor"`
				ExpectedCursor int64  `json:"expected_cursor"`
				ErrorCode      string `json:"error_code"`
				NextAction     string `json:"next_action"`
			} `json:"results"`
		}
		if err := json.Unmarshal(responseBody, &response); err != nil || len(response.Results) != 1 {
			return errors.New("invalid chunk response")
		}
		result := response.Results[0]
		if (result.Status == "accepted" || result.Status == "duplicate") && result.AckedCursor == chunk.EndCursor {
			return nil
		}
		return fmt.Errorf("chunk rejected: %s %s %s", result.ErrorCode, result.NextAction, result.Status)
	}
	return errors.New("chunk upload retries exhausted")
}

func streamChunkMultipartBody(metadataJSON, content []byte) (*io.PipeReader, string) {
	reader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)
	contentType := multipartWriter.FormDataContentType()
	go func() {
		var writeErr error
		if writeErr = multipartWriter.WriteField("metadata", string(metadataJSON)); writeErr == nil {
			var part io.Writer
			part, writeErr = multipartWriter.CreateFormFile("chunk_0", "chunk.jsonl")
			if writeErr == nil {
				_, writeErr = io.Copy(part, bytes.NewReader(content))
			}
		}
		if closeErr := multipartWriter.Close(); writeErr == nil {
			writeErr = closeErr
		}
		_ = pipeWriter.CloseWithError(writeErr)
	}()
	return reader, contentType
}

func finalizeSessionSource(cfg *Config, generationID string, cursor int64, prefixHash string) error {
	payload, _ := json.Marshal(map[string]any{
		"declared_end_cursor": cursor, "prefix_checkpoint_hash": prefixHash,
		"prefix_checkpoint_algorithm_version": prefixCheckpointAlgorithm,
	})
	status, body, err := doSessionSyncRequest(
		cfg, http.MethodPost, "/session-syncs/"+generationID+"/finalize", "application/json", bytes.NewReader(payload), defaultRequestTimeout,
	)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("finalize HTTP %d: %s", status, truncate(string(body), 300))
	}
	return nil
}

type generationReadiness struct {
	GenerationID            string `json:"generation_id"`
	GenerationStatus        string `json:"generation_status"`
	ContentStatus           string `json:"content_status"`
	ContentProjectionStatus string `json:"content_projection_status"`
	ReadyForReports         bool   `json:"ready_for_reports"`
	ErrorCode               string `json:"error_code"`
	ErrorMessage            string `json:"error_message"`
}

func waitForGenerationReadiness(cfg *Config, generationID string) (generationReadiness, error) {
	deadline := time.Now().Add(sessionReadinessTimeout)
	for {
		status, body, err := doSessionSyncRequest(
			cfg, http.MethodGet, "/session-syncs/"+generationID+"/status", "", nil, defaultRequestTimeout,
		)
		if err != nil {
			return generationReadiness{}, err
		}
		if status >= 300 {
			return generationReadiness{}, fmt.Errorf("readiness HTTP %d: %s", status, truncate(string(body), 300))
		}
		var readiness generationReadiness
		if err := json.Unmarshal(body, &readiness); err != nil {
			return generationReadiness{}, errors.New("invalid readiness response")
		}
		if readiness.ReadyForReports || readiness.ErrorCode != "" ||
			readiness.ContentProjectionStatus == "failed" || readiness.ContentStatus == "upload_failed" {
			return readiness, nil
		}
		if !time.Now().Before(deadline) {
			return readiness, nil
		}
		time.Sleep(sessionReadinessPollInterval)
	}
}

func abortSessionSource(cfg *Config, generationID string) error {
	status, body, err := doSessionSyncRequest(
		cfg, http.MethodPost, "/session-syncs/"+generationID+"/abort", "application/json",
		bytes.NewReader([]byte(`{}`)), defaultRequestTimeout,
	)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("abort HTTP %d: %s", status, truncate(string(body), 300))
	}
	return nil
}

func doSessionSyncRequest(
	cfg *Config,
	method, path, contentType string,
	body io.Reader,
	timeout time.Duration,
) (int, []byte, error) {
	request, err := http.NewRequestWithContext(context.Background(), method, apiBaseURL(cfg)+path, body)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Authorization", "Bearer "+cfg.Token)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	return response.StatusCode, responseBody, err
}

func sessionSyncErrorCode(body []byte) string {
	var response struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &response)
	return response.Code
}

func uploadStatePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aida", uploadStateFileName)
}

func loadUploadStates() (*uploadStateFile, error) {
	state := &uploadStateFile{Version: uploadStateVersion, Sources: map[string]localUploadState{}}
	content, err := os.ReadFile(uploadStatePath())
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(content, state); err != nil {
		return nil, fmt.Errorf("read upload state: %w", err)
	}
	if state.Version != uploadStateVersion {
		if state.Version != 1 {
			return nil, fmt.Errorf("unsupported upload state version %d", state.Version)
		}
		migrated := make(map[string]localUploadState, len(state.Sources))
		for key, value := range state.Sources {
			migrated[uploadModePersonal+"\n"+key] = value
		}
		state.Version = uploadStateVersion
		state.Sources = migrated
		if err := saveUploadStates(state); err != nil {
			return nil, err
		}
	}
	if state.Sources == nil {
		state.Sources = map[string]localUploadState{}
	}
	return state, nil
}

func saveUploadStates(state *uploadStateFile) error {
	if state == nil {
		return errors.New("upload state is required")
	}
	path := uploadStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".upload-state-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func uploadStateKey(agentType, sessionRef, sourceKey string) string {
	return uploadStateKeyForMode(uploadModePersonal, agentType, sessionRef, sourceKey)
}

func uploadStateKeyForMode(uploadMode, agentType, sessionRef, sourceKey string) string {
	if uploadMode == "" {
		uploadMode = uploadModePersonal
	}
	return uploadMode + "\n" + agentType + "\n" + sessionRef + "\n" + sourceKey
}

func normalizedAgentType(value string) string {
	if strings.TrimSpace(value) == "" || value == "claude" {
		return "claude_code"
	}
	return value
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = value.UTC()
	return &value
}

func initializeLocalPrefix(file *os.File, size int64) (hash.Hash, int64, error) {
	if size < 0 {
		return nil, 0, errors.New("prefix size must not be negative")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, 0, err
	}
	hasher := sha256.New()
	counter := &newlineCounter{}
	if size > 0 {
		written, err := io.CopyN(io.MultiWriter(hasher, counter), file, size)
		if err != nil || written != size {
			return nil, 0, fmt.Errorf("read local prefix: %w", firstError(err, io.ErrUnexpectedEOF))
		}
	}
	return hasher, counter.lines, nil
}

func hashLocalPrefix(path string, size int64) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hasher, lines, err := initializeLocalPrefix(file, size)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), lines, nil
}

type newlineCounter struct {
	lines int64
}

func (c *newlineCounter) Write(content []byte) (int, error) {
	c.lines += int64(bytes.Count(content, []byte{'\n'}))
	return len(content), nil
}

func firstError(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}

func localChunkEventRange(content []byte) (*time.Time, *time.Time) {
	var first, last time.Time
	for _, line := range bytes.Split(content, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var event struct {
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal(line, &event) != nil || event.Timestamp == "" {
			continue
		}
		occurredAt, err := time.Parse(time.RFC3339Nano, event.Timestamp)
		if err != nil {
			continue
		}
		if first.IsZero() || occurredAt.Before(first) {
			first = occurredAt
		}
		if last.IsZero() || occurredAt.After(last) {
			last = occurredAt
		}
	}
	return timePointer(first), timePointer(last)
}
