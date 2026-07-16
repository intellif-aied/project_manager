package sessiondigestv2

import (
	"strings"
	"testing"
	"time"
)

func TestDailySummariesPreserveLaterPeriodBeforeRevisionCompaction(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	digest := EmptyDigest()
	for index := 0; index < 80; index++ {
		day := 15
		if index >= 40 {
			day = 16
		}
		end := time.Date(2026, 7, day, 9, index%60, 0, 0, location)
		unit := WorkUnit{
			WorkUnitRef:     "wu-" + strconvItoa(index),
			Sequence:        index + 1,
			ActivityStartAt: end.Add(-time.Minute).UTC().Format(time.RFC3339),
			ActivityEndAt:   end.UTC().Format(time.RFC3339),
			PeriodRelation:  "unknown",
			Goal: Goal{
				Text:   strings.Repeat("实现报告期结果保留", 30),
				Source: "user_message",
			},
			Category:      "implementation",
			Status:        "completed",
			EvidenceGrade: "B",
			ResultStatements: []ResultStatement{{
				Text:         "完成第 " + strconvItoa(index) + " 项结果",
				Source:       "agent_claim_with_evidence",
				EvidenceRefs: []string{"ev-file-" + strconvItoa(index)},
			}},
			AgentClaims: []AgentClaim{},
			Evidence:    []Evidence{},
			Changes: []Change{{
				Path: "file-" + strconvItoa(index) + ".go",
			}},
			Validations: []Validation{},
			Unresolved:  []Unresolved{},
		}
		digest.WorkUnits = append(digest.WorkUnits, unit)
	}
	digest.DailySummaries = BuildDailySummaries(
		digest.WorkUnits, location, DefaultDailyHighlightMax,
	)
	digest.Coverage = Coverage{
		SourceWorkUnitCount:   len(digest.WorkUnits),
		DetailedWorkUnitCount: len(digest.WorkUnits),
		Representation:        "result_focused",
	}
	recalculateSummary(&digest)

	revision, encoded, truncated := EnforceItemBudget(digest, 20<<10)
	if !truncated || len(encoded) > 20<<10 {
		t.Fatalf("revision budget not enforced: truncated=%v bytes=%d", truncated, len(encoded))
	}
	if len(revision.DailySummaries) != 2 {
		t.Fatalf("daily summaries were lost: %#v", revision.DailySummaries)
	}

	period := time.Date(2026, 7, 16, 0, 0, 0, 0, location)
	selected, selectedJSON, _ := PrepareForPeriod(
		revision, period, period, location, DefaultPeriodItemBytes,
	)
	if len(selectedJSON) > DefaultPeriodItemBytes {
		t.Fatalf("period payload exceeds budget: %d", len(selectedJSON))
	}
	if selected.ReportPeriodSummary == nil ||
		len(selected.ReportPeriodSummary.Days) != 1 ||
		selected.ReportPeriodSummary.Days[0].Date != "2026-07-16" {
		t.Fatalf("wrong period summary: %#v", selected.ReportPeriodSummary)
	}
	highlights := selected.ReportPeriodSummary.Days[0].Highlights
	if len(highlights) == 0 || highlights[0].Sequence < 73 {
		t.Fatalf("later report-period results were not prioritized: %#v", highlights)
	}
	for _, day := range selected.ReportPeriodSummary.Days {
		if day.Date == "2026-07-15" {
			t.Fatalf("out-of-period day leaked into report summary: %#v", day)
		}
	}
}

func TestResultFocusedClaimDropsNextQuestionTail(t *testing.T) {
	got := resultFocusedClaim(
		"已完成展示名缓存方案并更新 ADR。 **问题 46：是否继续增加控制能力？**",
	)
	if got != "已完成展示名缓存方案并更新 ADR。" {
		t.Fatalf("unexpected focused claim: %q", got)
	}
	if got := resultFocusedClaim("**Q37：是否默认选择平台能力？**"); got != "" {
		t.Fatalf("question-only claim must be removed: %q", got)
	}
}

func TestResolveUnitMarksOngoingImplementationPartial(t *testing.T) {
	unit := WorkUnit{
		Category:      "implementation",
		Status:        "pending",
		EvidenceGrade: "D",
		AgentClaims: []AgentClaim{{
			Text: "前端实现已写入，但 Go 容器仍在下载依赖，尚未进入测试执行。",
		}},
		Changes:     []Change{{Path: "frontend/app.tsx", Operation: "update"}},
		Validations: []Validation{},
		Unresolved:  []Unresolved{},
	}
	resolveUnit(&unit)
	if unit.Status != "partial" {
		t.Fatalf("ongoing implementation status=%q want=partial", unit.Status)
	}
}

