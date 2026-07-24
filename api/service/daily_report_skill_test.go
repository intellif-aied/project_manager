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
		"frozen Context is the only evidence source",
		"## 1. Execute the run",
		"## 2. Interpret the evidence",
		"## 3. Write the report",
		"## 4. Keep internals private",
		"Call get_report_context exactly once with {\"run_id\": run_id}",
		"Call write_report_result exactly once with {\"run_id\": run_id, \"summary\": summary, \"content\": markdown}",
		"Do not call any legacy source tool",
		"Do not emit progress narration between tools",
		"Follow presentation_profile",
		"work_evidence.facts are compact outcomes or unresolved items, not automatic headings",
		"Reconstruct the smallest set of coherent workstreams",
		"Group implementation, documentation, deployment, validation, investigation, and fixes when they serve the same objective",
		"Git commands and metadata are trace data, not report content",
		"non-Git evidence explicitly supplies that outcome",
		"100% progress with a non-completed status is not completed",
		"covers every materially distinct supported outcome, failure, blocker, and unresolved action",
		"Use one dynamic level-two heading per coherent workstream",
		"Do not add a fixed 重点工作 heading",
		"Produce summary as one non-empty plain-text paragraph",
		"Produce content as non-empty Markdown without 工作总结",
		"本期无可核验的工作记录",
		"Do not create independent 明日计划, 下周计划, 后续计划, 建议, or 待协调 sections",
		"Return only the report through write_report_result",
		"Omit source diagnostics, coverage commentary, field names, IDs, references",
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
	if len(markdown) > 7000 || len(strings.Split(markdown, "\n")) > 80 {
		t.Fatalf("default report skill has regressed into a rule pile: chars=%d lines=%d", len(markdown), len(strings.Split(markdown, "\n")))
	}
}
