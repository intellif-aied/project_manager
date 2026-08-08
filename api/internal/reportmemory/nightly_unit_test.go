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

func TestMarshalWithinBudgetDropsOlderAnchorsBeforeRecentOverviews(t *testing.T) {
	recent := strings.Repeat("近期", 1200)
	anchor := strings.Repeat("锚点", 5000)
	input := ConsolidationInput{
		SchemaVersion: consolidationInputSchema, ResolverVersion: ResolverVersion,
		CurrentThemes:     []InputTheme{{ThemeRef: "theme-001", Title: "芯片验证平台"}},
		RecentOverviews:   []HistoricalReport{{Date: "2026-08-05", Overview: recent}},
		HistoricalAnchors: []HistoricalReport{{Date: "2026-07-01", Overview: anchor}},
	}
	payload, estimate, err := marshalWithinBudget(input)
	if err != nil {
		t.Fatal(err)
	}
	if estimate > maxInputTokens || strings.Contains(string(payload), anchor) {
		t.Fatalf("historical anchor was not trimmed first: estimate=%d", estimate)
	}
	if !strings.Contains(string(payload), recent) {
		t.Fatal("recent overview was trimmed before the older anchor")
	}
}

func TestProjectMemoryHistoryUsesTenRecentAndTenAnchorReports(t *testing.T) {
	if maxRecentReports != 10 || maxHistoricalAnchors != 10 || maxMemorySnapshotDepth != 20 {
		t.Fatalf("history windows = recent:%d anchors:%d snapshots:%d", maxRecentReports, maxHistoricalAnchors, maxMemorySnapshotDepth)
	}
}

func TestHistoricalProjectAnchorKeepsNamesWithoutDeliverableDetails(t *testing.T) {
	report := historicalReport{
		generationMode: "managed_agent", sourceType: "explicit_saved",
		briefPayload: `{"workstreams":[{"subject":"AI Coding 提效支撑","deliverables":[{"result":"完整后端测试通过并发布生产"}]},{"subject":"芯片验证平台"}]}`,
	}
	if got := historicalProjectAnchor(report); got != "AI Coding 提效支撑；芯片验证平台" {
		t.Fatalf("anchor = %q", got)
	}
}

func TestMatchingThemeRefsMarksBroadParentWithoutGenericCueInflation(t *testing.T) {
	themes := []InputTheme{
		{ThemeRef: "theme-001", Title: "Qwen3 4B DSpark训练"},
		{ThemeRef: "theme-002", Title: "GLM5.2 DSpark训练"},
	}
	parent := InputProject{
		CanonicalName: "AI Coding 提效支撑",
		Aliases:       []string{"Qwen3-4B", "GLM-5.2"},
	}
	if got := matchingThemeRefs(parent, themes); len(got) != 2 || got[0] != "theme-001" || got[1] != "theme-002" {
		t.Fatalf("parent matched themes = %#v", got)
	}
	child := InputProject{CanonicalName: "Qwen3 4B DSpark训练", WorkstreamCues: []string{"DSpark"}}
	if got := matchingThemeRefs(child, themes); len(got) != 1 || got[0] != "theme-001" {
		t.Fatalf("child matched themes = %#v", got)
	}
}

func TestWorkstreamsFromBriefKeepsBoundedParentChildOutline(t *testing.T) {
	raw := `{"workstreams":[{"subject":"IF-Knowledge","deliverables":[{"result":"完成 knowledge-map-search Skill，并应用到儿童睡前卡通动画生成场景"},{"result":"完成 Knowledge Map 产品判断"}]}]}`
	workstreams := workstreamsFromBrief(raw)
	if len(workstreams) != 1 || workstreams[0].Subject != "IF-Knowledge" || len(workstreams[0].Deliverables) != 2 {
		t.Fatalf("brief workstreams = %#v", workstreams)
	}
}

