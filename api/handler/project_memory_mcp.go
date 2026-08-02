package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/reportmemory"
)

const (
	toolGetProjectMemoryContext  = "get_project_memory_context"
	toolWriteProjectMemoryResult = "write_project_memory_result"
)

type ProjectMemoryMCPHandler struct {
	db *sql.DB
}

func NewProjectMemoryMCPHandler(database *sql.DB) *ProjectMemoryMCPHandler {
	return &ProjectMemoryMCPHandler{db: database}
}

func (h *ProjectMemoryMCPHandler) Serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req mcpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMCPError(w, nil, -32700, "invalid JSON", "")
		return
	}
	if len(req.ID) == 0 && strings.HasPrefix(req.Method, "notifications/") {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var result any
	var err error
	switch req.Method {
	case "initialize":
		result = map[string]any{
			"protocolVersion": mcpProtocolFallback,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": "aida-project-memory-mcp", "version": "1.0.0"},
		}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = map[string]any{"tools": projectMemoryMCPTools()}
	case "tools/call":
		result, err = h.callTool(r, req.Params)
	default:
		writeMCPError(w, req.ID, -32601, "method not found", "")
		return
	}
	if err != nil {
		writeMCPError(w, req.ID, -32000, err.Error(), "PROJECT_MEMORY_MCP_ERROR")
		return
	}
	writeMCPResult(w, req.ID, result)
}

func projectMemoryMCPTools() []map[string]any {
	return []map[string]any{
		{
			"name":        toolGetProjectMemoryContext,
			"description": "Read the complete bounded input for the authenticated Project Memory Job.",
			"inputSchema": map[string]any{
				"type": "object", "additionalProperties": false,
			},
		},
		{
			"name":        toolWriteProjectMemoryResult,
			"description": "Validate and write the single structured proposal for the authenticated Project Memory Job.",
			"inputSchema": map[string]any{
				"type": "object", "required": []string{"proposal_json"},
				"properties": map[string]any{
					"proposal_json": map[string]any{"type": "object"},
				},
				"additionalProperties": false,
			},
		},
	}
}

func (h *ProjectMemoryMCPHandler) callTool(r *http.Request, raw json.RawMessage) (any, error) {
	var params mcpToolCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, errors.New("invalid tool call params")
	}
	switch params.Name {
	case toolGetProjectMemoryContext:
		return h.getContext(r, params.Arguments)
	case toolWriteProjectMemoryResult:
		return h.writeResult(r, params.Arguments)
	default:
		return nil, fmt.Errorf("unknown tool: %s", params.Name)
	}
}

func (h *ProjectMemoryMCPHandler) getContext(r *http.Request, raw json.RawMessage) (any, error) {
	if !emptyJSONObject(raw) {
		return nil, errors.New("get_project_memory_context accepts only {}")
	}
	userID, date, fingerprint, err := boundProjectMemoryJob(r)
	if err != nil {
		return nil, err
	}
	var input []byte
	err = h.db.QueryRowContext(r.Context(), `
		SELECT input_json
		FROM report_project_memory_jobs
		WHERE user_id = $1 AND report_date = $2::date
		  AND claimed_source_fingerprint = $3
		  AND status IN ('submitting', 'running')`,
		userID, date, fingerprint).Scan(&input)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("project memory job is unavailable")
	}
	if err != nil {
		return nil, err
	}
	return mcpTextResult(json.RawMessage(input)), nil
}

func (h *ProjectMemoryMCPHandler) writeResult(r *http.Request, raw json.RawMessage) (any, error) {
	var args struct {
		ProposalJSON json.RawMessage `json:"proposal_json"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil || len(args.ProposalJSON) == 0 {
		return nil, errors.New("proposal_json is required")
	}
	userID, date, fingerprint, err := boundProjectMemoryJob(r)
	if err != nil {
		return nil, err
	}
	var inputPayload []byte
	var existing []byte
	err = h.db.QueryRowContext(r.Context(), `
		SELECT input_json, COALESCE(proposal_json, 'null'::jsonb)
		FROM report_project_memory_jobs
		WHERE user_id = $1 AND report_date = $2::date
		  AND claimed_source_fingerprint = $3
		  AND status IN ('submitting', 'running')
		FOR UPDATE`, userID, date, fingerprint).Scan(&inputPayload, &existing)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("project memory job is unavailable")
	}
	if err != nil {
		return nil, err
	}
	var input reportmemory.ConsolidationInput
	if err := json.Unmarshal(inputPayload, &input); err != nil {
		return nil, errors.New("stored project memory context is invalid")
	}
	_, normalized, outputEstimate, err := reportmemory.ValidateProposal(args.ProposalJSON, input)
	if err != nil {
		return nil, fmt.Errorf("PROJECT_MEMORY_RESULT_INVALID: %w", err)
	}
	if strings.TrimSpace(string(existing)) != "" && strings.TrimSpace(string(existing)) != "null" {
		var oldValue, newValue any
		if json.Unmarshal(existing, &oldValue) == nil && json.Unmarshal(normalized, &newValue) == nil &&
			reflect.DeepEqual(oldValue, newValue) {
			return mcpTextResult(map[string]any{"status": "saved", "already_written": true}), nil
		}
		return nil, errors.New("project memory result was already written")
	}
	result, err := h.db.ExecContext(r.Context(), `
		UPDATE report_project_memory_jobs SET
			proposal_json = $1::jsonb, output_token_estimate = $2, updated_at = now()
		WHERE user_id = $3 AND report_date = $4::date
		  AND claimed_source_fingerprint = $5
		  AND status IN ('submitting', 'running') AND proposal_json IS NULL`,
		string(normalized), outputEstimate, userID, date, fingerprint)
	if err != nil {
		return nil, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return nil, errors.New("project memory job changed before writeback")
	}
	return mcpTextResult(map[string]any{"status": "saved"}), nil
}

func boundProjectMemoryJob(r *http.Request) (string, string, string, error) {
	u, err := requireUser(r)
	if err != nil {
		return "", "", "", err
	}
	jobRef, _ := r.Context().Value(projectMemoryJobRefKey).(string)
	parts := strings.Split(strings.TrimSpace(jobRef), "|")
	if len(parts) != 2 {
		return "", "", "", errors.New("project memory job identity is missing")
	}
	if _, err := time.Parse("2006-01-02", parts[0]); err != nil || len(parts[1]) != 64 {
		return "", "", "", errors.New("project memory job identity is invalid")
	}
	return u.ID, parts[0], parts[1], nil
}

func emptyJSONObject(raw json.RawMessage) bool {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return true
	}
	var value map[string]any
	return json.Unmarshal(raw, &value) == nil && len(value) == 0
}
