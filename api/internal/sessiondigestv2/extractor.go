package sessiondigestv2

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
)

var patchFilePattern = regexp.MustCompile(`(?m)^\*\*\* (Add|Update|Delete) File: (.+)$`)

type pendingCall struct {
	UnitIndex int
	Command   string
	Family    string
	Kind      string
}

type workUnitBuilder struct {
	unit             WorkUnit
	startCursor      int64
	endCursor        int64
	lastAgentText    string
	seenClaims       map[string]struct{}
	seenChanges      map[string]struct{}
	validationByName map[string]int
	eventCount       int64
}

type Extractor struct {
	units              []workUnitBuilder
	currentUnit        int
	pendingCalls       map[string]pendingCall
	lastGoalKey        string
	lastGoalCursor     int64
	lastGoalTime       time.Time
	sourceEventCount   int64
	includedEventCount int64
	sourceBytes        int64
	ignoreSession      bool
}

func NewExtractor() *Extractor {
	return &Extractor{
		currentUnit:  -1,
		pendingCalls: map[string]pendingCall{},
	}
}

func (e *Extractor) Consume(event Event) {
	e.sourceEventCount++
	e.sourceBytes += event.PayloadBytes
	if e.ignoreSession {
		return
	}
	root := decodeObject(event.Payload)
	payload := objectValue(root["payload"])
	contributed := false

	switch event.EventType {
	case "event_msg.user_message":
		contributed = e.consumeGoal(event, stringValue(payload["message"])) || contributed
	case "response_item.message":
		role := strings.ToLower(stringValue(payload["role"]))
		phase := strings.ToLower(stringValue(payload["phase"]))
		texts := contentTexts(payload["content"])
		if role == "user" {
			for _, text := range texts {
				contributed = e.consumeGoal(event, text) || contributed
			}
		} else if role == "assistant" && phase == "final_answer" {
			for _, text := range texts {
				contributed = e.addAgentClaim(event, text) || contributed
			}
		}
	case "user":
		message := objectValue(root["message"])
		for _, text := range messageTexts(message) {
			contributed = e.consumeGoal(event, text) || contributed
		}
		contributed = e.consumeClaudeToolResults(event, message) || contributed
	case "event_msg.task_complete":
		contributed = e.addAgentClaim(event, stringValue(payload["last_agent_message"])) || contributed
	case "event_msg.agent_message":
		text := stringValue(payload["message"])
		if strings.EqualFold(stringValue(payload["phase"]), "final_answer") {
			contributed = e.addAgentClaim(event, text) || contributed
		} else {
			e.rememberAgentText(event, text)
		}
	case "assistant":
		contributed = e.consumeClaudeAssistant(event, objectValue(root["message"])) || contributed
	case "response_item.custom_tool_call":
		if strings.EqualFold(stringValue(payload["name"]), "apply_patch") {
			for _, match := range patchFilePattern.FindAllStringSubmatch(stringValue(payload["input"]), -1) {
				contributed = e.addFileChange(event, strings.ToLower(match[1]), match[2]) || contributed
			}
		}
	case "event_msg.patch_apply_end":
		changes := objectValue(payload["changes"])
		paths := make([]string, 0, len(changes))
		for file := range changes {
			paths = append(paths, file)
		}
		sort.Strings(paths)
		for _, file := range paths {
			contributed = e.addFileChange(event, "update", file) || contributed
		}
	case "response_item.function_call":
		arguments := decodeObject(json.RawMessage(stringValue(payload["arguments"])))
		command := stringValue(arguments["cmd"])
		if command == "" {
			command = stringValue(arguments["command"])
		}
		contributed = e.addCommandCall(event, stringValue(payload["call_id"]), command) || contributed
	case "response_item.function_call_output", "response_item.custom_tool_call_output":
		contributed = e.completeCommand(event, stringValue(payload["call_id"]), stringValue(payload["output"])) || contributed
	}

	if contributed {
		e.includedEventCount++
		if e.currentUnit >= 0 {
			e.units[e.currentUnit].eventCount++
		}
	}
}

