package reportcontext

import (
	"bytes"
	"encoding/json"
	"errors"
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

type workEvidenceFactIdentity struct {
	Kind   string
	Text   string
	Source string
}

type workEvidenceFactCandidate struct {
	Identity    workEvidenceFactIdentity
	Observation WorkEvidenceObservation
}

type workEvidenceGroupIdentity struct {
	SourceRef string
	Category  string
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
			if !errors.Is(err, ErrIncomplete) {
				return Payload{}, err
			}
			projected = payload
			if err := removeDuplicateLegacyDigest(&projected); err != nil {
				return Payload{}, err
			}
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
		Mode:     session.Mode,
		Timezone: digest.Timezone,
		Period: WorkEvidencePeriod{
			StartDate: digest.ReportPeriod.StartDate,
			EndDate:   digest.ReportPeriod.EndDate,
		},
		Facts: make([]WorkEvidenceFact, 0),
	}

	workUnitRefs := make(map[string]struct{})
	sourceItemRefs := make(map[string]struct{})
	sessionRefs := make(map[string]struct{})
	factIndexes := make(map[workEvidenceFactIdentity]int)
	factObservations := make([]map[WorkEvidenceObservation]struct{}, 0)
	resultCandidates := make(map[workEvidenceGroupIdentity][]workEvidenceFactCandidate)
	groupOrder := make([]workEvidenceGroupIdentity, 0)
	unresolvedCandidates := make([]workEvidenceFactCandidate, 0)
	for _, item := range digest.Items {
		if strings.TrimSpace(item.SourceItemRef) == "" || strings.TrimSpace(item.SessionRef) == "" {
			return WorkEvidence{}, ErrIncomplete
		}
		if _, exists := sourceItemRefs[item.SourceItemRef]; exists {
			return WorkEvidence{}, ErrIncomplete
		}
		sourceItemRefs[item.SourceItemRef] = struct{}{}
		sessionRefs[item.SessionRef] = struct{}{}
	}
	hasSourceRefs := false
	hasMissingSourceRefs := false
	for _, day := range digest.ReportPeriod.Days {
		if day.HighlightsTruncated || !day.OutcomeCoverage.Complete ||
			day.OutcomeCoverage.SourceCount != day.OutcomeCoverage.RepresentedCount ||
			day.OutcomeCoverage.RepresentedCount != len(day.Highlights) {
			return WorkEvidence{}, ErrIncomplete
		}
		for _, highlight := range day.Highlights {
			if strings.TrimSpace(highlight.WorkUnitRef) == "" {
				return WorkEvidence{}, ErrIncomplete
			}
			if _, exists := workUnitRefs[highlight.WorkUnitRef]; exists {
				return WorkEvidence{}, ErrIncomplete
			}
			workUnitRefs[highlight.WorkUnitRef] = struct{}{}
			sourceRef := strings.TrimSpace(highlight.SourceRef)
			if sourceRef == "" {
				hasMissingSourceRefs = true
				// Frozen contexts created before source_ref was added keep their
				// original one-result-per-Work-Unit behavior.
				sourceRef = "legacy:" + highlight.WorkUnitRef
			} else {
				hasSourceRefs = true
				if _, exists := sessionRefs[sourceRef]; !exists {
					return WorkEvidence{}, ErrIncomplete
				}
			}
			group := workEvidenceGroupIdentity{
				SourceRef: sourceRef,
				Category:  strings.TrimSpace(highlight.Category),
			}
			if group.Category == "" {
				group.Category = "unknown"
			}
			if _, exists := resultCandidates[group]; !exists {
				groupOrder = append(groupOrder, group)
			}
			observation := WorkEvidenceObservation{
				Date: day.Date, Status: highlight.Status,
			}
			for _, statement := range highlight.ResultStatements {
				if strings.TrimSpace(statement.Text) == "" || strings.TrimSpace(statement.Source) == "" {
					return WorkEvidence{}, ErrIncomplete
				}
				text := projectReportFactText(statement.Text)
				if text == "" {
					continue
				}
				resultCandidates[group] = append(
					resultCandidates[group],
					workEvidenceFactCandidate{
						Identity: workEvidenceFactIdentity{
							Kind: "result", Text: text, Source: statement.Source,
						},
						Observation: observation,
					},
				)
			}
			for _, unresolved := range highlight.Unresolved {
				if strings.TrimSpace(unresolved.Text) == "" {
					return WorkEvidence{}, ErrIncomplete
				}
				unresolvedCandidates = append(
					unresolvedCandidates,
					workEvidenceFactCandidate{
						Identity: workEvidenceFactIdentity{
							Kind: "unresolved", Text: unresolved.Text,
						},
						Observation: observation,
					},
				)
			}
		}
	}
	if hasSourceRefs && hasMissingSourceRefs {
		return WorkEvidence{}, ErrIncomplete
	}

	for _, group := range groupOrder {
		candidates := distinctWorkEvidenceCandidates(resultCandidates[group])
		if len(candidates) == 0 {
			continue
		}
		if !hasSourceRefs {
			for _, candidate := range candidates {
				appendWorkEvidenceFact(
					&projection, factIndexes, &factObservations,
					candidate.Identity, candidate.Observation,
				)
			}
			continue
		}
		appendWorkEvidenceFact(
			&projection, factIndexes, &factObservations,
			candidates[0].Identity, candidates[0].Observation,
		)
		if len(candidates) > 1 {
			latest := candidates[len(candidates)-1]
			appendWorkEvidenceFact(
				&projection, factIndexes, &factObservations,
				latest.Identity, latest.Observation,
			)
		}
	}
	for _, candidate := range unresolvedCandidates {
		appendWorkEvidenceFact(
			&projection, factIndexes, &factObservations,
			candidate.Identity, candidate.Observation,
		)
	}

	if len(projection.Facts) == 0 && digest.ReportPeriod.ResultWorkUnitCount > 0 {
		return WorkEvidence{}, ErrIncomplete
	}
	return projection, nil
}

