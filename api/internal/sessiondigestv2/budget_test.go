package sessiondigestv2

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBudgetWithoutCompleteCoverageNeverRanksOrDropsWorkUnits(t *testing.T) {
	digest := EmptyDigest()
	for index := 0; index < 40; index++ {
		unit := WorkUnit{
			WorkUnitRef:      "wu-" + strconvItoa(index),
			Sequence:         index + 1,
			PeriodRelation:   "unknown",
			Goal:             Goal{Text: strings.Repeat("讨论内容", 80), Source: "user_message"},
			Category:         "discussion",
			Status:           "pending",
			EvidenceGrade:    "D",
			ResultStatements: []ResultStatement{},
			AgentClaims:      []AgentClaim{},
			Evidence:         []Evidence{},
			Changes:          []Change{},
			Validations:      []Validation{},
			Unresolved:       []Unresolved{},
		}
		if index == 5 {
			unit.Goal.Text = "关键实现"
			unit.Category = "implementation"
			unit.Status = "partial"
			unit.EvidenceGrade = "A"
			unit.ResultStatements = []ResultStatement{{
				Text: "关键实现已产生文件变更，但最终测试失败", Source: "derived_evidence",
				EvidenceRefs: []string{"ev-test"},
			}}
			unit.Validations = []Validation{{
				Name: "go test", Attempts: 1, LastStatus: "failed", EvidenceRef: "ev-test",
			}}
			unit.Unresolved = []Unresolved{{Text: "go test 验证失败", EvidenceRef: "ev-test"}}
		}
		digest.WorkUnits = append(digest.WorkUnits, unit)
	}
	digest.Coverage = Coverage{
		SourceEventCount:      1000,
		IncludedEventCount:    100,
		OmittedEventCount:     900,
		SourceWorkUnitCount:   len(digest.WorkUnits),
		DetailedWorkUnitCount: len(digest.WorkUnits),
		Representation:        "result_focused",
	}
	recalculateSummary(&digest)

	compacted, encoded, truncated := EnforceItemBudget(digest, 6<<10)
	if !truncated || len(encoded) <= 6<<10 || !json.Valid(encoded) {
		t.Fatalf("complete payload must exceed warning target: truncated=%v bytes=%d", truncated, len(encoded))
	}
	if len(compacted.WorkUnits) != len(digest.WorkUnits) ||
		compacted.Coverage.AggregatedWorkUnitCount != 0 ||
		len(compacted.DiscussionAggregates) != 0 {
		t.Fatalf("budget ranked or dropped work units: coverage=%#v units=%d",
			compacted.Coverage, len(compacted.WorkUnits))
	}
	for index := range digest.WorkUnits {
		if compacted.WorkUnits[index].Goal.Text != digest.WorkUnits[index].Goal.Text {
			t.Fatalf("work unit %d was rewritten", index)
		}
	}
}

