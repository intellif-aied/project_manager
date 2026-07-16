package handler

import "fmt"

// mcpErrorCode is a structured Report MCP error. It carries a stable string code
// (per doc/v1/mcp修改方案.md §3.7 / §12) that clients can switch on, plus a human
// message. serve() converts *mcpErrorCode into a JSON-RPC error object.
type mcpErrorCode struct {
	Code    string
	Message string
}

func (e *mcpErrorCode) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func mcpErr(code, message string) *mcpErrorCode {
	return &mcpErrorCode{Code: code, Message: message}
}

var (
	errUnauthorized           = mcpErr("UNAUTHORIZED", "authentication required")
	errForbidden              = mcpErr("FORBIDDEN", "scope or target not allowed for current user")
	errReportTypeNotSupported = mcpErr("REPORT_TYPE_NOT_SUPPORTED", "unsupported report_type")
	errInvalidPeriod          = mcpErr("INVALID_PERIOD", "period is missing or malformed")
	errInvalidScope           = mcpErr("INVALID_SCOPE", "scope is missing or invalid")
	errInvalidTarget          = mcpErr("INVALID_TARGET", "target is missing or invalid")
	errRunNotFound            = mcpErr("RUN_NOT_FOUND", "run_id does not exist")
	errRunForbidden           = mcpErr("RUN_FORBIDDEN", "run_id does not belong to current user")
	errReportEditConflict     = mcpErr("REPORT_EDIT_CONFLICT", "Report has been modified by user")
	errReportNotFound         = mcpErr("REPORT_NOT_FOUND", "report does not exist")
	errMCPInternal            = mcpErr("MCP_INTERNAL_ERROR", "internal MCP error")
)
