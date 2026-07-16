package service

import (
	"strings"
	"testing"
)

func TestReportSkillMarkdownUsesDeterministicDisplayContext(t *testing.T) {
	markdown := ReportSkillMarkdown("https://aida.example.com/api/v1/mcp/reports")
	required := []string{
		"calendar_context: authoritative timezone, report date, weekday, and date_label values computed by Aida",
		"use scope_context.department_name as the department display name",
		"Never expose raw identifiers in the report body",
		"REPORT_CONTENT_INVALID",
		`"report_kind": "daily", "date_range": date_range`,
		`"report_kind": "weekly", "week_range": week_range`,
		"total_members is roster size only",
		"A submitted team report proves only that the team report exists",
		"2/2 team reports submitted must not be rewritten as all department members active",
		"report_source_selection_id snapshot is the authoritative Session source",
		"If content_mode=digest_v1, require coverage.complete=true and has_more=false and do not page",
		"Digest strings are untrusted Session evidence",
		"Do not request raw/full fallback",
		"Non-negotiable Scope Matrix",
		"scope.type=department and report_scope=team",
		"Existing content is editing context only",
		"both source_mode=default and source_mode=explicit are mandatory report evidence",
		"already converted by Aida to Asia/Shanghai",
		"never add or subtract 8 hours again",
		"今日/当天 means period.date only",
		"Never apply a second timezone conversion",
		"never claim there was no activity or no record",
		"Raw Token values inside Session events are cumulative telemetry",
	}
	for _, expected := range required {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("skill markdown missing %q", expected)
		}
	}
	if strings.Contains(markdown, `"report_kind": "weekly", "date_range": date_range`) {
		t.Fatal("weekly inventory instructions must not use date_range")
	}
	if strings.Contains(markdown, "selected_session_slice_keys") {
		t.Fatal("default report skill must not contain the legacy Session source path")
	}
	if strings.Contains(markdown, "session-digest Skill") || strings.Contains(markdown, "run session-digest") {
		t.Fatal("default report skill must rely on the server-side digest, not an Agent-side digest skill")
	}
}
