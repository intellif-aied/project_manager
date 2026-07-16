package sessiondigestv2

import (
	"encoding/json"
	"sort"
)

func EnforceItemBudget(input Digest, maxBytes int) (Digest, []byte, bool) {
	if maxBytes < 4<<10 {
		maxBytes = DefaultItemBytes
	}
	digest := cloneDigest(input)
	encoded, _ := json.Marshal(digest)
	if len(encoded) <= maxBytes {
		return digest, encoded, digest.Coverage.Truncated
	}

	truncated := true
	compactAllWorkUnits(&digest, false)
	compactDailySummaries(digest.DailySummaries, false)
	compactReportPeriodSummary(digest.ReportPeriodSummary, false)
	encoded, _ = json.Marshal(digest)
	if len(encoded) <= maxBytes {
		digest.Coverage.Truncated = true
		encoded, _ = json.Marshal(digest)
		return digest, encoded, truncated
	}

	for len(digest.WorkUnits) > 1 && len(encoded) > maxBytes {
		removeIndex := lowestPriorityIndex(digest.WorkUnits)
		removed := digest.WorkUnits[removeIndex]
		digest.WorkUnits = append(digest.WorkUnits[:removeIndex], digest.WorkUnits[removeIndex+1:]...)
		addAggregate(&digest, removed)
		digest.Coverage.DetailedWorkUnitCount = len(digest.WorkUnits)
		digest.Coverage.AggregatedWorkUnitCount++
		digest.Coverage.Truncated = true
		encoded, _ = json.Marshal(digest)
	}
	if len(encoded) <= maxBytes {
		return digest, encoded, truncated
	}

	compactAllWorkUnits(&digest, true)
	compactDailySummaries(digest.DailySummaries, true)
	compactReportPeriodSummary(digest.ReportPeriodSummary, true)
	for index := range digest.DiscussionAggregates {
		digest.DiscussionAggregates[index].Topic, _ = truncateUTF8Bytes(
			digest.DiscussionAggregates[index].Topic, 96,
		)
	}
	encoded, _ = json.Marshal(digest)
	if len(encoded) <= maxBytes {
		return digest, encoded, truncated
	}

	for len(digest.WorkUnits) > 1 && len(encoded) > maxBytes {
		removeIndex := lowestPriorityIndex(digest.WorkUnits)
		removed := digest.WorkUnits[removeIndex]
		digest.WorkUnits = append(digest.WorkUnits[:removeIndex], digest.WorkUnits[removeIndex+1:]...)
		addAggregate(&digest, removed)
		digest.Coverage.DetailedWorkUnitCount = len(digest.WorkUnits)
		digest.Coverage.AggregatedWorkUnitCount++
		encoded, _ = json.Marshal(digest)
	}
	if len(encoded) > maxBytes && len(digest.WorkUnits) == 1 {
		unit := &digest.WorkUnits[0]
		unit.Goal.Text, _ = truncateUTF8Bytes(unit.Goal.Text, 128)
		unit.ResultStatements = compactResults(unit.ResultStatements, 1, 192, 2)
		unit.AgentClaims = nil
		unit.Evidence = firstEvidence(unit.Evidence, 2)
		unit.Changes = firstChanges(unit.Changes, 2)
		unit.Validations = firstValidations(unit.Validations, 2)
		unit.Unresolved = firstUnresolved(unit.Unresolved, 1)
		encoded, _ = json.Marshal(digest)
	}
	for len(encoded) > maxBytes {
		trimmed := trimDailyHighlights(digest.DailySummaries)
		if digest.ReportPeriodSummary != nil {
			trimmed = trimDailyHighlights(digest.ReportPeriodSummary.Days) || trimmed
		}
		if !trimmed {
			break
		}
		digest.Coverage.Truncated = true
		encoded, _ = json.Marshal(digest)
	}
	if len(encoded) > maxBytes {
		digest.WorkUnits = []WorkUnit{}
		digest.DiscussionAggregates = []DiscussionAggregate{{
			Topic:         "详细工作单元超过单项摘要预算，已保留会话级结果计数",
			WorkUnitCount: digest.Coverage.SourceWorkUnitCount,
		}}
		if digest.ReportPeriodSummary != nil {
			for index := range digest.ReportPeriodSummary.Days {
				digest.ReportPeriodSummary.Days[index].Highlights = []DailyHighlight{}
				digest.ReportPeriodSummary.Days[index].HighlightsTruncated = true
			}
		} else {
			for index := range digest.DailySummaries {
				digest.DailySummaries[index].Highlights = []DailyHighlight{}
				digest.DailySummaries[index].HighlightsTruncated = true
			}
		}
		digest.Coverage.DetailedWorkUnitCount = 0
		digest.Coverage.AggregatedWorkUnitCount = digest.Coverage.SourceWorkUnitCount
		digest.Coverage.Truncated = true
		encoded, _ = json.Marshal(digest)
	}
	if len(encoded) > maxBytes {
		digest.DailySummaries = []DailySummary{}
		if digest.ReportPeriodSummary != nil {
			digest.ReportPeriodSummary.Days = []DailySummary{}
		}
		encoded, _ = json.Marshal(digest)
	}
	return digest, encoded, truncated
}