func distinctWorkEvidenceCandidates(values []workEvidenceFactCandidate) []workEvidenceFactCandidate {
	result := make([]workEvidenceFactCandidate, 0, len(values))
	seen := make(map[workEvidenceFactIdentity]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value.Identity]; exists {
			continue
		}
		seen[value.Identity] = struct{}{}
		result = append(result, value)
	}
	return result
}

func projectReportFactText(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	if unwrapped := unwrapTextEnvelope(text); unwrapped != "" {
		text = unwrapped
	}
	if isHandoffPromptArtifact(text) {
		return ""
	}
	if isInternalDelegationResult(text) || isInstructionAcknowledgement(text) {
		return ""
	}
	if isProcessNarration(text) {
		return ""
	}
	return compactReportFactLead(text)
}

const maxReportFactRunes = 240

func compactReportFactLead(value string) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	blocks := make([]string, 0, 4)
	paragraph := make([]string, 0, 4)
	inFence := false
	flush := func() {
		text := strings.Join(strings.Fields(strings.Join(paragraph, " ")), " ")
		paragraph = paragraph[:0]
		if text != "" {
			blocks = append(blocks, text)
		}
	}
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			flush()
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "#") || isMarkdownTableLine(line) {
			flush()
			continue
		}
		cleaned, listItem := stripMarkdownListMarker(line)
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, ">"))
		if cleaned == "" || isCommandOrPathOnly(cleaned) || isPureGitOperation(cleaned) {
			flush()
			continue
		}
		if listItem {
			flush()
			blocks = append(blocks, cleaned)
			continue
		}
		paragraph = append(paragraph, cleaned)
	}
	flush()
	for _, block := range blocks {
		if isLowInformationReportLead(block) {
			continue
		}
		block = stripFlattenedReportDetail(block)
		return reportFactLeadSentences(stripMarkdownLinkTargets(block), maxReportFactRunes)
	}
	return ""
}

func stripFlattenedReportDetail(value string) string {
	for _, marker := range []string{
		" ```", " ~~~", " ## ", " 主要改进：", " 核心修改：", " 主要修复：",
		" 修改内容：", " 关键闭环：", " 主要交付：", " 证据链：", "： - ", ": - ",
	} {
		if index := strings.Index(value, marker); index >= 0 {
			prefix := strings.TrimSpace(value[:index])
			if len([]rune(prefix)) >= 18 && !isLowInformationReportLead(prefix) {
				value = prefix
			}
		}
	}
	return strings.TrimSpace(value)
}

func stripMarkdownListMarker(value string) (string, bool) {
	for _, prefix := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(value, prefix)), true
		}
	}
	index := 0
	for index < len(value) && value[index] >= '0' && value[index] <= '9' {
		index++
	}
	if index > 0 && index+1 < len(value) && value[index] == '.' && value[index+1] == ' ' {
		return strings.TrimSpace(value[index+2:]), true
	}
	return value, false
}

func isMarkdownTableLine(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "|") && strings.Count(trimmed, "|") >= 2
}

func isCommandOrPathOnly(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(strings.Trim(value, "`")))
	for _, prefix := range []string{
		"$ ", "curl ", "go test ", "pytest ", "pnpm ", "npm ", "yarn ",
		"docker ", "kubectl ", "make ", "git ", "ssh ",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return strings.HasPrefix(lower, "/") && !strings.ContainsAny(lower, "，。；：,;: ")
}

func isPureGitOperation(value string) bool {
	lower := strings.ToLower(value)
	if !containsAnyText(lower, "git ", "commit", "merge", "rebase", "cherry-pick", "提交号", "分支", "worktree", "已合入") {
		return false
	}
	return !containsAnyText(lower,
		"修复", "实现", "开发", "设计", "部署", "发布", "上线",
		"回滚", "冲突", "解决", "定位", "分析", "文档", "方案", "功能", "故障",
	)
}