func TestBudgetDropsPathologicalEvidenceOnlyAfterCompleteDailyCoverage(t *testing.T) {
	digest := EmptyDigest()
	refs := make([]string, 0, 5000)
	for index := 0; index < cap(refs); index++ {
		refs = append(refs, "ev-file-"+strings.Repeat("a", 40)+strconvItoa(index))
	}
	digest.WorkUnits = []WorkUnit{{
		WorkUnitRef:     "wu-pathological",
		Sequence:        1,
		ActivityStartAt: "2026-07-16T01:00:00Z",
		ActivityEndAt:   "2026-07-16T02:00:00Z",
		PeriodRelation:  "unknown",
		Goal:            Goal{Text: "验证极端引用数量仍受硬预算保护", Source: "user_message"},
		Category:        "implementation",
		Status:          "completed",
		EvidenceGrade:   "A",
		ResultStatements: []ResultStatement{{
			Text:         strings.Repeat("结果说明", 200),
			Source:       "derived_evidence",
			EvidenceRefs: refs,
		}},
		AgentClaims: []AgentClaim{},
		Evidence:    []Evidence{},
		Changes:     []Change{},
		Validations: []Validation{},
		Unresolved:  []Unresolved{},
	}}
	digest.Coverage = Coverage{
		SourceEventCount:      5000,
		IncludedEventCount:    5000,
		SourceWorkUnitCount:   1,
		DetailedWorkUnitCount: 1,
		Representation:        "result_focused",
	}
	digest.DailySummaries = BuildDailySummaries(
		digest.WorkUnits,
		time.FixedZone("Asia/Shanghai", 8*60*60),
		0,
	)
	recalculateSummary(&digest)

	compacted, encoded, truncated := EnforceItemBudget(digest, 4<<10)
	if !truncated || !json.Valid(encoded) {
		t.Fatalf("budget compaction failed: truncated=%v bytes=%d", truncated, len(encoded))
	}
	if len(compacted.WorkUnits) != 0 || len(compacted.DailySummaries) != 1 ||
		len(compacted.DailySummaries[0].Highlights) != 1 ||
		!compacted.DailySummaries[0].OutcomeCoverage.Complete {
		t.Fatalf("complete daily outcome was not retained: work_units=%d days=%d",
			len(compacted.WorkUnits), len(compacted.DailySummaries))
	}
	got := compacted.DailySummaries[0].Highlights[0]
	if got.Goal != digest.WorkUnits[0].Goal.Text ||
		got.ResultStatements[0].Text != digest.WorkUnits[0].ResultStatements[0].Text {
		t.Fatalf("semantic result changed while dropping duplicate evidence: %#v", got)
	}
}

func TestBudgetNeverDropsCompleteDailyOutcomes(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	digest := EmptyDigest()
	for index := 0; index < 24; index++ {
		digest.WorkUnits = append(digest.WorkUnits, WorkUnit{
			WorkUnitRef:     "wu-complete-" + strconvItoa(index),
			Sequence:        index + 1,
			ActivityStartAt: "2026-07-16T01:00:00Z",
			ActivityEndAt:   "2026-07-16T02:00:00Z",
			Goal:            Goal{Text: "成果目标 " + strconvItoa(index)},
			Category:        "implementation",
			Status:          "completed",
			EvidenceGrade:   "A",
			ResultStatements: []ResultStatement{{
				Text:   "独立成果 " + strconvItoa(index) + "：" + strings.Repeat("用户可见能力", 80),
				Source: "agent_claim_with_evidence",
			}},
		})
	}
	digest.DailySummaries = BuildDailySummaries(digest.WorkUnits, location, 5)
	digest.Coverage = Coverage{
		SourceWorkUnitCount:   len(digest.WorkUnits),
		DetailedWorkUnitCount: len(digest.WorkUnits),
		Representation:        "result_focused",
	}

	compacted, _, truncated := EnforceItemBudget(digest, 4<<10)
	if !truncated || len(compacted.DailySummaries) != 1 ||
		len(compacted.DailySummaries[0].Highlights) != len(digest.WorkUnits) {
		t.Fatalf("byte compaction discarded outcomes: %#v", compacted.DailySummaries)
	}
	coverage := compacted.DailySummaries[0].OutcomeCoverage
	if !coverage.Complete || coverage.SourceCount != len(digest.WorkUnits) ||
		coverage.RepresentedCount != len(digest.WorkUnits) {
		t.Fatalf("budget compaction broke outcome coverage: %#v", coverage)
	}
}

func TestBudgetTargetDoesNotShortenReportFacingResults(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	fullText := "完整结果：" + strings.Repeat("保留有意义的结果上下文。", 90)
	digest := EmptyDigest()
	digest.WorkUnits = []WorkUnit{{
		WorkUnitRef: "wu-full-result", Sequence: 1,
		ActivityStartAt: "2026-07-16T01:00:00Z",
		ActivityEndAt:   "2026-07-16T02:00:00Z",
		Goal:            Goal{Text: "保留完整结果", Source: "user_message"},
		Category:        "discussion", Status: "unknown", EvidenceGrade: "C",
		ResultStatements: []ResultStatement{{
			Text: fullText, Source: "agent_claim",
		}},
	}}
	digest.DailySummaries = BuildDailySummaries(digest.WorkUnits, location, 0)
	digest.Coverage = Coverage{
		SourceWorkUnitCount: 1, DetailedWorkUnitCount: 1,
		Representation: "result_focused",
	}

	compacted, encoded, truncated := EnforceItemBudget(digest, 4<<10)
	if !truncated || len(encoded) <= 4<<10 {
		t.Fatalf("expected complete content to exceed warning target: bytes=%d", len(encoded))
	}
	got := compacted.DailySummaries[0].Highlights[0].ResultStatements[0].Text
	if got != fullText {
		t.Fatalf("report-facing result was shortened: got=%d want=%d", len(got), len(fullText))
	}
}

