package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aidashboard/api/model"
)

func TestUpdateMyAgentWithExplicitPromptFieldsSendsIntentionalEmptyValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/my/agents/agent-1" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if value, ok := payload["instructions"]; !ok || value != "" {
			t.Fatalf("instructions not explicitly cleared: %#v", payload)
		}
		if value, ok := payload["start_prompt_template"]; !ok || value != "" {
			t.Fatalf("start prompt not explicitly cleared: %#v", payload)
		}
		_ = json.NewEncoder(w).Encode(model.UpsertManagedAgentResponse{AgentID: "agent-1"})
	}))
	defer server.Close()

	client := NewManagedAgentClient(server.URL, "token")
	if _, err := client.UpdateMyAgentWithExplicitPromptFields(context.Background(), "agent-1", model.UpsertManagedAgentRequest{
		AgentID: "agent-1", Name: "Agent", Engine: "claude-code",
	}); err != nil {
		t.Fatal(err)
	}
}
