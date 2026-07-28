package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/aidashboard/api/internal/reportbrief"
)

type writeReportBriefArgs struct {
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
	args.RunID = strings.TrimSpace(args.RunID)
	if args.RunID == "" {
		return nil, mcpErr("REPORT_BRIEF_INVALID", "run_id is required")
	}
	stored, err := h.reportBrief.Accept(r.Context(), u.ID, args.RunID, reportbrief.Draft{
		Workstreams: args.Workstreams, ExcludedFacts: args.ExcludedFacts,
		NoReportableWork: args.NoReportableWork,
	})
	if err != nil {
		return nil, mapReportBriefError(err)
	}
	return mcpTextResult(stored.Accepted()), nil
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
