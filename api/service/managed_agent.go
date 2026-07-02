package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aidashboard/api/model"
)

const (
	ManagedAgentNotConfiguredCode = "MANAGED_AGENT_NOT_CONFIGURED"
	ManagedAgentUnreachableCode   = "MANAGED_AGENT_UNREACHABLE"
	ManagedAgentUpstreamErrorCode = "MANAGED_AGENT_UPSTREAM_ERROR"
)

type ManagedAgentError struct {
	Code       string
	Message    string
	StatusCode int
}

func (e *ManagedAgentError) Error() string {
	return e.Message
}

type ManagedAgentClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewManagedAgentClient(baseURL, token string) *ManagedAgentClient {
	return &ManagedAgentClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *ManagedAgentClient) Configured() bool {
	return c != nil && c.baseURL != "" && c.token != ""
}

func (c *ManagedAgentClient) WithToken(token string) *ManagedAgentClient {
	token = strings.TrimSpace(token)
	if c == nil || token == "" {
		return c
	}
	clone := *c
	clone.token = token
	return &clone
}

func ValidateManagedScope(scope string) string {
	switch model.ManagedScope(scope) {
	case model.ManagedScopeMine, model.ManagedScopePublic, model.ManagedScopeAll:
		return scope
	default:
		return string(model.ManagedScopeMine)
	}
}

func urlPathEscape(value string) string {
	return url.PathEscape(strings.TrimSpace(value))
}