func CompactDigest(input Digest) Digest {
	digest := cloneDigest(input)
	compactAllWorkUnits(&digest, true)
	compactDailySummaries(digest.DailySummaries, true)
	compactReportPeriodSummary(digest.ReportPeriodSummary, true)
	for len(digest.WorkUnits) > 3 {
		removeIndex := lowestPriorityIndex(digest.WorkUnits)
		removed := digest.WorkUnits[removeIndex]
		digest.WorkUnits = append(digest.WorkUnits[:removeIndex], digest.WorkUnits[removeIndex+1:]...)
		addAggregate(&digest, removed)
		digest.Coverage.AggregatedWorkUnitCount++
	}
	digest.Coverage.DetailedWorkUnitCount = len(digest.WorkUnits)
	digest.Coverage.Truncated = true
	return digest
}

func cloneDigest(input Digest) Digest {
	encoded, _ := json.Marshal(input)
	var output Digest
	_ = json.Unmarshal(encoded, &output)
	if output.WorkUnits == nil {
		output.WorkUnits = []WorkUnit{}
	}
	if output.DailySummaries == nil {
		output.DailySummaries = []DailySummary{}
	}
	if output.DiscussionAggregates == nil {
		output.DiscussionAggregates = []DiscussionAggregate{}
	}
	return output
}

func compactAllWorkUnits(digest *Digest, aggressive bool) {
	for index := range digest.WorkUnits {
		unit := &digest.WorkUnits[index]
		goalLimit := 384
		resultLimit := 4
		evidenceLimit := 8
		changeLimit := 8
		validationLimit := 6
		unresolvedLimit := 4
		if aggressive {
			goalLimit = 192
			resultLimit = 2
			evidenceLimit = 4
			changeLimit = 4
			validationLimit = 3
			unresolvedLimit = 2
		}
		unit.Goal.Text, _ = truncateUTF8Bytes(unit.Goal.Text, goalLimit)
		resultRefLimit := 8
		if aggressive {
			resultRefLimit = 4
		}
		unit.ResultStatements = compactResults(
			unit.ResultStatements, resultLimit, 320, resultRefLimit,
		)
		if len(unit.AgentClaims) > 1 {
			unit.AgentClaims = append([]AgentClaim(nil), unit.AgentClaims[len(unit.AgentClaims)-1])
		}
		for claimIndex := range unit.AgentClaims {
			unit.AgentClaims[claimIndex].Text, _ = truncateUTF8Bytes(unit.AgentClaims[claimIndex].Text, 256)
		}
		unit.Evidence = firstEvidence(unit.Evidence, evidenceLimit)
		unit.Changes = firstChanges(unit.Changes, changeLimit)
		unit.Validations = prioritizeValidations(unit.Validations, validationLimit)
		unit.Unresolved = firstUnresolved(unit.Unresolved, unresolvedLimit)
	}
	digest.Coverage.Truncated = true
}

