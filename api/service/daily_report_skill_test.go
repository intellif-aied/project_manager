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
		"group Facts into a two-level map",
		"Create Workstreams only from the first level",
		"Use exactly one subject per shared work object",
		"shortest evidence-supported user-facing shared capability",
		"title as the complete reader-facing headline",
		"Keep demos, test cases, validation scenarios, supporting metrics, and traces out of title",
		"Candidate IDs, model variants, stages, lanes, modules, repositories, directories, datasets, and evaluation runs are never subjects",
		"compare every subject pair",
		"sharing the same leading named entity",
		"A manual, document, report, or task package is an outcome, never a subject",
		"separate goal and independently reviewable outcome",
		"Read every work_evidence.fact and fact_ref before selecting reportable work",
		"A Fact does not need to appear just because it is verifiable",
		"project_memory_context",
		"optional context, not evidence or assignment",
		"Current Facts win",
		"related_fact_refs suggest similarity, not identity",
		"otherwise ignore it",
		"candidate_only is weak",
		"supporting evidence by default, not standalone deliverables",
		"at most three reader-worthy deliverables per workstream",
		"representative non-duplicate fact_refs",
		"references prove an outcome, not coverage",
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
		"likely, possible, or insufficient evidence must never become confirmed",
		"deliverable with only result and fact_refs",
		"Do not create state, environment, validation, next_action, recommendation, or audit fields",
		"normally keep excluded_facts empty and leave omitted Facts unreferenced",
		"server records every unreferenced Fact as not_selected",
		"Do not send not_selected yourself",
		"If no_reportable_work is true, explicitly exclude every available Fact",
		"Build exactly this inner JSON shape",
		"the workstream object, then the workstreams array before excluded_facts",
		"excluded_facts belongs to the root object, never inside a workstream",
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
		"Separate names from prose",
		"Skill, RTL, CUDA, and H20",
		"when evidence also contains a translated nickname, use the established literal",
		"say 调整弹窗点击后的跳转, not 通知深链",
		"same name-versus-prose distinction",
		"everyday Chinese without invented technical labels",
		"Do not select or rewrite Summary from deliverables",
		"copying each accepted workstream title verbatim",
		"several candidates under one subject remain one summary item",
		"an increase of X% means a total of 1+X/100 times the baseline",
		"State an explicit resource constraint as observed",
		"Never add whether work was not started, stopped, merged, released, or deployed",
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
		"Every fact_ref must be included in at least one deliverable or excluded",
		"Evaluation tools, datasets, review packages, and documentation stay as deliverables",
		"anchored workstream_subject/max_workstreams",
	} {
		if strings.Contains(markdown, forbidden) {
			t.Fatalf("managed Report Context skill retains obsolete instruction %q", forbidden)
		}
	}
	if len(markdown) > 11000 || len(strings.Split(markdown, "\n")) > 130 {
		t.Fatalf("default report skill has regressed into a rule pile: chars=%d lines=%d", len(markdown), len(strings.Split(markdown, "\n")))
	}
}
