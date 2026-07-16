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
	highlightLimit int,
) []DailySummary {
	if location == nil {
		location = defaultBusinessLocation
	}
	if highlightLimit <= 0 {
		highlightLimit = DefaultDailyHighlightMax
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
		if isResultBearingWorkUnit(unit) && hasReportFacingWorkUnit(unit) {
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
		builder.candidates = consolidateDailyCandidates(builder.candidates)
		sort.SliceStable(builder.candidates, func(i, j int) bool {
			left := dailyWorkUnitPriority(builder.candidates[i])
			right := dailyWorkUnitPriority(builder.candidates[j])
			if left != right {
				return left > right
			}
			return builder.candidates[i].Sequence > builder.candidates[j].Sequence
		})
		limit := min(len(builder.candidates), highlightLimit)
		for _, unit := range builder.candidates[:limit] {
			builder.summary.Highlights = append(
				builder.summary.Highlights,
				makeDailyHighlight(unit, false),
			)
		}
		builder.summary.HighlightsTruncated = len(builder.candidates) > limit
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

func dailyWorkUnitPriority(unit WorkUnit) int {
	score := workUnitPriority(unit)
	switch unit.Category {
	case "implementation", "deployment":
		score += 35
	case "verification":
		score -= 15
	case "investigation":
		score += 10
	case "document", "decision":
		score += 10
	case "discussion", "administrative":
		score -= 60
	}
	switch unit.Status {
	case "completed":
		score += 20
	case "partial":
		score -= 10
	}
	for _, result := range unit.ResultStatements {
		if result.Source == "agent_claim_with_evidence" &&
			resultFocusedClaim(result.Text) != "" {
			score += 12
			break
		}
	}
	if hasDeliveredOutcomeClaim(unit) {
		score += 45
	}
	return score
}

func hasDeliveredOutcomeClaim(unit WorkUnit) bool {
	for _, result := range unit.ResultStatements {
		text := strings.ToLower(reportFacingClaim(unit, result.Text))
		if text == "" {
			continue
		}
		if containsAny(
			text,
			"开发", "修复", "发布", "上线", "部署", "实现",
			"恢复", "支持", "启用", "接入", "交付", "改造", "优化",
		) {
			return true
		}
	}
	return false
}

func makeDailyHighlight(unit WorkUnit, aggressive bool) DailyHighlight {
	resultLimit := 2
	resultBytes := 360
	evidenceRefLimit := 4
	unresolvedLimit := 3
	goalBytes := 240
	if aggressive {
		resultLimit = 1
		resultBytes = 240
		evidenceRefLimit = 2
		unresolvedLimit = 2
		goalBytes = 160
	}
	results := make([]ResultStatement, 0, resultLimit)
	resultIndexes := map[string]int{}
	for _, statement := range unit.ResultStatements {
		text := reportFacingClaim(unit, statement.Text)
		if text == "" {
			continue
		}
		key := reportFacingResultKey(text)
		if index, exists := resultIndexes[key]; exists {
			if len(text) > len(results[index].Text) {
				results[index] = ResultStatement{
					Text: text, Source: statement.Source,
					EvidenceRefs: append([]string(nil), statement.EvidenceRefs...),
				}
			}
			continue
		}
		if len(results) == resultLimit {
			continue
		}
		text, _ = truncateUTF8Bytes(text, resultBytes)
		refs := append([]string(nil), statement.EvidenceRefs...)
		if len(refs) > evidenceRefLimit {
			refs = refs[:evidenceRefLimit]
		}
		results = append(results, ResultStatement{
			Text: text, Source: statement.Source, EvidenceRefs: refs,
		})
		resultIndexes[key] = len(results) - 1
	}
	goal, _ := truncateUTF8Bytes(reportFacingGoal(unit, results), goalBytes)
	return DailyHighlight{
		WorkUnitRef:      unit.WorkUnitRef,
		Sequence:         unit.Sequence,
		ActivityEndAt:    unit.ActivityEndAt,
		Category:         unit.Category,
		Status:           unit.Status,
		EvidenceGrade:    unit.EvidenceGrade,
		Goal:             goal,
		ResultStatements: results,
		Unresolved:       reportFacingUnresolved(unit.Unresolved, unresolvedLimit),
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
	limit := DefaultDailyHighlightMax
	if aggressive {
		limit = 3
	}
	for dayIndex := range summaries {
		day := &summaries[dayIndex]
		if len(day.Highlights) > limit {
			day.Highlights = append([]DailyHighlight(nil), day.Highlights[:limit]...)
			day.HighlightsTruncated = true
		}
		for highlightIndex := range day.Highlights {
			highlight := &day.Highlights[highlightIndex]
			goalBytes := 240
			resultLimit := 2
			resultBytes := 360
			evidenceRefLimit := 4
			unresolvedLimit := 3
			if aggressive {
				goalBytes = 160
				resultLimit = 1
				resultBytes = 240
				evidenceRefLimit = 2
				unresolvedLimit = 2
			}
			highlight.Goal, _ = truncateUTF8Bytes(highlight.Goal, goalBytes)
			highlight.ResultStatements = compactResults(
				highlight.ResultStatements,
				resultLimit,
				resultBytes,
				evidenceRefLimit,
			)
			highlight.Unresolved = firstUnresolved(
				highlight.Unresolved, unresolvedLimit,
			)
		}
	}
}

func compactReportPeriodSummary(summary *ReportPeriodSummary, aggressive bool) {
	if summary == nil {
		return
	}
	compactDailySummaries(summary.Days, aggressive)
}

func trimDailyHighlights(summaries []DailySummary) bool {
	selectedDay := -1
	selectedLength := 0
	for index := range summaries {
		if len(summaries[index].Highlights) > selectedLength &&
			len(summaries[index].Highlights) > 1 {
			selectedDay = index
			selectedLength = len(summaries[index].Highlights)
		}
	}
	if selectedDay < 0 {
		return false
	}
	day := &summaries[selectedDay]
	day.Highlights = day.Highlights[:len(day.Highlights)-1]
	day.HighlightsTruncated = true
	return true
}
