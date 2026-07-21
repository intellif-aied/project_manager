package canonicalupload

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aidashboard/daemon/internal/sessionadapter"
)

const prefixCheckpointAlgorithm = "sha256-prefix-v1"

type Config struct {
	BaseURL       string
	Token         string
	ClientVersion string
	HTTPClient    *http.Client
}

type Uploader struct {
	baseURL       string
	token         string
	clientVersion string
	client        *http.Client
}

type PreparedSource struct {
	SessionRef       string `json:"session_ref"`
	SourceKey        string `json:"source_key"`
	GenerationID     string `json:"generation_id"`
	GenerationStatus string `json:"generation_status"`
	ExpectedCursor   int64  `json:"expected_cursor"`
	PrefixHash       string `json:"prefix_checkpoint_hash"`
	Action           string `json:"action"`
	ErrorCode        string `json:"error_code"`
	NextAction       string `json:"next_action"`
}

type UploadedSource struct {
	SessionRef     string
	GenerationID   string
	UploadedChunks int
	Finalized      bool
}

func New(config Config) (*Uploader, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" || strings.TrimSpace(config.Token) == "" || strings.TrimSpace(config.ClientVersion) == "" {
		return nil, errors.New("base URL, token, and client version are required")
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Uploader{baseURL: baseURL, token: config.Token, clientVersion: config.ClientVersion, client: client}, nil
}

func (uploader *Uploader) PrepareFamily(ctx context.Context, family []sessionadapter.MaterializedSession) ([]PreparedSource, error) {
	return uploader.prepareFamily(ctx, family, nil)
}

func (uploader *Uploader) prepareFamily(ctx context.Context, family []sessionadapter.MaterializedSession, prefixHashes map[string]string) ([]PreparedSource, error) {
	if uploader == nil || len(family) == 0 || len(family) > 100 {
		return nil, errors.New("canonical family must contain 1 to 100 sessions")
	}
	type sourceRequest struct {
		SourceRole                       string `json:"source_role"`
		SourceKey                        string `json:"source_key"`
		LocalSize                        int64  `json:"local_size"`
		PrefixCheckpointHash             string `json:"prefix_checkpoint_hash"`
		PrefixCheckpointAlgorithmVersion string `json:"prefix_checkpoint_algorithm_version"`
		SourceFormat                     string `json:"source_format"`
		IngestionMetadata                any    `json:"ingestion_metadata"`
	}
	type sessionRequest struct {
		SessionRef       string          `json:"session_ref"`
		AgentType        string          `json:"agent_type"`
		ParentSessionRef string          `json:"parent_session_ref,omitempty"`
		ForkedAt         *time.Time      `json:"forked_at,omitempty"`
		ForkSource       string          `json:"fork_source,omitempty"`
		Summary          string          `json:"summary,omitempty"`
		StartedAt        *time.Time      `json:"started_at,omitempty"`
		LastActivityAt   *time.Time      `json:"last_activity_at,omitempty"`
		CWD              string          `json:"cwd,omitempty"`
		ProjectName      string          `json:"project_name,omitempty"`
		Sources          []sourceRequest `json:"sources"`
	}
	request := struct {
		ClientVersion string           `json:"client_version"`
		Sessions      []sessionRequest `json:"sessions"`
	}{ClientVersion: uploader.clientVersion, Sessions: make([]sessionRequest, 0, len(family))}
	seen := make(map[string]struct{}, len(family))
	for _, materialized := range family {
		descriptor := materialized.Descriptor
		clientType := strings.TrimSpace(string(descriptor.ClientType))
		sessionRef := strings.TrimSpace(descriptor.NativeSessionRef)
		if clientType == "" || sessionRef == "" {
			return nil, errors.New("canonical client type and native session ref are required")
		}
		identity := clientType + "\n" + sessionRef
		if _, exists := seen[identity]; exists {
			return nil, fmt.Errorf("duplicate canonical session %q", sessionRef)
		}
		seen[identity] = struct{}{}
		session := sessionRequest{
			SessionRef: sessionRef, AgentType: clientType, ParentSessionRef: strings.TrimSpace(descriptor.ParentRef),
			ForkedAt: timePointer(descriptor.ForkedAt), ForkSource: descriptor.ForkSource,
			Summary: descriptor.Summary, StartedAt: timePointer(descriptor.StartedAt),
			LastActivityAt: timePointer(descriptor.LastActivityAt), CWD: descriptor.CWD, ProjectName: descriptor.ProjectName,
		}
		if materialized.CanonicalPath != "" {
			if materialized.SourceFormat != "aida_event_v1" {
				return nil, fmt.Errorf("canonical session %q has unsupported source format %q", sessionRef, materialized.SourceFormat)
			}
			info, err := os.Stat(materialized.CanonicalPath)
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("canonical path for %q is not a regular file", sessionRef)
			}
			session.Sources = []sourceRequest{{
				SourceRole: "main", SourceKey: clientType + ":" + sessionRef + ":main", LocalSize: info.Size(),
				PrefixCheckpointHash:             prefixHashes[clientType+":"+sessionRef+":main"],
				PrefixCheckpointAlgorithmVersion: prefixCheckpointAlgorithm, SourceFormat: materialized.SourceFormat,
				IngestionMetadata: map[string]string{
					"adapter_version":       materialized.AdapterVersion,
					"native_client_version": descriptor.NativeVersion,
					"usage_capability":      string(materialized.UsageCapability),
				},
			}}
		}
		request.Sessions = append(request.Sessions, session)
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, uploader.baseURL+"/canonical-session-syncs/prepare", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+uploader.token)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := uploader.client.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 300 {
		return nil, fmt.Errorf("canonical prepare HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		Results []PreparedSource `json:"results"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode canonical prepare response: %w", err)
	}
	return decoded.Results, nil
}

