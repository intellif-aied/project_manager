package handler

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAggregateValueDaysUsesLatestSuccessfulRunOnly(t *testing.T) {
	date := "2026-07-28"
	generatedAt1 := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	generatedAt2 := generatedAt1.Add(time.Hour)
	key := valueDayKey("user-1", date)
	facts := valueFacts{
		People:  map[string]valueUserDay{key: {UserID: "user-1", UserName: "张三", ReportDate: date}},
		Reports: map[string]currentDailyReport{key: {ID: "report-1", UserID: "user-1", ReportDate: date, Content: "G2", RunID: "run-2", GenerationMode: "managed_agent"}},
		Runs: map[string][]valueRun{key: {
			{ID: "run-1", Status: "succeeded", CreatedAt: generatedAt1.Add(-time.Minute), Snapshot: testValueSnapshot("report-1", "G1", generatedAt1)},
			{ID: "run-2", Status: "succeeded", CreatedAt: generatedAt2.Add(-time.Minute), Snapshot: testValueSnapshot("report-1", "G2", generatedAt2)},
		}},
		Outcomes: map[string][]valueOutcome{key: {
			{ID: "outcome-1", RunID: "run-1", Action: "saved", Content: "G1 user edit", ActionAt: generatedAt1.Add(time.Minute)},
		}},
	}

	items := aggregateValueDays(facts, date, true)
	if len(items) != 1 {
		t.Fatalf("items = %d", len(items))
	}
	item := items[0]
	if item.CurrentRunID != "run-2" || item.OutcomeStatus != "observed_unchanged" || item.Diff != nil || !item.Regenerated {
		t.Fatalf("unexpected item: %#v", item)
	}
	if item.Runs[0].Outcome == nil || item.Runs[0].Outcome.ID != "outcome-1" || item.Runs[0].Diff == nil {
		t.Fatalf("historical run outcome was not retained: %#v", item.Runs[0])
	}
	if len(item.OutcomeEvents) != 1 {
		t.Fatalf("outcome timeline = %#v", item.OutcomeEvents)
	}
}

func TestAggregateValueDaysBuildsComparableOutcome(t *testing.T) {
	date := "2026-07-28"
	generatedAt := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	key := valueDayKey("user-1", date)
	facts := valueFacts{
		People:  map[string]valueUserDay{key: {UserID: "user-1", UserName: "张三", ReportDate: date}},
		Reports: map[string]currentDailyReport{key: {ID: "report-1", UserID: "user-1", ReportDate: date, Content: "相同", RunID: "run-1", GenerationMode: "managed_agent"}},
		Runs: map[string][]valueRun{key: {
			{ID: "run-1", Status: "succeeded", CreatedAt: generatedAt.Add(-time.Minute), Snapshot: testValueSnapshot("report-1", "相同", generatedAt)},
		}},
		Outcomes: map[string][]valueOutcome{key: {
			{ID: "outcome-1", RunID: "run-1", Action: "submitted", Content: "相同", ActionAt: generatedAt.Add(time.Minute)},
		}},
	}

	item := aggregateValueDays(facts, date, true)[0]
	if item.OutcomeStatus != "confirmed_direct_use" || item.Diff == nil || item.CurrentOutcomeID != "outcome-1" {
		t.Fatalf("unexpected item: %#v", item)
	}
}

func TestCalculateValueMetricsDoesNotCountFailedAttemptAsAIReport(t *testing.T) {
	date := "2026-07-28"
	key := valueDayKey("user-1", date)
	facts := valueFacts{
		People:   map[string]valueUserDay{key: {UserID: "user-1", UserName: "张三", ReportDate: date}},
		Reports:  map[string]currentDailyReport{key: {ID: "report-1", UserID: "user-1", ReportDate: date, Content: "手写", GenerationMode: "default"}},
		Runs:     map[string][]valueRun{key: {{ID: "run-failed", Status: "failed", FailureStage: "building_context"}}},
		Outcomes: map[string][]valueOutcome{},
	}
	items := aggregateValueDays(facts, date, false)
	metrics, _, _, failures, _ := calculateValueMetrics(items, facts, date)
	if metrics.TotalReports != 1 || metrics.AIReports != 0 || metrics.HandwrittenReports != 1 {
		t.Fatalf("metrics = %#v", metrics)
	}
	if failures["building_context"] != 1 {
		t.Fatalf("failure stages = %#v", failures)
	}
}

func TestAggregateValueDaysRedactionDoesNotMutateFacts(t *testing.T) {
	date := "2026-07-28"
	key := valueDayKey("user-1", date)
	facts := valueFacts{
		People:   map[string]valueUserDay{key: {UserID: "user-1", ReportDate: date}},
		Reports:  map[string]currentDailyReport{key: {ID: "report-1", UserID: "user-1", ReportDate: date, Content: "draft", RunID: "run-1"}},
		Runs:     map[string][]valueRun{key: {{ID: "run-1", Status: "succeeded", Snapshot: testValueSnapshot("report-1", "draft", time.Now())}}},
		Outcomes: map[string][]valueOutcome{},
	}

	redacted := aggregateValueDays(facts, date, false)
	if redacted[0].Runs[0].Snapshot.Generated != "" {
		t.Fatalf("list content was not redacted")
	}
	if facts.Runs[key][0].Snapshot.Generated != "draft" {
		t.Fatalf("source facts were mutated")
	}
}

func testValueSnapshot(reportID, generated string, createdAt time.Time) *valueSnapshot {
	return &valueSnapshot{
		ReportID: reportID, Generated: generated, GeneratedHash: "hash", SummaryHash: "summary-hash",
		Variant: json.RawMessage(`{"pipeline_profile":"digest_context_brief_final"}`), VariantHash: "variant", CreatedAt: createdAt,
	}
}
