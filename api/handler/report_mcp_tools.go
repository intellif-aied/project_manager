package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// reportMCPTools returns the 9 atomic tool schemas (doc §3.8).
func reportMCPTools() []map[string]any {
	const businessTimeContract = "Business dates use Asia/Shanghai. Returned RFC3339 timestamps are already converted to +08:00 and must not be converted again."
	targetSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"type":          map[string]any{"type": "string", "enum": []string{"self", "user", "team", "department"}},
			"user_id":       map[string]any{"type": "string"},
			"team_id":       map[string]any{"type": "string"},
			"department_id": map[string]any{"type": "string"},
		},
	}
	scopeSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"type":          map[string]any{"type": "string", "enum": []string{"self", "team", "department", "all"}},
			"team_id":       map[string]any{"type": "string"},
			"department_id": map[string]any{"type": "string"},
			"user_ids":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
	dateRangeSchema := map[string]any{
		"type":     "object",
		"required": []string{"start", "end"},
		"properties": map[string]any{
			"start": map[string]any{"type": "string"},
			"end":   map[string]any{"type": "string"},
		},
	}
	weekRangeSchema := map[string]any{
		"type":     "object",
		"required": []string{"week_start", "week_end"},
		"properties": map[string]any{
			"week_start": map[string]any{"type": "string"},
			"week_end":   map[string]any{"type": "string"},
		},
	}
	periodSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"date":       map[string]any{"type": "string"},
			"week_start": map[string]any{"type": "string"},
			"week_end":   map[string]any{"type": "string"},
		},
	}
	reportTypeSchema := map[string]any{"type": "string", "enum": supportedReportTypes}

	return []map[string]any{
		{
			"name":        toolGetSessions,
			"description": "Read an attached personal-report source snapshot in the server-frozen mode (digest_v1 and digest_v2 are complete bounded pages; digest_v2 exposes report_period_summary as the primary period projection; legacy full may paginate), or list sessions for an authenticated ad-hoc date query. " + businessTimeContract,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"scope"},
				"properties": map[string]any{
					"scope":       scopeSchema,
					"target":      targetSchema,
					"date_range":  dateRangeSchema,
					"report_type": reportTypeSchema,
					"period":      periodSchema,
					"run_id":      map[string]any{"type": "string"},
					"report_source_selection_id": map[string]any{
						"type":        "string",
						"description": "Attached source snapshot id injected by Aida for managed personal reports. When present in the run input, send the value only in this field; it may look like a UUID but it is not a session slice key.",
					},
					"page_cursor": map[string]any{
						"type":        "string",
						"description": "Cursor for the next legacy full page. Never send it for content_mode=digest_v1 or digest_v2.",
					},
					"next_cursor": map[string]any{
						"type":        "string",
						"description": "Compatibility alias for page_cursor. Do not send both fields with different values.",
					},
					"user_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"selected_session_slice_keys": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Legacy ad-hoc session activity slice keys in the format session_id:YYYY-MM-DD. Never send this field when report_source_selection_id is present, and never copy a report_source_selection_id UUID into this array.",
					},
					"limit":           map[string]any{"type": "integer"},
					"include_summary": map[string]any{"type": "boolean"},
				},
			},
		},
		{
			"name":        toolGetDailyReports,
			"description": "List daily reports within scope, optionally filtered by report_scope. Set include_content=true for report aggregation; omitted values default to true. " + businessTimeContract,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"scope", "date_range", "include_content"},
				"properties": map[string]any{
					"scope":           scopeSchema,
					"target":          targetSchema,
					"date_range":      dateRangeSchema,
					"report_scope":    map[string]any{"type": "string", "enum": []string{"personal", "team", "department"}},
					"user_ids":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"include_content": map[string]any{"type": "boolean"},
				},
			},
		},
		{
			"name":        toolGetWeeklyReports,
			"description": "List weekly reports within scope, optionally filtered by report_scope. Set include_content=true for report aggregation; omitted values default to true. " + businessTimeContract,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"scope", "week_range", "include_content"},
				"properties": map[string]any{
					"scope":           scopeSchema,
					"target":          targetSchema,
					"week_range":      weekRangeSchema,
					"report_scope":    map[string]any{"type": "string", "enum": []string{"personal", "team", "department"}},
					"user_ids":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"include_content": map[string]any{"type": "boolean"},
				},
			},
		},
		{
			"name":        toolGetTasks,
			"description": "List tasks visible to the current user within scope. date_range is an inclusive Asia/Shanghai business-date range. " + businessTimeContract,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"scope", "date_range"},
				"properties": map[string]any{
					"scope":               scopeSchema,
					"target":              targetSchema,
					"date_range":          dateRangeSchema,
					"status":              map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"include_requirement": map[string]any{"type": "boolean"},
				},
			},
		},
		{
			"name":        toolGetRequirements,
			"description": "List requirements visible to the current user within scope. date_range is an inclusive Asia/Shanghai business-date range. " + businessTimeContract,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"scope", "date_range"},
				"properties": map[string]any{
					"scope":         scopeSchema,
					"target":        targetSchema,
					"date_range":    dateRangeSchema,
					"include_tasks": map[string]any{"type": "boolean"},
					"include_risks": map[string]any{"type": "boolean"},
				},
			},
		},
		{
			"name":        toolGetExistingReport,
			"description": "Fetch the current content of a single report identified by report_type + period + target. " + businessTimeContract,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"report_type", "period"},
				"properties": map[string]any{
					"report_type": reportTypeSchema,
					"period":      periodSchema,
					"target":      targetSchema,
				},
			},
		},
		{
			"name":        toolGetReportInventory,
			"description": "Compute expected/existing/missing report coverage. For report_kind=daily, send date_range only. For report_kind=weekly, send week_range only.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"scope", "report_scope", "report_kind"},
				"properties": map[string]any{
					"scope":        scopeSchema,
					"target":       targetSchema,
					"report_scope": map[string]any{"type": "string", "enum": []string{"personal", "team", "department"}},
					"report_kind":  map[string]any{"type": "string", "enum": []string{"daily", "weekly"}},
					"date_range":   dateRangeSchema,
					"week_range":   weekRangeSchema,
				},
			},
		},
		{
			"name":        toolWriteReportResult,
			"description": "Write Agent-generated report content. run_id must belong to the current user.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"report_type", "period", "run_id", "content"},
				"properties": map[string]any{
					"report_type": reportTypeSchema,
					"period":      periodSchema,
					"target":      targetSchema,
					"run_id":      map[string]any{"type": "string"},
					"content":     map[string]any{"type": "string"},
					"summary":     map[string]any{"type": "string"},
				},
			},
		},
		{
			"name":        toolWriteReportFailure,
			"description": "Record an Agent generation failure for a run. Does not modify report content.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"report_type", "period", "run_id", "error_message"},
				"properties": map[string]any{
					"report_type":   reportTypeSchema,
					"period":        periodSchema,
					"target":        targetSchema,
					"run_id":        map[string]any{"type": "string"},
					"error_code":    map[string]any{"type": "string"},
					"error_message": map[string]any{"type": "string"},
				},
			},
		},
	}
}

