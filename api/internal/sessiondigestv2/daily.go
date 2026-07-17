package sessiondigestv2

import (
	"sort"
	"strings"
	"time"
)

var defaultBusinessLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type dailySummaryBuilder struct {
	summary    DailySummary
	candidates []WorkUnit
}

func BuildDailySummaries(
	units []WorkUnit,
	location *time.Location,
	_ int,
) []DailySummary {
	if location == nil {
		location = defaultBusinessLocation
	}
	builders := map[string]*dailySummaryBuilder{}
	for _, unit := range units {
		date := workUnitBusinessDate(unit, location)
		if date == "" {
			continue
		}
		builder := builders[date]
		if builder == nil {
			builder = &dailySummaryBuilder{
				summary: DailySummary{
					Date:       date,
					Highlights: []DailyHighlight{},
				},
			}
			builders[date] = builder
		}
		accumulateDailyCounts(&builder.summary, unit)
		if isReportRelevantWorkUnit(unit) {
			builder.candidates = append(builder.candidates, unit)
		}
	}

	dates := make([]string, 0, len(builders))
	for date := range builders {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	result := make([]DailySummary, 0, len(dates))
	for _, date := range dates {
		builder := builders[date]
		sort.SliceStable(builder.candidates, func(i, j int) bool {
			return builder.candidates[i].Sequence < builder.candidates[j].Sequence
		})
		for _, unit := range builder.candidates {
			builder.summary.Highlights = append(
				builder.summary.Highlights,
				makeDailyHighlight(unit, false),
			)
		}
		builder.summary.OutcomeCoverage = OutcomeCoverage{
			SourceCount:      len(builder.candidates),
			RepresentedCount: len(builder.summary.Highlights),
			Complete:         len(builder.candidates) == len(builder.summary.Highlights),
		}
		builder.summary.HighlightsTruncated = !builder.summary.OutcomeCoverage.Complete
		result = append(result, builder.summary)
	}
	return result
}

func PrepareForPeriod(
	input Digest,
	periodStart, periodEnd time.Time,
	location *time.Location,
	maxBytes int,
) (Digest, []byte, bool) {
	if location == nil {
		location = defaultBusinessLocation
	}
	startDate := dateKey(periodStart, location)
	endDate := dateKey(periodEnd, location)
	digest := cloneDigest(input)
	AnnotatePeriodRelations(&digest, periodStart, periodEnd, location)

	period := ReportPeriodSummary{
		StartDate: startDate,
		EndDate:   endDate,
		Days:      []DailySummary{},
	}
	for _, day := range digest.DailySummaries {
		if day.Date < startDate || day.Date > endDate {
			continue
		}
		accumulatePeriodCounts(&period, day)
		period.Days = append(period.Days, reportDayView(day))
	}

	digest.SessionSummary = SessionSummary{}
	digest.ReportPeriodSummary = &period
	digest.DailySummaries = []DailySummary{}
	digest.WorkUnits = []WorkUnit{}
	digest.DiscussionAggregates = []DiscussionAggregate{}
	digest.Coverage.DetailedWorkUnitCount = 0
	digest.Coverage.AggregatedWorkUnitCount = digest.Coverage.SourceWorkUnitCount
	digest.Coverage.Representation = "period_result_focused"
	clearReportPeriodMetrics(digest.ReportPeriodSummary)
	return EnforceItemBudget(digest, maxBytes)
}

func reportDayView(day DailySummary) DailySummary {
	day.WorkUnitCount = 0
	day.ResultWorkUnitCount = 0
	day.PrimaryResultCount = 0
	day.VerifiedResultCount = 0
	day.ChangeCount = 0
	day.ValidationCount = 0
	day.UnresolvedCount = 0
	day.StatusCounts = StatusCounts{}
	return day
}

func clearReportPeriodMetrics(period *ReportPeriodSummary) {
	if period == nil {
		return
	}
	period.WorkUnitCount = 0
	period.ResultWorkUnitCount = 0
	period.PrimaryResultCount = 0
	period.VerifiedResultCount = 0
	period.ChangeCount = 0
	period.ValidationCount = 0
	period.UnresolvedCount = 0
	period.StatusCounts = StatusCounts{}
}

func workUnitBusinessDate(unit WorkUnit, location *time.Location) string {
	if parsed, ok := parseTimestamp(unit.ActivityEndAt); ok {
		return dateKey(parsed, location)
	}
	if parsed, ok := parseTimestamp(unit.ActivityStartAt); ok {
		return dateKey(parsed, location)
	}
	return ""
}

func accumulateDailyCounts(summary *DailySummary, unit WorkUnit) {
	summary.WorkUnitCount++
	summary.PrimaryResultCount += len(unit.ResultStatements)
	summary.ChangeCount += len(unit.Changes)
	summary.ValidationCount += len(unit.Validations)
	summary.UnresolvedCount += len(unit.Unresolved)
	if isResultBearingWorkUnit(unit) {
		summary.ResultWorkUnitCount++
	}
	if unit.EvidenceGrade == "A" {
		for _, statement := range unit.ResultStatements {
			if statement.Source == "derived_evidence" {
				summary.VerifiedResultCount++
			}
		}
	}
	accumulateStatusCounts(&summary.StatusCounts, unit.Status)
}

func accumulatePeriodCounts(period *ReportPeriodSummary, day DailySummary) {
	period.WorkUnitCount += day.WorkUnitCount
	period.ResultWorkUnitCount += day.ResultWorkUnitCount
	period.PrimaryResultCount += day.PrimaryResultCount
	period.VerifiedResultCount += day.VerifiedResultCount
	period.ChangeCount += day.ChangeCount
	period.ValidationCount += day.ValidationCount
	period.UnresolvedCount += day.UnresolvedCount
	addStatusCounts(&period.StatusCounts, day.StatusCounts)
}

func accumulateStatusCounts(counts *StatusCounts, status string) {
	switch status {
	case "completed":
		counts.Completed++
	case "partial":
		counts.Partial++
	case "blocked":
		counts.Blocked++
	case "failed":
		counts.Failed++
	case "pending":
		counts.Pending++
	default:
		counts.Unknown++
	}
}

func addStatusCounts(target *StatusCounts, source StatusCounts) {
	target.Completed += source.Completed
	target.Partial += source.Partial
	target.Blocked += source.Blocked
	target.Failed += source.Failed
	target.Pending += source.Pending
	target.Unknown += source.Unknown
}

func isResultBearingWorkUnit(unit WorkUnit) bool {
	return len(unit.ResultStatements) > 0 ||
		len(unit.Unresolved) > 0 ||
		unit.Status == "failed" ||
		unit.Status == "blocked"
}

func isReportRelevantWorkUnit(unit WorkUnit) bool {
	if isResultBearingWorkUnit(unit) {
		return true
	}
	return unit.Goal.Source == "user_message" && !isLowInformationGoal(unit.Goal.Text)
}

func makeDailyHighlight(unit WorkUnit, aggressive bool) DailyHighlight {
	resultBytes := 4096
	evidenceRefLimit := 8
	unresolvedBytes := 512
	goalBytes := 2048
	if aggressive {
		resultBytes = 384
		evidenceRefLimit = 2
		unresolvedBytes = 192
		goalBytes = 192
	}
	results := make([]ResultStatement, 0, len(unit.ResultStatements))
	seenResults := map[string]struct{}{}
	for _, statement := range unit.ResultStatements {
		text := resultFocusedClaim(statement.Text)
		if text == "" {
			continue
		}
		key := canonicalKey(text)
		if _, exists := seenResults[key]; exists {
			continue
		}
		seenResults[key] = struct{}{}
		text, _ = truncateUTF8Bytes(text, resultBytes)
		refs := append([]string(nil), statement.EvidenceRefs...)
		if len(refs) > evidenceRefLimit {
			refs = refs[:evidenceRefLimit]
		}
		results = append(results, ResultStatement{
			Text: text, Source: statement.Source, EvidenceRefs: refs,
		})
	}
	goal, _ := truncateUTF8Bytes(strings.TrimSpace(unit.Goal.Text), goalBytes)
	unresolved := make([]Unresolved, 0, len(unit.Unresolved))
	for _, item := range unit.Unresolved {
		item.Text, _ = truncateUTF8Bytes(strings.TrimSpace(item.Text), unresolvedBytes)
		if item.Text == "" {
			continue
		}
		unresolved = append(unresolved, item)
	}
	return DailyHighlight{
		WorkUnitRef:      unit.WorkUnitRef,
		Sequence:         unit.Sequence,
		ActivityEndAt:    unit.ActivityEndAt,
		Category:         unit.Category,
		Status:           unit.Status,
		EvidenceGrade:    unit.EvidenceGrade,
		Goal:             goal,
		ResultStatements: results,
		Unresolved:       unresolved,
	}
}

func resultFocusedClaim(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"<oai-mem-citation>",
		" **问题 ",
		" **问题",
		" **q",
		" 问题 ",
	} {
		index := strings.Index(strings.ToLower(value), marker)
		if index < 0 {
			continue
		}
		if index == 0 {
			return ""
		}
		value = strings.TrimSpace(value[:index])
		lower = strings.ToLower(value)
	}
	if strings.HasPrefix(lower, "**q") ||
		strings.HasPrefix(lower, "**问题") ||
		strings.HasPrefix(lower, "问题 ") {
		return ""
	}
	if len(value) < 24 &&
		(strings.Contains(value, "是否同意") ||
			strings.Contains(value, "确认吗") ||
			strings.HasSuffix(value, "？")) {
		return ""
	}
	return value
}

