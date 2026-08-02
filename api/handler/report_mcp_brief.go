package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/aidashboard/api/internal/reportbrief"
)

type writeReportBriefArgs struct {
	BriefJSON        string                     `json:"brief_json"`
	RunID            string                     `json:"run_id"`
	Workstreams      []reportbrief.Workstream   `json:"workstreams"`
	ExcludedFacts    []reportbrief.ExcludedFact `json:"excluded_facts"`
	NoReportableWork bool                       `json:"no_reportable_work"`
}

func (h *ReportMCPHandler) toolWriteReportBrief(r *http.Request, rawArgs json.RawMessage) (any, error) {
	u, err := requireUser(r)
	if err != nil {
		return nil, err
	}
	if h.reportBrief == nil || !h.briefEnabled {
		return nil, mcpErr("REPORT_BRIEF_DISABLED", "two-pass personal daily reports are disabled")
	}
	var args writeReportBriefArgs
	if err := decodeArguments(rawArgs, &args); err != nil {
		return nil, err
	}
	runID, err := resolveReportRunID(r, args.RunID)
	if err != nil {
		return nil, err
	}
	draft := reportbrief.Draft{
		Workstreams: args.Workstreams, ExcludedFacts: args.ExcludedFacts,
		NoReportableWork: args.NoReportableWork,
	}
	briefJSON := strings.TrimSpace(args.BriefJSON)
	if briefJSON != "" {
		if err := decodeReportBriefJSON(briefJSON, &draft); err != nil {
			_, rejectErr := h.reportBrief.RejectInvalid(r.Context(), u.ID, runID,
				"brief_json must contain one valid Report Brief JSON object: "+err.Error())
			return nil, mapReportBriefError(rejectErr)
		}
	}
	if len(draft.Workstreams) == 0 && len(draft.ExcludedFacts) == 0 && !draft.NoReportableWork {
		return nil, mcpErr("REPORT_BRIEF_INVALID",
			"brief_json is required; submit one non-empty serialized Report Brief JSON object")
	}
	stored, err := h.reportBrief.Accept(r.Context(), u.ID, runID, draft)
	if err != nil {
		return nil, mapReportBriefError(err)
	}
	return mcpTextResult(stored.Accepted()), nil
}

func decodeReportBriefJSON(raw string, output any) error {
	err := json.Unmarshal([]byte(raw), output)
	if err == nil {
		return nil
	}
	repaired, ok := completeTruncatedJSON(raw)
	if !ok || json.Unmarshal([]byte(repaired), output) != nil {
		return err
	}
	return nil
}

// completeTruncatedJSON repairs only a syntactically complete JSON value that
// is missing trailing object/array closers. It never edits strings, commas,
// fields, or values; the decoded Brief still passes the full semantic validator.
func completeTruncatedJSON(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	stack := make([]byte, 0, 8)
	inString, escaped := false, false
	for index := 0; index < len(value); index++ {
		current := value[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if current == '\\' {
				escaped = true
				continue
			}
			if current == '"' {
				inString = false
			}
			continue
		}
		switch current {
		case '"':
			inString = true
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) == 0 || stack[len(stack)-1] != current {
				return "", false
			}
			stack = stack[:len(stack)-1]
		}
	}
	if inString || escaped || len(stack) == 0 {
		return "", false
	}
	for index := len(stack) - 1; index >= 0; index-- {
		value += string(stack[index])
	}
	return value, true
}

func mapReportBriefError(err error) error {
	switch {
	case errors.Is(err, reportbrief.ErrBriefRetryExhausted):
		return mcpErr("REPORT_BRIEF_RETRY_EXHAUSTED", errorDetailsForMCP(err, reportbrief.ErrBriefRetryExhausted))
	case errors.Is(err, reportbrief.ErrResultRetryExhausted):
		return mcpErr("REPORT_RESULT_RETRY_EXHAUSTED", errorDetailsForMCP(err, reportbrief.ErrResultRetryExhausted))
	case errors.Is(err, reportbrief.ErrResultInvalid):
		return mcpErr("REPORT_RESULT_INVALID", errorDetailsForMCP(err, reportbrief.ErrResultInvalid))
	case errors.Is(err, reportbrief.ErrInvalid):
		return mcpErr("REPORT_BRIEF_INVALID", errorDetailsForMCP(err, reportbrief.ErrInvalid))
	case errors.Is(err, reportbrief.ErrConflict):
		return mcpErr("REPORT_BRIEF_CONFLICT", "a different Report Brief was already accepted for this run")
	case errors.Is(err, reportbrief.ErrMismatch):
		return mcpErr("REPORT_BRIEF_MISMATCH", "Report Brief does not match this run or Report Context")
	case errors.Is(err, reportbrief.ErrNotFound):
		return mcpErr("REPORT_BRIEF_REQUIRED", "an accepted Report Brief is required")
	case errors.Is(err, reportbrief.ErrRunNotWritable):
		return mcpErr("RUN_NOT_WRITABLE", "run does not accept a Report Brief")
	default:
		return errMCPInternal
	}
}

func errorDetailsForMCP(err, sentinel error) string {
	message := strings.TrimSpace(err.Error())
	return strings.TrimPrefix(message, sentinel.Error()+": ")
}
