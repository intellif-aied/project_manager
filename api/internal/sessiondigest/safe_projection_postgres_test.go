package sessiondigest

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"

	projectdb "github.com/aidashboard/api/db"
)

// TestProjectSafeEventMatchesPostgresLegacySQL pins Digest V1 to the exact SQL
// projection and source byte accounting that the MinIO reader path replaces.
func TestProjectSafeEventMatchesPostgresLegacySQL(t *testing.T) {
	databaseURL := os.Getenv("AIDA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AIDA_TEST_DATABASE_URL is not set")
	}
	database, err := projectdb.Connect(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	longText := strings.Repeat("界", safeTextCharacters+1)
	longOutput := strings.Repeat("head", 500) + strings.Repeat("tail", 1000)
	tests := []struct {
		eventType string
		payload   any
	}{
		{"event_msg.user_message", map[string]any{"payload": map[string]any{"message": longText}}},
		{"event_msg.agent_message", map[string]any{"payload": map[string]any{"phase": "final_answer", "message": longText}}},
		{"event_msg.task_complete", map[string]any{"payload": map[string]any{"last_agent_message": longText}}},
		{"event_msg.patch_apply_end", map[string]any{"payload": map[string]any{"changes": map[string]any{"z.go": true, "a.go": false}}}},
		{"response_item.message", map[string]any{"payload": map[string]any{"role": "assistant", "phase": "final_answer", "content": []any{"one", "two"}}}},
		{"response_item.custom_tool_call", map[string]any{"payload": map[string]any{
			"name": "apply_patch", "call_id": "patch-1",
			"input": "*** Begin Patch\n*** Add File: a.go\nnoise\n*** Delete File: b.go\n*** End Patch",
		}}},
		{"response_item.function_call", map[string]any{"payload": map[string]any{"call_id": "call-1", "arguments": longText}}},
		{"response_item.function_call_output", map[string]any{"payload": map[string]any{"call_id": "call-1", "output": longOutput}}},
		{"response_item.custom_tool_call_output", map[string]any{"payload": map[string]any{"call_id": "call-2", "output": "short"}}},
		{"user", map[string]any{"message": map[string]any{"content": []any{
			map[string]any{"type": "text", "text": longText},
			map[string]any{"type": "tool_result", "tool_use_id": "tool-1", "content": longOutput},
			map[string]any{"type": "image"}, map[string]any{"type": ""}, map[string]any{},
		}}}},
		{"assistant", map[string]any{"message": map[string]any{"content": []any{
			map[string]any{"type": "text", "text": longText},
			map[string]any{"type": "tool_use", "id": "tool-1", "name": "Bash", "input": map[string]any{
				"file_path": longText, "command": longText, "secret": "discarded",
			}},
			map[string]any{"type": "image"}, map[string]any{"type": ""}, map[string]any{},
		}}}},
		{"response_item.reasoning", map[string]any{"payload": map[string]any{"summary": "private"}}},
	}

	for _, test := range tests {
		t.Run(test.eventType, func(t *testing.T) {
			source := safeProjectionSource(t, test.eventType, test.payload)
			actual, err := projectSafeEvent(source)
			if err != nil {
				t.Fatal(err)
			}
			var legacy sql.NullString
			var legacyBytes int64
			if err := database.QueryRowContext(
				context.Background(), legacySafePayloadSQL, test.eventType, string(source.Payload),
			).Scan(&legacy, &legacyBytes); err != nil {
				t.Fatal(err)
			}
			if actual.PayloadBytes != legacyBytes {
				t.Fatalf("source bytes differ: Go=%d PostgreSQL=%d", actual.PayloadBytes, legacyBytes)
			}
			if !legacy.Valid {
				if actual.Payload != nil {
					t.Fatalf("legacy payload is NULL, Go payload=%s", actual.Payload)
				}
				return
			}
			assertProjectionJSON(t, actual.Payload, json.RawMessage(legacy.String))
		})
	}
}

