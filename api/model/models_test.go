package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUpsertManagedAgentRequestOmitsEmptyPromptFields(t *testing.T) {
	payload, err := json.Marshal(UpsertManagedAgentRequest{AgentID: "agent-1", Name: "Agent", Engine: "claude-code"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if strings.Contains(text, `"instructions"`) || strings.Contains(text, `"start_prompt_template"`) {
		t.Fatalf("generic partial update must not clear prompt fields: %s", text)
	}
}
