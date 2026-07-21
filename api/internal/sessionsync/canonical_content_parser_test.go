package sessionsync

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestCanonicalContentParserProjectsReadableEventsAndFiltersControls(t *testing.T) {
	content := []byte(
		`{"schema":"aida.session.event.v1","event_id":"meta-1","timestamp":"2026-07-21T01:00:00Z","type":"session_meta","payload":{"summary":"control"}}` + "\n" +
			`{"schema":"aida.session.event.v1","event_id":"message-1","timestamp":"2026-07-21T01:00:01Z","type":"message","payload":{"message":"hello"}}` + "\n" +
			`{"schema":"aida.session.event.v1","event_id":"usage-1","timestamp":"2026-07-21T01:00:02Z","type":"usage","payload":{"total_tokens":10}}` + "\n" +
			`{"schema":"aida.session.event.v1","event_id":"tool-1","timestamp":"2026-07-21T01:00:03Z","type":"tool_call","payload":{"name":"Read"}}` + "\n",
	)
	result, err := ParseCanonicalContentChunk(bytes.NewReader(content), 17)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 || result.MalformedEventCount != 0 || result.EndCursor != 17+int64(len(content)) {
		t.Fatalf("result=%+v", result)
	}
	if result.Events[0].EventType != "canonical.message" || result.Events[0].Summary != "hello" || result.Events[1].EventType != "canonical.tool_call" || result.Events[1].Summary != "Read" {
		t.Fatalf("events=%+v", result.Events)
	}
}

func TestCanonicalContentParserRequiresUTCAndLineLimit(t *testing.T) {
	nonUTC := []byte(`{"schema":"aida.session.event.v1","event_id":"x","timestamp":"2026-07-21T09:00:00+08:00","type":"message","payload":{"message":"x"}}` + "\n")
	result, err := ParseCanonicalContentChunk(bytes.NewReader(nonUTC), 0)
	if err != nil || result.MalformedEventCount != 1 || len(result.Events) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	oversized := `{"schema":"aida.session.event.v1","event_id":"x","timestamp":"2026-07-21T01:00:00Z","type":"message","payload":{"message":"` + strings.Repeat("x", maxCanonicalContentLineBytes) + `"}}` + "\n"
	if _, err = ParseCanonicalContentChunk(strings.NewReader(oversized), 0); !errors.Is(err, ErrInvalidCanonicalContent) {
		t.Fatalf("oversized err=%v", err)
	}
}

func TestCanonicalContentParserRejectsUnknownSchemaAndDuplicateID(t *testing.T) {
	unknown := []byte(`{"schema":"other","event_id":"x","timestamp":"2026-07-21T01:00:00Z","type":"message","payload":{"message":"x"}}` + "\n")
	if _, err := ParseCanonicalContentChunk(bytes.NewReader(unknown), 0); !errors.Is(err, ErrInvalidCanonicalContent) {
		t.Fatalf("unknown schema err=%v", err)
	}
	line := `{"schema":"aida.session.event.v1","event_id":"same","timestamp":"2026-07-21T01:00:00Z","type":"message","payload":{"message":"x"}}` + "\n"
	if _, err := ParseCanonicalContentChunk(bytes.NewBufferString(line+line), 0); !errors.Is(err, ErrInvalidCanonicalContent) {
		t.Fatalf("duplicate err=%v", err)
	}
}

func TestCanonicalContentParserCountsBadTimestampAndRejectsNonObjectPayload(t *testing.T) {
	badTime := []byte(`{"schema":"aida.session.event.v1","event_id":"x","timestamp":"not-time","type":"message","payload":{"message":"x"}}` + "\n")
	result, err := ParseCanonicalContentChunk(bytes.NewReader(badTime), 0)
	if err != nil || result.MalformedEventCount != 1 || len(result.Events) != 0 || result.EndCursor != int64(len(badTime)) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	badPayload := []byte(`{"schema":"aida.session.event.v1","event_id":"x","timestamp":"2026-07-21T01:00:00Z","type":"message","payload":[]}` + "\n")
	if _, err = ParseCanonicalContentChunk(bytes.NewReader(badPayload), 0); !errors.Is(err, ErrInvalidCanonicalContent) {
		t.Fatalf("payload err=%v", err)
	}
}
