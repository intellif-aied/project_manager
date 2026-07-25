package reportcontext

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
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
	SourceRef   string
	Sequence    int
}

type workEvidenceGroupIdentity struct {
	SourceRef string
	Category  string
}

type workEvidenceObservationIdentity struct {
	Date     string
	Category string
	Status   string
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
// Report Agent. It compacts deterministic transport noise and exact duplicates,
// but never ranks facts or decides which conclusion is semantically correct.
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
	factObservations := make([]map[workEvidenceObservationIdentity]int, 0)
	factCandidates := make([]workEvidenceFactCandidate, 0)
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
			} else {
				hasSourceRefs = true
				if _, exists := sessionRefs[sourceRef]; !exists {
					return WorkEvidence{}, ErrIncomplete
				}
			}
			category := strings.TrimSpace(highlight.Category)
			if category == "" {
				category = "unknown"
			}
			observation := WorkEvidenceObservation{
				Date: day.Date, ObservedAt: highlight.ActivityEndAt,
				Category: category, Status: highlight.Status,
			}
			for _, statement := range highlight.ResultStatements {
				if strings.TrimSpace(statement.Text) == "" || strings.TrimSpace(statement.Source) == "" {
					return WorkEvidence{}, ErrIncomplete
				}
				text := projectReportFactText(statement.Text)
				if text == "" {
					continue
				}
				factCandidates = append(
					factCandidates,
					workEvidenceFactCandidate{
						Identity: workEvidenceFactIdentity{
							Kind: "result", Text: text, Source: statement.Source,
						},
						Observation: observation, SourceRef: sourceRef,
						Sequence: highlight.Sequence,
					},
				)
			}
			for _, unresolved := range highlight.Unresolved {
				if strings.TrimSpace(unresolved.Text) == "" {
					return WorkEvidence{}, ErrIncomplete
				}
				factCandidates = append(
					factCandidates,
					workEvidenceFactCandidate{
						Identity: workEvidenceFactIdentity{
							Kind: "unresolved", Text: unresolved.Text,
						},
						Observation: observation, SourceRef: sourceRef,
						Sequence: highlight.Sequence,
					},
				)
			}
		}
	}
	if hasSourceRefs && hasMissingSourceRefs {
		return WorkEvidence{}, ErrIncomplete
	}
	if hasSourceRefs {
		factCandidates = selectReportCheckpoints(factCandidates)
	}

	sortWorkEvidenceCandidates(factCandidates)
	for _, candidate := range factCandidates {
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

// selectReportCheckpoints keeps terminal outcomes and the current non-terminal
// state of each source/category. It never decides whether a conclusion is
// correct; the complete reply timeline remains immutable in the Digest.
func selectReportCheckpoints(candidates []workEvidenceFactCandidate) []workEvidenceFactCandidate {
	selected := make([]workEvidenceFactCandidate, 0, len(candidates))
	groups := make(map[workEvidenceGroupIdentity][]workEvidenceFactCandidate)
	for _, candidate := range candidates {
		if candidate.Identity.Kind == "unresolved" {
			selected = append(selected, candidate)
			continue
		}
		group := workEvidenceGroupIdentity{
			SourceRef: candidate.SourceRef, Category: candidate.Observation.Category,
		}
		groups[group] = append(groups[group], candidate)
		if isTerminalReportStatus(candidate.Observation.Status) {
			selected = append(selected, candidate)
		}
	}
	for _, group := range groups {
		sortWorkEvidenceCandidates(group)
		var latestTerminal *workEvidenceFactCandidate
		var latestNonTerminal *workEvidenceFactCandidate
		for index := range group {
			candidate := &group[index]
			if isTerminalReportStatus(candidate.Observation.Status) {
				latestTerminal = candidate
				continue
			}
			if isReportableNonTerminalStatus(
				candidate.Observation.Status, candidate.Observation.Category,
			) {
				latestNonTerminal = candidate
			}
		}
		if latestNonTerminal != nil &&
			(latestTerminal == nil || workEvidenceCandidateLess(*latestTerminal, *latestNonTerminal)) {
			selected = append(selected, *latestNonTerminal)
		}
	}
	return selected
}

func isTerminalReportStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "completed", "failed", "blocked":
		return true
	default:
		return false
	}
}

func isReportableNonTerminalStatus(status, category string) bool {
	switch strings.TrimSpace(status) {
	case "partial":
		return category != "discussion" && category != "administrative"
	case "unknown", "pending":
		return category == "investigation" || category == "decision"
	default:
		return false
	}
}

func sortWorkEvidenceCandidates(candidates []workEvidenceFactCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		return workEvidenceCandidateLess(candidates[i], candidates[j])
	})
}

