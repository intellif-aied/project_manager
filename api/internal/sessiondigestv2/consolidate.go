package sessiondigestv2

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	versionedArtifactPattern    = regexp.MustCompile(`(?i)\b([a-z][a-z0-9._-]{2,})@v?([0-9]+\.[0-9]+\.[0-9]+)\b`)
	namedArtifactVersionPattern = regexp.MustCompile(`(?i)\b(session[\s_-]*digest|digest)\s+v?([0-9]+\.[0-9]+(?:\.[0-9]+)?)\b`)
	asciiTopicPattern           = regexp.MustCompile(`(?i)[a-z][a-z0-9._-]{2,}`)
)

var genericTopicTokens = map[string]struct{}{
	"agent": {}, "aida": {}, "api": {}, "build": {}, "client": {},
	"code": {}, "config": {}, "current": {}, "default": {}, "develop": {},
	"development": {}, "digest": {}, "fix": {}, "go": {}, "manager": {}, "mcp": {},
	"project": {}, "report": {}, "server": {}, "skill": {}, "test": {},
	"testing": {}, "update": {}, "version": {}, "work": {},
}

var strongTopicTokens = map[string]struct{}{
	"rtk": {},
}

func consolidateDailyCandidates(units []WorkUnit) []WorkUnit {
	if len(units) < 2 {
		return units
	}
	latestFirst := append([]WorkUnit(nil), units...)
	sort.SliceStable(latestFirst, func(i, j int) bool {
		return latestFirst[i].Sequence > latestFirst[j].Sequence
	})
	kept := make([]WorkUnit, 0, len(latestFirst))
	for _, candidate := range latestFirst {
		superseded := false
		for index := range kept {
			if workUnitSupersedes(kept[index], candidate) {
				kept[index] = mergeSupersedingWorkUnit(kept[index], candidate)
				superseded = true
				break
			}
		}
		if !superseded {
			kept = append(kept, candidate)
		}
	}
	return kept
}

func mergeSupersedingWorkUnit(newer, older WorkUnit) WorkUnit {
	if isPlaceholderGoal(newer.Goal.Text) && !isPlaceholderGoal(older.Goal.Text) {
		newer.Goal = older.Goal
	}
	return newer
}

func isPlaceholderGoal(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.Contains(value, "未识别的历史工作单元")
}

func MergeReportPeriodSummaries(
	summaries []*ReportPeriodSummary,
	startDate, endDate string,
	highlightLimit int,
) *ReportPeriodSummary {
	if highlightLimit <= 0 {
		highlightLimit = DefaultDailyHighlightMax
	}
	merged := &ReportPeriodSummary{
		StartDate: startDate,
		EndDate:   endDate,
		Days:      []DailySummary{},
	}
	type dayCandidates struct {
		highlights []DailyHighlight
		truncated  bool
	}
	byDate := map[string]*dayCandidates{}
	for _, summary := range summaries {
		if summary == nil {
			continue
		}
		for _, day := range summary.Days {
			if (startDate != "" && day.Date < startDate) ||
				(endDate != "" && day.Date > endDate) {
				continue
			}
			candidates := byDate[day.Date]
			if candidates == nil {
				candidates = &dayCandidates{}
				byDate[day.Date] = candidates
			}
			candidates.highlights = append(candidates.highlights, day.Highlights...)
			candidates.truncated = candidates.truncated || day.HighlightsTruncated
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
			left, leftOK := parseTimestamp(candidates.highlights[i].ActivityEndAt)
			right, rightOK := parseTimestamp(candidates.highlights[j].ActivityEndAt)
			if leftOK && rightOK && !left.Equal(right) {
				return left.After(right)
			}
			if leftOK != rightOK {
				return leftOK
			}
			return candidates.highlights[i].Sequence > candidates.highlights[j].Sequence
		})
		units := make([]WorkUnit, 0, len(candidates.highlights))
		seen := map[string]struct{}{}
		for index, highlight := range candidates.highlights {
			key := highlight.WorkUnitRef
			if key == "" {
				key = highlight.Goal + "\x00" + highlight.ActivityEndAt
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			units = append(units, WorkUnit{
				WorkUnitRef:      highlight.WorkUnitRef,
				Sequence:         len(candidates.highlights) - index,
				ActivityEndAt:    highlight.ActivityEndAt,
				PeriodRelation:   "overlap",
				Goal:             Goal{Text: highlight.Goal, Source: "selection_period_summary"},
				Category:         highlight.Category,
				Status:           highlight.Status,
				EvidenceGrade:    highlight.EvidenceGrade,
				ResultStatements: append([]ResultStatement(nil), highlight.ResultStatements...),
				Unresolved:       append([]Unresolved(nil), highlight.Unresolved...),
			})
		}
		units = consolidateDailyCandidates(units)
		sort.SliceStable(units, func(i, j int) bool {
			left := dailyWorkUnitPriority(units[i])
			right := dailyWorkUnitPriority(units[j])
			if left != right {
				return left > right
			}
			return units[i].Sequence > units[j].Sequence
		})
		limit := min(len(units), highlightLimit)
		day := DailySummary{
			Date:                date,
			Highlights:          make([]DailyHighlight, 0, limit),
			HighlightsTruncated: candidates.truncated || len(units) > limit,
		}
		for _, unit := range units[:limit] {
			day.Highlights = append(day.Highlights, makeDailyHighlight(unit, false))
		}
		merged.Days = append(merged.Days, day)
	}
	clearReportPeriodMetrics(merged)
	return merged
}

