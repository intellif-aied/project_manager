package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/reportbrief"
	"github.com/aidashboard/api/internal/reportcontext"
	"github.com/aidashboard/api/internal/reportreview"
	"github.com/aidashboard/api/internal/reportsource"
	"github.com/aidashboard/api/model"
)

// Report MCP tool names and report_type values (doc §3.8 / §4).
const (
	mcpProtocolFallback = "2024-11-05"

	toolGetSessions        = "get_sessions"
	toolGetReportContext   = "get_report_context"
	toolGetDailyReports    = "get_daily_reports"
	toolGetWeeklyReports   = "get_weekly_reports"
	toolGetTasks           = "get_tasks"
	toolGetRequirements    = "get_requirements"
	toolGetExistingReport  = "get_existing_report"
	toolGetReportInventory = "get_report_inventory"
	toolWriteReportBrief   = "write_report_brief"
	toolWriteReportResult  = "write_report_result"
	toolWriteReportFailure = "write_report_failure"

	reportTypePersonalDaily    = "personal_daily"
	reportTypePersonalWeekly   = "personal_weekly"
	reportTypeTeamDaily        = "team_daily"
	reportTypeTeamWeekly       = "team_weekly"
	reportTypeDepartmentDaily  = "department_daily"
	reportTypeDepartmentWeekly = "department_weekly"

	reportEditConflictCode = "REPORT_EDIT_CONFLICT"
)

var supportedReportTypes = []string{
	reportTypePersonalDaily,
	reportTypePersonalWeekly,
	reportTypeTeamDaily,
	reportTypeTeamWeekly,
	reportTypeDepartmentDaily,
	reportTypeDepartmentWeekly,
}

// ReportMCPHandler serves /api/v1/mcp/reports. It is the single MCP entrypoint
// for all 6 report types. Legacy atomic reads remain available for compatibility;
// managed personal reports use the server-prepared Report Context V1 path.
type ReportMCPHandler struct {
	db            *sql.DB
	reportSource  *reportsource.Service
	reportContext *reportcontext.Service
	reportBrief   reportBriefService
	reportReview  reportReviewQueue
	briefEnabled  bool
}

type reportReviewQueue interface {
	Enabled() bool
	Queue(context.Context, string, string, reportbrief.Stored) (reportreview.QueueResult, error)
}

type reportBriefService interface {
	Accept(context.Context, string, string, reportbrief.Draft) (reportbrief.Stored, error)
	Compile(context.Context, string, string, reportbrief.Draft) (reportbrief.Compiled, error)
	RejectInvalid(context.Context, string, string, string) (reportbrief.Stored, error)
	ValidateForWrite(context.Context, string, string, string, string, string) (reportbrief.Stored, error)
	DegradedWriteReason(context.Context, string, string) (string, error)
}

func NewReportMCPHandler(db *sql.DB) *ReportMCPHandler {
	return &ReportMCPHandler{db: db}
}

func (h *ReportMCPHandler) ConfigureReportSourceSelection(service *reportsource.Service) {
	h.reportSource = service
}

func (h *ReportMCPHandler) ConfigureReportContext(service *reportcontext.Service) {
	h.reportContext = service
}

func (h *ReportMCPHandler) ConfigureReportBrief(service reportBriefService, enabled bool) {
	h.reportBrief = service
	h.briefEnabled = enabled
}

func (h *ReportMCPHandler) ConfigureReportReview(service reportReviewQueue) {
	h.reportReview = service
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

// mcpRPCError is the JSON-RPC error envelope. Code is a numeric transport code;
// the structured Report MCP error code is carried in Data.Code.
type mcpRPCError struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    *mcpErrorPayload `json:"data,omitempty"`
}

type mcpErrorPayload struct {
	Code string `json:"code"`
}

type mcpToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (h *ReportMCPHandler) Serve(w http.ResponseWriter, r *http.Request) {
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
		result = h.initializeResult(req.Params)
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = map[string]any{"tools": reportMCPToolsForToolsetWithBrief(r.Header.Get(managedReportMCPToolsetHeader), h.briefEnabled)}
	case "tools/call":
		result, err = h.callTool(r, req.Params)
	default:
		writeMCPError(w, req.ID, -32601, "method not found", "")
		return
	}
	if err != nil {
		h.writeServeError(w, req.ID, err)
		return
	}
	writeMCPResult(w, req.ID, result)
}

func (h *ReportMCPHandler) writeServeError(w http.ResponseWriter, id json.RawMessage, err error) {
	var mcpErr *mcpErrorCode
	if asErr(err, &mcpErr) {
		message := mcpErr.Message
		if reportAgentNeedsVisibleErrorCode(mcpErr.Code) {
			message = mcpErr.Code + ": " + message
		}
		writeMCPError(w, id, -32000, message, mcpErr.Code)
		return
	}
	writeMCPError(w, id, -32603, "internal error: "+err.Error(), errMCPInternal.Code)
}

func reportAgentNeedsVisibleErrorCode(code string) bool {
	return strings.HasPrefix(code, "REPORT_BRIEF_") ||
		strings.HasPrefix(code, "REPORT_RESULT_") ||
		code == "REPORT_RUN_MISMATCH"
}

func (h *ReportMCPHandler) initializeResult(params json.RawMessage) map[string]any {
	protocolVersion := mcpProtocolFallback
	var initParams struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 && json.Unmarshal(params, &initParams) == nil && initParams.ProtocolVersion != "" {
		protocolVersion = initParams.ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo": map[string]string{
			"name":    "aida-report-mcp",
			"version": "1.0.0",
		},
	}
}

