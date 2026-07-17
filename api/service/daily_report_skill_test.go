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
		"Never show user_id, team_id, report_id, session_id, or run_id in the report body",
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
		"Nested item summaries are intentionally omitted",
		"Out-of-period facts may appear only as clearly labeled context",
		"MCP business timestamps already use Asia/Shanghai",
		"never add or subtract 8 hours again",
		"今日/当天 means period.date only",
		"Do not apply a second timezone conversion",
		"never claim there was no activity or no record",
		"Raw Token values inside Session events are cumulative telemetry",
		"never in selected_session_slice_keys",
		"preserve materially distinct facts",
		"there is no fixed 3-to-5 or maximum-6 limit",
		"cover every materially distinct outcome",
		"outcome_coverage.complete=true",
		"Preserve concrete project, product, feature, bug, document, decision, environment, and delivery names",
		"PRD/ADR references, meaningful file names, versions, test results, deployment targets, and technical terms are valid report content",
		"Treat Digest output as a complete, low-loss evidence source, not as a ready-made report outline",
		"Do not collapse many distinct subjects into category-only headings",
		"Digest coverage fields and extraction statistics are internal diagnostics",
		"Never invent a 明日计划 or 后续计划 section",
		"Removing diagnostic counts must never remove the underlying named work",
		"Completeness outranks brevity",
		"Never choose a representative Top-K",
		"Avoid category-only language",
		"Validation, testing, review, commands, files, paths, versions, and implementation details may be included",
		"it does not rewrite or judge the report's prose",
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
	for _, obsolete := range []string{
		"REPORT_CONTENT_INVALID",
		"Never output raw URLs, file paths, file names",
		"It must contain no exact commit hash, PRD/ADR number or artifact name",
		"Never create sections titled 验证结果",
	} {
		if strings.Contains(markdown, obsolete) {
			t.Fatalf("skill markdown still contains obsolete censorship rule %q", obsolete)
		}
	}
}
