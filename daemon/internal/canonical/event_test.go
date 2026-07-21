package canonical_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aidashboard/daemon/internal/canonical"
)

func TestWriteFileIsDeterministicAndRejectsDuplicateEventIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	events := []canonical.Event{{
		Schema: canonical.SchemaV1, EventID: "event-1",
		Timestamp: time.Date(2026, 7, 21, 1, 2, 3, 123000000, time.UTC), Type: canonical.EventMessage,
		Payload: []byte(`{"role":"user","message":"hello","type":"user_message"}`),
	}}
	if err := canonical.WriteFile(path, events); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"schema\":\"aida.session.event.v1\",\"event_id\":\"event-1\",\"timestamp\":\"2026-07-21T01:02:03.123Z\",\"type\":\"message\",\"payload\":{\"message\":\"hello\",\"role\":\"user\",\"type\":\"user_message\"}}\n"
	if string(first) != want {
		t.Fatalf("canonical bytes changed\n got=%s\nwant=%s", first, want)
	}
	if err := canonical.WriteFile(path, events); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(second) != string(first) {
		t.Fatalf("second materialization differs\nfirst=%s\nsecond=%s", first, second)
	}

	duplicate := append(append([]canonical.Event(nil), events...), events[0])
	if err := canonical.WriteFile(path, duplicate); err == nil {
		t.Fatal("duplicate event ID was accepted")
	}
	afterRejectedWrite, _ := os.ReadFile(path)
	if string(afterRejectedWrite) != string(first) {
		t.Fatal("rejected write replaced the last valid canonical file")
	}
}
