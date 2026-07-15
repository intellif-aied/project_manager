package service

import (
	"strings"
	"testing"
)

func TestReportSkillMarkdownUsesDeterministicDisplayContext(t *testing.T) {
	markdown := ReportSkillMarkdown("https://aida.example.com/api/v1/mcp/reports")
	required := []string{
		"calendar_context: authoritative date, weekday, and date_label values computed by Aida",
		"use scope_context.department_name as the department display name",
		"Never expose raw identifiers in the report body",
		"REPORT_CONTENT_INVALID",
		`"report_kind": "daily", "date_range": date_range`,
		`"report_kind": "weekly", "week_range": week_range`,
		"total_members is roster size only",
		"A submitted team report proves only that the team report exists",
		"2/2 team reports submitted must not be rewritten as all department members active",
		"report_source_selection_id snapshot is the authoritative Session source",
		"Read every page through has_more=false",
	}
	for _, expected := range required {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("skill markdown missing %q", expected)
		}
	}
	if strings.Contains(markdown, `"report_kind": "weekly", "date_range": date_range`) {
		t.Fatal("weekly inventory instructions must not use date_range")
	}
	if strings.Contains(markdown, "selected_session_slice_keys") || strings.Contains(markdown, "legacy") {
		t.Fatal("default report skill must not contain the legacy Session source path")
	}
}