// periodArgs mirrors the period JSON shape shared by read/write tools.
type periodArgs struct {
	Date      string `json:"date,omitempty"`
	WeekStart string `json:"week_start,omitempty"`
	WeekEnd   string `json:"week_end,omitempty"`
}

type dateRangeArgs struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

type weekRangeArgs struct {
	WeekStart string `json:"week_start,omitempty"`
	WeekEnd   string `json:"week_end,omitempty"`
}

func parseDate(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("date is required")
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return "", fmt.Errorf("date must be YYYY-MM-DD")
	}
	return s, nil
}

func parseDateRange(r dateRangeArgs) (string, string, error) {
	start, err := parseDate(r.Start)
	if err != nil {
		return "", "", errInvalidPeriod
	}
	end, err := parseDate(r.End)
	if err != nil {
		return "", "", errInvalidPeriod
	}
	if end < start {
		return "", "", errInvalidPeriod
	}
	return start, end, nil
}

func normalizeSelectedSessionSliceKeys(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{}, nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		key := strings.TrimSpace(raw)
		if key == "" {
			continue
		}
		if strings.Count(key, ":") != 1 {
			return nil, fmt.Errorf("selected_session_slice_keys must use session_id:YYYY-MM-DD")
		}
		sessionID, date, _ := strings.Cut(key, ":")
		sessionID = strings.TrimSpace(sessionID)
		date = strings.TrimSpace(date)
		if sessionID == "" {
			return nil, fmt.Errorf("selected_session_slice_keys must include session_id")
		}
		if _, err := parseDate(date); err != nil {
			return nil, fmt.Errorf("selected_session_slice_keys must use YYYY-MM-DD dates")
		}
		normalized := sessionID + ":" + date
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) > 200 {
		return nil, fmt.Errorf("selected_session_slice_keys supports at most 200 values")
	}
	return out, nil
}

func parseWeekRange(r weekRangeArgs) (string, string, error) {
	ws, err := parseDate(r.WeekStart)
	if err != nil {
		return "", "", errInvalidPeriod
	}
	we, err := parseDate(r.WeekEnd)
	if err != nil {
		return "", "", errInvalidPeriod
	}
	if we < ws {
		return "", "", errInvalidPeriod
	}
	return ws, we, nil
}

// resolveReportPeriod validates a period for the given report_type and returns the
// concrete date or (week_start, week_end).
func resolveReportPeriod(reportType string, p periodArgs) (date string, weekStart string, weekEnd string, err error) {
	switch reportType {
	case reportTypePersonalDaily, reportTypeTeamDaily, reportTypeDepartmentDaily:
		date, err = parseDate(p.Date)
		if err != nil {
			return "", "", "", errInvalidPeriod
		}
		return date, "", "", nil
	case reportTypePersonalWeekly, reportTypeTeamWeekly, reportTypeDepartmentWeekly:
		weekStart, weekEnd, err = parseWeekRange(weekRangeArgs{WeekStart: p.WeekStart, WeekEnd: p.WeekEnd})
		if err != nil {
			return "", "", "", err
		}
		return "", weekStart, weekEnd, nil
	}
	return "", "", "", errReportTypeNotSupported
}

// decodeArguments unmarshals tool arguments; returns nil error only on success.
func decodeArguments(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		return fmt.Errorf("invalid arguments")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("invalid arguments")
	}
	return nil
}