func (h *ReportMCPHandler) callTool(r *http.Request, rawParams json.RawMessage) (any, error) {
	var params mcpToolCallParams
	if err := json.Unmarshal(rawParams, &params); err != nil {
		return nil, fmt.Errorf("invalid tool call params")
	}
	ctx := r.Context()
	switch params.Name {
	case toolGetReportContext:
		return h.toolGetReportContext(ctx, r, params.Arguments)
	case toolGetSessions:
		return h.toolGetSessions(ctx, r, params.Arguments)
	case toolGetDailyReports:
		return h.toolGetDailyReports(ctx, r, params.Arguments)
	case toolGetWeeklyReports:
		return h.toolGetWeeklyReports(ctx, r, params.Arguments)
	case toolGetTasks:
		return h.toolGetTasks(ctx, r, params.Arguments)
	case toolGetRequirements:
		return h.toolGetRequirements(ctx, r, params.Arguments)
	case toolGetExistingReport:
		return h.toolGetExistingReport(ctx, r, params.Arguments)
	case toolGetReportInventory:
		return h.toolGetReportInventory(ctx, r, params.Arguments)
	case toolWriteReportBrief:
		if !h.briefEnabled {
			return nil, fmt.Errorf("unknown tool: %s", params.Name)
		}
		return h.toolWriteReportBrief(r, params.Arguments)
	case toolWriteReportResult:
		return h.toolWriteReportResult(r, params.Arguments)
	case toolWriteReportFailure:
		return h.toolWriteReportFailure(r, params.Arguments)
	default:
		return nil, fmt.Errorf("unknown tool: %s", params.Name)
	}
}

func (h *ReportMCPHandler) toolGetReportContext(ctx context.Context, r *http.Request, raw json.RawMessage) (any, error) {
	u, err := requireUser(r)
	if err != nil {
		return nil, err
	}
	var args struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, errRunNotFound
	}
	runID, err := resolveReportRunID(r, args.RunID)
	if err != nil {
		return nil, err
	}
	if h.reportContext == nil {
		return nil, errMCPInternal
	}
	stored, err := h.reportContext.Get(ctx, u.ID, runID)
	if errors.Is(err, reportcontext.ErrNotFound) {
		return nil, errRunNotFound
	}
	if err != nil {
		return nil, errMCPInternal
	}
	return mcpTextResult(json.RawMessage(stored.Payload)), nil
}

func resolveReportRunID(r *http.Request, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	bound, _ := r.Context().Value(reportRunIDKey).(string)
	bound = strings.TrimSpace(bound)
	if bound == "" {
		if requested == "" {
			return "", errRunNotFound
		}
		return requested, nil
	}
	if requested != "" && requested != bound {
		return "", mcpErr("REPORT_RUN_MISMATCH", "run_id does not match the authenticated report run")
	}
	return bound, nil
}

// requireUser returns the authenticated user or an UNAUTHORIZED error.
func requireUser(r *http.Request) (*model.User, error) {
	u := getUser(r)
	if u == nil {
		return nil, errUnauthorized
	}
	return u, nil
}

// asErr is a thin wrapper over errors.As for mcpErrorCode detection.
func asErr(err error, target any) bool {
	return errors.As(err, target)
}

// validateReportType returns nil if reportType is one of the 6 supported values.
func validateReportType(reportType string) error {
	t := strings.TrimSpace(reportType)
	if t == "" {
		return errReportTypeNotSupported
	}
	for _, supported := range supportedReportTypes {
		if supported == t {
			return nil
		}
	}
	return errReportTypeNotSupported
}

func writeMCPResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, http.StatusOK, mcpResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func writeMCPError(w http.ResponseWriter, id json.RawMessage, code int, message, mcpCode string) {
	resp := mcpResponse{JSONRPC: "2.0", ID: id, Error: &mcpRPCError{Code: code, Message: message}}
	if mcpCode != "" {
		resp.Error.Data = &mcpErrorPayload{Code: mcpCode}
	}
	writeJSON(w, http.StatusOK, resp)
}

func mcpTextResult(value any) map[string]any {
	payload, _ := json.Marshal(value)
	return map[string]any{
		"content": []map[string]string{
			{"type": "text", "text": string(payload)},
		},
	}
}

// mcpModelTextResult removes persistence identifiers from read-tool payloads.
// Report generation needs names, content, dates and stable session references;
// database IDs are authorization details and must not become report material.
func mcpModelTextResult(value any) map[string]any {
	payload, _ := json.Marshal(value)
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return mcpTextResult(value)
	}
	if root, ok := decoded.(map[string]any); ok {
		if _, exists := root["timezone"]; !exists {
			root["timezone"] = biztime.Zone
		}
	}
	return mcpTextResult(redactReportMCPIdentifiers(decoded))
}

func redactReportMCPIdentifiers(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(typed))
		for key, item := range typed {
			if key == "id" || strings.HasSuffix(key, "_id") || strings.HasSuffix(key, "_ids") {
				continue
			}
			clean[key] = redactReportMCPIdentifiers(item)
		}
		return clean
	case []any:
		clean := make([]any, len(typed))
		for index, item := range typed {
			clean[index] = redactReportMCPIdentifiers(item)
		}
		return clean
	default:
		return value
	}
}