func TestConsolidationThemesUsesBriefSubjectsForUnchangedAIReport(t *testing.T) {
	report := historicalReport{
		generationMode: "managed_agent", sourceType: "auto_carried",
		content: "1. IF-Knowledge：完成 GPGPU 调研并推进儿童睡前场景",
	}
	themes := consolidationThemes(report, []InputWorkstream{{Subject: "IF-Knowledge"}})
	if len(themes) != 1 || themes[0].Title != "IF-Knowledge" {
		t.Fatalf("themes=%#v", themes)
	}
}

func TestConsolidationThemesUsesSingleLayerFinalReportForHumanEditedReport(t *testing.T) {
	report := historicalReport{
		generationMode: "managed_agent", sourceType: sourceHumanEdited,
		content: "1. 芯片验证平台：完成测试执行模块方案设计",
	}
	themes := consolidationThemes(report, []InputWorkstream{{Subject: "旧 Brief 主题"}})
	if len(themes) != 1 || themes[0].Title != "芯片验证平台：完成测试执行模块方案设计" {
		t.Fatalf("themes=%#v", themes)
	}
}

func TestHistoricalOverviewUsesBriefSubjectsForUnchangedAIReport(t *testing.T) {
	report := historicalReport{
		generationMode: "managed_agent", sourceType: "explicit_saved",
		content:      "1. IF-Knowledge：完成 GPGPU 调研并推进儿童睡前场景",
		briefPayload: `{"workstreams":[{"subject":"IF-Knowledge"},{"subject":"InfoAgent"}]}`,
	}
	if got := historicalOverviewForMemory(report); got != "1. IF-Knowledge\n2. InfoAgent" {
		t.Fatalf("overview=%q", got)
	}
}

func TestSnapshotProjectRefsUsesOnlyCanonicalSnapshotShape(t *testing.T) {
	projects := snapshotProjects([]byte(`{"projects":[{"project_ref":"project-1","canonical_name":"AIDA"},{"ProjectRef":"legacy-shadow"}]}`))
	if projects["project-1"].Stored.CanonicalName != "AIDA" {
		t.Fatalf("snapshot projects = %#v", projects)
	}
}

func TestProposalV2AllowsEmptyOperations(t *testing.T) {
	proposal, _, _, err := parseAndValidateProposal(`{"schema_version":"project-memory-maintenance/v2","operations":[]}`, ConsolidationInput{})
	if err != nil || len(proposal.Operations) != 0 || len(proposal.Rejected) != 0 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
}

func TestProposalV2KeepsValidOperationsAndRejectsInvalidOnes(t *testing.T) {
	input := ConsolidationInput{CurrentThemes: []InputTheme{{ThemeRef: "theme-001", EvidenceRef: "evidence-1", Title: "Report Agent"}}}
	raw := `{"schema_version":"project-memory-maintenance/v2","operations":[{"operation_id":"op-1","operation":"create_project","theme_ref":"theme-001","evidence_refs":["evidence-1"],"temp_ref":"new-1","canonical_name":"完成发布；下一步上线。","confidence":0.9},{"operation_id":"op-2","operation":"create_project","theme_ref":"theme-001","evidence_refs":["evidence-1"],"temp_ref":"new-2","canonical_name":"Report Agent","confidence":"high"}]}`
	proposal, _, _, err := parseAndValidateProposal(raw, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Operations) != 1 || proposal.Operations[0].OperationID != "op-2" || len(proposal.Rejected) != 1 {
		t.Fatalf("proposal=%+v", proposal)
	}
}

func TestProposalV2ValidatesTempRefDependencies(t *testing.T) {
	input := ConsolidationInput{CurrentThemes: []InputTheme{{ThemeRef: "theme-001", EvidenceRef: "evidence-1", Title: "芯片验证平台"}}}
	raw := `{"schema_version":"project-memory-maintenance/v2","operations":[{"operation_id":"op-1","operation":"create_project","theme_ref":"theme-001","evidence_refs":["evidence-1"],"temp_ref":"new-1","canonical_name":"芯片验证平台","confidence":0.9},{"operation_id":"op-2","operation":"upsert_signal","theme_ref":"theme-001","project_ref":"new-1","depends_on":["op-1"],"evidence_refs":["evidence-1"],"signal_type":"workstream_cue","value":"RTL","confidence":0.9}]}`
	proposal, _, _, err := parseAndValidateProposal(raw, input)
	if err != nil || len(proposal.Operations) != 2 || len(proposal.Rejected) != 0 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
}

