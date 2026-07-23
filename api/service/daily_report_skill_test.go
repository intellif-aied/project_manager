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
		"one frozen Report Context V1",
		"## 1. Execute",
		"## 2. Route sources",
		"## 3. Build a private fact ledger",
		"## 4. Compose for the reader",
		"## 5. Deliver only the report",
		"Call get_report_context exactly once with {\"run_id\": run_id}",
		"do not call get_sessions, get_tasks, get_requirements, get_daily_reports, get_weekly_reports, get_report_inventory, or get_existing_report",
		"Missing member reports do not authorize a Session fallback",
		"Record each possible reader-facing claim with its support",
		"Future plan or recommendation",
		"A missing or invalid report proves only source unavailability",
		"Progress 100% with todo or active status is inconsistent, not completed",
		"A title or description may name an item but cannot support a result, live blocker, environment, or plan",
		"Never use Top-K or a fixed item count",
		"Do not create 明日计划, 下周计划, 后续计划, 建议, or 待协调 sections unless the ledger contains an explicitly supported future action",
		"Source coverage, submission rates, missing-report tables, and reminders to submit are not default report content",
		"do not expose raw codes such as todo, active, pending, review, high, or urgent",
		"Never expose Context/MCP field names or diagnostics",
		"Output the report, not an audit trail",
		"Every claimed result has work evidence",
		"Every future action is explicitly planned in the source",
		"write_report_result with {\"run_id\": run_id",
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
	for _, legacyIdentity := range []string{"Read report_type, period, target", "\"report_type\": report_type", "\"period\": period", "\"target\": target"} {
		if strings.Contains(markdown, legacyIdentity) {
			t.Fatalf("report skill retains legacy run identity %q", legacyIdentity)
		}
	}
	if len(markdown) > 15000 || len(strings.Split(markdown, "\n")) > 120 {
		t.Fatalf("default report skill has regressed into a rule pile: chars=%d lines=%d", len(markdown), len(strings.Split(markdown, "\n")))
	}
}
