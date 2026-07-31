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
		"Call get_report_context exactly once with {}",
		"## 2. Mandatory personal daily Report Brief",
		"two distinct semantic passes in this same Agent Session",
		"user-authored goals",
		"correlation hints, never headings or report text",
		"subject is the shortest stable product, initiative, protocol, or business capability name",
		"manager could independently assign and review the business or engineering outcomes",
		"Evaluation tools, datasets, review packages, and documentation stay as deliverables",
		"Keep one to three workstreams by default and never more than five",
		"CWD, branch, file, artifact type, tool call",
		"operational traces as hidden association evidence only",
		"must not appear as a deliverable or final report statement",
		"what capability, design, problem, or user experience changed",
		"scope-boundary clauses such as only affects, does not change, or keeps something unchanged",
		"Never infer that work was not merged, not released, not deployed, unfinished, or unsuitable for production",
		"Assistant suggestions, cautions, release recommendations",
		"not user work results",
		"source starts with agent_claim contains Assistant-authored text",
		"does not verify every sentence",
		"deliverable with only result and fact_refs",
		"Do not create state, environment, validation, next_action, recommendation, or audit fields",
		"Every fact_ref must be included in at least one deliverable or excluded",
		"Build exactly this inner JSON shape",
		`"subject":"..."`,
		`"result":"...","fact_refs"`,
		"Call write_report_brief with {\"brief_json\":\"<serialized inner JSON object>\"}",
		"REPORT_BRIEF_RETRY_EXHAUSTED",
		"do not fail the run",
		"returned normalized Brief as the only writing source",
		"write only the accepted deliverable results",
		"put each result on its own Markdown ordered-list line",
		"Never add commit, merge, test, deployment, release, environment, validation, recommendation",
		"Omit statements that a change only affects something, does not change something, or keeps something unchanged",
		"Do not add a conclusion such as 尚未发布, 不建议上线, 可以合并, or 待部署",
		"summary as a reader-facing translation, not a shortened copy of the Brief",
		"A teammate unfamiliar with the implementation must understand each item on first read",
		"use everyday Chinese to say what problem was solved or what outcome changed",
		"translate internal architecture labels, method names, and newly coined concepts",
		"rewrite 优化日报工作主线关联与主题显著性 as 优化 AI 日报对项目工作的识别和筛选，减少同一件事被拆成多条",
		"rewrite 构建约束式评测体系 as 建立日报效果对比方法，用真实数据检验生成质量",
		"summary as a Markdown ordered list with exactly one item per accepted workstream",
		"Do not put blank lines between items",
		"REPORT_RESULT_RETRY_EXHAUSTED",
		"retry the same summary and content without brief_hash",
		"## 3. Direct flow: interpret the evidence",
		"For personal_daily, use Git, path, test, build, merge, and deployment data only to associate work with a subject",
		"## 4. Direct flow: write the report",
		"apply the same reader-facing, everyday-Chinese translation rule to the ordered-list summary",
		"Call write_report_result exactly once with {\"summary\": summary, \"content\": markdown}",
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
		`"objective":"..."`, `"state":"`, `"environment":"`,
		`"validation":"..."`, `"next_action":"..."`,
		"released/production needs explicit", "Preserve every deliverable's state",
	} {
		if strings.Contains(markdown, forbidden) {
			t.Fatalf("managed Report Context skill retains obsolete instruction %q", forbidden)
		}
	}
	if len(markdown) > 11000 || len(strings.Split(markdown, "\n")) > 130 {
		t.Fatalf("default report skill has regressed into a rule pile: chars=%d lines=%d", len(markdown), len(strings.Split(markdown, "\n")))
	}
}
