package reportmemory

import "testing"

func TestReportFingerprintChangesWhenSavedContentChanges(t *testing.T) {
	before := reportFingerprint([]historicalReport{{id: "report-1", date: "2026-07-31", content: "1. KV Cache"}})
	after := reportFingerprint([]historicalReport{{id: "report-1", date: "2026-07-31", content: "1. Knowledge Map"}})
	if before == after {
		t.Fatal("saved content change must invalidate Project Memory")
	}
}

func TestResolveFactMatchesExactChildAlias(t *testing.T) {
	projects := []storedProject{{
		ID: "project-kv", CanonicalName: "KV Cache 压缩算法研发", LastSeenOn: "2026-07-30",
		Aliases: []storedAlias{
			{Text: "KV Cache 压缩算法研发", Normalized: normalizeName("KV Cache 压缩算法研发"), Type: "canonical", SourceType: sourceManualFinal, SourceWeight: 1},
			{Text: "OSCAR 算法适配", Normalized: normalizeName("OSCAR 算法适配"), Type: "child_topic", SourceType: sourceManualFinal, SourceWeight: 1},
		},
	}}
	result := resolveFact(FactInput{FactRef: "fact-001", Text: "完成 OSCAR 算法适配与验证"}, projects, "2026-08-01")
	if result.Decision != "matched" || result.ProjectRef != "project-kv" {
		t.Fatalf("expected exact child alias match, got %#v", result)
	}
}

func TestResolveFactRejectsAliasSharedByMultipleProjects(t *testing.T) {
	fact := FactInput{FactRef: "fact-001", Text: "完成使用手册整体复查"}
	projects := []storedProject{
		{
			ID: "project-1", CanonicalName: "芯片验证平台", LastSeenOn: "2026-07-30",
			Aliases: []storedAlias{{Text: "使用手册", Normalized: "使用手册", SourceType: sourceManualFinal, SourceWeight: 1}},
		},
		{
			ID: "project-2", CanonicalName: "Aida", LastSeenOn: "2026-07-30",
			Aliases: []storedAlias{{Text: "使用手册", Normalized: "使用手册", SourceType: sourceManualFinal, SourceWeight: 1}},
		},
	}

	resolution := resolveFact(fact, projects, "2026-07-31")
	if resolution.Decision != "unmatched" || resolution.ProjectRef != "" {
		t.Fatalf("resolution = %#v, want ambiguous shared alias to remain unmatched", resolution)
	}
}

func TestResolveFactKeepsUnrelatedWorkUnmatched(t *testing.T) {
	projects := []storedProject{{
		ID: "project-map", CanonicalName: "Knowledge Map", LastSeenOn: "2026-07-31",
		Aliases: []storedAlias{{Text: "Knowledge Map", Normalized: normalizeName("Knowledge Map"), Type: "canonical", SourceType: sourceManualFinal, SourceWeight: 1}},
	}}
	result := resolveFact(FactInput{FactRef: "fact-002", Text: "优化日报弹窗跳转位置"}, projects, "2026-08-01")
	if result.Decision != "unmatched" || result.ProjectRef != "" {
		t.Fatalf("unrelated work must remain unmatched: %#v", result)
	}
}

func TestResolveFactUsesThreadGoalForProjectIdentity(t *testing.T) {
	projects := []storedProject{{
		ID: "project-baigong", CanonicalName: "baigong demo 协议设计", LastSeenOn: "2026-07-29",
		Aliases: []storedAlias{{Text: "baigong demo 协议设计", Normalized: normalizeName("baigong demo 协议设计"), Type: "canonical", SourceType: sourceManualFinal, SourceWeight: 1}},
	}}
	result := resolveFact(FactInput{
		FactRef: "fact-003", Text: "补充消息字段并完成兼容性验证",
		ThreadGoals: []string{"继续完成 baigong demo 协议设计"},
	}, projects, "2026-08-01")
	if result.Decision != "matched" || result.ProjectRef != "project-baigong" {
		t.Fatalf("expected thread goal match, got %#v", result)
	}
}

func TestNgramSimilarityDoesNotBecomeHighConfidenceWithoutExactAlias(t *testing.T) {
	projects := []storedProject{{
		ID: "project-agent", CanonicalName: "Agent 平台优化", LastSeenOn: "2026-07-31",
		Aliases: []storedAlias{{Text: "Agent 平台优化", Normalized: normalizeName("Agent 平台优化"), Type: "canonical", SourceType: sourceExplicitSaved, SourceWeight: 0.75}},
	}}
	result := resolveFact(FactInput{FactRef: "fact-004", Text: "Agent 运行日志检查"}, projects, "2026-08-01")
	if result.Decision != "unmatched" {
		t.Fatalf("similarity-only candidate must be rejectable: %#v", result)
	}
}