func (e *Extractor) Result() (Digest, int64, int64, int64, bool, []byte) {
	e.finishPendingCalls()
	units := make([]WorkUnit, 0, len(e.units))
	for index := range e.units {
		e.finalizeUnit(index)
		units = append(units, e.units[index].unit)
	}
	digest := EmptyDigest()
	digest.WorkUnits = units
	digest.DailySummaries = BuildDailySummaries(units, defaultBusinessLocation)
	digest.Coverage = Coverage{
		SourceEventCount:        e.sourceEventCount,
		IncludedEventCount:      e.includedEventCount,
		OmittedEventCount:       e.sourceEventCount - e.includedEventCount,
		SourceWorkUnitCount:     len(units),
		DetailedWorkUnitCount:   len(units),
		AggregatedWorkUnitCount: 0,
		Truncated:               false,
		Representation:          "result_focused",
	}
	recalculateSummary(&digest)
	encoded, _ := json.Marshal(digest)
	return digest, e.sourceEventCount, e.includedEventCount,
		e.sourceEventCount - e.includedEventCount, false, encoded
}

func (e *Extractor) SourceBytes() int64 { return e.sourceBytes }

func (e *Extractor) consumeGoal(event Event, raw string) bool {
	if e.ignoreSession {
		return false
	}
	if isApprovalAssessmentHeader(raw) {
		e.ignoreSession = true
		e.units = nil
		e.currentUnit = -1
		e.pendingCalls = map[string]pendingCall{}
		e.includedEventCount = 0
		return false
	}
	for _, text := range contentTexts(raw) {
		normalized := normalizeText(text)
		if normalized == "" || isNoiseGoal(normalized) {
			continue
		}
		if isUserCompletionConfirmation(normalized) && e.currentUnit >= 0 {
			return e.addUserCompletionConfirmation(event, normalized)
		}
		key := canonicalKey(normalized)
		if key == e.lastGoalKey && duplicateGoalEvent(event, e.lastGoalCursor, e.lastGoalTime) {
			return false
		}
		e.finalizeCurrentUnit()
		ref := stableRef("wu", event, key)
		unit := WorkUnit{
			WorkUnitRef:      ref,
			Sequence:         len(e.units) + 1,
			ActivityStartAt:  formatTime(event.OccurredAt),
			ActivityEndAt:    formatTime(event.OccurredAt),
			PeriodRelation:   "unknown",
			Goal:             Goal{Text: normalized, Source: "user_message"},
			Category:         "discussion",
			Status:           "pending",
			EvidenceGrade:    "D",
			ResultStatements: []ResultStatement{},
			AgentClaims:      []AgentClaim{},
			Evidence:         []Evidence{},
			Changes:          []Change{},
			Validations:      []Validation{},
			Unresolved:       []Unresolved{},
		}
		e.units = append(e.units, workUnitBuilder{
			unit:             unit,
			startCursor:      event.StartCursor,
			endCursor:        event.EndCursor,
			seenClaims:       map[string]struct{}{},
			seenChanges:      map[string]struct{}{},
			validationByName: map[string]int{},
		})
		e.currentUnit = len(e.units) - 1
		e.lastGoalKey = key
		e.lastGoalCursor = event.EndCursor
		e.lastGoalTime = event.OccurredAt
		return true
	}
	return false
}

func isUserCompletionConfirmation(value string) bool {
	value = strings.ToLower(strings.Trim(strings.TrimSpace(value), "。！？!? "))
	switch value {
	case "ok", "agree", "agreed", "同意", "确认", "验收通过", "测试通过":
		return true
	default:
		return false
	}
}

func (e *Extractor) addUserCompletionConfirmation(event Event, value string) bool {
	index := e.currentUnit
	if index < 0 || index >= len(e.units) {
		return false
	}
	e.touchUnit(index, event)
	ref := stableRef("user_confirmation", event, canonicalKey(value))
	for _, evidence := range e.units[index].unit.Evidence {
		if evidence.Ref == ref {
			return false
		}
	}
	e.units[index].unit.Evidence = append(e.units[index].unit.Evidence, Evidence{
		Ref: ref, Kind: "user_confirmation", Status: "confirmed",
		Summary: "用户确认完成",
	})
	return true
}