func (c *ManagedAgentClient) ListSkills(ctx context.Context, scope string) (*model.ListManagedSkillsResponse, error) {
	var out model.ListManagedSkillsResponse
	if err := c.do(ctx, http.MethodGet, "/api/skill/list?scope="+ValidateManagedScope(scope), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type CreateManagedSkillRequest struct {
	Slug        string `json:"slug"`
	Version     string `json:"version"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	SkillMD     string `json:"skill_md"`
}

type CreateManagedSkillResponse struct {
	SkillID     string `json:"skill_id"`
	Owner       string `json:"owner,omitempty"`
	PublishedBy string `json:"published_by,omitempty"`
	Slug        string `json:"slug"`
	Version     string `json:"version"`
	SHA256      string `json:"sha256,omitempty"`
}

func (c *ManagedAgentClient) CreateSkill(ctx context.Context, req CreateManagedSkillRequest) (*CreateManagedSkillResponse, error) {
	fields := map[string]string{
		"slug":        req.Slug,
		"version":     req.Version,
		"name":        req.Name,
		"description": req.Description,
		"skill_md":    req.SkillMD,
	}
	var out CreateManagedSkillResponse
	if err := c.doMultipart(ctx, http.MethodPost, "/api/skill", fields, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type ArchiveManagedSkillRequest struct {
	Archived bool `json:"archived"`
}

func (c *ManagedAgentClient) ArchiveSkill(ctx context.Context, slug, version string, archived bool) (map[string]any, error) {
	var out map[string]any
	if err := c.do(ctx, http.MethodPost, "/api/skill/"+urlPathEscape(slug)+"/"+urlPathEscape(version)+"/archive", ArchiveManagedSkillRequest{Archived: archived}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *ManagedAgentClient) DeleteSkill(ctx context.Context, slug, version string) (map[string]any, error) {
	var out map[string]any
	if err := c.do(ctx, http.MethodDelete, "/api/skill/"+urlPathEscape(slug)+"/"+urlPathEscape(version), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *ManagedAgentClient) GetSkillFile(ctx context.Context, owner, slug, version, path string) ([]byte, error) {
	return c.doRaw(ctx, http.MethodGet, "/api/skill/"+urlPathEscape(owner)+"/"+urlPathEscape(slug)+"/"+urlPathEscape(version)+"/file?path="+url.QueryEscape(path), nil)
}

func (c *ManagedAgentClient) ListMCPEntries(ctx context.Context, scope string) (*model.ListManagedMCPEntriesResponse, error) {
	var out model.ListManagedMCPEntriesResponse
	if err := c.do(ctx, http.MethodGet, "/api/mcp/list?scope="+ValidateManagedScope(scope), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ManagedAgentClient) CreateMCPEntry(ctx context.Context, req model.CreateManagedMCPEntryRequest) (*model.ManagedMCPEntry, error) {
	var out model.ManagedMCPEntry
	if err := c.do(ctx, http.MethodPost, "/api/mcp", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type ArchiveManagedMCPEntryRequest struct {
	Archived bool `json:"archived"`
}

func (c *ManagedAgentClient) ArchiveMCPEntry(ctx context.Context, slug, version string, archived bool) (map[string]any, error) {
	var out map[string]any
	if err := c.do(ctx, http.MethodPost, "/api/mcp/"+urlPathEscape(slug)+"/"+urlPathEscape(version)+"/archive", ArchiveManagedMCPEntryRequest{Archived: archived}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *ManagedAgentClient) DeleteMCPEntry(ctx context.Context, slug, version string) (map[string]any, error) {
	var out map[string]any
	if err := c.do(ctx, http.MethodDelete, "/api/mcp/"+urlPathEscape(slug)+"/"+urlPathEscape(version), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *ManagedAgentClient) ListMyAgents(ctx context.Context) (*model.ListManagedAgentsResponse, error) {
	var out model.ListManagedAgentsResponse
	if err := c.do(ctx, http.MethodGet, "/api/my/agents", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ManagedAgentClient) CreateMyAgent(ctx context.Context, req model.UpsertManagedAgentRequest) (*model.UpsertManagedAgentResponse, error) {
	var out model.UpsertManagedAgentResponse
	if err := c.do(ctx, http.MethodPost, "/api/my/agents", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ManagedAgentClient) UpdateMyAgent(ctx context.Context, agentID string, req model.UpsertManagedAgentRequest) (*model.UpsertManagedAgentResponse, error) {
	var out model.UpsertManagedAgentResponse
	if err := c.do(ctx, http.MethodPut, "/api/my/agents/"+urlPathEscape(agentID), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type ArchiveManagedAgentRequest struct {
	Archived bool `json:"archived"`
}

func (c *ManagedAgentClient) ArchiveMyAgent(ctx context.Context, agentID string, archived bool) (map[string]any, error) {
	var out map[string]any
	if err := c.do(ctx, http.MethodPost, "/api/my/agents/"+urlPathEscape(agentID)+"/archive", ArchiveManagedAgentRequest{Archived: archived}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type CreateManagedCredentialRequest struct {
	Name        string            `json:"name"`
	Kind        string            `json:"kind,omitempty"`
	Description string            `json:"description,omitempty"`
	Value       string            `json:"value"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type CreateManagedCredentialResponse struct {
	CredentialID string `json:"credential_id"`
}

func (c *ManagedAgentClient) ListCredentials(ctx context.Context) (*model.ListManagedCredentialsResponse, error) {
	var out model.ListManagedCredentialsResponse
	if err := c.do(ctx, http.MethodGet, "/api/credential/list", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ManagedAgentClient) CreateCredential(ctx context.Context, req CreateManagedCredentialRequest) (*CreateManagedCredentialResponse, error) {
	var out CreateManagedCredentialResponse
	if err := c.do(ctx, http.MethodPost, "/api/credential", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ManagedAgentClient) DeleteCredential(ctx context.Context, credentialID string) (map[string]any, error) {
	var out map[string]any
	if err := c.do(ctx, http.MethodDelete, "/api/credential/"+urlPathEscape(credentialID), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type CreateManagedSessionRequest struct {
	AgentID             string            `json:"agent_id"`
	ModelID             string            `json:"model_id,omitempty"`
	StartPromptValues   map[string]string `json:"start_prompt_values,omitempty"`
	Message             string            `json:"message,omitempty"`
	CredentialOverrides map[string]string `json:"credential_overrides,omitempty"`
}

type CreateManagedSessionResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	ModelID   string `json:"model_id,omitempty"`
}

func (c *ManagedAgentClient) CreateSession(ctx context.Context, req CreateManagedSessionRequest) (*CreateManagedSessionResponse, error) {
	var out CreateManagedSessionResponse
	if err := c.do(ctx, http.MethodPost, "/api/session", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type SubmitManagedTaskRequest struct {
	AgentID    string            `json:"agent_id"`
	ModelID    string            `json:"model_id,omitempty"`
	Params     map[string]string `json:"params,omitempty"`
	InputFiles []string          `json:"input_files,omitempty"`
}

type SubmitManagedTaskResponse struct {
	TaskID  string `json:"task_id"`
	Status  string `json:"status"`
	ModelID string `json:"model_id,omitempty"`
}

type ManagedTaskStatus struct {
	TaskID         string          `json:"task_id"`
	AgentID        string          `json:"agent_id"`
	AgentVersionID int             `json:"agent_version_id"`
	ModelID        string          `json:"model_id"`
	Status         string          `json:"status"`
	Result         string          `json:"result"`
	Error          string          `json:"error"`
	Progress       string          `json:"progress"`
	FinishedAt     int64           `json:"finished_at"`
	Raw            json.RawMessage `json:"-"`
}

func (c *ManagedAgentClient) SubmitTask(ctx context.Context, req SubmitManagedTaskRequest) (*SubmitManagedTaskResponse, error) {
	var out SubmitManagedTaskResponse
	if err := c.do(ctx, http.MethodPost, "/api/task/submit", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ManagedAgentClient) GetTaskResult(ctx context.Context, taskID string) (*ManagedTaskStatus, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/task/"+taskID+"/result", nil, &raw); err != nil {
		return nil, err
	}
	var out ManagedTaskStatus
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	out.Raw = raw
	return &out, nil
}

func (c *ManagedAgentClient) GetTaskStatus(ctx context.Context, taskID string) (*ManagedTaskStatus, error) {
	var out ManagedTaskStatus
	if err := c.do(ctx, http.MethodGet, "/api/task/"+taskID+"/status", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ManagedAgentClient) do(ctx context.Context, method, path string, in any, out any) error {
	if !c.Configured() {
		return &ManagedAgentError{
			Code:    ManagedAgentNotConfiguredCode,
			Message: "managed agent platform is not configured",
		}
	}

	var body io.Reader
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return &ManagedAgentError{
			Code:    ManagedAgentUnreachableCode,
			Message: "managed agent platform is unreachable: " + err.Error(),
		}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return &ManagedAgentError{
			Code:    ManagedAgentUnreachableCode,
			Message: "managed agent platform is unreachable: " + err.Error(),
		}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return &ManagedAgentError{
			Code:       ManagedAgentUpstreamErrorCode,
			Message:    fmt.Sprintf("managed agent platform returned HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 240)),
			StatusCode: resp.StatusCode,
		}
	}
	if out == nil {
		return nil
	}
	if raw, ok := out.(*json.RawMessage); ok {
		*raw = append((*raw)[0:0], respBody...)
		return nil
	}
	if len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

func (c *ManagedAgentClient) doRaw(ctx context.Context, method, path string, in any) ([]byte, error) {
	if !c.Configured() {
		return nil, &ManagedAgentError{
			Code:    ManagedAgentNotConfiguredCode,
			Message: "managed agent platform is not configured",
		}
	}

	var body io.Reader
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, &ManagedAgentError{
			Code:    ManagedAgentUnreachableCode,
			Message: "managed agent platform is unreachable: " + err.Error(),
		}
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Authorization", "Bearer "+c.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &ManagedAgentError{
			Code:    ManagedAgentUnreachableCode,
			Message: "managed agent platform is unreachable: " + err.Error(),
		}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, &ManagedAgentError{
			Code:       ManagedAgentUpstreamErrorCode,
			Message:    fmt.Sprintf("managed agent platform returned HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 240)),
			StatusCode: resp.StatusCode,
		}
	}
	return respBody, nil
}

func (c *ManagedAgentClient) doMultipart(ctx context.Context, method, path string, fields map[string]string, out any) error {
	if !c.Configured() {
		return &ManagedAgentError{
			Code:    ManagedAgentNotConfiguredCode,
			Message: "managed agent platform is not configured",
		}
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, &body)
	if err != nil {
		return &ManagedAgentError{
			Code:    ManagedAgentUnreachableCode,
			Message: "managed agent platform is unreachable: " + err.Error(),
		}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return &ManagedAgentError{
			Code:    ManagedAgentUnreachableCode,
			Message: "managed agent platform is unreachable: " + err.Error(),
		}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return &ManagedAgentError{
			Code:       ManagedAgentUpstreamErrorCode,
			Message:    fmt.Sprintf("managed agent platform returned HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 240)),
			StatusCode: resp.StatusCode,
		}
	}
	if out == nil {
		return nil
	}
	if len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

func ParseManagedReportDraft(result string) (model.GenerateReportDraftResponse, error) {
	var out model.GenerateReportDraftResponse
	raw := strings.TrimSpace(result)
	if raw == "" {
		return out, errors.New("managed agent returned empty result")
	}
	raw = stripJSONFence(raw)
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return out, err
	}
	if strings.TrimSpace(out.ReportMarkdown) == "" {
		return out, errors.New("managed agent returned empty report_markdown")
	}
	return out, nil
}

func stripJSONFence(raw string) string {
	if !strings.HasPrefix(raw, "```") {
		return raw
	}
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "```")
	return strings.TrimSpace(raw)
}
