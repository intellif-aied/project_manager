package sessiondigest

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/aidashboard/api/internal/contentreader"
	"github.com/aidashboard/api/internal/jsonbcompat"
)

func TestProjectSafeEventPreservesLegacyV1Shapes(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		source    any
		expected  any
	}{
		{
			name: "response message", eventType: "response_item.message",
			source: map[string]any{"payload": map[string]any{
				"role": "assistant", "phase": "final_answer", "content": []any{"one", "two"},
			}},
			expected: map[string]any{"payload": map[string]any{
				"role": "assistant", "phase": "final_answer", "content": `["one", "two"]`,
			}},
		},
		{
			name: "v1 patch action normalization", eventType: "response_item.custom_tool_call",
			source: map[string]any{"payload": map[string]any{
				"name": "apply_patch", "call_id": "patch-1",
				"input": "*** Add File: a.go\n*** Delete File: b.go",
			}},
			expected: map[string]any{"payload": map[string]any{
				"name": "apply_patch", "call_id": "patch-1",
				"input": "*** Update File: a.go\n*** Update File: b.go",
			}},
		},
		{
			name: "v1 output separator", eventType: "response_item.function_call_output",
			source:   map[string]any{"payload": map[string]any{"call_id": "call-1", "output": "passed"}},
			expected: map[string]any{"payload": map[string]any{"call_id": "call-1", "output": "passed passed"}},
		},
		{
			name: "claude user", eventType: "user",
			source: map[string]any{"message": map[string]any{"content": []any{
				map[string]any{"type": "text", "text": "please fix"},
				map[string]any{"type": "tool_result", "tool_use_id": "tool-1", "content": "ok"},
				map[string]any{"type": ""}, map[string]any{},
			}}},
			expected: map[string]any{"message": map[string]any{"content": []any{
				map[string]any{"type": "text", "text": "please fix"},
				map[string]any{"type": "tool_result", "tool_use_id": "tool-1", "content": "ok ok"},
				map[string]any{"type": ""}, map[string]any{"type": "unknown"},
			}}},
		},
		{
			name: "claude assistant", eventType: "assistant",
			source: map[string]any{"message": map[string]any{"content": []any{
				map[string]any{"type": "tool_use", "id": "tool-1", "name": "Bash", "input": map[string]any{
					"file_path": "/tmp/result", "command": "go test ./...", "secret": "discarded",
				}},
			}}},
			expected: map[string]any{"message": map[string]any{"content": []any{
				map[string]any{"type": "tool_use", "id": "tool-1", "name": "Bash", "input": map[string]any{
					"file_path": "/tmp/result", "command": "go test ./...",
				}},
			}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := safeProjectionSource(t, test.eventType, test.source)
			event, err := projectSafeEvent(source)
			if err != nil {
				t.Fatal(err)
			}
			assertProjectionJSON(t, event.Payload, test.expected)
			var decoded any
			decoder := json.NewDecoder(strings.NewReader(string(source.Payload)))
			decoder.UseNumber()
			if err := decoder.Decode(&decoded); err != nil {
				t.Fatal(err)
			}
			if event.PayloadBytes != int64(len(jsonbcompat.Text(decoded))) || event.StartCursor != 10 ||
				event.EndCursor != 30 || event.ContentSHA != strings.Repeat("a", 64) {
				t.Fatalf("projection metadata changed: %+v", event)
			}
		})
	}
}

func TestProjectSafeEventEnforcesV1Limits(t *testing.T) {
	message := strings.Repeat("界", safeTextCharacters+1)
	event, err := projectSafeEvent(safeProjectionSource(t, "event_msg.user_message", map[string]any{
		"payload": map[string]any{"message": message},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var messagePayload struct {
		Payload struct {
			Message string `json:"message"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(event.Payload, &messagePayload); err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(messagePayload.Payload.Message) != safeTextCharacters {
		t.Fatalf("message characters=%d", utf8.RuneCountInString(messagePayload.Payload.Message))
	}

	patches := make([]string, 0, safePatchFiles+1)
	for index := 0; index <= safePatchFiles; index++ {
		patches = append(patches, "*** Add File: file-"+strconv.Itoa(index)+".go")
	}
	patchEvent, err := projectSafeEvent(safeProjectionSource(t, "response_item.custom_tool_call", map[string]any{
		"payload": map[string]any{"input": strings.Join(patches, "\n")},
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
