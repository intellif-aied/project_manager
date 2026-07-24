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
		"one frozen Report Context",
		"## 1. Read the complete frozen Context",
		"## 2. Interpret frozen sources",
		"## 3. Build one private objective-outcome ledger",
		"## 4. Compose around objectives and outcomes",
		"## 5. Deliver only the report",
		"Call get_report_context exactly once with {\"run_id\": run_id}",
		"call write_report_result exactly once with {\"run_id\": run_id, \"summary\": summary, \"content\": markdown}",
		"Do not call get_sessions, get_tasks, get_requirements, get_daily_reports, get_weekly_reports, get_report_inventory, or get_existing_report",
		"presentation_profile is present, use its summary_focus and content_grouping as the only report-type-specific presentation contract",
		"Do not choose a source route from report_type",
		"work_evidence.facts contains ordinary JSON objects",
		"Exactly repeated fact text from the same evidence source appears once with its distinct date/status observations",
		"there are no lookup tables, positional rows, or source-goal groups to reconstruct",
		"Reconstruct coherent workstreams from supported outcomes and relationships across facts",
		"Group features, documents, deployment, validation, investigation, and fixes under one workstream",
		"Keep work separate only when the business objective, delivered outcome, owner scope, or current state is genuinely independent",
		"Git commands, output, commit messages, commit metadata, hashes, branches, merges, reverts, pushes, pulls, checkouts, and conflict operations are trace data only",
		"They never independently support a work result, completion, release, recovery, validation, or risk conclusion",
		"non-Git evidence independently links it to a work objective",
		"There are no operation-type exceptions",
		"Objective and workstream membership",
		"Progress 100% with todo or active status is inconsistent, not completed",
		"Never use Top-K, a fixed theme count, or silent omission",
		"Use a dynamic level-two heading for each real workstream",
		"Never add a fixed 重点工作 heading and never rank work as important or unimportant",
		"Produce summary as one plain-text paragraph with no Markdown heading or list",
		"Produce content as the complete Markdown body without a 工作总结 section",
		"本期无可核验的工作记录",
		"Never create independent 明日计划, 下周计划, 后续计划, 建议, or 待协调 sections",
		"Preserve an explicitly supported future action only inside its related workstream",
		"Source coverage, submission rates, missing-report tables, and reminders are not default report content",
		"Avoid a chronological transcript and avoid repeating the same facts in a separate status summary",
		"do not expose raw codes such as todo, active, pending, review, high, or urgent",
		"Never expose Context/MCP field names or diagnostics",
		"Output the report, not an audit trail",
		"The complete frozen Context was read once",
		"Sections represent coherent objectives and outcomes rather than source fragments",
		"Summary is one non-empty plain-text paragraph; content is non-empty Markdown without 工作总结 or fixed 重点工作",
	}
	for _, expected := range required {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("skill markdown missing %q", expected)
		}
	}
	for _, legacyRead := range []string{"get_daily_reports: {", "get_weekly_reports: {", "get_report_inventory for daily", "legacy read routing", "write that the report is based on saved reports", "list them as no activity/no saved report"} {
		if strings.Contains(markdown, legacyRead) {
			t.Fatalf("managed Report Context skill must not retain legacy read routing %q", legacyRead)
		}
	}
	for _, obsoleteRouting := range []string{"personal_daily uses frozen", "personal_weekly uses frozen", "team reports use frozen", "Department reports use the frozen team"} {
		if strings.Contains(markdown, obsoleteRouting) {
			t.Fatalf("report skill retains report-type source routing %q", obsoleteRouting)
		}
	}
	for _, legacyIdentity := range []string{"Read report_type, period, target", "\"report_type\": report_type", "\"period\": period", "\"target\": target"} {
		if strings.Contains(markdown, legacyIdentity) {
			t.Fatalf("report skill retains legacy run identity %q", legacyIdentity)
		}
	}
	for _, internalPath := range []string{"sessions[].digest", "report_period_summary.days", "period_result_focused", "work_units"} {
		if strings.Contains(markdown, internalPath) {
			t.Fatalf("report skill depends on internal Digest path %q", internalPath)
		}
	}
	for _, obsoleteProjection := range []string{"Decode every row", "one-based lookup tables", "resolve every result-text reference", "Exact source-goal groups"} {
		if strings.Contains(markdown, obsoleteProjection) {
			t.Fatalf("report skill retains obsolete columnar projection instruction %q", obsoleteProjection)
		}
	}
	if len(markdown) > 15000 || len(strings.Split(markdown, "\n")) > 120 {
		t.Fatalf("default report skill has regressed into a rule pile: chars=%d lines=%d", len(markdown), len(strings.Split(markdown, "\n")))
	}
}
