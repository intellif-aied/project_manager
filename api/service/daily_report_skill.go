package service

import (
	"fmt"
	"strings"
)

const (
	ReportSkillSlug         = "aida-report"
	ReportSkillVersion      = "1.0.0"
	ReportSkillName         = "Aida Report Skill"
	ReportMCPSlug           = "aida-report-mcp"
	ReportMCPVersion        = "report-v1"
	ReportMCPCredentialSlot = "AIDA_REPORT_MCP_AUTH"

	// Backward-compatible aliases for legacy draft code/tests. New Report Agent
	// configuration should use ReportSkill*.
	DailyReportSkillSlug    = ReportSkillSlug
	DailyReportSkillVersion = ReportSkillVersion
	DailyReportSkillName    = ReportSkillName
)

func DailyReportOutputContract() string {
	return `Return strict JSON only: {"report_markdown":"...","task_progress_suggestions":[{"task_id":"...","task_title":"...","requirement_id":"...","requirement_title":"...","suggested_status":"todo|in_progress|done","suggested_progress":0,"evidence_session_ids":["..."],"evidence_session_titles":["..."],"reason":"..."}]}. Do not invent facts outside the provided Aida context.`
}

type ReportSkillTemplateData struct {
	MCPURL               string
	MCPSlug              string
	MCPVersion           string
	CredentialSlot       string
	SupportedReportTypes []string
}

func DefaultReportSkillTemplateData(mcpURL string) ReportSkillTemplateData {
	return ReportSkillTemplateData{
		MCPURL:         mcpURL,
		MCPSlug:        ReportMCPSlug,
		MCPVersion:     ReportMCPVersion,
		CredentialSlot: ReportMCPCredentialSlot,
		SupportedReportTypes: []string{
			"personal_daily",
			"personal_weekly",
			"team_daily",
			"team_weekly",
			"department_daily",
			"department_weekly",
		},
	}
}

func normalizeReportSkillTemplateData(data ReportSkillTemplateData) ReportSkillTemplateData {
	data.MCPURL = strings.TrimSpace(data.MCPURL)
	data.MCPSlug = strings.TrimSpace(data.MCPSlug)
	if data.MCPSlug == "" {
		data.MCPSlug = ReportMCPSlug
	}
	data.MCPVersion = strings.TrimSpace(data.MCPVersion)
	if data.MCPVersion == "" {
		data.MCPVersion = ReportMCPVersion
	}
	data.CredentialSlot = strings.TrimSpace(data.CredentialSlot)
	if data.CredentialSlot == "" {
		data.CredentialSlot = ReportMCPCredentialSlot
	}
	if len(data.SupportedReportTypes) == 0 {
		data.SupportedReportTypes = DefaultReportSkillTemplateData(data.MCPURL).SupportedReportTypes
	}
	return data
}

func ReportSkillMarkdown(mcpURL string) string {
	return ReportSkillMarkdownWithConfig(DefaultReportSkillTemplateData(mcpURL))
}

