package sessiondigestv2

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	versionedArtifactPattern    = regexp.MustCompile(`(?i)\b([a-z][a-z0-9._-]{2,})@v?([0-9]+\.[0-9]+\.[0-9]+)\b`)
	namedArtifactVersionPattern = regexp.MustCompile(`(?i)\b(session[\s_-]*digest|digest|aida[\s_-]*cli|aida[\s_-]*report)\s+v?([0-9]+\.[0-9]+(?:\.[0-9]+)?)\b`)
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
	"rtk":            {},
	"session-digest": {},
	"subagent":       {},
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
	seen := map[string]struct{}{}
	for _, statement := range newer.ResultStatements {
		key := strings.ToLower(resultFocusedClaim(statement.Text))
		if key != "" {
			seen[key] = struct{}{}
		}
	}
	for _, statement := range older.ResultStatements {
		if len(newer.ResultStatements) >= 3 {
			break
		}
		key := strings.ToLower(resultFocusedClaim(statement.Text))
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		newer.ResultStatements = append(newer.ResultStatements, statement)
		seen[key] = struct{}{}
	}
	if len(newer.Unresolved) == 0 && len(older.Unresolved) > 0 {
		newer.Unresolved = append([]Unresolved(nil), older.Unresolved...)
	}
	return newer
}

func isPlaceholderGoal(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.Contains(value, "未识别的历史工作单元")
}

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
		if artifact == "aida-cli" &&
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
	addTopicAliases(newerTokens, workUnitEvidenceText(newer))
	addTopicAliases(olderTokens, workUnitEvidenceText(older))
	if sharedStrongTopicToken(newerTokens, olderTokens) {
		if older.Category == "investigation" && newer.Category == "investigation" {
			return true
		}
		if deliveryCategory(newer.Category) && deliveryCategory(older.Category) {
			return true
		}
	}
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
	value = strings.ReplaceAll(value, "`", "")
	value = strings.ReplaceAll(value, "**", "")
	for _, match := range versionedArtifactPattern.FindAllStringSubmatch(value, -1) {
		artifact := strings.ToLower(match[1])
		version := match[2]
		if existing, ok := result[artifact]; !ok || compareSemanticVersion(version, existing) > 0 {
			result[artifact] = version
		}
	}
	for _, match := range namedArtifactVersionPattern.FindAllStringSubmatch(value, -1) {
		artifactName := strings.ToLower(match[1])
		artifact := "session-digest"
		switch {
		case strings.Contains(artifactName, "report"):
			artifact = "aida-report"
		case strings.Contains(artifactName, "cli"):
			artifact = "aida-cli"
		}
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
	lower := strings.ToLower(value)
	for _, raw := range asciiTopicPattern.FindAllString(lower, -1) {
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
	addTopicAliases(result, lower)
	return result
}

func addTopicAliases(result map[string]struct{}, value string) {
	lower := strings.ToLower(value)
	for _, alias := range []struct {
		token   string
		needles []string
	}{
		{
			token: "session-digest",
			needles: []string{
				"session digest", "服务端 digest", "日报摘要",
				"digest_v2", "digest v2",
			},
		},
		{
			token: "subagent",
			needles: []string{
				"sub-agent", "sub agent", "子 agent", "子agent",
				"子代理", "parentsessionref", "parent_session_ref",
			},
		},
		{
			token: "session-list",
			needles: []string{
				"session 列表", "session列表", "会话列表",
			},
		},
		{
			token: "interactive-upload",
			needles: []string{
				"bubble tea", "真实 tty", "交互界面", "选择界面",
			},
		},
		{
			token: "auto-update",
			needles: []string{
				"自动升级", "自动更新", "install.sh",
			},
		},
	} {
		for _, needle := range alias.needles {
			if strings.Contains(lower, needle) {
				result[alias.token] = struct{}{}
				break
			}
		}
	}
}

func versionedNumber(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}