func (uploader *Uploader) UploadFamily(ctx context.Context, family []sessionadapter.MaterializedSession) ([]UploadedSource, error) {
	prefixes := map[string]string{}
	var prepared []PreparedSource
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		prepared, err = uploader.prepareFamily(ctx, family, prefixes)
		if err != nil {
			return nil, err
		}
		converged := true
		for _, source := range prepared {
			if source.Action == "rejected" && source.ErrorCode == "INVALID_CHECKPOINT" && source.ExpectedCursor > 0 {
				path, ok := canonicalPathForSource(family, source.SourceKey)
				if !ok {
					return nil, fmt.Errorf("canonical source %q is not materialized", source.SourceKey)
				}
				hash, hashErr := hashPrefix(path, source.ExpectedCursor)
				if hashErr != nil {
					return nil, hashErr
				}
				prefixes[source.SourceKey] = hash
				converged = false
			}
		}
		if converged {
			break
		}
		if attempt == 2 {
			return nil, errors.New("canonical prepare checkpoint did not converge")
		}
	}
	results := make([]UploadedSource, 0, len(prepared))
	for _, source := range prepared {
		if source.Action == "rejected" || source.Action == "content_cleared" {
			return nil, fmt.Errorf("canonical prepare %s: %s %s", source.Action, source.ErrorCode, source.NextAction)
		}
		path, ok := canonicalPathForSource(family, source.SourceKey)
		if !ok {
			return nil, fmt.Errorf("canonical source %q is not materialized", source.SourceKey)
		}
		result, uploadErr := uploader.uploadSource(ctx, path, source)
		if uploadErr != nil {
			if source.GenerationStatus == "staging" {
				_ = uploader.postJSON(ctx, "/session-syncs/"+source.GenerationID+"/abort", []byte(`{}`), nil)
			}
			return results, uploadErr
		}
		results = append(results, result)
	}
	return results, nil
}

func canonicalPathForSource(family []sessionadapter.MaterializedSession, sourceKey string) (string, bool) {
	for _, m := range family {
		key := string(m.Descriptor.ClientType) + ":" + m.Descriptor.NativeSessionRef + ":main"
		if key == sourceKey && m.CanonicalPath != "" {
			return m.CanonicalPath, true
		}
	}
	return "", false
}

