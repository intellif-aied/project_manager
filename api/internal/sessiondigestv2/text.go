package sessiondigestv2

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"

	"github.com/aidashboard/api/internal/sessiondigest"
)

var serializedTextFieldPattern = regexp.MustCompile(
	`"(?:text|input_text|output_text)"\s*:\s*"((?:\\.|[^"\\])*)"`,
)

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

func contentTexts(value any) []string {
	if text := stringValue(value); text != "" {
		trimmed := strings.TrimSpace(text)
		var decoded any
		structured := strings.HasPrefix(trimmed, "[") ||
			strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, `"`)
		if structured {
			if json.Unmarshal([]byte(trimmed), &decoded) == nil {
				if nested := contentTexts(decoded); len(nested) > 0 {
					return nested
				}
			}
			// The safe projection bounds one field, so serialized multimodal
			// content may be cut before the closing JSON delimiter. Recover only
			// text fields and never retain image/base64 payloads as user goals.
			if recovered := recoverSerializedTextFields(trimmed); len(recovered) > 0 {
				return recovered
			}
			if containsSerializedBinaryPayload(trimmed) {
				return nil
			}
		}
		return []string{text}
	}
	if object := objectValue(value); len(object) > 0 {
		for _, key := range []string{"text", "output_text", "input_text", "content"} {
			if nested := contentTexts(object[key]); len(nested) > 0 {
				return nested
			}
		}
	}
	texts := []string{}
	for _, item := range arrayValue(value) {
		texts = append(texts, contentTexts(item)...)
	}
	return texts
}

func recoverSerializedTextFields(value string) []string {
	matches := serializedTextFieldPattern.FindAllStringSubmatch(value, -1)
	result := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		var decoded string
		if json.Unmarshal([]byte(`"`+match[1]+`"`), &decoded) != nil {
			continue
		}
		decoded = strings.TrimSpace(decoded)
		if decoded == "" {
			continue
		}
		key := canonicalKey(decoded)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, decoded)
	}
	return result
}

func containsSerializedBinaryPayload(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, `"input_image"`) ||
		strings.Contains(lower, `"image_url"`) ||
		strings.Contains(lower, "data:image/") ||
		strings.Contains(lower, ";base64,")
}

func messageTexts(message map[string]any) []string {
	texts := []string{}
	for _, item := range arrayValue(message["content"]) {
		block := objectValue(item)
		if stringValue(block["type"]) != "text" {
			continue
		}
		texts = append(texts, contentTexts(block["text"])...)
	}
	return texts
}

func normalizeText(value string) string {
	value = sessiondigest.Redact(value)
	value = strings.ReplaceAll(value, "\x00", "\uFFFD")
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	return value
}

func normalizeFilePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(sessiondigest.Redact(value), "\\", "/"))
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
		if len(parts) > 5 {
			parts = parts[len(parts)-5:]
		}
		cleaned = strings.Join(parts, "/")
	}
	return cleaned
}

func isNoiseGoal(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	prefixes := []string{
		"<environment_context>",
		"<permissions instructions>",
		"<subagent_notification>",
		"<skill>",
		"<skills_instructions>",
		"<apps_instructions>",
		"<plugins_instructions>",
		"<collaboration_mode>",
		"<multi_agent_mode>",
		"<turn_aborted>",
		"<image name=",
		"</image>",
		"# agents.md instructions",
		"# repository guidelines",
		"## available skills",
		"## skills",
		"## memory",
		"<instructions>",
		"you are codex",
		"you are chatgpt",
		"the following is the codex agent history",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	if strings.Contains(lower, "filesystem sandboxing defines which files") ||
		strings.Contains(lower, "valid channels: analysis") ||
		(strings.Contains(lower, "# agents.md instructions for") &&
			strings.Contains(lower, "<instructions>")) {
		return true
	}
	// Some clients persist an injected skill block as a truncated serialized
	// content array. In that form the text no longer starts with <skill>, so
	// prefix-only filtering mistakes it for a user goal.
	if strings.Contains(lower, "<skill>") &&
		strings.Contains(lower, "<name>") &&
		(strings.Contains(lower, "<path>") || strings.Contains(lower, "skill.md")) {
		return true
	}
	if strings.Contains(lower, "<skills_instructions>") ||
		(strings.Contains(lower, "### available skills") &&
			strings.Contains(lower, "skill.md")) {
		return true
	}
	return false
}

func isApprovalAssessmentHeader(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(
		lower,
		"codex agent history whose request action you are assessing",
	) || strings.Contains(
		lower,
		"codex agent history added since your last approval assessment",
	)
}

func isNoiseAgentClaim(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(lower, "{") {
		return false
	}
	return (strings.Contains(lower, `"outcome":"allow"`) ||
		strings.Contains(lower, `"outcome":"deny"`)) &&
		(strings.Contains(lower, `"risk_level"`) ||
			strings.HasPrefix(lower, `{"outcome"`))
}

func canonicalKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}