func workEvidenceCandidateLess(left, right workEvidenceFactCandidate) bool {
	if left.Observation.Date != right.Observation.Date {
		return left.Observation.Date < right.Observation.Date
	}
	if left.Observation.ObservedAt != right.Observation.ObservedAt {
		return left.Observation.ObservedAt < right.Observation.ObservedAt
	}
	if left.Sequence != right.Sequence {
		return left.Sequence < right.Sequence
	}
	if left.Observation.Category != right.Observation.Category {
		return left.Observation.Category < right.Observation.Category
	}
	if left.Observation.Status != right.Observation.Status {
		return left.Observation.Status < right.Observation.Status
	}
	if left.Identity.Kind != right.Identity.Kind {
		return left.Identity.Kind < right.Identity.Kind
	}
	if left.Identity.Text != right.Identity.Text {
		return left.Identity.Text < right.Identity.Text
	}
	if left.Identity.Source != right.Identity.Source {
		return left.Identity.Source < right.Identity.Source
	}
	return left.SourceRef < right.SourceRef
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
	lead := stripGitTraceClauses(compactReportFactLead(text))
	if isPureGitOperation(lead) || isLowInformationReportLead(lead) ||
		isInternalDelegationResult(lead) || isProcessNarration(lead) {
		return ""
	}
	return lead
}

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
		if strings.HasPrefix(line, "#") {
			flush()
			if isReportDetailHeading(strings.TrimSpace(strings.TrimLeft(line, "#"))) {
				break
			}
			continue
		}
		if isMarkdownTableLine(line) {
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
	selected := make([]string, 0, len(blocks))
	for _, rawBlock := range blocks {
		for _, block := range splitFlattenedReportBlocks(stripFlattenedCodeBlock(rawBlock)) {
			if isLowInformationReportLead(block) {
				continue
			}
			block = stripGitTraceClauses(stripMarkdownLinkTargets(block))
			if block == "" || isLowInformationReportLead(block) || isCommandOrPathOnly(block) ||
				isPureGitOperation(block) || isInternalDelegationResult(block) ||
				isProcessNarration(block) || isReportDetailBlock(block) {
				continue
			}
			selected = append(selected, block)
		}
	}
	return strings.Trim(strings.ReplaceAll(strings.Join(selected, "\n"), "**", ""), "*` ")
}

func splitFlattenedReportBlocks(value string) []string {
	parts := strings.Split(value, " - ")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func isReportDetailHeading(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return containsAnyText(lower,
		"实现细节", "技术细节", "代码细节", "命令详情",
		"文件清单", "完整日志", "原始输出",
	)
}

func isReportDetailBlock(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	for _, prefix := range []string{
		"修改文件：", "修改文件:", "涉及文件：", "涉及文件:",
		"文件清单：", "文件清单:", "changed files:", "files changed:",
		"运行代码：", "运行代码:", "api 镜像：", "api 镜像:",
		"当前主分支：", "当前主分支:", "当前 head：", "当前 head:",
		"新 head：", "新 head:", "未推送", "not pushed",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func stripFlattenedCodeBlock(value string) string {
	for _, marker := range []string{
		" ```", " ~~~",
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
	for _, negative := range []string{"未提交", "未推送", "未部署", "未合并", "未变基"} {
		lower = strings.ReplaceAll(lower, negative, "")
	}
	if !containsAnyText(lower,
		"git ", "commit", "merge", "rebase", "cherry-pick", "提交号", "提交：", "提交 `", "分支",
		"worktree", "工作树", "已合入", "合并", "推送", "变基", "当前 head", "新 head",
	) {
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
		containsAnyText(lower, "发给", "同步给", "交给", "回传", "发送给", "提交给", "向主代理提交")
}

func stripGitTraceClauses(value string) string {
	text := strings.TrimSpace(value)
	for _, replacement := range [][2]string{
		{" commit 和", ""}, {" commit及", ""}, {" commit、", ""},
		{" Commit 和", ""}, {" Commit及", ""}, {" Commit、", ""},
	} {
		text = strings.ReplaceAll(text, replacement[0], replacement[1])
	}
	lower := strings.ToLower(text)
	cut := false
	for _, marker := range []string{
		"并已提交、推送", "并提交、推送", "提交并推送", "并已推送", "并推送",
		"，当前 head", "；当前 head", ", current head", "; current head",
		"，新 head", "；新 head", " - 新 worktree", " - worktree", " - 分支：", " - 分支:",
	} {
		if index := strings.Index(lower, marker); index >= 0 {
			text = strings.TrimSpace(text[:index])
			cut = true
			break
		}
	}
	if cut {
		text = strings.TrimRight(text, "，,；;：: ")
		runes := []rune(text)
		if len(runes) > 0 && !strings.ContainsRune("。！？!?", runes[len(runes)-1]) {
			text += "。"
		}
	}
	return strings.TrimSpace(text)
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
	observationIndexes *[]map[workEvidenceObservationIdentity]int,
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
		*observationIndexes = append(
			*observationIndexes, make(map[workEvidenceObservationIdentity]int),
		)
	}
	observationIdentity := workEvidenceObservationIdentity{
		Date: observation.Date, Category: observation.Category, Status: observation.Status,
	}
	observationIndex, exists := (*observationIndexes)[index][observationIdentity]
	if !exists {
		observation.OccurrenceCount = 1
		observationIndex = len(projection.Facts[index].Observations)
		(*observationIndexes)[index][observationIdentity] = observationIndex
		projection.Facts[index].Observations = append(
			projection.Facts[index].Observations, observation,
		)
		return
	}
	existing := &projection.Facts[index].Observations[observationIndex]
	existing.OccurrenceCount++
	if existing.FirstObservedAt == "" && existing.ObservedAt != "" &&
		observation.ObservedAt != "" && existing.ObservedAt != observation.ObservedAt {
		existing.FirstObservedAt = existing.ObservedAt
	}
	if existing.FirstObservedAt != "" && observation.ObservedAt != "" &&
		observation.ObservedAt < existing.FirstObservedAt {
		existing.FirstObservedAt = observation.ObservedAt
	}
	if existing.ObservedAt == "" ||
		(observation.ObservedAt != "" && observation.ObservedAt >= existing.ObservedAt) {
		existing.ObservedAt = observation.ObservedAt
		existing.Category = observation.Category
		existing.Status = observation.Status
	}
}