func duplicateGoalEvent(event Event, lastCursor int64, lastTime time.Time) bool {
	cursorDelta := event.StartCursor - lastCursor
	if cursorDelta < 0 {
		cursorDelta = -cursorDelta
	}
	if cursorDelta <= 64<<10 {
		return true
	}
	if !event.OccurredAt.IsZero() && !lastTime.IsZero() {
		delta := event.OccurredAt.Sub(lastTime)
		if delta < 0 {
			delta = -delta
		}
		return delta <= 3*time.Second
	}
	return false
}

func (e *Extractor) ensureUnit(event Event) int {
	if e.currentUnit >= 0 {
		e.touchUnit(e.currentUnit, event)
		return e.currentUnit
	}
	ref := stableRef("wu", event, "unattributed")
	e.units = append(e.units, workUnitBuilder{
		unit: WorkUnit{
			WorkUnitRef:      ref,
			Sequence:         len(e.units) + 1,
			ActivityStartAt:  formatTime(event.OccurredAt),
			ActivityEndAt:    formatTime(event.OccurredAt),
			PeriodRelation:   "unknown",
			Goal:             Goal{Text: "未识别的历史工作单元", Source: "derived"},
			Category:         "administrative",
			Status:           "unknown",
			EvidenceGrade:    "D",
			ResultStatements: []ResultStatement{},
			AgentClaims:      []AgentClaim{},
			Evidence:         []Evidence{},
			Changes:          []Change{},
			Validations:      []Validation{},
			Unresolved:       []Unresolved{},
		},
		startCursor:      event.StartCursor,
		endCursor:        event.EndCursor,
		seenClaims:       map[string]struct{}{},
		seenChanges:      map[string]struct{}{},
		validationByName: map[string]int{},
	})
	e.currentUnit = len(e.units) - 1
	return e.currentUnit
}

func (e *Extractor) touchUnit(index int, event Event) {
	if index < 0 || index >= len(e.units) {
		return
	}
	unit := &e.units[index]
	if event.EndCursor > unit.endCursor {
		unit.endCursor = event.EndCursor
	}
	if !event.OccurredAt.IsZero() {
		if unit.unit.ActivityStartAt == "" {
			unit.unit.ActivityStartAt = formatTime(event.OccurredAt)
		}
		unit.unit.ActivityEndAt = formatTime(event.OccurredAt)
	}
}

func (e *Extractor) addAgentClaim(event Event, raw string) bool {
	text := normalizeText(raw)
	if text == "" || isNoiseGoal(text) || isNoiseAgentClaim(text) {
		return false
	}
	if e.currentUnit < 0 {
		return false
	}
	index := e.currentUnit
	e.touchUnit(index, event)
	unit := &e.units[index]
	key := canonicalKey(text)
	if _, exists := unit.seenClaims[key]; exists {
		return false
	}
	unit.seenClaims[key] = struct{}{}
	unit.unit.AgentClaims = append(unit.unit.AgentClaims, AgentClaim{
		Text:    text,
		Support: "unsupported",
	})
	unit.lastAgentText = text
	return true
}

func (e *Extractor) rememberAgentText(event Event, raw string) {
	text := normalizeText(raw)
	if text == "" || isNoiseAgentClaim(text) || e.currentUnit < 0 {
		return
	}
	index := e.currentUnit
	e.touchUnit(index, event)
	e.units[index].lastAgentText = text
}

func (e *Extractor) consumeClaudeAssistant(event Event, message map[string]any) bool {
	contributed := false
	for _, item := range arrayValue(message["content"]) {
		block := objectValue(item)
		switch stringValue(block["type"]) {
		case "text":
			e.rememberAgentText(event, stringValue(block["text"]))
		case "tool_use":
			input := objectValue(block["input"])
			name := strings.ToLower(stringValue(block["name"]))
			switch name {
			case "edit", "write", "multiedit", "notebookedit":
				contributed = e.addFileChange(event, "update", stringValue(input["file_path"])) || contributed
			case "bash":
				contributed = e.addCommandCall(event, stringValue(block["id"]), stringValue(input["command"])) || contributed
			}
		}
	}
	return contributed
}

