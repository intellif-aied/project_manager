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
		"correlation hints, never report text",
		"Group Facts into a two-level map",
		"Create Workstreams only from the first level",
		"Use one subject per shared work object",
		"Keep workspace_context groups separate",
		"Default to one Workstream per group",
		"Split only for distinct project names in Facts",
		"put unnamed modules below",
		"Hints may link groups",
		"MCP cannot",
		"shortest evidence-supported user-facing capability",
		"title as one natural headline of 16–52 Chinese characters",
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
		"Current Facts always determine the reportable outcomes",
		"workspace_semantic Hint",
		"semantic_fact_refs are strong current-day anchors",
		"workspace_fact_refs are co-located candidates",
		"Final parent check is mandatory",
		"merge two or more proposed Workstreams containing one Hint's semantic_fact_refs",
		"Keep a workspace-only Workstream separate",
		"Use canonical_name for anchored compatible Facts",
		"Other Hints remain weak",
		"supporting evidence by default, not standalone deliverables",
		"keep one or two reader-worthy deliverables per workstream by default",
		"use a third only for an independent necessary outcome",
		"35–120 Chinese characters",
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
		"one canonical report",
		"server deterministically renders the accepted Brief into one report body",
		"renders deliverable results as its nested bullet items",
		"Do not add headings, 工作概览, 工作详情",
		"Separate names from prose",
		"Skill, RTL, CUDA, and H20",
		"when evidence also contains a translated nickname, use the established literal",
		"say 调整弹窗点击后的跳转, not 通知深链",
		"Do not perform another semantic selection",
		"REPORT_RESULT_RETRY_EXHAUSTED",
		"retry the same summary and content without brief_hash",
		"## 3. Direct flow: interpret the evidence",
		"For personal_daily, use Git, path, test, build, merge, and deployment data only to associate work with a subject",
		"## 4. Direct flow: write the report",
		"write one Markdown ordered list",
		"Call write_report_result exactly once with {\"summary\": report, \"content\": report}",
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
