package sessiondigestv2

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/aidashboard/api/internal/contentreader"
	"github.com/aidashboard/api/internal/jsonbcompat"
)

const (
	safeTextCharacters       = 8192
	safeOutputHeadCharacters = 1024
	safeOutputTailCharacters = 4096
	safeFilePathCharacters   = 1024
	safePatchFiles           = 200
	safeClaudeBlocks         = 100
)

// projectSafeEvent is the Go equivalent of the former PostgreSQL safe-event projection.
// Limits are character-based to preserve PostgreSQL left/right semantics for UTF-8 text.
func projectSafeEvent(source contentreader.Event) (Event, error) {
	event := Event{
		StartCursor:  source.SourceStartCursor,
		EndCursor:    source.SourceEndCursor,
		OccurredAt:   source.OccurredAt,
		EventType:    source.EventType,
		Summary:      "",
		Excerpt:      "",
		ContentSHA:   source.ContentSHA256,
		PayloadBytes: source.SourceEndCursor - source.SourceStartCursor,
	}
	root, err := decodeSafeProjectionObject(source.Payload)
	if err != nil {
		return Event{}, fmt.Errorf("decode digest v2 source event at cursor %d: %w", source.SourceStartCursor, err)
	}
	payload := safeObject(root["payload"])

	var projected any
	switch source.EventType {
	case "event_msg.user_message":
		projected = payloadEnvelope(map[string]any{
			"message": safeLeft(safeText(payload["message"]), safeTextCharacters),
		})
	case "event_msg.agent_message":
		projected = payloadEnvelope(map[string]any{
			"phase":   safeText(payload["phase"]),
			"message": safeLeft(safeText(payload["message"]), safeTextCharacters),
		})
	case "event_msg.task_complete":
		projected = payloadEnvelope(map[string]any{
			"last_agent_message": safeLeft(safeText(payload["last_agent_message"]), safeTextCharacters),
		})
	case "event_msg.patch_apply_end":
		changes := safeObject(payload["changes"])
		paths := make([]string, 0, len(changes))
		for path := range changes {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		if len(paths) > safePatchFiles {
			paths = paths[:safePatchFiles]
		}
		safeChanges := make(map[string]any, len(paths))
		for _, path := range paths {
			safeChanges[path] = map[string]any{}
		}
		projected = payloadEnvelope(map[string]any{"changes": safeChanges})
	case "response_item.message":
		projected = payloadEnvelope(map[string]any{
			"role":    safeText(payload["role"]),
			"phase":   safeText(payload["phase"]),
			"content": safeLeft(safeText(payload["content"]), safeTextCharacters),
		})
	case "response_item.custom_tool_call":
		matches := patchFilePattern.FindAllStringSubmatch(safeText(payload["input"]), safePatchFiles)
		patches := make([]string, 0, len(matches))
		for _, match := range matches {
			patches = append(patches, "*** "+match[1]+" File: "+match[2])
		}
		projected = payloadEnvelope(map[string]any{
			"name":    safeText(payload["name"]),
			"call_id": safeText(payload["call_id"]),
			"input":   strings.Join(patches, "\n"),
		})
	case "response_item.function_call":
		projected = payloadEnvelope(map[string]any{
			"call_id":   safeText(payload["call_id"]),
			"arguments": safeLeft(safeText(payload["arguments"]), safeTextCharacters),
		})
	case "response_item.function_call_output", "response_item.custom_tool_call_output":
		projected = payloadEnvelope(map[string]any{
			"call_id": safeText(payload["call_id"]),
			"output":  safeHeadTail(safeText(payload["output"])),
		})
	case "user":
		projected = claudeUserProjection(root)
	case "assistant":
		projected = claudeAssistantProjection(root)
	default:
		return event, nil
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		return Event{}, fmt.Errorf("encode digest v2 safe event at cursor %d: %w", source.SourceStartCursor, err)
	}
	event.Payload = encoded
	return event, nil
}

func claudeUserProjection(root map[string]any) map[string]any {
	blocks := safeArray(safeObject(root["message"])["content"])
	if len(blocks) > safeClaudeBlocks {
		blocks = blocks[:safeClaudeBlocks]
	}
	safeBlocks := make([]any, 0, len(blocks))
	for _, rawBlock := range blocks {
		block := safeObject(rawBlock)
		switch safeText(block["type"]) {
		case "text":
			safeBlocks = append(safeBlocks, map[string]any{
				"type": "text",
				"text": safeLeft(safeText(block["text"]), safeTextCharacters),
			})
		case "tool_result":
			safeBlocks = append(safeBlocks, map[string]any{
				"type":        "tool_result",
				"tool_use_id": safeText(block["tool_use_id"]),
				"content":     safeHeadTail(safeText(block["content"])),
			})
		default:
			blockType, present := safeProjectionText(block["type"])
			if !present {
				blockType = "unknown"
			}
			safeBlocks = append(safeBlocks, map[string]any{"type": blockType})
		}
	}
	return map[string]any{"message": map[string]any{"content": safeBlocks}}
}

func claudeAssistantProjection(root map[string]any) map[string]any {
	blocks := safeArray(safeObject(root["message"])["content"])
	if len(blocks) > safeClaudeBlocks {
		blocks = blocks[:safeClaudeBlocks]
	}
	safeBlocks := make([]any, 0, len(blocks))
	for _, rawBlock := range blocks {
		block := safeObject(rawBlock)
		switch safeText(block["type"]) {
		case "text":
			safeBlocks = append(safeBlocks, map[string]any{
				"type": "text",
				"text": safeLeft(safeText(block["text"]), safeTextCharacters),
			})
		case "tool_use":
			input := safeObject(block["input"])
			safeBlocks = append(safeBlocks, map[string]any{
				"type": "tool_use",
				"id":   safeText(block["id"]),
				"name": safeText(block["name"]),
				"input": map[string]any{
					"file_path": safeLeft(safeText(input["file_path"]), safeFilePathCharacters),
					"command":   safeLeft(safeText(input["command"]), safeTextCharacters),
				},
			})
		default:
			blockType, present := safeProjectionText(block["type"])
			if !present {
				blockType = "unknown"
			}
			safeBlocks = append(safeBlocks, map[string]any{"type": blockType})
		}
	}
	return map[string]any{"message": map[string]any{"content": safeBlocks}}
}

func decodeSafeProjectionObject(raw json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("event payload root must be an object")
	}
	return root, nil
}

func payloadEnvelope(payload map[string]any) map[string]any {
	return map[string]any{"payload": payload}
}

func safeObject(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return map[string]any{}
}

func safeArray(value any) []any {
	if array, ok := value.([]any); ok {
		return array
	}
	return []any{}
}

func safeText(value any) string {
	text, _ := safeProjectionText(value)
	return text
}

func safeProjectionText(value any) (string, bool) {
	switch typed := value.(type) {
	case nil:
		return "", false
	case string:
		return typed, true
	default:
		return jsonbcompat.Text(typed), true
	}
}

func safeLeft(value string, limit int) string {
	characters := []rune(value)
	if len(characters) <= limit {
		return value
	}
	return string(characters[:limit])
}

func safeRight(value string, limit int) string {
	characters := []rune(value)
	if len(characters) <= limit {
		return value
	}
	return string(characters[len(characters)-limit:])
}

func safeHeadTail(value string) string {
	return safeLeft(value, safeOutputHeadCharacters) + "\n" + safeRight(value, safeOutputTailCharacters)
}
