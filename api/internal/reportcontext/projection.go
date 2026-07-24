package reportcontext

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/aidashboard/api/internal/reportsource"
	"github.com/aidashboard/api/internal/sessiondigestv2"
)

type frozenDigestV2 struct {
	ContentMode      string                               `json:"content_mode"`
	Timezone         string                               `json:"timezone"`
	DigestVersion    string                               `json:"digest_version"`
	RedactionVersion string                               `json:"redaction_version"`
	ContentSnapshot  string                               `json:"content_snapshot_at"`
	Completeness     string                               `json:"completeness"`
	ReturnedCount    int                                  `json:"returned_item_count"`
	HasMore          bool                                 `json:"has_more"`
	Coverage         reportsource.DigestCoverage          `json:"coverage"`
	ReportPeriod     *sessiondigestv2.ReportPeriodSummary `json:"report_period_summary"`
	Items            []frozenDigestV2Item                 `json:"items"`
}

type frozenDigestV2Item struct {
	SourceItemRef string                          `json:"source_item_ref"`
	SessionRef    string                          `json:"session_ref"`
	AgentType     string                          `json:"agent_type"`
	ActivityStart string                          `json:"activity_start_at"`
	ActivityEnd   string                          `json:"activity_end_at"`
	DigestSHA256  string                          `json:"digest_sha256"`
	Coverage      reportsource.DigestItemCoverage `json:"coverage"`
	Digest        struct {
		Coverage sessiondigestv2.Coverage `json:"coverage"`
	} `json:"digest"`
}

func (e WorkEvidence) MarshalJSON() ([]byte, error) {
	type alias WorkEvidence
	return json.Marshal(struct {
		RowReferenceBase  int      `json:"row_reference_base"`
		LookupColumns     []string `json:"lookup_columns"`
		FactColumns       []string `json:"fact_columns"`
		ResultColumns     []string `json:"result_columns"`
		UnresolvedColumns []string `json:"unresolved_columns"`
		SourceColumns     []string `json:"source_columns"`
		alias
	}{
		RowReferenceBase:  1,
		LookupColumns:     []string{"id", "value"},
		FactColumns:       []string{"work_unit_ref", "sequence", "period_day_ref", "activity_end_at", "category_ref", "status_ref", "evidence_grade_ref", "results", "unresolved"},
		ResultColumns:     []string{"text_ref", "source_ref", "evidence_refs"},
		UnresolvedColumns: []string{"text", "evidence_ref"},
		SourceColumns:     []string{"source_item_ref", "session_ref", "agent_type", "activity_start_at", "activity_end_at", "digest_sha256", "source_event_count", "included_event_count", "omitted_event_count", "truncated", "source_work_unit_count", "detailed_work_unit_count", "aggregated_work_unit_count"},
		alias:             alias(e),
	})
}

func (v WorkEvidenceLookup) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{v.ID, v.Value})
}

func (v WorkEvidenceResult) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{v.TextRef, v.SourceRef, v.EvidenceRefs})
}

func (v WorkEvidenceUnresolved) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{v.Text, v.EvidenceRef})
}

func (v WorkEvidenceFact) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{
		v.WorkUnitRef, v.Sequence, v.DateRef, v.ActivityEndAt,
		v.CategoryRef, v.StatusRef, v.EvidenceGradeRef, v.Results, v.Unresolved,
	})
}

func (v WorkEvidenceSource) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{
		v.SourceItemRef, v.SessionRef, v.AgentType, v.ActivityStartAt,
		v.ActivityEndAt, v.DigestSHA256, v.SourceEventCount,
		v.IncludedEventCount, v.OmittedEventCount, v.Truncated,
		v.SourceWorkUnitCount, v.DetailedWorkUnitCount, v.AggregatedWorkUnitCount,
	})
}

func projectPayloadForRepresentation(payload Payload, representation string) (Payload, error) {
	switch representation {
	case "":
		return payload, nil
	case RepresentationWorkEvidence:
		profile, err := presentationProfileFor(payload.Run.ReportType)
		if err != nil {
			return Payload{}, err
		}
		projected, err := projectPayload(payload)
		if err != nil {
			return Payload{}, err
		}
		projected.PresentationProfile = &profile
		return projected, nil
	default:
		return Payload{}, ErrInvalidRequest
	}
}

