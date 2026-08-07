package reportreview

import (
	"strings"
	"testing"

	"github.com/aidashboard/api/internal/reportbrief"
	"github.com/aidashboard/api/internal/reportcontext"
)

func TestBuildInputKeepsSelectedFactsAndLimitsMemoryToCandidates(t *testing.T) {
	context := reportcontext.StoredContext{
		Hash: strings.Repeat("a", 64),
		Payload: []byte(`{
			"work_evidence":{"facts":[
				{"fact_ref":"fact-001","text":"尚未证明项目命名更准确","source":"agent_claim"},
				{"fact_ref":"fact-002","text":"邮件能力默认关闭，未执行真实 SMTP","source":"user"},
				{"fact_ref":"fact-003","text":"完成 Windows 自动同步修复","source":"tool_observation"}
			]},
			"project_memory_context":{"hints":[{
				"project_ref":"project-aida","canonical_name":"AIDA日报系统Project Memory与CLI修复",
				"semantic_fact_refs":["fact-001"],"workspace_fact_refs":["fact-002"],
				"aliases":["AIDA","日报平台"],"workstream_cues":["Project Memory","CLI"],
				"confidence":0.99,"match_basis":"workspace_semantic"
			}]}
		}`),
	}
	candidate := reportbrief.Stored{
		BriefHash: strings.Repeat("b", 64), ContextHash: context.Hash,
		Payload: reportbrief.Payload{
			SchemaVersion: reportbrief.SchemaVersion, ReportType: reportcontext.ReportTypePersonalDaily,
			Period: reportbrief.Period{Start: "2026-08-06", End: "2026-08-06"},
			Workstreams: []reportbrief.Workstream{{
				Subject: "AIDA", Title: "推进 AIDA",
				Deliverables: []reportbrief.Deliverable{{Result: "项目命名更准确", FactRefs: []string{"fact-001"}}},
			}},
		},
	}
	input, _, err := BuildInput("run-1", context, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.SelectedFacts) != 1 || input.SelectedFacts[0].FactRef != "fact-001" {
		t.Fatalf("selected facts = %#v", input.SelectedFacts)
	}
	if len(input.ReviewCandidates) != 2 || input.ReviewCandidates[0].FactRef != "fact-002" {
		t.Fatalf("review candidates = %#v", input.ReviewCandidates)
	}
	if len(input.ProjectCandidates) != 1 || input.ProjectCandidates[0].CanonicalName == "" {
		t.Fatalf("project candidates = %#v", input.ProjectCandidates)
	}
	if strings.Join(input.ProjectCandidates[0].Aliases, ",") != "AIDA,日报平台" ||
		strings.Join(input.ProjectCandidates[0].WorkstreamCues, ",") != "Project Memory,CLI" {
		t.Fatalf("project identity cues were not preserved: %#v", input.ProjectCandidates[0])
	}
	if input.ProjectCandidates[0].IdentityUsage != "parent_label_for_matching_cues" {
		t.Fatalf("project identity usage was not derived: %#v", input.ProjectCandidates[0])
	}
}

func TestMatchingWorkstreamTargetsRequiresCurrentCueMatches(t *testing.T) {
	targets := matchingWorkstreamTargets([]reportbrief.Workstream{
		{Subject: "OSCAR-vLLM", Deliverables: []reportbrief.Deliverable{{Result: "完成 CUDA Graph 验证"}}},
		{Subject: "Qwen3", Deliverables: []reportbrief.Deliverable{{Result: "OSCAR TTFT 改善"}}},
		{Subject: "日报邮件"},
	}, []string{"OSCAR", "KV Cache"})
	if strings.Join(targets, ",") != "w1,w2" {
		t.Fatalf("workstream cue targets = %#v", targets)
	}
}

func TestCompactReviewFactTextKeepsBoundedHeadAndTail(t *testing.T) {
	value := strings.Repeat("开", 700) + "中间" + strings.Repeat("结", 300)
	compacted := compactReviewFactText(value)
	if len([]rune(compacted)) > maxReviewFactRunes || !strings.HasPrefix(compacted, "开开") || !strings.HasSuffix(compacted, "结结") || !strings.Contains(compacted, "…") {
		t.Fatalf("fact text was not compacted with head and tail: %d", len([]rune(compacted)))
	}
}

func TestAgentViewOmitsCompilerAuditExclusions(t *testing.T) {
	fullResult := strings.Repeat("长", 160)
	input := Input{Candidate: reportbrief.Payload{
		Workstreams: []reportbrief.Workstream{{
			Subject: "项目", Title: "项目推进",
			Deliverables: []reportbrief.Deliverable{{Result: fullResult, FactRefs: []string{"fact-001"}}},
		}},
		ExcludedFacts: []reportbrief.ExcludedFact{{FactRef: "fact-002", Reason: "not_selected"}},
	}}
	view := input.AgentView()
	if len(view.Candidate.ExcludedFacts) != 0 || len(input.Candidate.ExcludedFacts) != 1 {
		t.Fatalf("agent view must omit exclusions without mutating stored input: %#v %#v", view, input)
	}
	got := view.Candidate.Workstreams[0].Deliverables[0].Result
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != 120 {
		t.Fatalf("agent view result = %q, want bounded reviewer copy", got)
	}
	if input.Candidate.Workstreams[0].Deliverables[0].Result != fullResult {
		t.Fatal("agent view mutated the stored semantic Brief")
	}
}
