package sessiondigestv2

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/aidashboard/api/internal/contentreader"
)

func TestProjectSafeEventMatchesLegacyProjectionShapes(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		source    any
		expected  any
	}{
		{
			name: "codex user message", eventType: "event_msg.user_message",
			source:   map[string]any{"payload": map[string]any{"message": "build reader"}},
			expected: map[string]any{"payload": map[string]any{"message": "build reader"}},
		},
		{
			name: "codex agent message", eventType: "event_msg.agent_message",
			source: map[string]any{"payload": map[string]any{
				"phase": "final_answer", "message": "done",
			}},
			expected: map[string]any{"payload": map[string]any{
				"phase": "final_answer", "message": "done",
			}},
		},
		{
			name: "task complete", eventType: "event_msg.task_complete",
			source:   map[string]any{"payload": map[string]any{"last_agent_message": "complete"}},
			expected: map[string]any{"payload": map[string]any{"last_agent_message": "complete"}},
		},
		{
			name: "patch result", eventType: "event_msg.patch_apply_end",
			source: map[string]any{"payload": map[string]any{"changes": map[string]any{
				"z.go": map[string]any{"kind": "update"}, "a.go": true,
			}}},
			expected: map[string]any{"payload": map[string]any{"changes": map[string]any{
				"a.go": map[string]any{}, "z.go": map[string]any{},
			}}},
		},
		{
			name: "response message", eventType: "response_item.message",
			source: map[string]any{"payload": map[string]any{
				"role": "assistant", "phase": "final_answer",
				"content": []any{map[string]any{"type": "text", "text": "done"}},
			}},
			expected: map[string]any{"payload": map[string]any{
				"role": "assistant", "phase": "final_answer",
				"content": `[{"text":"done","type":"text"}]`,
			}},
		},
		{
			name: "custom tool call", eventType: "response_item.custom_tool_call",
			source: map[string]any{"payload": map[string]any{
				"name": "apply_patch", "call_id": "patch-1",
				"input": "*** Begin Patch\n*** Add File: a.go\nnoise\n*** Update File: b.go\n*** End Patch",
			}},
			expected: map[string]any{"payload": map[string]any{
				"name": "apply_patch", "call_id": "patch-1",
				"input": "*** Add File: a.go\n*** Update File: b.go",
			}},
		},
		{
			name: "function call", eventType: "response_item.function_call",
			source: map[string]any{"payload": map[string]any{
				"call_id": "call-1", "arguments": `{"cmd":"go test ./..."}`,
			}},
			expected: map[string]any{"payload": map[string]any{
				"call_id": "call-1", "arguments": `{"cmd":"go test ./..."}`,
			}},
		},
		{
			name: "short function output keeps legacy separator", eventType: "response_item.function_call_output",
			source: map[string]any{"payload": map[string]any{
				"call_id": "call-1", "output": "passed",
			}},
			expected: map[string]any{"payload": map[string]any{
				"call_id": "call-1", "output": "passed\npassed",
			}},
		},
		{
			name: "claude user", eventType: "user",
			source: map[string]any{"message": map[string]any{"content": []any{
				map[string]any{"type": "text", "text": "please fix"},
				map[string]any{"type": "tool_result", "tool_use_id": "tool-1", "content": "ok"},
				map[string]any{"type": "image", "source": "discarded"},
				map[string]any{"type": "", "source": "discarded"},
				map[string]any{"source": "discarded"},
			}}},
			expected: map[string]any{"message": map[string]any{"content": []any{
				map[string]any{"type": "text", "text": "please fix"},
				map[string]any{"type": "tool_result", "tool_use_id": "tool-1", "content": "ok\nok"},
				map[string]any{"type": "image"},
				map[string]any{"type": ""},
				map[string]any{"type": "unknown"},
			}}},
		},
		{
			name: "claude assistant", eventType: "assistant",
			source: map[string]any{"message": map[string]any{"content": []any{
				map[string]any{"type": "text", "text": "working"},
				map[string]any{"type": "tool_use", "id": "tool-1", "name": "Bash", "input": map[string]any{
					"file_path": "/tmp/result", "command": "go test ./...", "secret": "discarded",
				}},
			}}},
			expected: map[string]any{"message": map[string]any{"content": []any{
				map[string]any{"type": "text", "text": "working"},
				map[string]any{"type": "tool_use", "id": "tool-1", "name": "Bash", "input": map[string]any{
					"file_path": "/tmp/result", "command": "go test ./...",
				}},
			}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event, err := projectSafeEvent(safeProjectionSource(t, test.eventType, test.source))
			if err != nil {
				t.Fatal(err)
			}
			assertProjectionJSON(t, event.Payload, test.expected)
			if event.StartCursor != 10 || event.EndCursor != 30 || event.PayloadBytes != 20 ||
				event.ContentSHA != strings.Repeat("a", 64) || event.Summary != "" || event.Excerpt != "" {
				t.Fatalf("projection metadata changed: %+v", event)
			}
		})
	}
}