func ReportSkillMarkdownWithConfig(data ReportSkillTemplateData) string {
	data = normalizeReportSkillTemplateData(data)
	return fmt.Sprintf(`# Aida Report Skill

Use this skill when generating Aida reports. The run input must include report_type, period, target, and run_id. It may include selected_session_slice_keys injected by Aida. Do not ask the user to provide session ids, urls, MCP tokens, or credentials.

## Supported report_type

%s

## Required MCP

The Aida Report MCP server is already bound to this Agent by Aida as server name %s.

The MCP server requires the current Aida user token in the Authorization header. The token is supplied by Aida through the %s credential slot at run time. Never ask the user for a token, never print credentials, and never hand-build an Authorization header.

Call the bound MCP tools by tool name. Do not manually fetch MCP URLs or construct raw HTTP requests.

Tool results use the MCP text-content shape:

    {"content":[{"type":"text","text":"{\"key\":\"value\"}"}]}

Always parse content[0].text as JSON before reasoning over the returned data.

## Input Mapping

Derive these shared values from run input:

- daily period: {"date":"YYYY-MM-DD"}
- weekly period: {"week_start":"YYYY-MM-DD","week_end":"YYYY-MM-DD"}
- date_range for daily reports or sessions: {"start": date, "end": date}
- date_range for weekly context: {"start": week_start, "end": week_end}
- week_range for weekly reports: {"week_start": week_start, "week_end": week_end}
- selected_session_slice_keys: optional JSON array of session slice keys in the form "session_id:YYYY-MM-DD"; when present and non-empty, pass it unchanged to get_sessions.

Use scope by report_type:

- personal_daily / personal_weekly: scope.type=self, target from run input.
- team_daily / team_weekly: scope.type=team, target from run input.
- department_daily / department_weekly: scope.type=department, target from run input.

Use report_scope by source:

- personal source reports: report_scope=personal
- team source reports: report_scope=team
- department source reports: report_scope=department

Use this exact tool argument contract:

- get_sessions: {"scope": scope, "target": target, "date_range": date_range, "include_summary": true, "selected_session_slice_keys": optional_selected_session_slice_keys}.
- get_daily_reports: {"scope": scope, "target": target, "date_range": date_range, "report_scope": report_scope, "include_content": true}.
- get_weekly_reports: {"scope": scope, "target": target, "week_range": week_range, "report_scope": report_scope, "include_content": true}.
- get_tasks: {"scope": scope, "target": target, "date_range": date_range, "include_requirement": true}.
- get_requirements: {"scope": scope, "target": target, "date_range": date_range, "include_tasks": true, "include_risks": true}.
- get_existing_report: {"report_type": report_type, "period": period, "target": target}.
- get_report_inventory: {"scope": scope, "target": target, "report_scope": report_scope, "report_kind": "daily|weekly", "date_range": date_range, "week_range": optional_week_range}.
- write_report_result: {"report_type": report_type, "period": period, "target": target, "run_id": run_id, "content": markdown, "summary": optional_summary}.
- write_report_failure: {"report_type": report_type, "period": period, "target": target, "run_id": run_id, "error_message": error_message}.

Do not send period to read-list tools that require date_range or week_range. Do not send date_range or week_range to write_report_result, write_report_failure, or get_existing_report.

## Workflow

1. Read report_type, period, target, run_id, and optional selected_session_slice_keys from the run input.
2. Call get_existing_report first with {"report_type": report_type, "period": period, "target": target}.
3. Select context tools by report_type:
   - personal_daily: get_sessions, get_tasks, get_requirements with scope.type=self and date_range for period.date.
   - personal_weekly: get_daily_reports(report_scope=personal), get_sessions, get_tasks, get_requirements with scope.type=self and date_range for the week.
   - team_daily: get_daily_reports(report_scope=personal), get_sessions, get_tasks, get_requirements, get_report_inventory(report_scope=personal, report_kind=daily) with scope.type=team and date_range for period.date.
   - team_weekly: get_weekly_reports(report_scope=personal), get_daily_reports(report_scope=personal), get_sessions, get_tasks, get_requirements, get_report_inventory(report_scope=personal, report_kind=weekly) with scope.type=team.
   - department_daily: get_daily_reports(report_scope=team), get_report_inventory(report_scope=team, report_kind=daily), get_requirements with scope.type=department and date_range for period.date.
   - department_weekly: get_weekly_reports(report_scope=team), get_daily_reports(report_scope=department), get_weekly_reports(report_scope=personal), get_requirements, get_report_inventory(report_scope=team, report_kind=weekly) with scope.type=department.
4. If selected_session_slice_keys is present and non-empty, every get_sessions call must include it so MCP filters to those slices. Use only facts returned by MCP tools. Do not invent tasks, sessions, blockers, progress, members, teams, or departments.
5. Produce concise Chinese Markdown suitable for the selected report_type.
6. Call write_report_result with {"report_type": report_type, "period": period, "target": target, "run_id": run_id, "content": markdown, "summary": optional_summary}.
7. If generation fails, call write_report_failure with {"report_type": report_type, "period": period, "target": target, "run_id": run_id, "error_message": error_message}.

## Output Rules

- The final report content must be non-empty Markdown.
- If there is insufficient context, say so in the Markdown instead of filling gaps.
- Missing daily/weekly reports are facts; include them only when relevant to the selected report type.
- Never expose run_id, MCP URLs, token, credential slots, or internal configuration in the user-facing report.
`, formatReportTypeList(data.SupportedReportTypes), data.MCPSlug, data.CredentialSlot)
}

func formatReportTypeList(reportTypes []string) string {
	descriptions := map[string]string{
		"personal_daily":    "current user's daily report.",
		"personal_weekly":   "current user's weekly report.",
		"team_daily":        "team daily report for the current user's allowed team scope.",
		"team_weekly":       "team weekly report for the current user's allowed team scope.",
		"department_daily":  "department daily report for the current user's allowed department scope.",
		"department_weekly": "department weekly report for the current user's allowed department scope.",
	}
	lines := make([]string, 0, len(reportTypes))
	for _, reportType := range reportTypes {
		reportType = strings.TrimSpace(reportType)
		if reportType == "" {
			continue
		}
		description := descriptions[reportType]
		if description == "" {
			description = "custom report type."
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", reportType, description))
	}
	return strings.Join(lines, "\n")
}

func DailyReportSkillMarkdown(mcpURL string) string {
	return ReportSkillMarkdown(mcpURL)
}
