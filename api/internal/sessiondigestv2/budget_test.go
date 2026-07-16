package sessiondigestv2

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBudgetPrioritizesResultsAndRecordsAggregation(t *testing.T) {
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
	if !truncated || len(encoded) > 6<<10 || !json.Valid(encoded) {
		t.Fatalf("budget not enforced: truncated=%v bytes=%d", truncated, len(encoded))
	}
	foundCritical := false
	for _, unit := range compacted.WorkUnits {
		if unit.Goal.Text == "关键实现" {
			foundCritical = true
		}
	}
	if !foundCritical {
		t.Fatalf("critical failed result was discarded: %#v", compacted.WorkUnits)
	}
	if compacted.Coverage.AggregatedWorkUnitCount == 0 || len(compacted.DiscussionAggregates) == 0 {
		t.Fatalf("removed work units were not represented: %#v", compacted.Coverage)
	}
}

func TestBudgetHardLimitTrimsPathologicalEvidenceRefs(t *testing.T) {
	digest := EmptyDigest()
	refs := make([]string, 0, 5000)
	for index := 0; index < cap(refs); index++ {
		refs = append(refs, "ev-file-"+strings.Repeat("a", 40)+strconvItoa(index))
	}
	digest.WorkUnits = []WorkUnit{{
		WorkUnitRef:    "wu-pathological",
		Sequence:       1,
		PeriodRelation: "unknown",
		Goal:           Goal{Text: "验证极端引用数量仍受硬预算保护", Source: "user_message"},
		Category:       "implementation",
		Status:         "completed",
		EvidenceGrade:  "A",
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
	recalculateSummary(&digest)

	compacted, encoded, truncated := EnforceItemBudget(digest, 4<<10)
	if !truncated || len(encoded) > 4<<10 || !json.Valid(encoded) {
		t.Fatalf("hard budget not enforced: truncated=%v bytes=%d", truncated, len(encoded))
	}
	if len(compacted.WorkUnits) == 1 &&
		len(compacted.WorkUnits[0].ResultStatements) == 1 &&
		len(compacted.WorkUnits[0].ResultStatements[0].EvidenceRefs) > 8 {
		t.Fatalf(
			"pathological evidence refs were not bounded: %d",
			len(compacted.WorkUnits[0].ResultStatements[0].EvidenceRefs),
		)
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