func TestProjectSafeEventEnforcesCharacterAndCollectionLimits(t *testing.T) {
	message := strings.Repeat("界", safeTextCharacters+1)
	event, err := projectSafeEvent(safeProjectionSource(t, "event_msg.user_message", map[string]any{
		"payload": map[string]any{"message": message},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var projected struct {
		Payload struct {
			Message string `json:"message"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(event.Payload, &projected); err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(projected.Payload.Message) != safeTextCharacters {
		t.Fatalf("message characters=%d", utf8.RuneCountInString(projected.Payload.Message))
	}

	patchLines := make([]string, 0, safePatchFiles+5)
	for index := 0; index < safePatchFiles+5; index++ {
		patchLines = append(patchLines, "*** Add File: file-"+strconv.Itoa(index)+".go")
	}
	patchEvent, err := projectSafeEvent(safeProjectionSource(t, "response_item.custom_tool_call", map[string]any{
		"payload": map[string]any{"input": strings.Join(patchLines, "\n")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var patchPayload struct {
		Payload struct {
			Input string `json:"input"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(patchEvent.Payload, &patchPayload); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(patchPayload.Payload.Input, "\n"); len(lines) != safePatchFiles ||
		!strings.Contains(lines[len(lines)-1], "file-199.go") {
		t.Fatalf("patch projection count/ordering changed: count=%d last=%q", len(lines), lines[len(lines)-1])
	}

	blocks := make([]any, safeClaudeBlocks+1)
	for index := range blocks {
		blocks[index] = map[string]any{"type": "text", "text": strconv.Itoa(index)}
	}
	claudeEvent, err := projectSafeEvent(safeProjectionSource(t, "assistant", map[string]any{
		"message": map[string]any{"content": blocks},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var claudePayload struct {
		Message struct {
			Content []any `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(claudeEvent.Payload, &claudePayload); err != nil {
		t.Fatal(err)
	}
	if len(claudePayload.Message.Content) != safeClaudeBlocks {
		t.Fatalf("claude block count=%d", len(claudePayload.Message.Content))
	}
}

func TestProjectSafeEventOmitsUnsupportedPayloadAndRejectsInvalidJSON(t *testing.T) {
	unsupported, err := projectSafeEvent(safeProjectionSource(t, "response_item.reasoning", map[string]any{
		"payload": map[string]any{"summary": "must not be copied"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if unsupported.Payload != nil {
		t.Fatalf("unsupported payload must remain SQL-NULL equivalent: %s", unsupported.Payload)
	}

	invalid := safeProjectionSource(t, "event_msg.user_message", map[string]any{})
	invalid.Payload = json.RawMessage("[")
	if _, err := projectSafeEvent(invalid); err == nil {
		t.Fatal("expected invalid source JSON to fail")
	}
}

func safeProjectionSource(t *testing.T, eventType string, payload any) contentreader.Event {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return contentreader.Event{
		SourceStartCursor: 10,
		SourceEndCursor:   30,
		OccurredAt:        time.Date(2026, 7, 18, 1, 2, 3, 0, time.UTC),
		EventType:         eventType,
		Payload:           encoded,
		ContentSHA256:     strings.Repeat("a", 64),
	}
}

func assertProjectionJSON(t *testing.T, actual json.RawMessage, expected any) {
	t.Helper()
	var actualValue any
	if err := json.Unmarshal(actual, &actualValue); err != nil {
		t.Fatal(err)
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	var expectedValue any
	if err := json.Unmarshal(expectedJSON, &expectedValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actualValue, expectedValue) {
		t.Fatalf("projection mismatch\nactual:   %s\nexpected: %s", actual, expectedJSON)
	}
}
