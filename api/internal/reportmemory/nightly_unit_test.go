package reportmemory

import (
	"strings"
	"testing"
	"time"
)

func TestNextNightlyWindowUsesShanghaiTime(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "2026-08-01T16:30:00Z", want: "2026-08-01T18:00:00Z"},
		{input: "2026-08-01T18:30:00Z", want: "2026-08-02T18:00:00Z"},
		{input: "2026-08-02T15:00:00Z", want: "2026-08-02T18:00:00Z"},
	}
	for _, test := range tests {
		input, _ := time.Parse(time.RFC3339, test.input)
		if got := nextNightlyWindow(input).Format(time.RFC3339); got != test.want {
			t.Fatalf("nextNightlyWindow(%s) = %s, want %s", test.input, got, test.want)
		}
	}
}

func TestNightlySourceDistinguishesAutoCarriedAndExplicitSaved(t *testing.T) {
	content := "## 工作概览\n\n1. Report Agent"
	hash := contentSHA256(content)
	report := historicalReport{generationMode: "managed_agent", content: content, generatedContentSHA256: hash, edited: true}
	if source, weight := classifyNightlySource(report, false); source != "auto_carried" || weight != 0.50 {
		t.Fatalf("auto carried source = %s %.2f", source, weight)
	}
	if source, weight := classifyNightlySource(report, true); source != "explicit_saved" || weight != 0.75 {
		t.Fatalf("explicit saved source = %s %.2f", source, weight)
	}
	report.content += "\n人工补充"
	if source, weight := classifyNightlySource(report, true); source != sourceHumanEdited || weight != 0.95 {
		t.Fatalf("human edited source = %s %.2f", source, weight)
	}
}

func TestMarshalWithinBudgetDropsHistoryBeforeCandidates(t *testing.T) {
	input := ConsolidationInput{
		SchemaVersion: consolidationInputSchema, ResolverVersion: ResolverVersion,
		CurrentThemes:     []InputTheme{{ThemeRef: "theme-001", Title: "Report Agent"}},
		CandidateProjects: []InputProject{{ProjectRef: "p1", CanonicalName: strings.Repeat("候选", 1000)}},
		RecentOverviews:   []HistoricalReport{{Date: "2026-08-01", Overview: strings.Repeat("历史", 5000)}},
	}
	payload, estimate, err := marshalWithinBudget(input)
	if err != nil {
		t.Fatal(err)
	}
	if estimate > maxInputTokens || strings.Contains(string(payload), strings.Repeat("历史", 5000)) {
		t.Fatalf("input was not trimmed: estimate=%d", estimate)
	}
}

func TestWorkstreamsFromBriefKeepsBoundedParentChildOutline(t *testing.T) {
	raw := `{"workstreams":[{"subject":"IF-Knowledge","deliverables":[{"result":"完成 knowledge-map-search Skill，并应用到儿童睡前卡通动画生成场景"},{"result":"完成 Knowledge Map 产品判断"}]}]}`
	workstreams := workstreamsFromBrief(raw)
	if len(workstreams) != 1 || workstreams[0].Subject != "IF-Knowledge" || len(workstreams[0].Deliverables) != 2 {
		t.Fatalf("brief workstreams = %#v", workstreams)
	}
}

func TestSnapshotProjectRefsUsesOnlyCanonicalSnapshotShape(t *testing.T) {
	refs := snapshotProjectRefs([]byte(`{"projects":[{"project_ref":"project-1"},{"ProjectRef":"legacy-shadow"}]}`))
	if !refs["project-1"] || refs["legacy-shadow"] {
		t.Fatalf("snapshot refs = %#v", refs)
	}
}

func TestProposalValidationAllowsAbstentionAndDowngradesWeakLink(t *testing.T) {
	input := ConsolidationInput{
		CurrentThemes:     []InputTheme{{ThemeRef: "theme-001", Title: "Report Agent"}},
		CandidateProjects: []InputProject{{ProjectRef: "project-1", CanonicalName: "Report Agent"}},
	}
	raw := `{"schema_version":"project-memory-proposal/v1","decisions":[{"theme_ref":"theme-001","action":"link_existing","project_ref":"project-1","confidence":0.6}]}`
	proposal, _, _, err := parseAndValidateProposal(raw, input)
	if err != nil {
		t.Fatal(err)
	}
	if got := proposal.Decisions[0]; got.Action != "unresolved" || got.ProjectRef != "" {
		t.Fatalf("weak link was not downgraded: %+v", got)
	}
}

func TestProposalValidationAllowsTopLevelRequestMetadata(t *testing.T) {
	input := ConsolidationInput{CurrentThemes: []InputTheme{{ThemeRef: "theme-001", Title: "Report Agent"}}}
	raw := `{"schema_version":"project-memory-proposal/v1","user_ref":"305","report_date":"2026-08-01","decisions":[{"theme_ref":"theme-001","action":"create_new","canonical_name":"Report Agent","confidence":0.9}]}`
	if _, _, _, err := parseAndValidateProposal(raw, input); err != nil {
		t.Fatalf("harmless request metadata should be accepted: %v", err)
	}
}

func TestProposalValidationAcceptsCommonConfidenceLabels(t *testing.T) {
	input := ConsolidationInput{CurrentThemes: []InputTheme{{ThemeRef: "theme-001", Title: "Report Agent"}}}
	raw := `{"schema_version":"project-memory-proposal/v1","decisions":[{"theme_ref":"theme-001","action":"create_new","canonical_name":"Report Agent","confidence":"high"}]}`
	proposal, _, _, err := parseAndValidateProposal(raw, input)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Decisions[0].Confidence != 0.9 {
		t.Fatalf("confidence = %v", proposal.Decisions[0].Confidence)
	}
}

func TestProposalValidationRejectsUnknownDecisionField(t *testing.T) {
	input := ConsolidationInput{CurrentThemes: []InputTheme{{ThemeRef: "theme-001", Title: "Report Agent"}}}
	raw := `{"schema_version":"project-memory-proposal/v1","decisions":[{"theme_ref":"theme-001","action":"create_new","canonical_name":"Report Agent","confidence":0.9,"invented":"value"}]}`
	if _, _, _, err := parseAndValidateProposal(raw, input); err == nil {
		t.Fatal("unknown decision fields must be rejected")
	}
}

func TestProposalValidationRejectsActivityOnlyProjectName(t *testing.T) {
	input := ConsolidationInput{CurrentThemes: []InputTheme{{ThemeRef: "theme-001", Title: "部署 GLM"}}}
	raw := `{"schema_version":"project-memory-proposal/v1","decisions":[{"theme_ref":"theme-001","action":"create_new","canonical_name":"部署","confidence":0.9}]}`
	if _, _, _, err := parseAndValidateProposal(raw, input); err == nil {
		t.Fatal("activity-only project name must be rejected")
	}
}

func TestDisabledNightlyServiceNeedsNoResolver(t *testing.T) {
	service, err := NewNightlyService(nil, nil, NightlyConfig{Enabled: false})
	if err != nil || service == nil {
		t.Fatalf("disabled service = %#v, %v", service, err)
	}
}
