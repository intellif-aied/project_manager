package sessiondigestv2

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestExtractorDoesNotTruncateContentBySize(t *testing.T) {
	goal := "目标-" + strings.Repeat("甲", 400_000)
	result := "结果-" + strings.Repeat("乙", 400_000)
	extractor := NewExtractor()
	extractor.Consume(Event{
		StartCursor: 1,
		EndCursor:   2,
		OccurredAt:  time.Now().UTC(),
		EventType:   "event_msg.user_message",
		Payload:     mustDigestEventPayload(t, map[string]any{"message": goal}),
	})
	extractor.Consume(Event{
		StartCursor: 3,
		EndCursor:   4,
		OccurredAt:  time.Now().UTC().Add(time.Second),
		EventType:   "event_msg.agent_message",
		Payload: mustDigestEventPayload(t, map[string]any{
			"phase": "final_answer", "message": result,
		}),
	})

	digest, _, _, _, truncated, encoded := extractor.Result()
	if truncated || digest.Coverage.Truncated {
		t.Fatal("v2.10 digest must not report size truncation")
	}
	if len(encoded) <= 1<<20 {
		t.Fatalf("fixture did not exceed warning threshold: %d", len(encoded))
	}
	if len(digest.WorkUnits) != 1 || digest.WorkUnits[0].Goal.Text != goal ||
		len(digest.WorkUnits[0].AgentClaims) != 1 ||
		digest.WorkUnits[0].AgentClaims[0].Text != result {
		t.Fatal("content changed or was removed because of payload size")
	}
}

func mustDigestEventPayload(t *testing.T, payload map[string]any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"payload": payload})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