func (e *Extractor) consumeClaudeToolResults(event Event, message map[string]any) bool {
	contributed := false
	for _, item := range arrayValue(message["content"]) {
		block := objectValue(item)
		if stringValue(block["type"]) != "tool_result" {
			continue
		}
		content := stringValue(block["content"])
		if content == "" {
			content = strings.Join(contentTexts(block["content"]), " ")
		}
		contributed = e.completeCommand(event, stringValue(block["tool_use_id"]), content) || contributed
	}
	return contributed
}

func (e *Extractor) addFileChange(event Event, operation, rawPath string) bool {
	file := normalizeFilePath(rawPath)
	if file == "" {
		return false
	}
	index := e.ensureUnit(event)
	unit := &e.units[index]
	key := operation + ":" + strings.ToLower(file)
	if _, exists := unit.seenChanges[key]; exists {
		return false
	}
	unit.seenChanges[key] = struct{}{}
	ref := stableRef("file_change", event, key)
	unit.unit.Changes = append(unit.unit.Changes, Change{
		Path:        file,
		Operation:   operation,
		EvidenceRef: ref,
	})
	unit.unit.Evidence = append(unit.unit.Evidence, Evidence{
		Ref:     ref,
		Kind:    "file_change",
		Status:  "changed",
		Summary: operationText(operation) + " " + file,
	})
	return true
}

func (e *Extractor) addCommandCall(event Event, callID, command string) bool {
	family, kind := classifyCommand(command)
	if family == "" || strings.TrimSpace(callID) == "" {
		return false
	}
	index := e.ensureUnit(event)
	e.pendingCalls[callID] = pendingCall{
		UnitIndex: index,
		Command:   command,
		Family:    family,
		Kind:      kind,
	}
	return true
}

func (e *Extractor) completeCommand(event Event, callID, output string) bool {
	call, exists := e.pendingCalls[callID]
	if !exists || call.UnitIndex < 0 || call.UnitIndex >= len(e.units) {
		return false
	}
	delete(e.pendingCalls, callID)
	reduced := ReduceCommandOutput(call.Command, output)
	if !reduced.Recognized {
		reduced.CommandFamily = call.Family
		reduced.Kind = call.Kind
	}
	unit := &e.units[call.UnitIndex]
	e.touchUnit(call.UnitIndex, event)
	ref := stableRef(reduced.Kind, event, callID+":"+call.Family)
	summary := normalizeText(reduced.Summary)
	unit.unit.Evidence = append(unit.unit.Evidence, Evidence{
		Ref:           ref,
		Kind:          reduced.Kind,
		Status:        reduced.Status,
		Summary:       summary,
		CommandFamily: call.Family,
		ExitCode:      reduced.ExitCode,
	})
	if call.Kind == "validation" {
		e.addValidationAttempt(unit, call.Family, reduced.Status, event.OccurredAt, summary, ref)
	}
	return true
}

func (e *Extractor) addValidationAttempt(
	unit *workUnitBuilder,
	name, status string,
	occurredAt time.Time,
	summary, evidenceRef string,
) {
	index, exists := unit.validationByName[name]
	if !exists {
		index = len(unit.unit.Validations)
		unit.validationByName[name] = index
		unit.unit.Validations = append(unit.unit.Validations, Validation{
			Name:       name,
			LastStatus: "unknown",
		})
	}
	validation := &unit.unit.Validations[index]
	validation.Attempts++
	validation.LastStatus = status
	validation.LastOccurredAt = formatTime(occurredAt)
	validation.Summary = summary
	validation.EvidenceRef = evidenceRef
}

