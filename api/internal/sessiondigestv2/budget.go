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
	// The item budget is a cost-warning target, not permission to shorten or
	// rank report-facing facts. First remove duplicated engineering details.
	// Every report-relevant user goal and final answer is already represented
	// in the daily view before detailed Work Units may be removed.
	if completeOutcomeCoverage(digest) {
		digest.WorkUnits = []WorkUnit{}
		digest.DiscussionAggregates = []DiscussionAggregate{{
			Topic:         "详细证据已压缩，所有报告相关工作单元保留在 daily_summaries",
			WorkUnitCount: digest.Coverage.SourceWorkUnitCount,
		}}
		digest.Coverage.DetailedWorkUnitCount = 0
		digest.Coverage.AggregatedWorkUnitCount = digest.Coverage.SourceWorkUnitCount
		digest.Coverage.Truncated = true
	} else {
		// Without complete daily outcome coverage, no detailed Work Unit can be
		// proven redundant. Return the complete semantic payload above the cost
		// target instead of ranking or dropping user work.
		digest.Coverage.Truncated = true
	}
	encoded, _ = json.Marshal(digest)
	// If meaningful content itself is larger than the target, return it intact.
	// The exact frozen selection size drives the 1 MiB UI confirmation and the
	// separate selection envelope remains the infrastructure hard limit.
	digest.Coverage.Truncated = true
	return digest, encoded, truncated
}

func CompactDigest(input Digest) Digest {
	digest := cloneDigest(input)
	compactAllWorkUnits(&digest, true)
	compactDailySummaries(digest.DailySummaries, true)
	compactReportPeriodSummary(digest.ReportPeriodSummary, true)
	stripWorkUnitEvidenceDetails(&digest)
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
		evidenceLimit := 8
		changeLimit := 8
		validationLimit := 6
		if aggressive {
			goalLimit = 192
			evidenceLimit = 4
			changeLimit = 4
			validationLimit = 3
		}
		unit.Goal.Text, _ = truncateUTF8Bytes(unit.Goal.Text, goalLimit)
		resultRefLimit := 8
		if aggressive {
			resultRefLimit = 4
		}
		unit.ResultStatements = compactResultsPreservingCount(
			unit.ResultStatements, 320, resultRefLimit,
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
		if len(unit.Unresolved) > 0 {
			unresolvedBytes := 320
			if aggressive {
				unresolvedBytes = 160
			}
			for unresolvedIndex := range unit.Unresolved {
				unit.Unresolved[unresolvedIndex].Text, _ = truncateUTF8Bytes(
					unit.Unresolved[unresolvedIndex].Text, unresolvedBytes,
				)
			}
		}
	}
	digest.Coverage.Truncated = true
}

func stripWorkUnitEvidenceDetails(digest *Digest) {
	for index := range digest.WorkUnits {
		unit := &digest.WorkUnits[index]
		unit.AgentClaims = []AgentClaim{}
		unit.Evidence = []Evidence{}
		unit.Changes = []Change{}
		unit.Validations = []Validation{}
		for resultIndex := range unit.ResultStatements {
			unit.ResultStatements[resultIndex].EvidenceRefs = []string{}
		}
		for unresolvedIndex := range unit.Unresolved {
			unit.Unresolved[unresolvedIndex].EvidenceRef = ""
		}
	}
	digest.Coverage.Truncated = true
}

func completeOutcomeCoverage(digest Digest) bool {
	resultUnits := 0
	for _, unit := range digest.WorkUnits {
		if isReportRelevantWorkUnit(unit) {
			resultUnits++
		}
	}
	days := digest.DailySummaries
	if digest.ReportPeriodSummary != nil {
		days = digest.ReportPeriodSummary.Days
	}
	represented := 0
	for _, day := range days {
		if day.HighlightsTruncated || !day.OutcomeCoverage.Complete ||
			day.OutcomeCoverage.SourceCount != day.OutcomeCoverage.RepresentedCount ||
			day.OutcomeCoverage.RepresentedCount != len(day.Highlights) {
			return false
		}
		represented += day.OutcomeCoverage.RepresentedCount
	}
	if digest.ReportPeriodSummary != nil {
		return ReportPeriodOutcomeCoverageComplete(digest.ReportPeriodSummary)
	}
	return represented == resultUnits
}

func compactResultsPreservingCount(
	values []ResultStatement,
	textBytes int,
	evidenceRefLimit int,
) []ResultStatement {
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