func TestDailySummaryHidesEngineeringEvidenceFromReportView(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	unit := WorkUnit{
		WorkUnitRef:     "wu-1",
		Sequence:        1,
		ActivityStartAt: "2026-07-16T01:00:00Z",
		ActivityEndAt:   "2026-07-16T02:00:00Z",
		Goal:            Goal{Text: "完成日报质量优化"},
		Category:        "implementation",
		Status:          "completed",
		EvidenceGrade:   "A",
		ResultStatements: []ResultStatement{{
			Text: "日报质量优化已完成", Source: "agent_claim_with_evidence",
		}},
		Changes:     []Change{{Path: "api/internal/example.go"}},
		Validations: []Validation{{Name: "go test", Attempts: 12, LastStatus: "passed"}},
	}
	digest := EmptyDigest()
	digest.WorkUnits = []WorkUnit{unit}
	digest.DailySummaries = BuildDailySummaries(digest.WorkUnits, location, 6)
	recalculateSummary(&digest)
	period := time.Date(2026, 7, 16, 0, 0, 0, 0, location)
	_, encoded, _ := PrepareForPeriod(digest, period, period, location, DefaultPeriodItemBytes)
	text := string(encoded)
	for _, forbidden := range []string{
		"change_count", "changed_files", "validation_count", "validations",
		`"work_unit_count":`, `"status_counts":`, "go test", "12",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("report view leaked %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, "日报质量优化已完成") {
		t.Fatalf("material outcome missing from report view: %s", text)
	}
}

func TestDailyCandidatesCollapseOlderSkillVersionAndSameDigestTopic(t *testing.T) {
	units := []WorkUnit{
		{
			Sequence: 1,
			Goal:     Goal{Text: "发布 aida-report@1.0.6 并启用 Session Digest"},
			Category: "deployment",
			ResultStatements: []ResultStatement{{
				Text: "已发布 aida-report@1.0.6", Source: "agent_claim_with_evidence",
			}},
		},
		{
			Sequence: 2,
			Goal:     Goal{Text: "完成 Session Digest v2.2 和 aida-report@1.0.11 发布"},
			Category: "deployment",
			ResultStatements: []ResultStatement{{
				Text:   "已发布 aida-report@1.0.11，Session Digest v2.2 已验证",
				Source: "agent_claim_with_evidence",
			}},
		},
	}
	got := consolidateDailyCandidates(units)
	if len(got) != 1 || got[0].Sequence != 2 {
		t.Fatalf("older final state was not superseded: %#v", got)
	}
}

func TestSkillVersionDoesNotHideDifferentPrimaryOutcome(t *testing.T) {
	units := []WorkUnit{
		{
			Sequence: 1,
			Goal:     Goal{Text: "修复日报上海时区契约"},
			Category: "implementation",
			ResultStatements: []ResultStatement{{
				Text:   "时区问题已修复，并使用 aida-report@1.0.7 和 Session Digest 验证",
				Source: "agent_claim_with_evidence",
			}},
		},
		{
			Sequence: 2,
			Goal:     Goal{Text: "发布 aida-report@1.0.11"},
			Category: "deployment",
			ResultStatements: []ResultStatement{{
				Text: "aida-report@1.0.11 已发布", Source: "agent_claim_with_evidence",
			}},
		},
	}
	got := consolidateDailyCandidates(units)
	if len(got) != 2 {
		t.Fatalf("different primary outcome was hidden by asset version: %#v", got)
	}
}

func TestMergeReportPeriodSummariesKeepsLatestCrossSliceFinalState(t *testing.T) {
	older := &ReportPeriodSummary{
		StartDate: "2026-07-16",
		EndDate:   "2026-07-16",
		Days: []DailySummary{{
			Date: "2026-07-16",
			Highlights: []DailyHighlight{
				{
					WorkUnitRef:   "old-digest",
					Sequence:      64,
					ActivityEndAt: "2026-07-16T12:54:06Z",
					Category:      "implementation",
					Status:        "completed",
					EvidenceGrade: "A",
					Goal:          "开始吧",
					ResultStatements: []ResultStatement{{
						Text:   "已完成 Session Digest v2.1 开发和测试环境部署。",
						Source: "agent_claim_with_evidence",
					}},
				},
				{
					WorkUnitRef:   "old-rtk",
					Sequence:      61,
					ActivityEndAt: "2026-07-16T11:15:44Z",
					Category:      "investigation",
					Status:        "completed",
					EvidenceGrade: "A",
					Goal:          "调研 RTK 清洗 Session 的方式",
					ResultStatements: []ResultStatement{{
						Text:   "RTK 可以借鉴。",
						Source: "agent_claim_with_evidence",
					}},
				},
			},
		}},
	}
	newer := &ReportPeriodSummary{
		StartDate: "2026-07-16",
		EndDate:   "2026-07-16",
		Days: []DailySummary{{
			Date: "2026-07-16",
			Highlights: []DailyHighlight{
				{
					WorkUnitRef:   "new-digest",
					Sequence:      4,
					ActivityEndAt: "2026-07-16T14:43:09Z",
					Category:      "implementation",
					Status:        "completed",
					EvidenceGrade: "A",
					Goal:          "走完日报质量优化和真实流程测试",
					ResultStatements: []ResultStatement{{
						Text:   "Session Digest v2.4 已完成结果质量优化。",
						Source: "agent_claim_with_evidence",
					}},
				},
				{
					WorkUnitRef:   "new-rtk",
					Sequence:      3,
					ActivityEndAt: "2026-07-16T13:50:00Z",
					Category:      "investigation",
					Status:        "completed",
					EvidenceGrade: "B",
					Goal:          "完成 RTK Session 清洗借鉴与稳定性影响评审",
					ResultStatements: []ResultStatement{{
						Text:   "RTK 借鉴范围已收敛。",
						Source: "agent_claim_with_evidence",
					}},
				},
			},
		}},
	}

	got := MergeReportPeriodSummaries(
		[]*ReportPeriodSummary{older, newer},
		"2026-07-16",
		"2026-07-16",
		6,
	)
	if got == nil || len(got.Days) != 1 {
		t.Fatalf("unexpected merged period: %#v", got)
	}
	refs := map[string]bool{}
	for _, highlight := range got.Days[0].Highlights {
		refs[highlight.WorkUnitRef] = true
	}
	if refs["old-digest"] || refs["old-rtk"] ||
		!refs["new-digest"] || !refs["new-rtk"] {
		t.Fatalf("cross-slice final state was not consolidated: %#v", got.Days[0].Highlights)
	}
}