func TestProposalV2RejectsWeakLinkWithoutFailingBatch(t *testing.T) {
	input := ConsolidationInput{
		CurrentThemes:     []InputTheme{{ThemeRef: "theme-001", EvidenceRef: "evidence-1", Title: "Report Agent"}},
		CandidateProjects: []InputProject{{ProjectRef: "project-1", CanonicalName: "Report Agent"}},
	}
	raw := `{"schema_version":"project-memory-maintenance/v2","operations":[{"operation_id":"op-1","operation":"link_existing","theme_ref":"theme-001","evidence_refs":["evidence-1"],"project_ref":"project-1","confidence":0.6}]}`
	proposal, _, _, err := parseAndValidateProposal(raw, input)
	if err != nil || len(proposal.Operations) != 0 || len(proposal.Rejected) != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
}

func TestProposalV2RejectsSignalWithoutCitedThemeEvidence(t *testing.T) {
	input := ConsolidationInput{
		CurrentThemes:     []InputTheme{{ThemeRef: "theme-001", EvidenceRef: "evidence-1", Title: "芯片验证平台"}},
		CandidateProjects: []InputProject{{ProjectRef: "project-1", CanonicalName: "芯片验证平台"}},
	}
	raw := `{"schema_version":"project-memory-maintenance/v2","operations":[{"operation_id":"op-1","operation":"upsert_signal","project_ref":"project-1","signal_type":"workstream_cue","value":"RTL","confidence":0.9}]}`
	proposal, _, _, err := parseAndValidateProposal(raw, input)
	if err != nil || len(proposal.Operations) != 0 || len(proposal.Rejected) != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
}

func TestEvidenceAuthorityComesFromOperationTheme(t *testing.T) {
	input := ConsolidationInput{CurrentThemes: []InputTheme{
		{ThemeRef: "theme-ai", EvidenceRef: "evidence-ai", ReportDate: "2026-08-08", SourceType: "auto_carried", SourceWeight: 0.5},
		{ThemeRef: "theme-human", EvidenceRef: "evidence-human", ReportDate: "2026-08-07", SourceType: sourceHumanEdited, SourceWeight: 0.95},
	}}
	metadata, err := evidenceMetadata(input, MemoryOperation{
		ThemeRef: "theme-ai", EvidenceRefs: []string{"evidence-ai", "evidence-human"},
	})
	if err != nil || metadata.SourceType != "auto_carried" || metadata.SourceWeight != 0.5 {
		t.Fatalf("metadata=%+v err=%v", metadata, err)
	}
}

func TestDerivedProjectKeysKeepsStableMixedIdentifiers(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{name: "nnp412量化适配", want: []string{"nnp412"}},
		{name: "GLM-5.2 DSpark训练", want: []string{"GLM-5.2"}},
		{name: "芯片验证平台", want: nil},
	}
	for _, test := range tests {
		got := derivedProjectKeys(test.name)
		if strings.Join(got, ",") != strings.Join(test.want, ",") {
			t.Fatalf("derivedProjectKeys(%q) = %#v, want %#v", test.name, got, test.want)
		}
	}
}

func TestMarshalStringArrayUsesArrayForEmptyCues(t *testing.T) {
	if got := string(marshalStringArray(nil)); got != "[]" {
		t.Fatalf("empty cue JSON = %q", got)
	}
}

func TestUpsertAliasRejectsNarrativeTextBeforeDatabaseWrite(t *testing.T) {
	err := upsertAlias(t.Context(), nil, "project-1", historicalReport{}, "完成 Report Agent 方案；下一步发布生产。", "child_topic", 0.9)
	if err != nil {
		t.Fatalf("invalid alias should be ignored: %v", err)
	}
}

func TestDisabledNightlyServiceNeedsNoResolver(t *testing.T) {
	service, err := NewNightlyService(nil, nil, NightlyConfig{Enabled: false})
	if err != nil || service == nil {
		t.Fatalf("disabled service = %#v, %v", service, err)
	}
}
