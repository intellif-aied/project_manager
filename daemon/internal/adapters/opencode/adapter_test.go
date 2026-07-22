package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"github.com/aidashboard/daemon/internal/sessionadapter"
	"os"
	"testing"
)

type fakeRunner struct{ calls [][]string }

func (f *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	if args[0] == "session" {
		return []byte(`[{"id":"s1","title":"Fix bug","directory":"/tmp/p","createdAt":"2026-07-21T01:00:00Z","updatedAt":"2026-07-21T02:00:00Z"}]`), nil
	}
	return []byte(`{"info":{"id":"s1"},"messages":[{"id":"m1","role":"user","text":"fix the bug"},{"id":"m2","role":"assistant","text":"fixed"}]}`), nil
}
func TestDiscoverAndMaterializeOfficialCommands(t *testing.T) {
	runner := new(fakeRunner)
	adapter := NewWithRunner(t.TempDir(), runner)
	sessions, diagnostics := adapter.Discover(context.Background(), sessionadapter.DiscoverOptions{})
	if len(diagnostics) != 0 || len(sessions) != 1 || sessions[0].NativeSessionRef != "s1" {
		t.Fatalf("sessions=%+v diagnostics=%+v", sessions, diagnostics)
	}
	materialized, err := adapter.Materialize(context.Background(), sessions[0])
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(materialized.CanonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) == 0 || materialized.UsageCapability != sessionadapter.UsageUnavailable {
		t.Fatalf("materialized=%+v", materialized)
	}
	roles := canonicalTopLevelRoles(t, content)
	if !roles["user"] || !roles["assistant"] {
		t.Fatalf("canonical roles are missing: %s", content)
	}
	if len(runner.calls) != 2 || runner.calls[1][0] != "export" {
		t.Fatalf("calls=%v", runner.calls)
	}
}

func canonicalTopLevelRoles(t *testing.T, content []byte) map[string]bool {
	t.Helper()
	roles := map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		var event struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "message" {
			role, _ := event.Payload["role"].(string)
			roles[role] = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return roles
}

func TestMaterializeRejectsMissingStableTime(t *testing.T) {
	adapter := NewWithRunner(t.TempDir(), new(fakeRunner))
	_, err := adapter.Materialize(context.Background(), sessionadapter.Descriptor{ClientType: "opencode", NativeSessionRef: "s"})
	if err == nil {
		t.Fatal("expected missing stable timestamp error")
	}
}