func presentationProfileFor(reportType string) (PresentationProfile, error) {
	profiles := map[string]PresentationProfile{
		ReportTypePersonalDaily: {
			SummaryFocus:    "个人当日推进的主要目标、关键成果、验证和整体状态；只有存在明确证据时才提及风险或阻塞。",
			ContentGrouping: "按个人工作目标归并；同一目标下的开发、文档、部署、验证和修复合并表达。",
		},
		ReportTypePersonalWeekly: {
			SummaryFocus:    "个人本周核心进展、里程碑、最新状态和明确风险。",
			ContentGrouping: "跨日期归并持续目标，不按星期或日报逐条复述。",
		},
		ReportTypeTeamDaily: {
			SummaryFocus:    "小组当日共同目标、团队交付、整体状态和共享阻塞。",
			ContentGrouping: "按小组共同目标归并，不默认逐人罗列；只有解释职责或阻塞归属时提及成员。",
		},
		ReportTypeTeamWeekly: {
			SummaryFocus:    "小组本周交付、关键里程碑、协作状态和明确风险。",
			ContentGrouping: "按小组业务目标与里程碑归并，不按成员或日期罗列。",
		},
		ReportTypeDepartmentDaily: {
			SummaryFocus:    "部门当日重要进展、整体状态和需要管理关注的跨团队问题。",
			ContentGrouping: "按部门级目标归并，不机械罗列小组；只有解释责任或依赖时提及小组。",
		},
		ReportTypeDepartmentWeekly: {
			SummaryFocus:    "部门本周整体成果、关键进度、跨团队依赖和管理关注项。",
			ContentGrouping: "按部门级目标和关键里程碑归并，不按小组逐份复述。",
		},
	}
	profile, ok := profiles[strings.TrimSpace(reportType)]
	if !ok || strings.TrimSpace(profile.SummaryFocus) == "" || strings.TrimSpace(profile.ContentGrouping) == "" {
		return PresentationProfile{}, ErrInvalidRequest
	}
	return profile, nil
}

// projectPayload produces the single immutable representation exposed to the
// Report Agent. It only removes data when equality can be proven from the
// frozen payload; it never ranks, summarizes, or semantically merges facts.
func projectPayload(payload Payload) (Payload, error) {
	if err := removeDuplicateLegacyDigest(&payload); err != nil {
		return Payload{}, err
	}
	if len(payload.Sessions) != 1 || payload.Sessions[0].Mode != reportsource.ReadModeDigestV2 {
		return payload, nil
	}

	workEvidence, err := projectDigestV2(payload.Sessions[0])
	if err != nil {
		return Payload{}, err
	}
	payload.WorkEvidence = &workEvidence
	payload.Sessions = nil
	return payload, nil
}

func removeDuplicateLegacyDigest(payload *Payload) error {
	if len(payload.Sessions) != 1 || len(payload.Sources.SessionDigest) == 0 {
		return nil
	}
	if bytes.Equal(payload.Sessions[0].Digest, payload.Sources.SessionDigest) {
		payload.Sources.SessionDigest = nil
	}
	return nil
}

