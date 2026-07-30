package service

import (
	"strings"
	"testing"
)

func TestReportSkillMarkdownUsesReportBriefForPersonalDaily(t *testing.T) {
	markdown := ReportSkillMarkdown("https://aida.example.com/api/v1/mcp/reports")
	if !strings.HasPrefix(markdown, "---\nname: aida-report\ndescription:") {
		t.Fatal("skill markdown must include discoverable YAML frontmatter")
	}
	required := []string{
		"frozen Context is the only evidence source",
		"credential already identifies the current user and Report Run",
		"Call get_report_context exactly once with {}",
		"If that tool is unavailable, or for every other report type, use the unchanged direct composition flow",
		"## 2. Mandatory personal daily Report Brief",
		"two distinct semantic passes in this same Agent Session",
		"Read work_evidence.threads as the work-thread dictionary",
		"Facts sharing a thread_ref belong to the same work thread",
		"After grouping candidates by work thread and goal",
		"The Brief continues to cite evidence only through fact_refs",
		"Every fact_ref must be included in at least one deliverable or excluded",
		"Reviewing every fact does not mean promoting every real activity to a report heading",
		"Keep one to three primary workstreams by default",
		"Use four or five only when every additional workstream has an independently important outcome",
		"do not establish importance by themselves",
		"through fact_refs instead of creating extra headings",
		"same-product technical analysis or Q&A",
		"A personal_daily is a status update, not an audit record",
		"one paragraph of at most three sentences per workstream",
		"resolved failures",
		"reason secondary_activity",
		"Never use secondary_activity to hide a fact",
		"Never exceed five",
		"released requires explicit production evidence and environment=production",
		"Build exactly this inner JSON shape",
		"Never use name instead of title",
		"reason, state, and environment must use one exact English enum value",
		"call write_report_brief with {\"brief_json\":\"<serialized inner JSON object>\"}",
		"correct every reported violation together",
		"correct an invalid Brief at most twice",
		"REPORT_BRIEF_RETRY_EXHAUSTED",
		"compose a degraded report from the last submitted Brief draft",
		"without brief_hash",
		"accepted_brief.brief_hash",
		"normalized Brief as the only writing source",
		"For personal_daily, produce summary as a Markdown ordered list",
		"exactly one item per accepted Brief workstream in the same order",
		"Do not put blank lines between items",
		"Do not add the 工作概览 or 工作详情 heading",
		"If it returns REPORT_RESULT_INVALID",
		"REPORT_RESULT_RETRY_EXHAUSTED",
		"retry the same summary and content without brief_hash",
		"Organize headings by work objective",
		"Use exactly one descriptive level-three heading per accepted Brief workstream and keep the same order",
		"never use 报表",
		"Never write 报告Agent, 报告MCP, or 深链",
		"点击通知直接打开对应报告",
		"## 3. Direct flow: interpret the evidence",
		"Follow presentation_profile for the current report's summary focus and grouping. It controls presentation, not evidence scope.",
		"Git commands and metadata are trace data, not report content",
		"## 4. Direct flow: write the report",
		"Call write_report_result exactly once with {\"summary\": summary, \"content\": markdown}",
		"Never pass a report identity field",
		"## 5. Keep internals private",
	}
	for _, expected := range required {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("skill markdown missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		"get_daily_reports: {", "get_weekly_reports: {", "sessions[].digest",
		"report_period_summary.days", "Decode every row", "one-based lookup tables",
	} {
		if strings.Contains(markdown, forbidden) {
			t.Fatalf("managed Report Context skill retains obsolete instruction %q", forbidden)
		}
	}
	if len(markdown) > 11000 || len(strings.Split(markdown, "\n")) > 130 {
		t.Fatalf("default report skill has regressed into a rule pile: chars=%d lines=%d", len(markdown), len(strings.Split(markdown, "\n")))
	}
}