func workUnitSupersedes(newer, older WorkUnit) bool {
	if newer.Sequence <= older.Sequence {
		return false
	}
	newerText := workUnitTopicText(newer)
	olderText := workUnitTopicText(older)
	if newerText == "" || olderText == "" {
		return false
	}
	if artifact := supersededArtifact(workUnitEvidenceText(newer), workUnitEvidenceText(older)); artifact != "" {
		goal := strings.ToLower(older.Goal.Text)
		if artifact == "session-digest" &&
			deliveryCategory(newer.Category) && deliveryCategory(older.Category) {
			return true
		}
		if strings.Contains(goal, artifact) ||
			older.Category == "administrative" ||
			isLowInformationGoal(older.Goal.Text) {
			return true
		}
	}
	if (older.Category == "investigation") != (newer.Category == "investigation") ||
		older.Category == "decision" || newer.Category == "decision" {
		return false
	}
	newerTokens := topicTokens(newerText)
	olderTokens := topicTokens(olderText)
	if len(newerTokens) < 2 || len(olderTokens) < 2 {
		return false
	}
	shared := 0
	for token := range olderTokens {
		if _, ok := newerTokens[token]; ok {
			shared++
		}
	}
	if shared < 2 {
		if older.Category == "investigation" && newer.Category == "investigation" &&
			sharedStrongTopicToken(newerTokens, olderTokens) {
			return true
		}
		return false
	}
	if older.Category == "investigation" && newer.Category == "investigation" {
		return true
	}
	return deliveryCategory(newer.Category) && deliveryCategory(older.Category)
}

func sharedStrongTopicToken(left, right map[string]struct{}) bool {
	for token := range strongTopicTokens {
		if _, leftOK := left[token]; !leftOK {
			continue
		}
		if _, rightOK := right[token]; rightOK {
			return true
		}
	}
	return false
}

func supersededArtifact(newerText, olderText string) string {
	newerVersions := artifactVersions(newerText)
	olderVersions := artifactVersions(olderText)
	for artifact, olderVersion := range olderVersions {
		newerVersion, ok := newerVersions[artifact]
		if !ok || newerVersion == olderVersion {
			continue
		}
		if compareSemanticVersion(newerVersion, olderVersion) >= 0 {
			return artifact
		}
	}
	return ""
}

func deliveryCategory(category string) bool {
	return category == "implementation" ||
		category == "deployment" ||
		category == "document"
}

func artifactVersions(value string) map[string]string {
	result := map[string]string{}
	for _, match := range versionedArtifactPattern.FindAllStringSubmatch(value, -1) {
		artifact := strings.ToLower(match[1])
		version := match[2]
		if existing, ok := result[artifact]; !ok || compareSemanticVersion(version, existing) > 0 {
			result[artifact] = version
		}
	}
	for _, match := range namedArtifactVersionPattern.FindAllStringSubmatch(value, -1) {
		artifact := "session-digest"
		version := match[2]
		if existing, ok := result[artifact]; !ok || compareSemanticVersion(version, existing) > 0 {
			result[artifact] = version
		}
	}
	return result
}

func compareSemanticVersion(left, right string) int {
	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	for index := 0; index < 3; index++ {
		leftValue := 0
		if index < len(leftParts) {
			leftValue, _ = strconv.Atoi(leftParts[index])
		}
		rightValue := 0
		if index < len(rightParts) {
			rightValue, _ = strconv.Atoi(rightParts[index])
		}
		switch {
		case leftValue < rightValue:
			return -1
		case leftValue > rightValue:
			return 1
		}
	}
	return 0
}

func workUnitTopicText(unit WorkUnit) string {
	if !isLowInformationGoal(unit.Goal.Text) {
		return unit.Goal.Text
	}
	for _, statement := range unit.ResultStatements {
		if statement.Source == "agent_claim_with_evidence" {
			return firstOutcomeSentence(statement.Text)
		}
	}
	return ""
}

func workUnitEvidenceText(unit WorkUnit) string {
	parts := []string{unit.Goal.Text}
	for _, statement := range unit.ResultStatements {
		parts = append(parts, statement.Text)
	}
	return strings.Join(parts, "\n")
}

func firstOutcomeSentence(value string) string {
	value = strings.TrimSpace(value)
	for _, separator := range []string{"。", "\n", "！", "？", ". "} {
		if index := strings.Index(value, separator); index >= 0 {
			value = strings.TrimSpace(value[:index])
		}
	}
	return value
}

func topicTokens(value string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, raw := range asciiTopicPattern.FindAllString(strings.ToLower(value), -1) {
		token := strings.Trim(raw, "._-")
		token = strings.TrimPrefix(token, "v")
		if token == "" || versionedNumber(token) {
			continue
		}
		if _, generic := genericTopicTokens[token]; generic {
			continue
		}
		result[token] = struct{}{}
	}
	return result
}

func versionedNumber(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}
