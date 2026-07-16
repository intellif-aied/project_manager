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
		"do not call get_existing_report",
		"report_period_summary.days[].highlights",
		"selection-level report_period_summary",
		"Nested item report_period_summary values are intentionally absent",
		"nested item summaries are intentionally omitted",
		"Synthesize outcomes, not a turn-by-turn conversation recap",
		"Out-of-period facts may appear only as clearly labeled context",
		"already converted by Aida to Asia/Shanghai",
		"never add or subtract 8 hours again",
		"今日/当天 means period.date only",
		"Never apply a second timezone conversion",
		"never claim there was no activity or no record",
		"Raw Token values inside Session events are cumulative telemetry",
		"never in selected_session_slice_keys",
		"Do not state aggregate work-item counts",
		"Never create sections titled 验证结果",
		"Never output raw URLs, file paths, file names",
		"Never output document counts, file counts, line counts",
		"retain only the latest effective version",
		"Do not expose Registry owner, Skill ID, test account",
		"3 to 5 material outcomes",
		"never exceed 6",
		"include every one exactly once",
		"never write “无”, “暂无”",
		"coverage check keyed by canonical highlight work_unit_ref",
		"If a completed ref is missing",
		"health-check status",
		"silently rewrite every retained highlight into a user-facing outcome",
		"If a highlight cannot be stated as a meaningful user-facing outcome",
		"must not be a title-only label",
		"Title-only numbered items are invalid",
		"Do not retain an item whose only result is tests passed",
		"Never output a standalone outcome such as “全流程测试完成”",
	}
	for _, expected := range required {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("skill markdown missing %q", expected)
		}
	}
	if strings.Contains(markdown, `"report_kind": "weekly", "date_range": date_range`) {
		t.Fatal("weekly inventory instructions must not use date_range")
	}
	if strings.Contains(markdown, "session-digest Skill") || strings.Contains(markdown, "run session-digest") {
		t.Fatal("default report skill must rely on the server-side digest, not an Agent-side digest skill")
	}
}