func (e *Extractor) finishPendingCalls() {
	callIDs := make([]string, 0, len(e.pendingCalls))
	for callID := range e.pendingCalls {
		callIDs = append(callIDs, callID)
	}
	sort.Strings(callIDs)
	for _, callID := range callIDs {
		call := e.pendingCalls[callID]
		if call.UnitIndex < 0 || call.UnitIndex >= len(e.units) || call.Kind != "validation" {
			continue
		}
		unit := &e.units[call.UnitIndex]
		event := Event{
			StartCursor: unit.startCursor,
			EndCursor:   unit.endCursor,
			EventType:   "pending_command",
			ContentSHA:  HashBytes([]byte(callID + ":" + call.Command)),
		}
		ref := stableRef("validation", event, callID)
		unit.unit.Evidence = append(unit.unit.Evidence, Evidence{
			Ref:           ref,
			Kind:          "validation",
			Status:        "unknown",
			Summary:       "命令结果缺失",
			CommandFamily: call.Family,
		})
		e.addValidationAttempt(unit, call.Family, "unknown", time.Time{}, "命令结果缺失", ref)
	}
	e.pendingCalls = map[string]pendingCall{}
}

func (e *Extractor) finalizeCurrentUnit() {
	if e.currentUnit >= 0 {
		e.finalizeUnit(e.currentUnit)
	}
}

func (e *Extractor) finalizeUnit(index int) {
	if index < 0 || index >= len(e.units) {
		return
	}
	builder := &e.units[index]
	unit := &builder.unit
	if len(unit.AgentClaims) == 0 && builder.lastAgentText != "" {
		key := canonicalKey(builder.lastAgentText)
		if _, exists := builder.seenClaims[key]; !exists {
			builder.seenClaims[key] = struct{}{}
			unit.AgentClaims = append(unit.AgentClaims, AgentClaim{
				Text:    builder.lastAgentText,
				Support: "unsupported",
			})
		}
	}
	resolveUnit(unit)
	for claimIndex := range unit.AgentClaims {
		if unit.EvidenceGrade == "A" || unit.EvidenceGrade == "B" {
			unit.AgentClaims[claimIndex].Support = "partially_supported"
		} else {
			unit.AgentClaims[claimIndex].Support = "unsupported"
		}
	}
	buildResultStatements(unit, map[string]struct{}{})
}

func resolveUnit(unit *WorkUnit) {
	hasChanges := len(unit.Changes) > 0
	hasPassed := false
	hasFailed := false
	hasUnknownValidation := false
	hasConfirmation := false
	for _, evidence := range unit.Evidence {
		if evidence.Kind == "user_confirmation" && evidence.Status == "confirmed" {
			hasConfirmation = true
			break
		}
	}
	for _, validation := range unit.Validations {
		switch validation.LastStatus {
		case "passed":
			hasPassed = true
		case "failed":
			hasFailed = true
		default:
			hasUnknownValidation = true
		}
	}
	switch {
	case hasFailed && (hasChanges || len(unit.AgentClaims) > 0 || hasConfirmation):
		unit.Status = "partial"
	case hasFailed:
		unit.Status = "failed"
	case hasIncompleteAgentClaim(unit.AgentClaims):
		unit.Status = "partial"
	case hasChanges || hasPassed || hasConfirmation:
		unit.Status = "completed"
	case hasUnknownValidation:
		unit.Status = "unknown"
	case len(unit.AgentClaims) > 0:
		unit.Status = "unknown"
	default:
		unit.Status = "pending"
	}
	switch {
	case hasPassed:
		unit.EvidenceGrade = "A"
	case hasChanges || hasConfirmation:
		unit.EvidenceGrade = "B"
	case len(unit.AgentClaims) > 0:
		unit.EvidenceGrade = "C"
	default:
		unit.EvidenceGrade = "D"
	}
	switch {
	case hasChanges && allMarkdownChanges(unit.Changes):
		unit.Category = "document"
	case hasChanges:
		unit.Category = "implementation"
	case len(unit.Validations) > 0:
		unit.Category = "verification"
	case len(unit.AgentClaims) > 0:
		unit.Category = "discussion"
	default:
		unit.Category = "discussion"
	}
	refineUnitCategory(unit)
	unit.Unresolved = unit.Unresolved[:0]
	for _, validation := range unit.Validations {
		if validation.LastStatus == "failed" {
			unit.Unresolved = append(unit.Unresolved, Unresolved{
				Text:        validation.Name + " 验证失败",
				EvidenceRef: validation.EvidenceRef,
			})
		}
	}
}

