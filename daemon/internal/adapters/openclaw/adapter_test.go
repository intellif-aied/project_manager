package openclaw

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aidashboard/daemon/internal/sessionadapter"
)

type fakeRunner struct {
	escape bool
}

func (runner *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	if len(args) > 1 && args[0] == "sessions" && args[1] != "export-trajectory" {
		return []byte(`{"hasMore":false,"sessions":[{"agentId":"work","key":"agent:work:telegram:direct:123","sessionId":"native-1","label":"coding task","model":"gpt-test","sessionStartedAt":1784600000000,"updatedAt":1784601000000},{"agentId":"work","key":"agent:work:subagent:abc","sessionId":"native-child","label":"coding child","parentSessionKey":"agent:work:telegram:direct:123","spawnedBy":"agent:work:telegram:direct:123","forkedFromParent":true,"sessionStartedAt":1784600500000,"updatedAt":1784600900000},{"agentId":"work","key":"agent:work:cron:job-1","sessionId":"cron-1","label":"private automation"}]}`), nil
	}
	workspace := argument(args, "--workspace")
	output := argument(args, "--output")
	dir := filepath.Join(workspace, ".openclaw", "trajectory-exports", output)
	if runner.escape {
		dir = filepath.Join(filepath.Dir(workspace), "outside-export")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	key := argument(args, "--session-key")
	manifest, _ := json.Marshal(map[string]any{"traceSchema": "openclaw-trajectory", "schemaVersion": 1, "sessionId": "native-1", "sessionKey": key})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0600); err != nil {
		return nil, err
	}
	events := strings.Join([]string{
		`{"traceSchema":"openclaw-trajectory","schemaVersion":1,"traceId":"trace-1","source":"runtime","type":"prompt.submitted","ts":"2026-07-21T01:00:00Z","sourceSeq":1,"sessionId":"native-1","data":{"prompt":"must-not-upload"}}`,
		`{"traceSchema":"openclaw-trajectory","schemaVersion":1,"traceId":"trace-1","source":"transcript","type":"user.message","ts":"2026-07-21T01:00:01Z","sourceSeq":1,"sessionId":"native-1","entryId":"entry-1","data":{"message":{"role":"user","content":[{"type":"text","text":"fix the bug"}]}}}`,
		`{"traceSchema":"openclaw-trajectory","schemaVersion":1,"traceId":"trace-1","source":"transcript","type":"tool.call","ts":"2026-07-21T01:00:02Z","sourceSeq":2,"sessionId":"native-1","entryId":"entry-2","data":{"name":"Read","arguments":{"secret":"must-not-upload"}}}`,
		`{"traceSchema":"openclaw-trajectory","schemaVersion":1,"traceId":"trace-1","source":"transcript","type":"session.compaction","ts":"2026-07-21T01:00:03Z","sourceSeq":3,"sessionId":"native-1","entryId":"entry-3","data":{"summary":"must-not-upload"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(events), 0600); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"outputDir": dir, "sessionId": "native-1"})
}

func argument(args []string, name string) string {
	for index := range args {
		if args[index] == name && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}

func TestDiscoverAndMaterializeOfficialTrajectoryContract(t *testing.T) {
	adapter := NewWithRunner(t.TempDir(), new(fakeRunner))
	sessions, diagnostics := adapter.Discover(context.Background(), sessionadapter.DiscoverOptions{})
	if len(diagnostics) != 0 || len(sessions) != 2 {
		t.Fatalf("sessions=%+v diagnostics=%+v", sessions, diagnostics)
	}
	var descriptor, child sessionadapter.Descriptor
	for _, session := range sessions {
		if session.Summary == "coding task" {
			descriptor = session
		} else if session.Summary == "coding child" {
			child = session
		}
	}
	if descriptor.NativeSessionRef == "" || child.ParentRef != descriptor.NativeSessionRef || child.NativeSessionRef == descriptor.NativeSessionRef {
		t.Fatalf("root=%+v child=%+v", descriptor, child)
	}
	if strings.Contains(descriptor.NativeSessionRef, "telegram") || strings.Contains(descriptor.NativeSessionRef, "123") || descriptor.Capability.Usage != sessionadapter.UsageUnavailable {
		t.Fatalf("descriptor=%+v", descriptor)
	}
	var locator opaqueLocator
	if json.Unmarshal([]byte(descriptor.OpaqueLocator), &locator) != nil || locator.SessionKey != "agent:work:telegram:direct:123" || locator.SessionID != "native-1" {
		t.Fatalf("locator=%+v", locator)
	}
	var first []byte
	for run := 0; run < 3; run++ {
		materialized, err := adapter.Materialize(context.Background(), descriptor)
		if err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(materialized.CanonicalPath)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(content, []byte("must-not-upload")) || !bytes.Contains(content, []byte("fix the bug")) || !bytes.Contains(content, []byte("Read")) {
			t.Fatalf("canonical content=%s", content)
		}
		if run == 0 {
			first = content
		} else if !bytes.Equal(first, content) {
			t.Fatalf("materialize run %d changed bytes", run+1)
		}
	}
}

func TestMaterializeRejectsExportOutsidePrivateWorkspace(t *testing.T) {
	adapter := NewWithRunner(t.TempDir(), &fakeRunner{escape: true})
	sessions, diagnostics := adapter.Discover(context.Background(), sessionadapter.DiscoverOptions{})
	if len(diagnostics) != 0 || len(sessions) != 2 {
		t.Fatalf("sessions=%+v diagnostics=%+v", sessions, diagnostics)
	}
	if _, err := adapter.Materialize(context.Background(), sessions[0]); err == nil {
		t.Fatal("expected escaped export path to fail")
	}
}
