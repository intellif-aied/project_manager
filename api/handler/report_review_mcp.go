package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/aidashboard/api/internal/reportbrief"
	"github.com/aidashboard/api/internal/reportreview"
)

const (
	toolGetReportReviewContext = "get_report_review_context"
	toolWriteReportReview      = "write_report_review"
)

type ReportReviewMCPHandler struct {
	review *reportreview.Service
}

func NewReportReviewMCPHandler(review *reportreview.Service) *ReportReviewMCPHandler {
	return &ReportReviewMCPHandler{review: review}
}

func (handler *ReportReviewMCPHandler) Serve(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var rpc mcpRequest
	if err := json.NewDecoder(request.Body).Decode(&rpc); err != nil {
		writeMCPError(w, nil, -32700, "invalid JSON", "")
		return
	}
	if len(rpc.ID) == 0 && strings.HasPrefix(rpc.Method, "notifications/") {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var result any
	var err error
	switch rpc.Method {
	case "initialize":
		result = map[string]any{
			"protocolVersion": mcpProtocolFallback,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": "aida-report-review-mcp", "version": "1.0.0"},
		}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = map[string]any{"tools": reportReviewTools()}
	case "tools/call":
		result, err = handler.callTool(request, rpc.Params)
	default:
		writeMCPError(w, rpc.ID, -32601, "method not found", "")
		return
	}
	if err != nil {
		writeMCPError(w, rpc.ID, -32000, err.Error(), "REPORT_REVIEW_INVALID")
		return
	}
	writeMCPResult(w, rpc.ID, result)
}

func reportReviewTools() []map[string]any {
	issueSchema := map[string]any{
		"type": "object", "required": []string{"code", "target"},
		"properties": map[string]any{
			"code": map[string]any{"type": "string", "enum": []string{
				"unsupported_project", "cross_project_merge", "project_fragmentation", "overclaim", "lost_qualifier", "memory_injection", "major_omission",
			}},
			"target":    map[string]any{"type": "string"},
			"fact_refs": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"reason":    map[string]any{"type": "string"},
		},
	}
	refArray := map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string"}}
	patchVariants := make([]map[string]any, 0, 11)
	for _, op := range []string{"replace_subject", "replace_title", "replace_text", "replace_result", "add_qualifier"} {
		patchVariants = append(patchVariants, map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"op", "target", "value", "supporting_fact_refs"},
			"properties": map[string]any{
				"op": map[string]any{"type": "string", "enum": []string{op}}, "target": map[string]any{"type": "string"},
				"value": map[string]any{"type": "string"}, "supporting_fact_refs": refArray,
			},
		})
	}
	for _, op := range []string{"drop_deliverable", "drop_workstream"} {
		patchVariants = append(patchVariants, map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"op", "target"},
			"properties": map[string]any{"op": map[string]any{"type": "string", "enum": []string{op}}, "target": map[string]any{"type": "string"}},
		})
	}
	patchVariants = append(patchVariants,
		map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"op", "target", "destination"},
			"properties": map[string]any{
				"op":     map[string]any{"type": "string", "enum": []string{"merge_workstream"}},
				"target": map[string]any{"type": "string"}, "destination": map[string]any{"type": "string"},
			},
		},
		map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"op", "target", "result", "fact_refs"},
			"properties": map[string]any{
				"op": map[string]any{"type": "string", "enum": []string{"add_deliverable"}}, "target": map[string]any{"type": "string"},
				"result": map[string]any{"type": "string"}, "fact_refs": refArray,
			},
		},
		map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"op", "subject", "title", "result", "fact_refs"},
			"properties": map[string]any{
				"op":      map[string]any{"type": "string", "enum": []string{"add_workstream"}},
				"subject": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"},
				"result": map[string]any{"type": "string"}, "fact_refs": refArray,
			},
		},
	)
	patchSchema := map[string]any{"oneOf": patchVariants}
	projectAttachmentSchema := map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"canonical_name", "targets", "fact_refs"},
		"properties": map[string]any{
			"canonical_name": map[string]any{"type": "string"},
			"targets":        map[string]any{"type": "array", "minItems": 1, "maxItems": 5, "items": map[string]any{"type": "string"}},
			"fact_refs":      refArray,
		},
	}
	return []map[string]any{
		{
			"name":        toolGetReportReviewContext,
			"description": "Read the frozen candidate report, selected Facts, bounded omission candidates, and optional project candidates for this review job.",
			"inputSchema": map[string]any{"type": "object", "additionalProperties": false},
		},
		{
			"name":        toolWriteReportReview,
			"description": "Submit exactly one bounded semantic review for the bound report review job.",
			"inputSchema": map[string]any{
				"type": "object", "required": []string{"decision", "project_attachments"},
				"properties": map[string]any{
					"decision":            map[string]any{"type": "string", "enum": []string{"accept", "repair", "conservative"}},
					"issues":              map[string]any{"type": "array", "items": issueSchema},
					"patches":             map[string]any{"type": "array", "maxItems": 8, "items": patchSchema},
					"project_attachments": map[string]any{"type": "array", "maxItems": 5, "items": projectAttachmentSchema},
				},
			},
		},
	}
}

func (handler *ReportReviewMCPHandler) callTool(request *http.Request, raw json.RawMessage) (any, error) {
	if handler == nil || handler.review == nil || !handler.review.Enabled() {
		return nil, errors.New("report review is unavailable")
	}
	var params mcpToolCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, errors.New("invalid tool call params")
	}
	user, err := requireUser(request)
	if err != nil {
		return nil, err
	}
	jobRef, _ := request.Context().Value(reportReviewJobRefKey).(string)
	if strings.TrimSpace(jobRef) == "" {
		return nil, errors.New("report review job identity is missing")
	}
	switch params.Name {
	case toolGetReportReviewContext:
		if !emptyJSONObject(params.Arguments) {
			return nil, errors.New("get_report_review_context accepts only {}")
		}
		input, err := handler.review.GetContext(request.Context(), user.ID, jobRef)
		if err != nil {
			return nil, err
		}
		return mcpTextResult(input.AgentView()), nil
	case toolWriteReportReview:
		var decision reportbrief.ReviewDecision
		if err := decodeArguments(params.Arguments, &decision); err != nil {
			return nil, err
		}
		finalized, err := handler.review.WriteDecision(request.Context(), user.ID, jobRef, decision)
		if err != nil {
			return nil, err
		}
		return mcpTextResult(map[string]any{
			"status": "saved", "decision": decision.Decision,
			"finalization_mode": finalized.Mode, "brief_hash": finalized.Stored.BriefHash,
		}), nil
	default:
		return nil, errors.New("unknown report review tool")
	}
}