func lowestPriorityIndex(units []WorkUnit) int {
	index := 0
	lowest := workUnitPriority(units[0])
	for candidate := 1; candidate < len(units); candidate++ {
		priority := workUnitPriority(units[candidate])
		if priority < lowest || (priority == lowest && units[candidate].Sequence < units[index].Sequence) {
			index = candidate
			lowest = priority
		}
	}
	return index
}

func workUnitPriority(unit WorkUnit) int {
	score := 0
	switch unit.EvidenceGrade {
	case "A":
		score += 100
	case "B":
		score += 70
	case "C":
		score += 20
	}
	switch unit.Status {
	case "failed", "blocked":
		score += 35
	case "partial":
		score += 30
	case "completed":
		score += 20
	}
	score += min(len(unit.ResultStatements), 5) * 8
	score += min(len(unit.Unresolved), 3) * 8
	if unit.Category == "discussion" || unit.Category == "administrative" {
		score -= 20
	}
	return score
}

func addAggregate(digest *Digest, unit WorkUnit) {
	if len(digest.DiscussionAggregates) == 0 {
		digest.DiscussionAggregates = append(digest.DiscussionAggregates, DiscussionAggregate{
			Topic:           "其余工作单元已按摘要预算聚合",
			ActivityStartAt: unit.ActivityStartAt,
			ActivityEndAt:   unit.ActivityEndAt,
		})
	}
	aggregate := &digest.DiscussionAggregates[0]
	aggregate.WorkUnitCount++
	if aggregate.ActivityStartAt == "" ||
		(unit.ActivityStartAt != "" && unit.ActivityStartAt < aggregate.ActivityStartAt) {
		aggregate.ActivityStartAt = unit.ActivityStartAt
	}
	if unit.ActivityEndAt > aggregate.ActivityEndAt {
		aggregate.ActivityEndAt = unit.ActivityEndAt
	}
	if unit.Status == "pending" || unit.Status == "unknown" {
		aggregate.PendingQuestionCount++
	}
}

func firstResults(values []ResultStatement, limit int) []ResultStatement {
	if len(values) <= limit {
		return values
	}
	return append([]ResultStatement(nil), values[:limit]...)
}

func compactResults(
	values []ResultStatement,
	limit int,
	textBytes int,
	evidenceRefLimit int,
) []ResultStatement {
	values = firstResults(values, limit)
	result := append([]ResultStatement(nil), values...)
	for index := range result {
		result[index].Text, _ = truncateUTF8Bytes(result[index].Text, textBytes)
		if len(result[index].EvidenceRefs) > evidenceRefLimit {
			result[index].EvidenceRefs = append(
				[]string(nil), result[index].EvidenceRefs[:evidenceRefLimit]...,
			)
		}
	}
	return result
}

func firstEvidence(values []Evidence, limit int) []Evidence {
	if len(values) <= limit {
		return values
	}
	return append([]Evidence(nil), values[:limit]...)
}

func firstChanges(values []Change, limit int) []Change {
	if len(values) <= limit {
		return values
	}
	return append([]Change(nil), values[:limit]...)
}

func firstValidations(values []Validation, limit int) []Validation {
	if len(values) <= limit {
		return values
	}
	return append([]Validation(nil), values[:limit]...)
}

func prioritizeValidations(values []Validation, limit int) []Validation {
	if len(values) <= limit {
		return values
	}
	result := append([]Validation(nil), values...)
	sort.SliceStable(result, func(i, j int) bool {
		rank := func(status string) int {
			switch status {
			case "failed":
				return 0
			case "unknown":
				return 1
			default:
				return 2
			}
		}
		return rank(result[i].LastStatus) < rank(result[j].LastStatus)
	})
	return result[:limit]
}

func firstUnresolved(values []Unresolved, limit int) []Unresolved {
	if len(values) <= limit {
		return values
	}
	return append([]Unresolved(nil), values[:limit]...)
}
