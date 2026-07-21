package kimicode

import (
	"bytes"
	"context"
	"github.com/aidashboard/daemon/internal/sessionadapter"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverAndMaterializeDocumentedSessionFiles(t *testing.T) {
	home := t.TempDir()
	session := filepath.Join(home, "sessions", "wd_p", "s1")
	for _, agent := range []string{"main", "agent-0"} {
		if err := os.MkdirAll(filepath.Join(session, "agents", agent), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(session, "agents", agent, "wire.jsonl"), []byte(`{"timestamp":"2026-07-21T01:30:00Z","type":"message","content":"hello"}`+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(`{"sessionId":"s1","sessionDir":"sessions/wd_p/s1","workDir":"/tmp/p"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(session, "state.json"), []byte(`{"title":"Task","createdAt":"2026-07-21T01:00:00Z","updatedAt":"2026-07-21T02:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}
	adapter := New(home, t.TempDir())
	sessions, diagnostics := adapter.Discover(context.Background(), sessionadapter.DiscoverOptions{})
	if len(diagnostics) != 0 || len(sessions) != 2 {
		t.Fatalf("sessions=%+v diagnostics=%+v", sessions, diagnostics)
	}
	var child sessionadapter.Descriptor
	for _, descriptor := range sessions {
		if descriptor.ParentRef == "s1" {
			child = descriptor
		}
	}
	if child.NativeSessionRef != "s1:agent-0" {
		t.Fatalf("child=%+v", child)
	}
	materialized, err := adapter.Materialize(context.Background(), child)
	if err != nil {
		t.Fatal(err)
	}
	if materialized.UsageCapability != sessionadapter.UsageUnavailable {
		t.Fatalf("materialized=%+v", materialized)
	}
	if _, err = os.Stat(materialized.CanonicalPath); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(materialized.CanonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		again, materializeErr := adapter.Materialize(context.Background(), child)
		if materializeErr != nil {
			t.Fatal(materializeErr)
		}
		content, readErr := os.ReadFile(again.CanonicalPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(first, content) {
			t.Fatalf("materialize run %d produced different bytes", run+2)
		}
	}
}

func TestDiscoverRejectsStateSymlinkOutsideRoot(t *testing.T) {
	home := t.TempDir()
	session := filepath.Join(home, "sessions", "wd_p", "s1")
	if err := os.MkdirAll(session, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(outside, []byte(`{"title":"outside"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(session, "state.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(`{"sessionId":"s1","sessionDir":"sessions/wd_p/s1","workDir":"/tmp/p"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sessions, diagnostics := New(home, t.TempDir()).Discover(context.Background(), sessionadapter.DiscoverOptions{})
	if len(sessions) != 0 || len(diagnostics) != 1 || diagnostics[0].Code != "STATE_PATH_OUTSIDE_ROOT" {
		t.Fatalf("sessions=%+v diagnostics=%+v", sessions, diagnostics)
	}
}

func TestDiscoverRejectsSessionPathOutsideRoot(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(`{"sessionId":"escape","sessionDir":"/etc","workDir":"/tmp"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sessions, diagnostics := New(home, t.TempDir()).Discover(context.Background(), sessionadapter.DiscoverOptions{})
	if len(sessions) != 0 || len(diagnostics) != 1 || diagnostics[0].Code != "SESSION_PATH_OUTSIDE_ROOT" {
		t.Fatalf("sessions=%+v diagnostics=%+v", sessions, diagnostics)
	}
}