func projectDigestV2(session SessionSource) (WorkEvidence, error) {
	var digest frozenDigestV2
	if err := json.Unmarshal(session.Digest, &digest); err != nil {
		return WorkEvidence{}, ErrIncomplete
	}
	if digest.ContentMode != reportsource.ReadModeDigestV2 ||
		digest.Completeness != "complete" || digest.HasMore ||
		!digest.Coverage.Complete || digest.ReportPeriod == nil ||
		digest.ReturnedCount != len(digest.Items) ||
		digest.Coverage.SourceItemCount != digest.Coverage.RepresentedItemCount ||
		digest.Coverage.RepresentedItemCount != len(digest.Items) {
		return WorkEvidence{}, ErrIncomplete
	}

	projection := WorkEvidence{
		SelectionID:      session.SelectionID,
		Mode:             session.Mode,
		Timezone:         digest.Timezone,
		DigestVersion:    digest.DigestVersion,
		RedactionVersion: digest.RedactionVersion,
		ContentSnapshot:  digest.ContentSnapshot,
		Completeness:     digest.Completeness,
		Coverage: WorkEvidenceCoverage{
			Complete:             digest.Coverage.Complete,
			SourceItemCount:      digest.Coverage.SourceItemCount,
			RepresentedItemCount: digest.Coverage.RepresentedItemCount,
			SourceEventCount:     digest.Coverage.SourceEventCount,
			IncludedEventCount:   digest.Coverage.IncludedEventCount,
			OmittedEventCount:    digest.Coverage.OmittedEventCount,
			TruncatedItemCount:   digest.Coverage.TruncatedItemCount,
		},
		Period: WorkEvidencePeriod{
			StartDate:           digest.ReportPeriod.StartDate,
			EndDate:             digest.ReportPeriod.EndDate,
			WorkUnitCount:       digest.ReportPeriod.WorkUnitCount,
			ResultWorkUnitCount: digest.ReportPeriod.ResultWorkUnitCount,
			PrimaryResultCount:  digest.ReportPeriod.PrimaryResultCount,
			VerifiedResultCount: digest.ReportPeriod.VerifiedResultCount,
			ChangeCount:         digest.ReportPeriod.ChangeCount,
			ValidationCount:     digest.ReportPeriod.ValidationCount,
			UnresolvedCount:     digest.ReportPeriod.UnresolvedCount,
			Days:                make([]WorkEvidenceDay, 0, len(digest.ReportPeriod.Days)),
		},
		Categories:     []WorkEvidenceLookup{},
		Statuses:       []WorkEvidenceLookup{},
		EvidenceGrades: []WorkEvidenceLookup{},
		ResultSources:  []WorkEvidenceLookup{},
		ResultTexts:    []WorkEvidenceLookup{},
		EvidenceByGoal: []ExactGoalEvidence{},
		Sources:        make([]WorkEvidenceSource, 0, len(digest.Items)),
	}

	goalIndexes := make(map[string]int)
	categoryIDs := make(map[string]int)
	statusIDs := make(map[string]int)
	evidenceGradeIDs := make(map[string]int)
	resultSourceIDs := make(map[string]int)
	textIDs := make(map[string]int)
	workUnitRefs := make(map[string]struct{})
	sourceItemRefs := make(map[string]struct{})
	representedFacts := 0
	for dayIndex, day := range digest.ReportPeriod.Days {
		if day.HighlightsTruncated || !day.OutcomeCoverage.Complete ||
			day.OutcomeCoverage.SourceCount != day.OutcomeCoverage.RepresentedCount ||
			day.OutcomeCoverage.RepresentedCount != len(day.Highlights) {
			return WorkEvidence{}, ErrIncomplete
		}
		projection.Period.Days = append(projection.Period.Days, WorkEvidenceDay{
			Date:                 day.Date,
			WorkUnitCount:        day.WorkUnitCount,
			ResultWorkUnitCount:  day.ResultWorkUnitCount,
			PrimaryResultCount:   day.PrimaryResultCount,
			VerifiedResultCount:  day.VerifiedResultCount,
			ChangeCount:          day.ChangeCount,
			ValidationCount:      day.ValidationCount,
			UnresolvedCount:      day.UnresolvedCount,
			SourceFactCount:      day.OutcomeCoverage.SourceCount,
			RepresentedFactCount: day.OutcomeCoverage.RepresentedCount,
			Complete:             day.OutcomeCoverage.Complete,
			SourceTextCompacted:  day.OutcomeCoverage.TextCompacted,
			SourceFactsTruncated: day.HighlightsTruncated,
		})
		for _, highlight := range day.Highlights {
			if strings.TrimSpace(highlight.WorkUnitRef) == "" {
				return WorkEvidence{}, ErrIncomplete
			}
			if _, exists := workUnitRefs[highlight.WorkUnitRef]; exists {
				return WorkEvidence{}, ErrIncomplete
			}
			workUnitRefs[highlight.WorkUnitRef] = struct{}{}
			groupIndex, ok := goalIndexes[highlight.Goal]
			if !ok {
				groupIndex = len(projection.EvidenceByGoal)
				goalIndexes[highlight.Goal] = groupIndex
				projection.EvidenceByGoal = append(projection.EvidenceByGoal, ExactGoalEvidence{
					Goal: highlight.Goal, Facts: []WorkEvidenceFact{},
				})
			}
			fact := WorkEvidenceFact{
				WorkUnitRef:      highlight.WorkUnitRef,
				Sequence:         highlight.Sequence,
				DateRef:          dayIndex + 1,
				ActivityEndAt:    highlight.ActivityEndAt,
				CategoryRef:      internLookup(categoryIDs, &projection.Categories, highlight.Category),
				StatusRef:        internLookup(statusIDs, &projection.Statuses, highlight.Status),
				EvidenceGradeRef: internLookup(evidenceGradeIDs, &projection.EvidenceGrades, highlight.EvidenceGrade),
				Results:          make([]WorkEvidenceResult, 0, len(highlight.ResultStatements)),
				Unresolved:       make([]WorkEvidenceUnresolved, 0, len(highlight.Unresolved)),
			}
			for _, statement := range highlight.ResultStatements {
				if strings.TrimSpace(statement.Text) == "" || strings.TrimSpace(statement.Source) == "" {
					return WorkEvidence{}, ErrIncomplete
				}
				fact.Results = append(fact.Results, WorkEvidenceResult{
					TextRef:      internLookup(textIDs, &projection.ResultTexts, statement.Text),
					SourceRef:    internLookup(resultSourceIDs, &projection.ResultSources, statement.Source),
					EvidenceRefs: statement.EvidenceRefs,
				})
			}
			for _, unresolved := range highlight.Unresolved {
				if strings.TrimSpace(unresolved.Text) == "" {
					return WorkEvidence{}, ErrIncomplete
				}
				fact.Unresolved = append(fact.Unresolved, WorkEvidenceUnresolved{
					Text: unresolved.Text, EvidenceRef: unresolved.EvidenceRef,
				})
			}
			projection.EvidenceByGoal[groupIndex].Facts = append(projection.EvidenceByGoal[groupIndex].Facts, fact)
			representedFacts++
		}
	}

	for _, item := range digest.Items {
		if strings.TrimSpace(item.SourceItemRef) == "" {
			return WorkEvidence{}, ErrIncomplete
		}
		if _, exists := sourceItemRefs[item.SourceItemRef]; exists {
			return WorkEvidence{}, ErrIncomplete
		}
		sourceItemRefs[item.SourceItemRef] = struct{}{}
		projection.Sources = append(projection.Sources, WorkEvidenceSource{
			SourceItemRef:           item.SourceItemRef,
			SessionRef:              item.SessionRef,
			AgentType:               item.AgentType,
			ActivityStartAt:         item.ActivityStart,
			ActivityEndAt:           item.ActivityEnd,
			DigestSHA256:            item.DigestSHA256,
			SourceEventCount:        item.Coverage.SourceEventCount,
			IncludedEventCount:      item.Coverage.IncludedEventCount,
			OmittedEventCount:       item.Coverage.OmittedEventCount,
			Truncated:               item.Coverage.Truncated,
			SourceWorkUnitCount:     item.Digest.Coverage.SourceWorkUnitCount,
			DetailedWorkUnitCount:   item.Digest.Coverage.DetailedWorkUnitCount,
			AggregatedWorkUnitCount: item.Digest.Coverage.AggregatedWorkUnitCount,
		})
	}
	if representedFacts == 0 && digest.ReportPeriod.ResultWorkUnitCount > 0 {
		return WorkEvidence{}, ErrIncomplete
	}
	return projection, nil
}

func internLookup(index map[string]int, values *[]WorkEvidenceLookup, value string) int {
	if id, ok := index[value]; ok {
		return id
	}
	id := len(*values) + 1
	index[value] = id
	*values = append(*values, WorkEvidenceLookup{ID: id, Value: value})
	return id
}