const legacySafePayloadSQL = `
	WITH source(event_type, content_payload) AS (VALUES ($1::text, $2::jsonb))
	SELECT CASE event_type
		WHEN 'event_msg.user_message' THEN jsonb_build_object('payload', jsonb_build_object(
			'message', left(COALESCE(content_payload #>> '{payload,message}', ''), 8192)))
		WHEN 'event_msg.agent_message' THEN jsonb_build_object('payload', jsonb_build_object(
			'phase', COALESCE(content_payload #>> '{payload,phase}', ''),
			'message', left(COALESCE(content_payload #>> '{payload,message}', ''), 8192)))
		WHEN 'event_msg.task_complete' THEN jsonb_build_object('payload', jsonb_build_object(
			'last_agent_message', left(COALESCE(content_payload #>> '{payload,last_agent_message}', ''), 8192)))
		WHEN 'event_msg.patch_apply_end' THEN jsonb_build_object('payload', jsonb_build_object(
			'changes', COALESCE((
				SELECT jsonb_object_agg(file_name, jsonb_build_object())
				FROM (
					SELECT file_name
					FROM jsonb_object_keys(CASE
						WHEN jsonb_typeof(content_payload #> '{payload,changes}') = 'object'
						THEN content_payload #> '{payload,changes}' ELSE '{}'::jsonb END) AS file_name
					ORDER BY file_name LIMIT 200
				) files
			), '{}'::jsonb)))
		WHEN 'response_item.message' THEN jsonb_build_object('payload', jsonb_build_object(
			'role', COALESCE(content_payload #>> '{payload,role}', ''),
			'phase', COALESCE(content_payload #>> '{payload,phase}', ''),
			'content', left(COALESCE(content_payload #>> '{payload,content}', ''), 8192)))
		WHEN 'response_item.custom_tool_call' THEN jsonb_build_object('payload', jsonb_build_object(
			'name', COALESCE(content_payload #>> '{payload,name}', ''),
			'call_id', COALESCE(content_payload #>> '{payload,call_id}', ''),
			'input', COALESCE((
				SELECT string_agg('*** Update File: ' || match[1], E'\n')
				FROM (
					SELECT match
					FROM regexp_matches(
						COALESCE(content_payload #>> '{payload,input}', ''),
						'(?m)^\*\*\* (?:Add|Update|Delete) File: (.+)$', 'g'
					) WITH ORDINALITY AS found(match, ordinal)
					ORDER BY ordinal LIMIT 200
				) matches
			), '')))
		WHEN 'response_item.function_call' THEN jsonb_build_object('payload', jsonb_build_object(
			'call_id', COALESCE(content_payload #>> '{payload,call_id}', ''),
			'arguments', left(COALESCE(content_payload #>> '{payload,arguments}', ''), 8192)))
		WHEN 'response_item.function_call_output' THEN
			jsonb_build_object('payload', jsonb_build_object(
				'call_id', COALESCE(content_payload #>> '{payload,call_id}', ''),
				'output', left(COALESCE(content_payload #>> '{payload,output}', ''), 512) || ' ' ||
					right(COALESCE(content_payload #>> '{payload,output}', ''), 2048)))
		WHEN 'response_item.custom_tool_call_output' THEN
			jsonb_build_object('payload', jsonb_build_object(
				'call_id', COALESCE(content_payload #>> '{payload,call_id}', ''),
				'output', left(COALESCE(content_payload #>> '{payload,output}', ''), 512) || ' ' ||
					right(COALESCE(content_payload #>> '{payload,output}', ''), 2048)))
		WHEN 'user' THEN jsonb_build_object('message', jsonb_build_object('content', COALESCE((
			SELECT jsonb_agg(safe_block ORDER BY ordinal)
			FROM (
				SELECT ordinal, CASE block->>'type'
					WHEN 'text' THEN jsonb_build_object(
						'type', 'text', 'text', left(COALESCE(block->>'text', ''), 8192))
					WHEN 'tool_result' THEN jsonb_build_object(
						'type', 'tool_result', 'tool_use_id', COALESCE(block->>'tool_use_id', ''),
						'content', left(COALESCE(block->>'content', ''), 512) || ' ' ||
							right(COALESCE(block->>'content', ''), 2048))
					ELSE jsonb_build_object('type', COALESCE(block->>'type', 'unknown'))
				END AS safe_block
				FROM jsonb_array_elements(CASE
					WHEN jsonb_typeof(content_payload #> '{message,content}') = 'array'
					THEN content_payload #> '{message,content}' ELSE '[]'::jsonb END)
					WITH ORDINALITY AS blocks(block, ordinal)
				ORDER BY ordinal LIMIT 100
			) safe_blocks
		), '[]'::jsonb)))
		WHEN 'assistant' THEN jsonb_build_object('message', jsonb_build_object('content', COALESCE((
			SELECT jsonb_agg(safe_block ORDER BY ordinal)
			FROM (
				SELECT ordinal, CASE block->>'type'
					WHEN 'text' THEN jsonb_build_object(
						'type', 'text', 'text', left(COALESCE(block->>'text', ''), 8192))
					WHEN 'tool_use' THEN jsonb_build_object(
						'type', 'tool_use', 'id', COALESCE(block->>'id', ''),
						'name', COALESCE(block->>'name', ''),
						'input', jsonb_build_object(
							'file_path', left(COALESCE(block #>> '{input,file_path}', ''), 1024),
							'command', left(COALESCE(block #>> '{input,command}', ''), 8192)))
					ELSE jsonb_build_object('type', COALESCE(block->>'type', 'unknown'))
				END AS safe_block
				FROM jsonb_array_elements(CASE
					WHEN jsonb_typeof(content_payload #> '{message,content}') = 'array'
					THEN content_payload #> '{message,content}' ELSE '[]'::jsonb END)
					WITH ORDINALITY AS blocks(block, ordinal)
				ORDER BY ordinal LIMIT 100
			) safe_blocks
		), '[]'::jsonb)))
		ELSE NULL END,
		octet_length(content_payload::text)
	FROM source`
