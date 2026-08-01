package reportsource

import (
	"testing"
	"time"

	"github.com/aidashboard/api/internal/sessiondigestv2"
)

func TestPrepareDigestV2ForExplicitSelectionKeepsSelectedDaysOutsideReportPeriod(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	periodStart := time.Date(2026, 7, 6, 0, 0, 0, 0, location)
	periodEnd := time.Date(2026, 7, 12, 0, 0, 0, 0, location)
	digest := digestV2PolicyFixture(location)

	prepared, _, _ := prepareDigestV2ForSelectionMode(
		digest, "explicit", periodStart, periodEnd, location,
	)
	assertDigestV2PolicyDays(t, prepared.ReportPeriodSummary, []string{
		"2026-07-01", "2026-07-07",
	})
	if prepared.ReportPeriodSummary.StartDate != "2026-07-06" ||
		prepared.ReportPeriodSummary.EndDate != "2026-07-12" {
		t.Fatalf("explicit selection changed report period: %+v", prepared.ReportPeriodSummary)
	}
}

func TestPrepareDigestV2ForDefaultSelectionStillUsesReportPeriod(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	periodStart := time.Date(2026, 7, 6, 0, 0, 0, 0, location)
	periodEnd := time.Date(2026, 7, 12, 0, 0, 0, 0, location)

	prepared, _, _ := prepareDigestV2ForSelectionMode(
		digestV2PolicyFixture(location), "default", periodStart, periodEnd, location,
	)
	assertDigestV2PolicyDays(t, prepared.ReportPeriodSummary, []string{"2026-07-07"})
}

func TestMergeDigestV2ExplicitSelectionKeepsSelectedDaysOutsideReportPeriod(t *testing.T) {
	summary := &sessiondigestv2.ReportPeriodSummary{
		StartDate: "2026-07-06",
		EndDate:   "2026-07-12",
		Days: []sessiondigestv2.DailySummary{
			{Date: "2026-07-01", Highlights: []sessiondigestv2.DailyHighlight{{WorkUnitRef: "outside"}}},
			{Date: "2026-07-07", Highlights: []sessiondigestv2.DailyHighlight{{WorkUnitRef: "inside"}}},
		},
	}
	merged := mergeDigestV2SelectionSummaries(
		"explicit",
		[]sessiondigestv2.ReportPeriodSummarySource{{SourceRef: "session-a", Summary: summary}},
		"2026-07-06",
		"2026-07-12",
		0,
	)
	assertDigestV2PolicyDays(t, merged, []string{"2026-07-01", "2026-07-07"})
	if merged.StartDate != "2026-07-06" || merged.EndDate != "2026-07-12" {
		t.Fatalf("explicit merge changed report period: %+v", merged)
	}
}

func digestV2PolicyFixture(location *time.Location) sessiondigestv2.Digest {
	digest := sessiondigestv2.EmptyDigest()
	digest.WorkUnits = []sessiondigestv2.WorkUnit{
		{
			WorkUnitRef: "outside", Sequence: 1,
			ActivityStartAt: "2026-07-01T01:00:00Z",
			ActivityEndAt:   "2026-07-01T02:00:00Z",
			Goal:            sessiondigestv2.Goal{Text: "保留明确选择的历史切片", Source: "user_message"},
			Status:          "completed",
			ResultStatements: []sessiondigestv2.ResultStatement{{
				Text: "历史切片结果", Source: "derived_evidence",
			}},
		},
		{
			WorkUnitRef: "inside", Sequence: 2,
			ActivityStartAt: "2026-07-07T01:00:00Z",
			ActivityEndAt:   "2026-07-07T02:00:00Z",
			Goal:            sessiondigestv2.Goal{Text: "保留报告期内切片", Source: "user_message"},
			Status:          "completed",
			ResultStatements: []sessiondigestv2.ResultStatement{{
				Text: "报告期内结果", Source: "derived_evidence",
			}},
		},
	}
	digest.DailySummaries = sessiondigestv2.BuildDailySummaries(digest.WorkUnits, location)
	return digest
}

func assertDigestV2PolicyDays(
	t *testing.T,
	summary *sessiondigestv2.ReportPeriodSummary,
	want []string,
) {
	t.Helper()
	if summary == nil || len(summary.Days) != len(want) {
		t.Fatalf("report days=%+v want=%v", summary, want)
	}
	for index, date := range want {
		if summary.Days[index].Date != date {
			t.Fatalf("report days=%+v want=%v", summary.Days, want)
		}
	}
}
