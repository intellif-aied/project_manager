package sessiondigestv2

import (
	"sort"
	"strconv"
	"strings"
)

type ReportPeriodSummarySource struct {
	SourceRef string
	Summary   *ReportPeriodSummary
}

func MergeReportPeriodSummaries(
	summaries []*ReportPeriodSummary,
	startDate, endDate string,
	highlightLimit int,
) *ReportPeriodSummary {
	sources := make([]ReportPeriodSummarySource, 0, len(summaries))
	for index, summary := range summaries {
		sources = append(sources, ReportPeriodSummarySource{
			SourceRef: "summary-" + strconv.Itoa(index),
			Summary:   summary,
		})
	}
	return MergeReportPeriodSummarySources(
		sources, startDate, endDate, highlightLimit,
	)
}

func MergeReportPeriodSummarySources(
	sources []ReportPeriodSummarySource,
	startDate, endDate string,
	_ int,
) *ReportPeriodSummary {
	merged := &ReportPeriodSummary{
		StartDate: startDate,
		EndDate:   endDate,
		Days:      []DailySummary{},
	}
	type sourcedHighlight struct {
		highlight DailyHighlight
		sourceRef string
	}
	type dayCandidates struct {
		highlights []sourcedHighlight
		complete   bool
		compacted  bool
	}
	byDate := map[string]*dayCandidates{}
	for index, source := range sources {
		summary := source.Summary
		if summary == nil {
			continue
		}
		sourceRef := strings.TrimSpace(source.SourceRef)
		if sourceRef == "" {
			sourceRef = "summary-" + strconv.Itoa(index)
		}
		for _, day := range summary.Days {
			if (startDate != "" && day.Date < startDate) ||
				(endDate != "" && day.Date > endDate) {
				continue
			}
			candidates := byDate[day.Date]
			if candidates == nil {
				candidates = &dayCandidates{complete: true}
				byDate[day.Date] = candidates
			}
			for _, highlight := range day.Highlights {
				candidates.highlights = append(candidates.highlights, sourcedHighlight{
					highlight: highlight,
					sourceRef: sourceRef,
				})
			}
			dayComplete := !day.HighlightsTruncated
			if day.OutcomeCoverage.SourceCount != 0 ||
				day.OutcomeCoverage.RepresentedCount != 0 {
				dayComplete = dayComplete && day.OutcomeCoverage.Complete &&
					day.OutcomeCoverage.SourceCount == day.OutcomeCoverage.RepresentedCount &&
					day.OutcomeCoverage.RepresentedCount == len(day.Highlights)
			}
			candidates.complete = candidates.complete && dayComplete
			candidates.compacted = candidates.compacted || day.OutcomeCoverage.TextCompacted
		}
	}
	dates := make([]string, 0, len(byDate))
	for date := range byDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	for _, date := range dates {
		candidates := byDate[date]
		sort.SliceStable(candidates.highlights, func(i, j int) bool {
			left, leftOK := parseTimestamp(candidates.highlights[i].highlight.ActivityEndAt)
			right, rightOK := parseTimestamp(candidates.highlights[j].highlight.ActivityEndAt)
			if leftOK && rightOK && !left.Equal(right) {
				return left.Before(right)
			}
			if leftOK != rightOK {
				return leftOK
			}
			return candidates.highlights[i].highlight.Sequence <
				candidates.highlights[j].highlight.Sequence
		})
		seen := map[string]struct{}{}
		highlights := make([]DailyHighlight, 0, len(candidates.highlights))
		for _, candidate := range candidates.highlights {
			highlight := candidate.highlight
			key := highlight.WorkUnitRef
			if key == "" {
				key = canonicalKey(highlight.Goal + "\x00" +
					highlight.ActivityEndAt + "\x00" + joinedResultText(highlight.ResultStatements))
			}
			key = candidate.sourceRef + "\x00" + key
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			highlights = append(highlights, highlight)
		}

		day := DailySummary{
			Date:       date,
			Highlights: highlights,
			OutcomeCoverage: OutcomeCoverage{
				SourceCount:      len(highlights),
				RepresentedCount: len(highlights),
				Complete:         candidates.complete,
				TextCompacted:    candidates.compacted,
			},
		}
		day.HighlightsTruncated = !day.OutcomeCoverage.Complete
		merged.Days = append(merged.Days, day)
	}
	clearReportPeriodMetrics(merged)
	return merged
}

func joinedResultText(values []ResultStatement) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, value.Text)
	}
	return strings.Join(parts, "\x00")
}
