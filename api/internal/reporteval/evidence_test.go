package reporteval

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aidashboard/api/internal/contentreader"
	"github.com/aidashboard/api/internal/reportsource"
)

type evidenceReaderStub struct {
	events map[string][]contentreader.Event
}

func (stub evidenceReaderStub) Stream(
	_ context.Context,
	request contentreader.Request,
	consume func(contentreader.Event) error,
) (contentreader.Result, error) {
	events := stub.events[request.RevisionID]
	for _, event := range events {
		if err := consume(event); err != nil {
			return contentreader.Result{}, err
		}
	}
	return contentreader.Result{
		StartCursor: request.StartCursor, EndCursor: request.EndCursor, EventCount: int64(len(events)),
	}, nil
}

func TestEvidenceFreezerCapturesPreDigestEventsAndRedactsSecrets(t *testing.T) {
	when := time.Date(2026, 7, 30, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	selection := reportsource.Selection{Items: []reportsource.SelectionItem{{
		SliceID: "11111111-1111-4111-8111-111111111111", SessionID: "22222222-2222-4222-8222-222222222222",
		SourceID: "33333333-3333-4333-8333-333333333333", GenerationID: "44444444-4444-4444-8444-444444444444",
		ProjectionRevision: "55555555-5555-4555-8555-555555555555", SessionRef: "session-a", AgentType: "codex",
		ContentEpoch: 3, StartCursor: 10, EndCursor: 20,
	}}}
	reader := evidenceReaderStub{events: map[string][]contentreader.Event{
		"55555555-5555-4555-8555-555555555555": {{
			OccurredAt: when, EventType: "assistant", Summary: "完成协议设计",
			Excerpt: "Authorization: Bearer secret-value",
			Payload: json.RawMessage(`{"text":"token=secret-value","usage":{"input_tokens":99},"project":"baigong"}`),
		}},
	}}
	snapshot, err := (EvidenceFreezer{Reader: reader}).Freeze(context.Background(), selection)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SourceIdentitySHA256 == "" || len(snapshot.Items) != 1 || len(snapshot.Items[0].Events) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	event := snapshot.Items[0].Events[0]
	if event.EvidenceRef != "source-001/event-000001" || event.OccurredAt.Location() != time.UTC {
		t.Fatalf("event = %#v", event)
	}
	encoded := string(event.Payload)
	if strings.Contains(encoded, "secret-value") || strings.Contains(encoded, "input_tokens") || !strings.Contains(encoded, "baigong") {
		t.Fatalf("payload was not safely redacted: %s", encoded)
	}
}

func TestEvidenceFreezerRejectsTruncation(t *testing.T) {
	selection := reportsource.Selection{Items: []reportsource.SelectionItem{{
		SliceID: "11111111-1111-4111-8111-111111111111", SessionID: "22222222-2222-4222-8222-222222222222",
		SourceID: "33333333-3333-4333-8333-333333333333", GenerationID: "44444444-4444-4444-8444-444444444444",
		ProjectionRevision: "55555555-5555-4555-8555-555555555555", SessionRef: "session-a", AgentType: "codex",
		ContentEpoch: 3, StartCursor: 10, EndCursor: 20,
	}}}
	reader := evidenceReaderStub{events: map[string][]contentreader.Event{
		"55555555-5555-4555-8555-555555555555": {{
			OccurredAt: time.Now(), EventType: "assistant", Payload: json.RawMessage(`{"text":"one"}`),
		}},
	}}
	_, err := (EvidenceFreezer{Reader: reader, MaxEvents: 0, MaxBytes: 4}).Freeze(context.Background(), selection)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected evidence size failure, got %v", err)
	}
}

func TestValidateBaselineSourceRefsRejectsUnknownEvent(t *testing.T) {
	source := SourceEvidence{
		SchemaVersion: SourceSchemaVersion, SourceIdentitySHA256: strings.Repeat("a", 64),
		RedactionVersion: EvidenceRedactionVersion,
		Items: []SourceEvidenceItem{{EvidenceSourceID: "source-001", AgentType: "codex", Events: []SourceEvidenceEvent{{
			EvidenceRef: "source-001/event-000001", OccurredAt: time.Date(2026, 6, 11, 1, 0, 0, 0, time.UTC), EventType: "assistant", Payload: json.RawMessage(`{}`),
		}}}},
	}
	baseline := validDataset().Cases[0].EvidenceBaseline
	baseline.Items[0].SourceRefs = []string{"source-001/event-999999"}
	if err := ValidateBaselineSourceRefs(baseline, source, "2026-06-11"); err == nil {
		t.Fatal("expected unknown source reference to be rejected")
	}
}

func TestValidateBaselineSourceRefsEnforcesBusinessDateForReportableFacts(t *testing.T) {
	source := SourceEvidence{
		SchemaVersion: SourceSchemaVersion, SourceIdentitySHA256: strings.Repeat("a", 64),
		RedactionVersion: EvidenceRedactionVersion,
		Items: []SourceEvidenceItem{{EvidenceSourceID: "source-001", AgentType: "codex", Events: []SourceEvidenceEvent{{
			EvidenceRef: "source-001/event-000001",
			OccurredAt:  time.Date(2026, 6, 10, 16, 30, 0, 0, time.UTC),
			EventType:   "assistant", Payload: json.RawMessage(`{}`),
		}, {
			EvidenceRef: "source-001/event-000002",
			OccurredAt:  time.Date(2026, 6, 11, 16, 30, 0, 0, time.UTC),
			EventType:   "assistant", Payload: json.RawMessage(`{}`),
		}}}},
	}
	baseline := validDataset().Cases[0].EvidenceBaseline
	baseline.Items[0].SourceRefs = []string{"source-001/event-000002"}
	if err := ValidateBaselineSourceRefs(baseline, source, "2026-06-11"); err == nil || !strings.Contains(err.Error(), "outside report date") {
		t.Fatalf("expected adjacent-day required evidence to fail, got %v", err)
	}

	baseline.Items[0].Disposition = "exclude"
	if err := ValidateBaselineSourceRefs(baseline, source, "2026-06-11"); err != nil {
		t.Fatalf("exclude evidence may document an adjacent-day fact: %v", err)
	}
	baseline.Items[0].Disposition = "required"
	baseline.Items[0].SourceRefs = []string{"source-001/event-000001"}
	if err := ValidateBaselineSourceRefs(baseline, source, "2026-06-11"); err != nil {
		t.Fatalf("same Shanghai business-date evidence should pass: %v", err)
	}
}