func hashPrefix(path string, end int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if end < 0 || end > info.Size() {
		return "", errors.New("canonical checkpoint exceeds local source")
	}
	hasher := sha256.New()
	written, err := io.CopyN(hasher, file, end)
	if err != nil && !(errors.Is(err, io.EOF) && written == end) {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (uploader *Uploader) uploadSource(ctx context.Context, path string, prepared PreparedSource) (UploadedSource, error) {
	file, err := os.Open(path)
	if err != nil {
		return UploadedSource{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return UploadedSource{}, err
	}
	if prepared.GenerationID == "" || prepared.ExpectedCursor < 0 || prepared.ExpectedCursor > info.Size() {
		return UploadedSource{}, errors.New("server returned an invalid canonical checkpoint")
	}
	prefixHash, err := hashPrefix(path, prepared.ExpectedCursor)
	if err != nil {
		return UploadedSource{}, err
	}
	if prefixHash != prepared.PrefixHash {
		return UploadedSource{}, errors.New("canonical source changed after prepare")
	}
	if _, err = file.Seek(prepared.ExpectedCursor, io.SeekStart); err != nil {
		return UploadedSource{}, err
	}
	line := int64(1)
	if prepared.ExpectedCursor > 0 {
		prefix := make([]byte, prepared.ExpectedCursor)
		prefixFile, openErr := os.Open(path)
		if openErr != nil {
			return UploadedSource{}, openErr
		}
		_, readErr := io.ReadFull(prefixFile, prefix)
		_ = prefixFile.Close()
		if readErr != nil {
			return UploadedSource{}, readErr
		}
		line += int64(bytes.Count(prefix, []byte{'\n'}))
	}
	reader := bufio.NewReaderSize(file, 64<<10)
	cursor := prepared.ExpectedCursor
	hasher := sha256.New()
	whole, openErr := os.Open(path)
	if openErr != nil {
		return UploadedSource{}, openErr
	}
	_, err = io.Copy(hasher, whole)
	_ = whole.Close()
	if err != nil {
		return UploadedSource{}, err
	}
	finalHash := hex.EncodeToString(hasher.Sum(nil))
	result := UploadedSource{SessionRef: prepared.SessionRef, GenerationID: prepared.GenerationID}
	for cursor < info.Size() {
		var chunk bytes.Buffer
		start := cursor
		startLine := line
		for chunk.Len() < 8<<20 {
			part, readErr := reader.ReadBytes('\n')
			if len(part) > 0 {
				chunk.Write(part)
				cursor += int64(len(part))
				line++
			}
			if errors.Is(readErr, io.EOF) {
				if len(part) > 0 && !bytes.HasSuffix(part, []byte{'\n'}) {
					return UploadedSource{}, errors.New("canonical source has an incomplete JSONL tail")
				}
				break
			}
			if readErr != nil {
				return UploadedSource{}, readErr
			}
			if chunk.Len() >= 8<<20 {
				break
			}
		}
		if chunk.Len() == 0 {
			break
		}
		if err = uploader.uploadChunk(ctx, prepared.GenerationID, start, cursor, startLine, line-1, chunk.Bytes()); err != nil {
			return UploadedSource{}, err
		}
		result.UploadedChunks++
	}
	if cursor != info.Size() {
		return UploadedSource{}, errors.New("canonical upload did not reach local high water")
	}
	if result.UploadedChunks > 0 || prepared.GenerationStatus == "staging" {
		payload, _ := json.Marshal(map[string]any{"declared_end_cursor": cursor, "prefix_checkpoint_hash": finalHash, "prefix_checkpoint_algorithm_version": prefixCheckpointAlgorithm})
		if err = uploader.postJSON(ctx, "/session-syncs/"+prepared.GenerationID+"/finalize", payload, nil); err != nil {
			return UploadedSource{}, err
		}
		result.Finalized = true
	}
	return result, nil
}

func (uploader *Uploader) uploadChunk(ctx context.Context, generationID string, start, end, startLine, endLine int64, content []byte) error {
	metadata, _ := json.Marshal(map[string]any{"chunks": []map[string]any{{"generation_id": generationID, "file_field": "chunk_0", "content_encoding": "identity", "uncompressed_size": len(content), "start_cursor": start, "end_cursor": end, "start_line": startLine, "end_line": endLine, "content_sha256": hashBytes(content)}}})
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("metadata", string(metadata)); err != nil {
		return err
	}
	part, err := writer.CreateFormFile("chunk_0", "canonical.jsonl")
	if err != nil {
		return err
	}
	if _, err = part.Write(content); err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploader.baseURL+"/session-chunks/batch", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+uploader.token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := uploader.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if response.StatusCode >= 300 {
		return fmt.Errorf("canonical chunk HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var decoded struct {
		Results []struct {
			Status      string `json:"status"`
			AckedCursor int64  `json:"acked_cursor"`
		} `json:"results"`
	}
	if json.Unmarshal(responseBody, &decoded) != nil || len(decoded.Results) != 1 || (decoded.Results[0].Status != "accepted" && decoded.Results[0].Status != "duplicate") || decoded.Results[0].AckedCursor != end {
		return errors.New("canonical chunk was not acknowledged")
	}
	return nil
}

func (uploader *Uploader) postJSON(ctx context.Context, path string, payload []byte, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploader.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+uploader.token)
	req.Header.Set("Content-Type", "application/json")
	response, err := uploader.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if readErr != nil {
		return readErr
	}
	if response.StatusCode >= 300 {
		return fmt.Errorf("canonical request %s HTTP %d: %s", path, response.StatusCode, strings.TrimSpace(string(body)))
	}
	if target != nil {
		return json.Unmarshal(body, target)
	}
	return nil
}

func hashBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = value.UTC()
	return &value
}