func TestAliasCoverageProducesCandidateButDoesNotForceMatch(t *testing.T) {
	projects := []storedProject{{
		ID: "project-report", CanonicalName: "报告生成流程两阶段改造与生成体验优化", LastSeenOn: "2026-07-31",
		Aliases: []storedAlias{{
			Text:       "报告生成流程两阶段改造与生成体验优化",
			Normalized: normalizeName("报告生成流程两阶段改造与生成体验优化"), Type: "canonical",
			SourceType: sourceExplicitSaved, SourceWeight: 0.75,
		}},
	}}
	result := resolveFact(FactInput{
		FactRef: "fact-005", Text: "完成报告生成两阶段流程的 Brief 校验调整与体验优化",
	}, projects, "2026-08-01")
	if result.Decision != "unmatched" || len(result.CandidateList) == 0 {
		t.Fatalf("coverage should recall a rejectable candidate: %#v", result)
	}
}

func TestClassifyHistoricalSource(t *testing.T) {
	manual := historicalReport{generationMode: "default", content: "1. baigong demo 协议设计"}
	if source, weight := classifyHistoricalSource(manual); source != sourceManualFinal || weight != 1 {
		t.Fatalf("manual source = %q %.2f", source, weight)
	}
	generated := "## 工作概览\n\n1. Knowledge Map"
	ai := historicalReport{
		generationMode: "managed_agent", content: generated,
		generatedContentSHA256: contentSHA256(generated),
		briefPayload:           `{"workstreams":[{"subject":"Knowledge Map"}]}`,
	}
	if source, weight := classifyHistoricalSource(ai); source != sourceExplicitSaved || weight != 0.75 {
		t.Fatalf("AI source = %q %.2f", source, weight)
	}
	ai.content += "\n\n人工补充"
	if source, weight := classifyHistoricalSource(ai); source != sourceHumanEdited || weight != 0.95 {
		t.Fatalf("edited source = %q %.2f", source, weight)
	}
}

func TestLegacyAIOverviewWithoutBriefHasLowerWeight(t *testing.T) {
	content := "## 工作概览\n\n1. 完成报告生成流程优化。"
	report := historicalReport{
		generationMode: "managed_agent", content: content,
		generatedContentSHA256: contentSHA256(content),
	}
	if source, weight := classifyHistoricalSource(report); source != sourceExplicitSaved || weight != 0.55 {
		t.Fatalf("legacy AI overview source = %q %.2f", source, weight)
	}
}

func TestAIConfirmedUsesBriefSubjectInsteadOfDetailOrOverviewSentence(t *testing.T) {
	report := historicalReport{
		content:      "## 工作概览\n\n1. 完成 Knowledge Map 产品判断和 Skill 落地。\n\n## 工作详情\n\n### Knowledge Map 详情",
		sourceType:   sourceExplicitSaved,
		briefPayload: `{"workstreams":[{"subject":"Knowledge Map"}]}`,
	}
	themes := themesForHistoricalReport(report)
	if len(themes) != 1 || themes[0].Title != "Knowledge Map" {
		t.Fatalf("expected Brief Subject, got %#v", themes)
	}
}

func TestManualSourceScoresHigherThanAIConfirmed(t *testing.T) {
	base := storedProject{CanonicalName: "Knowledge Map", LastSeenOn: "2026-07-31"}
	manual := base
	manual.ID = "manual"
	manual.Aliases = []storedAlias{{
		Text: "Knowledge Map", Normalized: normalizeName("Knowledge Map"), Type: "canonical",
		SourceType: sourceManualFinal, SourceWeight: 1,
	}}
	ai := base
	ai.ID = "ai"
	ai.Aliases = []storedAlias{{
		Text: "Knowledge Map", Normalized: normalizeName("Knowledge Map"), Type: "canonical",
		SourceType: sourceExplicitSaved, SourceWeight: 0.75,
	}}
	manualCandidate, _ := scoreProject(normalizeName("继续 Knowledge Map 开发"), manual, "2026-08-01")
	aiCandidate, _ := scoreProject(normalizeName("继续 Knowledge Map 开发"), ai, "2026-08-01")
	if manualCandidate.Score <= aiCandidate.Score {
		t.Fatalf("manual %.3f must exceed AI %.3f", manualCandidate.Score, aiCandidate.Score)
	}
}