func TestDefaultOneMiBTargetKeepsEveryOversizedDailyOutcome(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	const count = 340
	fullText := "完整成果：" + strings.Repeat("保留有意义的结果上下文。", 80)
	digest := EmptyDigest()
	for index := 0; index < count; index++ {
		digest.WorkUnits = append(digest.WorkUnits, WorkUnit{
			WorkUnitRef: "wu-over-target-" + strconvItoa(index), Sequence: index + 1,
			ActivityStartAt: "2026-07-16T01:00:00Z",
			ActivityEndAt:   "2026-07-16T02:00:00Z",
			Goal: Goal{
				Text:   "成果目标 " + strconvItoa(index),
				Source: "user_message",
			},
			Category: "implementation", Status: "completed", EvidenceGrade: "B",
			ResultStatements: []ResultStatement{{
				Text: fullText + strconvItoa(index), Source: "agent_claim",
			}},
		})
	}
	digest.DailySummaries = BuildDailySummaries(digest.WorkUnits, location, 0)
	digest.Coverage = Coverage{
		SourceWorkUnitCount: count, DetailedWorkUnitCount: count,
		Representation: "result_focused",
	}

	compacted, encoded, truncated := EnforceItemBudget(digest, DefaultItemBytes)
	if !truncated || len(encoded) <= DefaultItemBytes {
		t.Fatalf("expected semantic payload above 1 MiB: truncated=%v bytes=%d", truncated, len(encoded))
	}
	if len(compacted.WorkUnits) != 0 || len(compacted.DailySummaries) != 1 ||
		len(compacted.DailySummaries[0].Highlights) != count {
		t.Fatalf("outcomes were dropped above target: work_units=%d highlights=%d",
			len(compacted.WorkUnits), len(compacted.DailySummaries[0].Highlights))
	}
	day := compacted.DailySummaries[0]
	if !day.OutcomeCoverage.Complete || day.OutcomeCoverage.SourceCount != count ||
		day.OutcomeCoverage.RepresentedCount != count || day.HighlightsTruncated {
		t.Fatalf("coverage broke above target: %#v", day.OutcomeCoverage)
	}
	for index, highlight := range day.Highlights {
		want := fullText + strconvItoa(index)
		if len(highlight.ResultStatements) != 1 ||
			highlight.ResultStatements[0].Text != want {
			t.Fatalf("outcome %d was shortened", index)
		}
	}
}

func TestAnnotatePeriodRelations(t *testing.T) {
	digest := EmptyDigest()
	digest.WorkUnits = []WorkUnit{
		{ActivityStartAt: "2026-07-15T15:00:00Z", ActivityEndAt: "2026-07-15T15:30:00Z"},
		{ActivityStartAt: "2026-07-16T01:00:00Z", ActivityEndAt: "2026-07-16T02:00:00Z"},
		{ActivityStartAt: "2026-07-17T01:00:00Z", ActivityEndAt: "2026-07-17T02:00:00Z"},
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	period := time.Date(2026, 7, 16, 0, 0, 0, 0, location)
	AnnotatePeriodRelations(&digest, period, period, location)
	got := []string{
		digest.WorkUnits[0].PeriodRelation,
		digest.WorkUnits[1].PeriodRelation,
		digest.WorkUnits[2].PeriodRelation,
	}
	want := []string{"before", "overlap", "after"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("relation %d=%q want=%q", index, got[index], want[index])
		}
	}
}
