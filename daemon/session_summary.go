package main

import (
	"encoding/json"
	"strings"
	"time"
	"unicode"
)

type summaryCandidate struct {
	Text   string
	At     time.Time
	Source string
}

type sessionSummaryCollector struct {
	first  *summaryCandidate
	recent *summaryCandidate
}

var injectedUserBlockNames = map[string]struct{}{
	"apps_instructions": {}, "collaboration_mode": {}, "command-args": {},
	"command-message": {}, "command-name": {}, "environment_context": {},
	"ide_opened_file": {}, "ide_opened_files": {}, "ide_selection": {},
	"local-command-caveat": {}, "local-command-stdout": {}, "memory": {},
	"multi_agent_mode": {}, "oai-mem-citation": {}, "permissions instructions": {},
	"plugins_instructions": {}, "skill": {}, "skills_instructions": {},
	"subagent_notification": {}, "system-reminder": {}, "task-notification": {},
	"tool-use-id": {}, "turn_aborted": {},
}

var lowInformationUserMessages = map[string]struct{}{
	"ok": {}, "okay": {}, "yes": {}, "好": {}, "好的": {}, "可以": {},
	"继续": {}, "继续吧": {}, "开始": {}, "开始吧": {}, "确认": {}, "收到": {}, "行": {},
	"可以开始": {}, "可以开始吧": {}, "可以继续": {}, "可以继续吧": {},
	"好开始吧": {}, "好的开始吧": {}, "现在可以开发吧": {}, "exit": {}, "quit": {},
}

func (c *sessionSummaryCollector) Add(raw string, at time.Time, source string) {
	cleaned := cleanInjectedUserText(raw)
	text := compactSessionText(cleaned)
	if text == "" {
		return
	}
	candidate := &summaryCandidate{Text: truncate(text, 200), At: at, Source: source}
	if c.first == nil && !isLowInformationUserMessage(text) {
		copy := *candidate
		c.first = &copy
	}
	c.setRecent(candidate)
}

func (c *sessionSummaryCollector) AddReply(raw string, at time.Time, source string) {
	text := compactSessionText(raw)
	if text == "" || isMachineControlReply(text) {
		return
	}
	c.setRecent(&summaryCandidate{Text: truncate(text, 200), At: at, Source: source})
}

func (c *sessionSummaryCollector) setRecent(candidate *summaryCandidate) {
	if c.recent == nil || !candidate.At.Before(c.recent.At) {
		copy := *candidate
		c.recent = &copy
	}
}

func (c *sessionSummaryCollector) Apply(session *SessionInfo) {
	if session == nil {
		return
	}
	if c.first != nil {
		session.Summary = c.first.Text
		session.SummaryAt = c.first.At
		session.SummaryStatus = "ok"
		session.SummarySource = c.first.Source
	}
	if c.recent != nil {
		session.RecentSummary = c.recent.Text
		session.RecentSummaryAt = c.recent.At
	}
}

func isMachineControlReply(text string) bool {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return false
	}
	var value map[string]any
	if json.Unmarshal([]byte(trimmed), &value) != nil {
		return false
	}
	_, hasOutcome := value["outcome"]
	return hasOutcome
}

func cleanInjectedUserText(raw string) string {
	text := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
	if text == "" {
		return ""
	}
	if decoded, ok := decodeSerializedInjectedText(text); ok {
		text = decoded
	}
	text = extractIDERequest(text)
	for range 32 {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return ""
		}
		name, openEnd, ok := leadingInjectedBlock(trimmed)
		if !ok {
			text = trimmed
			break
		}
		closing := "</" + name + ">"
		lower := strings.ToLower(trimmed)
		if closeStart := strings.Index(lower[openEnd:], closing); closeStart >= 0 {
			text = trimmed[openEnd+closeStart+len(closing):]
			continue
		}
		if newline := strings.IndexByte(trimmed, '\n'); newline >= 0 {
			text = trimmed[newline+1:]
			continue
		}
		return ""
	}
	text = strings.TrimSpace(text)
	if looksLikeInjectedDocument(text) {
		return ""
	}
	return stripLeadingCommandToken(text)
}

func stripLeadingCommandToken(text string) string {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return text
	}
	token := fields[0]
	if len(token) < 2 || (token[0] != '$' && token[0] != '/') {
		return text
	}
	for _, value := range token[1:] {
		if !unicode.IsLetter(value) && !unicode.IsDigit(value) && value != '-' && value != '_' {
			return text
		}
	}
	return strings.TrimSpace(strings.TrimPrefix(text, token))
}

func leadingInjectedBlock(text string) (string, int, bool) {
	lower := strings.ToLower(strings.TrimSpace(text))
	if strings.HasPrefix(lower, "<") {
		end := strings.IndexByte(lower, '>')
		if end > 0 && end < 128 {
			name := strings.TrimSpace(strings.TrimPrefix(lower[1:end], "/"))
			if _, ok := injectedUserBlockNames[name]; ok {
				return name, end + 1, true
			}
		}
	}
	for name := range injectedUserBlockNames {
		prefix := name + ">"
		if strings.HasPrefix(lower, prefix) {
			return name, len(prefix), true
		}
	}
	return "", 0, false
}

func decodeSerializedInjectedText(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if (!strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "{")) || !containsInjectedSignature(trimmed) {
		return "", false
	}
	var value any
	if json.Unmarshal([]byte(trimmed), &value) != nil {
		return "", false
	}
	var values []string
	collectSerializedText(value, &values)
	if len(values) == 0 {
		return "", false
	}
	return strings.Join(values, "\n"), true
}

func collectSerializedText(value any, values *[]string) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			collectSerializedText(item, values)
		}
	case map[string]any:
		if text, ok := typed["text"].(string); ok {
			*values = append(*values, text)
			return
		}
		for _, key := range []string{"content", "message"} {
			if nested, ok := typed[key]; ok {
				collectSerializedText(nested, values)
			}
		}
	}
}

func containsInjectedSignature(text string) bool {
	lower := strings.ToLower(text)
	for name := range injectedUserBlockNames {
		if strings.Contains(lower, "<"+name) || strings.Contains(lower, name+">") {
			return true
		}
	}
	return false
}

func extractIDERequest(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	if !strings.HasPrefix(lower, "# context from my ide setup:") &&
		!strings.HasPrefix(lower, "context from my ide setup:") {
		return text
	}
	const marker = "## my request for codex:"
	if index := strings.Index(lower, marker); index >= 0 {
		return text[index+len(marker):]
	}
	return ""
}

func looksLikeInjectedDocument(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	if strings.HasPrefix(lower, "# agents.md instructions") ||
		strings.HasPrefix(lower, "# repository guidelines") {
		return true
	}
	if strings.HasPrefix(lower, "you are codex") &&
		(strings.Contains(lower, "valid channels:") || strings.Contains(lower, "developer instructions")) {
		return true
	}
	if (strings.HasPrefix(lower, "## skills") || strings.HasPrefix(lower, "### available skills")) &&
		strings.Contains(lower, "skill.md") {
		return true
	}
	return false
}

func isLowInformationUserMessage(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	normalized = strings.Map(func(value rune) rune {
		if unicode.IsSpace(value) || strings.ContainsRune("。！？!?.,，、；;：:", value) {
			return -1
		}
		return value
	}, normalized)
	_, ok := lowInformationUserMessages[normalized]
	return ok
}

func displayRecentSummary(session *SessionInfo) string {
	if session == nil {
		return ""
	}
	return compactSessionText(firstNonEmpty(session.RecentSummary, session.Summary))
}
