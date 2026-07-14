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
	uploadStateVersion         = 1
	prefixCheckpointAlgorithm  = "sha256-prefix-v1"
	defaultSyncChunkEvents     = 500
	defaultSyncChunkBytes      = 4 << 20
	defaultSyncMaxLineBytes    = 8 << 20
	sessionChunkRequestTimeout = 5 * time.Minute
)

var errSessionSyncNotEnabled = errors.New("incremental session sync is not enabled")

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
	Sessions      []prepareSessionRequest `json:"sessions"`
}

type prepareSessionRequest struct {
	SessionRef       string                 `json:"session_ref"`
	AgentType        string                 `json:"agent_type"`
	ParentSessionRef string                 `json:"parent_session_ref,omitempty"`
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
	SessionRef     string
	Status         string
	UploadedChunks int
	PendingTail    bool
}

func uploadSessionGroupIncremental(cfg *Config, items []sessionWithFile, parentSessionRef string) ([]incrementalUploadResult, error) {
	states, err := loadUploadStates()
	if err != nil {
		return nil, err
	}
	results := make([]incrementalUploadResult, 0, len(items))
	for index, item := range items {
		parentRef := ""
		if index > 0 {
			parentRef = parentSessionRef
		}
		result, err := uploadSessionSourceIncremental(cfg, states, item, parentRef)
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
) (incrementalUploadResult, error) {
	session := item.info
	if session == nil || session.SessionRef == "" || item.filePath == "" {
		return incrementalUploadResult{}, errors.New("session metadata and source file are required")
	}
	fileInfo, err := os.Stat(item.filePath)
	if err != nil {
		return incrementalUploadResult{}, err
	}
	agentType := normalizedAgentType(session.AgentType)
	sourceKey := fmt.Sprintf("%s:%s:main", agentType, session.SessionRef)
	stateKey := uploadStateKey(agentType, session.SessionRef, sourceKey)
	state := states.Sources[stateKey]
	prepared, err := prepareSessionSource(cfg, session, item.filePath, parentSessionRef, sourceKey, fileInfo.Size(), state)
	if err != nil {
		return incrementalUploadResult{}, err
	}
	result := incrementalUploadResult{SessionRef: session.SessionRef, Status: prepared.Action}
	if prepared.Action == "content_cleared" {
		return result, nil
	}
	if prepared.Action == "rejected" {
		return incrementalUploadResult{}, fmt.Errorf("prepare rejected: %s %s", prepared.ErrorCode, prepared.NextAction)
	}
	if prepared.GenerationID == "" || prepared.ExpectedCursor < 0 || prepared.ExpectedCursor > fileInfo.Size() {
		return incrementalUploadResult{}, errors.New("server returned an invalid generation checkpoint")
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

	latestInfo, err := os.Stat(item.filePath)
	if err != nil {
		return incrementalUploadResult{}, err
	}
	snapshotUnchanged := latestInfo.Size() == fileInfo.Size() && latestInfo.ModTime().Equal(fileInfo.ModTime())
	needsFinalize := prepared.GenerationStatus == "staging" || prepared.Action == "rebuild_required" || prepared.Action == "restore"
	if needsFinalize && !pendingTail && snapshotUnchanged && state.LastAckedCursor == fileInfo.Size() {
		if err := finalizeSessionSource(cfg, prepared.GenerationID, state.LastAckedCursor, state.PrefixCheckpointHash); err != nil {
			return incrementalUploadResult{}, err
		}
		if result.Status == "rebuild_required" {
			result.Status = "uploaded"
		}
	}
	return result, nil
}

func prepareSessionSource(
	cfg *Config,
	session *SessionInfo,
	sourcePath string,
	parentSessionRef, sourceKey string,
	localSize int64,
	state localUploadState,
) (prepareSourceResult, error) {
	prefixHash := ""
	if state.SourceKey == sourceKey && state.LastAckedCursor >= 0 && state.PrefixCheckpointAlgorithmVersion == prefixCheckpointAlgorithm {
		prefixHash = state.PrefixCheckpointHash
	}
	for attempt := 0; attempt < 3; attempt++ {
		request := prepareBatchRequest{
			ClientVersion: Version,
			Sessions: []prepareSessionRequest{{
				SessionRef: session.SessionRef, AgentType: normalizedAgentType(session.AgentType),
				ParentSessionRef: parentSessionRef, StartedAt: timePointer(session.StartedAt),
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
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("metadata", string(metadataJSON)); err != nil {
			return err
		}
		part, err := writer.CreateFormFile("chunk_0", "chunk.jsonl")
		if err != nil {
			return err
		}
		if _, err := part.Write(chunk.Content); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		status, responseBody, requestErr := doSessionSyncRequest(
			cfg, http.MethodPost, "/session-chunks/batch", writer.FormDataContentType(), &body, sessionChunkRequestTimeout,
		)
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

func doSessionSyncRequest(
	cfg *Config,
	method, path, contentType string,
	body io.Reader,
	timeout time.Duration,
) (int, []byte, error) {
	request, err := http.NewRequestWithContext(context.Background(), method, cfg.APIURL+path, body)
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
		return nil, fmt.Errorf("unsupported upload state version %d", state.Version)
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
	return agentType + "\n" + sessionRef + "\n" + sourceKey
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