func hasIncompleteAgentClaim(claims []AgentClaim) bool {
	for _, claim := range claims {
		lower := strings.ToLower(claim.Text)
		for _, marker := range []string{
			"尚未", "仍在", "仍需", "仍停在", "正在", "卡点",
			"先只", "随后再", "下一步继续", "下一步开始",
			"接下来开始", "尚待", "还未", "待完成", "等待完成",
			"not yet", "still running", "still needs", "in progress",
		} {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}
	return false
}

func buildResultStatements(unit *WorkUnit, seen map[string]struct{}) {
	unit.ResultStatements = unit.ResultStatements[:0]
	for claimIndex := len(unit.AgentClaims) - 1; claimIndex >= 0; claimIndex-- {
		claim := unit.AgentClaims[claimIndex]
		if claim.Support != "partially_supported" {
			continue
		}
		resultText := resultFocusedClaim(claim.Text)
		if resultText == "" {
			continue
		}
		refs := make([]string, 0, min(len(unit.Evidence), 8))
		for _, evidence := range unit.Evidence {
			if evidence.Status == "changed" || evidence.Status == "passed" ||
				evidence.Status == "confirmed" {
				refs = append(refs, evidence.Ref)
				if len(refs) == 8 {
					break
				}
			}
		}
		if len(refs) > 0 {
			addResultStatementWithSource(
				unit, seen, resultText, refs, "agent_claim_with_evidence",
			)
		}
		break
	}
	if len(unit.ResultStatements) == 0 {
		addGoalBackedResult(unit, seen)
	}
	if len(unit.ResultStatements) == 0 {
		addAgentClaimResult(unit, seen)
	}
}

func addAgentClaimResult(unit *WorkUnit, seen map[string]struct{}) {
	for claimIndex := len(unit.AgentClaims) - 1; claimIndex >= 0; claimIndex-- {
		text := resultFocusedClaim(unit.AgentClaims[claimIndex].Text)
		if text == "" {
			continue
		}
		addResultStatementWithSource(unit, seen, text, nil, "agent_claim")
		return
	}
}

func addGoalBackedResult(unit *WorkUnit, seen map[string]struct{}) {
	if unit.EvidenceGrade != "A" && unit.EvidenceGrade != "B" {
		return
	}
	if unit.Status != "completed" && unit.Status != "partial" {
		return
	}
	goal := resultFocusedClaim(unit.Goal.Text)
	if isLowInformationGoal(goal) {
		return
	}
	refs := make([]string, 0, min(len(unit.Evidence), 8))
	for _, evidence := range unit.Evidence {
		if evidence.Status != "changed" && evidence.Status != "passed" &&
			evidence.Status != "confirmed" {
			continue
		}
		refs = append(refs, evidence.Ref)
		if len(refs) == 8 {
			break
		}
	}
	if len(refs) == 0 {
		return
	}
	prefix := "已完成："
	if unit.Status == "partial" {
		prefix = "已完成部分工作："
	}
	addResultStatement(unit, seen, prefix+goal, refs)
}

func refineUnitCategory(unit *WorkUnit) {
	goal := strings.ToLower(strings.TrimSpace(unit.Goal.Text))
	if containsAny(goal,
		"还没有发布", "尚未发布", "还在开发中", "仍在开发中",
		"没有所谓", "不需要兼容", "不是兼容问题", "不是上线问题",
	) {
		unit.Category = "decision"
		return
	}
	if containsAny(goal,
		"可以开始", "能开始", "是否可以开始", "有没有冲突",
		"可行性验证", "账号在哪里", "不知道在哪里",
		"安装命令", "服务令牌", "部署凭据",
	) {
		unit.Category = "administrative"
		return
	}
	if containsAny(goal,
		"人工测试", "真实流程测试", "按照真实流程测试",
		"走完流程", "跑完全部流程", "完整流程测试",
		"验收结果", "测试了吗",
	) {
		unit.Category = "verification"
		return
	}
	if containsAny(goal,
		"调研", "借鉴", "分析", "评估", "复盘", "为什么", "原因是什么",
	) {
		unit.Category = "investigation"
		return
	}
	if containsAny(goal, "部署", "发布", "上线", "重启", "更新环境") &&
		(unit.EvidenceGrade == "A" || unit.EvidenceGrade == "B") {
		unit.Category = "deployment"
	}
}

func isLowInformationGoal(value string) bool {
	value = strings.TrimSpace(strings.Trim(value, "。！？!? "))
	if value == "" {
		return true
	}
	lower := strings.ToLower(value)
	for _, prefix := range []string{
		"开始吧", "那你做", "速度处理", "保留并继续",
		"帮我走完流程", "可以，帮我走完流程",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	if len([]rune(value)) <= 12 && containsAny(strings.ToLower(value),
		"开始吧", "那你做", "做啊", "可以", "继续", "处理吧", "执行吧",
	) {
		return true
	}
	return false
}

func containsAny(value string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func addResultStatement(unit *WorkUnit, seen map[string]struct{}, text string, refs []string) {
	addResultStatementWithSource(unit, seen, text, refs, "derived_evidence")
}

func addResultStatementWithSource(
	unit *WorkUnit,
	seen map[string]struct{},
	text string,
	refs []string,
	source string,
) {
	text = normalizeText(text)
	key := canonicalKey(text)
	if key == "" {
		return
	}
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}
	unit.ResultStatements = append(unit.ResultStatements, ResultStatement{
		Text:         text,
		Source:       source,
		EvidenceRefs: refs,
	})
}

func recalculateSummary(digest *Digest) {
	digest.SessionSummary = SessionSummary{}
	for _, unit := range digest.WorkUnits {
		digest.SessionSummary.PrimaryResultCount += len(unit.ResultStatements)
		if unit.EvidenceGrade == "A" {
			for _, result := range unit.ResultStatements {
				if result.Source == "derived_evidence" {
					digest.SessionSummary.VerifiedResultCount++
				}
			}
		}
		digest.SessionSummary.UnresolvedCount += len(unit.Unresolved)
		switch unit.Status {
		case "completed":
			digest.SessionSummary.StatusCounts.Completed++
		case "partial":
			digest.SessionSummary.StatusCounts.Partial++
		case "blocked":
			digest.SessionSummary.StatusCounts.Blocked++
		case "failed":
			digest.SessionSummary.StatusCounts.Failed++
		case "pending":
			digest.SessionSummary.StatusCounts.Pending++
		default:
			digest.SessionSummary.StatusCounts.Unknown++
		}
	}
}

func stableRef(kind string, event Event, suffix string) string {
	contentHash := strings.TrimSpace(event.ContentSHA)
	if contentHash == "" {
		sum := sha256.Sum256(event.Payload)
		contentHash = hex.EncodeToString(sum[:])
	}
	raw := Version + "\n" + kind + "\n" + strconvItoa64(event.StartCursor) + "\n" +
		strconvItoa64(event.EndCursor) + "\n" + event.EventType + "\n" + contentHash + "\n" + suffix
	return refPrefix(kind) + ":" + HashBytes([]byte(raw))[:16]
}

func refPrefix(kind string) string {
	switch kind {
	case "wu":
		return "wu"
	case "file_change":
		return "ev-file"
	case "validation":
		return "ev-test"
	case "commit":
		return "ev-commit"
	case "runtime_change", "api_check":
		return "ev-runtime"
	default:
		return "ev"
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func operationText(operation string) string {
	switch strings.ToLower(operation) {
	case "add":
		return "新增"
	case "delete":
		return "删除"
	default:
		return "修改"
	}
}

func statusText(status string) string {
	switch status {
	case "passed":
		return "通过"
	case "failed":
		return "失败"
	default:
		return "待确认"
	}
}

func allMarkdownChanges(changes []Change) bool {
	if len(changes) == 0 {
		return false
	}
	for _, change := range changes {
		lower := strings.ToLower(change.Path)
		if !strings.HasSuffix(lower, ".md") && !strings.HasSuffix(lower, ".mdx") {
			return false
		}
	}
	return true
}

func strconvItoa(value int) string {
	return strconvItoa64(int64(value))
}

func strconvItoa64(value int64) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var buffer [32]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		index--
		buffer[index] = '-'
	}
	return string(buffer[index:])
}
