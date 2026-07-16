package sessiondigest

import (
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var patchFilePattern = regexp.MustCompile(`(?m)^\*\*\* (?:Add|Update|Delete) File: (.+)$`)
var processExitCodePattern = regexp.MustCompile(`(?i)process exited with code\s+(-?[0-9]+)`)
var jsonExitCodePattern = regexp.MustCompile(`(?i)"exit_code"\s*:\s*(-?[0-9]+)`)
var plainExitCodePattern = regexp.MustCompile(`(?i)exit code\s+(-?[0-9]+)`)

type Extractor struct {
	digest             Digest
	seenGoals          map[string]struct{}
	seenOutcomes       map[string]struct{}
	seenFiles          map[string]struct{}
	seenBlockers       map[string]struct{}
	validationByName   map[string]int
	validationByCallID map[string]int
	fallbackOutcome    string
	sourceEventCount   int64
	includedEventCount int64
	sourceBytes        int64
	fieldWasTruncated  bool
}

func NewExtractor() *Extractor {
	return &Extractor{
		digest:             EmptyDigest(),
		seenGoals:          map[string]struct{}{},
		seenOutcomes:       map[string]struct{}{},
		seenFiles:          map[string]struct{}{},
		seenBlockers:       map[string]struct{}{},
		validationByName:   map[string]int{},
		validationByCallID: map[string]int{},
	}
}

func (e *Extractor) Consume(event Event) {
	e.sourceEventCount++
	e.sourceBytes += event.PayloadBytes
	root := decodeObject(event.Payload)
	payload := objectValue(root["payload"])
	contributed := false

	switch event.EventType {
	case "event_msg.user_message":
		contributed = e.addGoal(stringValue(payload["message"])) || contributed
	case "user":
		for _, text := range messageTexts(objectValue(root["message"]), "text") {
			contributed = e.addGoal(text) || contributed
		}
		contributed = e.consumeClaudeToolResults(objectValue(root["message"])) || contributed
	case "response_item.message":
		role := strings.ToLower(stringValue(payload["role"]))
		phase := strings.ToLower(stringValue(payload["phase"]))
		texts := contentTexts(payload["content"])
		if role == "user" {
			for _, text := range texts {
				contributed = e.addGoal(text) || contributed
			}
		} else if phase == "final_answer" {
			for _, text := range texts {
				contributed = e.addOutcome(text) || contributed
			}
		}
	case "event_msg.task_complete":
		contributed = e.addOutcome(stringValue(payload["last_agent_message"])) || contributed
	case "event_msg.agent_message":
		message := stringValue(payload["message"])
		if strings.EqualFold(stringValue(payload["phase"]), "final_answer") {
			contributed = e.addOutcome(message) || contributed
		} else if cleanEvidence(message) != "" {
			e.fallbackOutcome = message
		}
	case "assistant":
		texts, files, calls := claudeAssistantEvidence(objectValue(root["message"]))
		for _, text := range texts {
			if cleanEvidence(text) != "" {
				e.fallbackOutcome = text
			}
		}
		for _, file := range files {
			contributed = e.addFile(file) || contributed
		}
		for _, call := range calls {
			if e.addValidation(call.ID, call.Command) {
				contributed = true
			}
		}
	case "response_item.custom_tool_call":
		if strings.EqualFold(stringValue(payload["name"]), "apply_patch") {
			for _, match := range patchFilePattern.FindAllStringSubmatch(stringValue(payload["input"]), -1) {
				contributed = e.addFile(match[1]) || contributed
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
			contributed = e.addFile(file) || contributed
		}
	case "response_item.function_call":
		arguments := decodeObject(json.RawMessage(stringValue(payload["arguments"])))
		command := stringValue(arguments["cmd"])
		if command == "" {
			command = stringValue(arguments["command"])
		}
		contributed = e.addValidation(stringValue(payload["call_id"]), command) || contributed
	case "response_item.function_call_output", "response_item.custom_tool_call_output":
		contributed = e.completeValidation(stringValue(payload["call_id"]), stringValue(payload["output"])) || contributed
	}

	if contributed {
		e.includedEventCount++
	}
}

func (e *Extractor) Result(itemMaxBytes int) (Digest, int64, int64, int64, bool, []byte) {
	if len(e.digest.Outcomes) == 0 {
		e.addOutcome(e.fallbackOutcome)
	}
	for _, outcome := range e.digest.Outcomes {
		if blockerText(outcome) {
			e.addBlocker(outcome)
		}
	}
	digest, encoded, truncated := EnforceItemBudget(e.digest, itemMaxBytes)
	return digest, e.sourceEventCount, e.includedEventCount,
		e.sourceEventCount - e.includedEventCount, truncated || e.fieldWasTruncated, encoded
}

func (e *Extractor) SourceBytes() int64 { return e.sourceBytes }

func (e *Extractor) addGoal(value string) bool {
	value, truncated := normalizeEvidence(value, 384)
	if value == "" || isNoiseGoal(value) {
		return false
	}
	e.fieldWasTruncated = e.fieldWasTruncated || truncated
	return addUniqueString(&e.digest.Goals, e.seenGoals, value)
}

func (e *Extractor) addOutcome(value string) bool {
	value, truncated := normalizeEvidence(value, 512)
	if value == "" {
		return false
	}
	e.fieldWasTruncated = e.fieldWasTruncated || truncated
	return addUniqueString(&e.digest.Outcomes, e.seenOutcomes, value)
}

func (e *Extractor) addBlocker(value string) bool {
	value, truncated := normalizeEvidence(value, 384)
	if value == "" {
		return false
	}
	e.fieldWasTruncated = e.fieldWasTruncated || truncated
	return addUniqueString(&e.digest.Blockers, e.seenBlockers, value)
}

func (e *Extractor) addFile(value string) bool {
	value = normalizeFilePath(value)
	if value == "" {
		return false
	}
	return addUniqueString(&e.digest.FilesChanged, e.seenFiles, value)
}

func (e *Extractor) addValidation(callID, command string) bool {
	name := validationCommandName(command)
	if name == "" {
		return false
	}
	index, exists := e.validationByName[name]
	if !exists {
		index = len(e.digest.Validations)
		e.validationByName[name] = index
		e.digest.Validations = append(e.digest.Validations, Validation{Name: name, Status: "unknown"})
	}
	if callID != "" {
		e.validationByCallID[callID] = index
	}
	return true
}

func (e *Extractor) completeValidation(callID, output string) bool {
	index, ok := e.validationByCallID[callID]
	if !ok || index < 0 || index >= len(e.digest.Validations) {
		return false
	}
	status := validationStatus(output)
	e.digest.Validations[index].Status = status
	switch status {
	case "passed":
		e.digest.Validations[index].Summary = "命令执行成功"
	case "failed":
		e.digest.Validations[index].Summary = "命令执行失败"
	default:
		e.digest.Validations[index].Summary = "结果状态无法可靠判断"
	}
	return true
}

func (e *Extractor) consumeClaudeToolResults(message map[string]any) bool {
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
		contributed = e.completeValidation(stringValue(block["tool_use_id"]), content) || contributed
	}
	return contributed
}

type claudeCall struct {
	ID      string
	Command string
}

func claudeAssistantEvidence(message map[string]any) ([]string, []string, []claudeCall) {
	texts := []string{}
	files := []string{}
	calls := []claudeCall{}
	for _, item := range arrayValue(message["content"]) {
		block := objectValue(item)
		switch stringValue(block["type"]) {
		case "text":
			texts = append(texts, stringValue(block["text"]))
		case "tool_use":
			input := objectValue(block["input"])
			name := stringValue(block["name"])
			if name == "Edit" || name == "Write" || name == "MultiEdit" || name == "NotebookEdit" {
				files = append(files, stringValue(input["file_path"]))
			}
			if name == "Bash" {
				calls = append(calls, claudeCall{ID: stringValue(block["id"]), Command: stringValue(input["command"])})
			}
		}
	}
	return texts, files, calls
}

func messageTexts(message map[string]any, requiredType string) []string {
	texts := []string{}
	for _, item := range arrayValue(message["content"]) {
		block := objectValue(item)
		if requiredType != "" && stringValue(block["type"]) != requiredType {
			continue
		}
		if text := stringValue(block["text"]); text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

func contentTexts(value any) []string {
	if text := stringValue(value); text != "" {
		var decoded any
		if (strings.HasPrefix(strings.TrimSpace(text), "[") || strings.HasPrefix(strings.TrimSpace(text), "{")) &&
			json.Unmarshal([]byte(text), &decoded) == nil {
			if nested := contentTexts(decoded); len(nested) > 0 {
				return nested
			}
		}
		return []string{text}
	}
	if object := objectValue(value); len(object) > 0 {
		for _, key := range []string{"text", "output_text", "input_text"} {
			if text := stringValue(object[key]); text != "" {
				return []string{text}
			}
		}
	}
	texts := []string{}
	for _, item := range arrayValue(value) {
		block := objectValue(item)
		for _, key := range []string{"text", "output_text", "input_text"} {
			if text := stringValue(block[key]); text != "" {
				texts = append(texts, text)
				break
			}
		}
	}
	return texts
}

func decodeObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return map[string]any{}
	}
	return value
}

func objectValue(value any) map[string]any {
	result, _ := value.(map[string]any)
	if result == nil {
		return map[string]any{}
	}
	return result
}

func arrayValue(value any) []any {
	result, _ := value.([]any)
	return result
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func addUniqueString(target *[]string, seen map[string]struct{}, value string) bool {
	key := strings.ToLower(strings.TrimSpace(value))
	if key == "" {
		return false
	}
	if _, exists := seen[key]; exists {
		return false
	}
	seen[key] = struct{}{}
	*target = append(*target, value)
	return true
}

func normalizeEvidence(value string, maxBytes int) (string, bool) {
	value = cleanEvidence(Redact(value))
	if value == "" {
		return "", false
	}
	return truncateUTF8Bytes(value, maxBytes)
}

func cleanEvidence(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func isNoiseGoal(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "<environment_context>") ||
		strings.HasPrefix(lower, "<permissions instructions>") ||
		strings.HasPrefix(lower, "# repository guidelines") ||
		strings.HasPrefix(lower, "you are codex")
}

func blockerText(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"阻塞", "未完成", "无法继续", "blocked", "not completed", "cannot continue"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func normalizeFilePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(Redact(value), "\\", "/"))
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	value = strings.Trim(value, "\"'` ")
	if value == "" || strings.Contains(value, "[REDACTED]") {
		return ""
	}
	for _, marker := range []string{"/project_manager/", "/workspace/"} {
		if index := strings.LastIndex(value, marker); index >= 0 {
			value = value[index+len(marker):]
			break
		}
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	if strings.HasPrefix(cleaned, "/") {
		parts := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
		if len(parts) > 2 && parts[0] == "home" {
			parts = parts[2:]
		}
		if len(parts) > 4 {
			parts = parts[len(parts)-4:]
		}
		cleaned = strings.Join(parts, "/")
	}
	cleaned, _ = truncateUTF8Bytes(cleaned, 256)
	return cleaned
}

func validationCommandName(command string) string {
	lower := strings.ToLower(command)
	patterns := []struct {
		needle string
		name   string
	}{
		{"go test", "go test"}, {"go vet", "go vet"}, {"golangci-lint", "golangci-lint"},
		{"pnpm typecheck", "pnpm typecheck"}, {"pnpm lint", "pnpm lint"}, {"pnpm build", "pnpm build"},
		{"pnpm test", "pnpm test"}, {"npm test", "npm test"}, {"npm run build", "npm run build"},
		{"pytest", "pytest"}, {"cargo test", "cargo test"}, {"make test", "make test"},
	}
	for _, candidate := range patterns {
		if strings.Contains(lower, candidate.needle) {
			return candidate.name
		}
	}
	return ""
}

func validationStatus(output string) string {
	lower := strings.ToLower(output)
	for _, pattern := range []*regexp.Regexp{processExitCodePattern, jsonExitCodePattern, plainExitCodePattern} {
		matches := pattern.FindAllStringSubmatch(output, -1)
		if len(matches) == 0 {
			continue
		}
		code, err := strconv.Atoi(matches[len(matches)-1][1])
		if err == nil && code == 0 {
			return "passed"
		}
		if err == nil {
			return "failed"
		}
	}
	for _, marker := range []string{"tests failed", "test failed"} {
		if strings.Contains(lower, marker) {
			return "failed"
		}
	}
	for _, marker := range []string{"script completed"} {
		if strings.Contains(lower, marker) {
			return "passed"
		}
	}
	return "unknown"
}
