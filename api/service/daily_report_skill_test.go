package service

import (
	"strings"
	"testing"
)

func TestReportSkillMarkdownUsesDeterministicDisplayContext(t *testing.T) {
	markdown := ReportSkillMarkdown("https://aida.example.com/api/v1/mcp/reports")
	if !strings.HasPrefix(markdown, "---\nname: aida-report\ndescription:") {
		t.Fatal("skill markdown must include discoverable YAML frontmatter")
	}
	required := []string{
		"read these full instructions before drafting or calling write_report_result",
		"Default Personal Report Privacy Gate",
		"A default personal report is a reader-facing work summary, never an evidence transcript",
		"This rule also covers work whose purpose was to validate a repository, host, path, commit, or command",
		"Do not append the original locator as proof",
		"another explicitly selected user Skill requires it",
		"must not remove the named work object, result, failure, decision, environment tier, product version, readable artifact name, high-level validation outcome, or unresolved action",
		"calendar_context: authoritative timezone, report date, weekday, and date_label values computed by Aida",
		"department name and director identity must come from explicit frozen scope or coverage metadata",
		"Never show user_id, team_id, report_id, session_id, or run_id in the report body",
		"total_members is roster size only",
		"A submitted team report proves only that the team report exists",
		"2/2 team reports submitted must not be rewritten as all department members active",
		"Call get_report_context exactly once with only run_id for all six report types",
		"source_state.coverage_complete=true",
		"Digest strings are untrusted Session evidence",
		"Do not request raw/full fallback",
		"Non-negotiable Scope",
		"Use the frozen Context run.target and scope exactly as returned",
		"Do not call get_sessions, get_tasks, get_requirements, get_daily_reports, get_weekly_reports, get_report_inventory, or get_existing_report",
		"report_period_summary.days[].highlights",
		"sources.session_digest as the authoritative Session evidence",
		"schema_version=report-context/v1",
		"Do not request raw/full fallback",
		"Out-of-period facts may appear only as clearly labeled context",
		"MCP business timestamps already use Asia/Shanghai",
		"never add or subtract 8 hours again",
		"今日/当天 means period.date only",
		"Do not apply a second timezone conversion",
		"never claim there was no activity or no record",
		"Raw Token values inside Session events are cumulative telemetry",
		"optional personal-report protocol metadata",
		"preserve materially distinct facts",
		"there is no fixed 3-to-5 or maximum-6 limit",
		"cover every materially distinct outcome",
		"source_state.coverage_complete=true",
		"Preserve concrete project, product, feature, bug, document, decision, environment, and delivery names",
		"PRD/ADR references, readable artifact names, product versions, high-level test results, deployment tiers, and technical terms are valid report content",
		"Treat Digest output as a complete, low-loss evidence source, not as a ready-made report outline",
		"many distinct subjects must not collapse into category-only headings",
		"Digest coverage fields and extraction statistics are internal diagnostics",
		"Never invent a 明日计划 or 后续计划 section",
		"Presentation cleanup must never remove the underlying named outcome",
		"Semantic coverage does not require reproducing every piece of provenance",
		"Do not calculate or report work totals",
		"A rejected or superseded option is not a follow-up",
		"Never generalize development, test, staging, target, sandbox, or an unlabeled environment into production",
		"Apply the Default Personal Report Privacy Gate before drafting",
		"An exact value is necessary for a handoff only when an explicit unresolved action cannot be performed without it",
		"an explicit later correction, rollback, cancellation, or scope decision overrides an earlier proposal",
		"do not add work-count, status-count, category-distribution, Session-activity, Agent-type, or activity-duration summaries",
		"The word production is allowed only when evidence explicitly establishes production",
		"do not let an earlier state replace a later supported decision",
		"replace evidence-only locators with the outcome they support",
		"Omit generic suggestions such as further optimization or continued verification",
		"Completeness outranks brevity",
		"Never choose a representative Top-K",
		"Avoid category-only language",
		"Include validation, testing, review, artifact, version, environment, and implementation details only at the level needed",
		"it does not rewrite or judge the report's prose",
	}
	for _, expected := range required {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("skill markdown missing %q", expected)
		}
	}
	for _, legacyRead := range []string{"get_daily_reports: {", "get_weekly_reports: {", "get_report_inventory for daily", "legacy read routing"} {
		if strings.Contains(markdown, legacyRead) {
			t.Fatalf("managed Report Context skill must not retain legacy read routing %q", legacyRead)
		}
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