func compactDailySummaries(summaries []DailySummary, aggressive bool) {
	for dayIndex := range summaries {
		day := &summaries[dayIndex]
		for highlightIndex := range day.Highlights {
			highlight := &day.Highlights[highlightIndex]
			goalBytes := 384
			resultBytes := 768
			evidenceRefLimit := 4
			unresolvedBytes := 384
			if aggressive {
				goalBytes = 160
				resultBytes = 256
				evidenceRefLimit = 2
				unresolvedBytes = 160
			}
			highlight.Goal, _ = truncateUTF8Bytes(highlight.Goal, goalBytes)
			highlight.ResultStatements = compactResultsPreservingCount(
				highlight.ResultStatements,
				resultBytes,
				evidenceRefLimit,
			)
			for index := range highlight.Unresolved {
				highlight.Unresolved[index].Text, _ = truncateUTF8Bytes(
					highlight.Unresolved[index].Text, unresolvedBytes,
				)
				if aggressive {
					highlight.Unresolved[index].EvidenceRef = ""
				}
			}
		}
		day.OutcomeCoverage.RepresentedCount = len(day.Highlights)
		day.OutcomeCoverage.Complete =
			day.OutcomeCoverage.SourceCount == len(day.Highlights) &&
				!day.HighlightsTruncated
		day.OutcomeCoverage.TextCompacted = true
	}
}

func compactReportPeriodSummary(summary *ReportPeriodSummary, aggressive bool) {
	if summary == nil {
		return
	}
	compactDailySummaries(summary.Days, aggressive)
}

func CompactReportPeriodSummary(summary *ReportPeriodSummary, aggressive bool) {
	compactReportPeriodSummary(summary, aggressive)
}

func ReportPeriodOutcomeCoverageComplete(summary *ReportPeriodSummary) bool {
	if summary == nil {
		return false
	}
	for _, day := range summary.Days {
		if day.HighlightsTruncated || !day.OutcomeCoverage.Complete ||
			day.OutcomeCoverage.SourceCount != day.OutcomeCoverage.RepresentedCount ||
			day.OutcomeCoverage.RepresentedCount != len(day.Highlights) {
			return false
		}
	}
	return true
}