func isLowInformationReportLead(value string) bool {
	trimmed := strings.TrimSpace(strings.Trim(value, "。！？!?：: "))
	if trimmed == "" {
		return true
	}
	if len([]rune(trimmed)) <= 24 && containsAnyText(strings.ToLower(trimmed),
		"可以", "好的", "没问题", "已处理", "处理好了", "已经完成", "完成了", "已结束", "结论如下", "结果如下",
	) {
		return true
	}
	return len([]rune(trimmed)) <= 80 &&
		(strings.HasSuffix(value, "如下：") || strings.HasSuffix(value, "如下:"))
}

func isInternalDelegationResult(value string) bool {
	lower := strings.ToLower(value)
	return containsAnyText(lower, "主代理", "父代理", "父任务", "主任务") &&
		containsAnyText(lower, "发给", "同步给", "交给", "回传", "发送给")
}

func isInstructionAcknowledgement(value string) bool {
	lower := strings.ToLower(value)
	return containsAnyText(lower, "已完整阅读 agents.md", "已阅读 agents.md", "后续任务我会严格遵守")
}

func isProcessNarration(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	for _, prefix := range []string{
		"我会", "我先", "接下来", "下一步", "现在先", "先只", "你的担忧", "你的判断", "你的理解",
		"只读分析结论如下", "只读检查结果如下", "审查结论如下",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func reportFactLeadSentences(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) == 0 {
		return ""
	}
	cut := 0
	for index, value := range runes {
		if strings.ContainsRune("。！？!?", value) ||
			(value == '.' && index+1 < len(runes) && runes[index+1] == ' ') {
			cut = index + 1
			if cut >= 18 {
				break
			}
		}
		if index+1 >= limit {
			break
		}
	}
	if cut < 18 || cut > limit {
		cut = min(limit, len(runes))
	}
	result := strings.TrimSpace(string(runes[:cut]))
	if cut < len(runes) && !strings.ContainsRune("。！？!?", runes[cut-1]) {
		result += "…"
	}
	return strings.Trim(strings.ReplaceAll(result, "**", ""), "*` ")
}

func stripMarkdownLinkTargets(value string) string {
	for {
		open := strings.Index(value, "[")
		if open < 0 {
			return value
		}
		middleOffset := strings.Index(value[open:], "](")
		if middleOffset < 0 {
			return value
		}
		middle := open + middleOffset
		closeOffset := strings.Index(value[middle+2:], ")")
		if closeOffset < 0 {
			return value
		}
		closeIndex := middle + 2 + closeOffset
		value = value[:open] + value[open+1:middle] + value[closeIndex+1:]
	}
}

func containsAnyText(value string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func unwrapTextEnvelope(value string) string {
	var items []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(value), &items) != nil || len(items) == 0 {
		return ""
	}
	texts := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(item.Text)
		if text == "" {
			return ""
		}
		texts = append(texts, text)
	}
	return strings.Join(texts, "\n\n")
}

func isHandoffPromptArtifact(value string) bool {
	prefix := value
	if len(prefix) > 300 {
		prefix = prefix[:300]
	}
	prefix = strings.ToLower(prefix)
	introducesPayload := strings.Contains(prefix, "下面这段") || strings.Contains(prefix, "以下内容") ||
		strings.Contains(prefix, `[{"text": "下面这段`) || strings.Contains(prefix, `[{"text":"下面这段`)
	transfersPayload := strings.Contains(prefix, "直接") || strings.Contains(prefix, "复制") ||
		strings.Contains(prefix, "发给") || strings.Contains(prefix, "交给")
	targetsModel := strings.Contains(prefix, "模型") || strings.Contains(prefix, "agent")
	return introducesPayload && transfersPayload && targetsModel
}

func appendWorkEvidenceFact(
	projection *WorkEvidence,
	indexes map[workEvidenceFactIdentity]int,
	observationSets *[]map[WorkEvidenceObservation]struct{},
	identity workEvidenceFactIdentity,
	observation WorkEvidenceObservation,
) {
	index, exists := indexes[identity]
	if !exists {
		index = len(projection.Facts)
		indexes[identity] = index
		projection.Facts = append(projection.Facts, WorkEvidenceFact{
			Kind: identity.Kind, Text: identity.Text, Source: identity.Source,
			Observations: []WorkEvidenceObservation{},
		})
		*observationSets = append(*observationSets, make(map[WorkEvidenceObservation]struct{}))
	}
	if _, exists := (*observationSets)[index][observation]; exists {
		return
	}
	(*observationSets)[index][observation] = struct{}{}
	projection.Facts[index].Observations = append(projection.Facts[index].Observations, observation)
}
