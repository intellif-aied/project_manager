package reporteval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/contentreader"
	"github.com/aidashboard/api/internal/reportsource"
	"github.com/aidashboard/api/internal/sessiondigest"
)

const (
	EvidenceRedactionVersion = "daily-report-evaluation-source-redaction/v1"
	defaultMaxEvidenceEvents = 100_000
	defaultMaxEvidenceBytes  = 64 << 20
)

type EvidenceContentReader interface {
	Stream(context.Context, contentreader.Request, func(contentreader.Event) error) (contentreader.Result, error)
}

// EvidenceFreezer hides source ordering, immutable range reads, redaction and
// evidence-ref assignment behind one interface used by the evaluation handler.
type EvidenceFreezer struct {
	Reader    EvidenceContentReader
	MaxEvents int
	MaxBytes  int
}

func (freezer EvidenceFreezer) Freeze(ctx context.Context, selection reportsource.Selection) (SourceEvidence, error) {
	if freezer.Reader == nil {
		return SourceEvidence{}, errors.New("evidence content reader is required")
	}
	items, identityHash, err := reportsource.CanonicalSourceIdentity(selection.Items)
	if err != nil {
		return SourceEvidence{}, err
	}
	if len(items) == 0 {
		return SourceEvidence{}, errors.New("source selection is empty")
	}
	maxEvents := freezer.MaxEvents
	if maxEvents <= 0 {
		maxEvents = defaultMaxEvidenceEvents
	}
	maxBytes := freezer.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxEvidenceBytes
	}

	snapshot := SourceEvidence{
		SchemaVersion: SourceSchemaVersion, SourceIdentitySHA256: identityHash,
		RedactionVersion: EvidenceRedactionVersion, Items: make([]SourceEvidenceItem, 0, len(items)),
	}
	totalEvents := 0
	totalBytes := 0
	for itemIndex, item := range items {
		sourceID := fmt.Sprintf("source-%03d", itemIndex+1)
		frozen := SourceEvidenceItem{
			EvidenceSourceID: sourceID, AgentType: strings.TrimSpace(item.AgentType),
			Events: []SourceEvidenceEvent{},
		}
		result, err := freezer.Reader.Stream(ctx, contentreader.Request{
			RevisionID: item.ProjectionRevision, StartCursor: item.StartCursor, EndCursor: item.EndCursor,
			ValidationMode: contentreader.ValidationIndexedRange,
		}, func(event contentreader.Event) error {
			totalEvents++
			if totalEvents > maxEvents {
				return fmt.Errorf("source evidence exceeds %d events", maxEvents)
			}
			payload, payloadErr := sanitizeEvidencePayload(event.Payload)
			if payloadErr != nil {
				return payloadErr
			}
			value := SourceEvidenceEvent{
				EvidenceRef: fmt.Sprintf("%s/event-%06d", sourceID, len(frozen.Events)+1),
				OccurredAt:  event.OccurredAt.UTC(), EventType: strings.TrimSpace(event.EventType),
				Summary: sessiondigest.Redact(event.Summary), Excerpt: sessiondigest.Redact(event.Excerpt),
				Payload: payload,
			}
			encoded, encodeErr := json.Marshal(value)
			if encodeErr != nil {
				return encodeErr
			}
			totalBytes += len(encoded)
			if totalBytes > maxBytes {
				return fmt.Errorf("source evidence exceeds %d bytes", maxBytes)
			}
			frozen.Events = append(frozen.Events, value)
			return nil
		})
		if err != nil {
			return SourceEvidence{}, fmt.Errorf("freeze %s: %w", sourceID, err)
		}
		if result.StartCursor != item.StartCursor || result.EndCursor != item.EndCursor || result.EventCount != int64(len(frozen.Events)) {
			return SourceEvidence{}, fmt.Errorf("freeze %s returned an incomplete immutable range", sourceID)
		}
		snapshot.Items = append(snapshot.Items, frozen)
	}
	if err := snapshot.Validate(); err != nil {
		return SourceEvidence{}, err
	}
	return snapshot, nil
}

func (source SourceEvidence) Validate() error {
	if source.SchemaVersion != SourceSchemaVersion || !isSHA256(source.SourceIdentitySHA256) {
		return errors.New("source evidence schema or identity is invalid")
	}
	if source.RedactionVersion != EvidenceRedactionVersion || len(source.Items) == 0 {
		return errors.New("source evidence redaction or items are invalid")
	}
	seenSources := map[string]bool{}
	seenRefs := map[string]bool{}
	for itemIndex, item := range source.Items {
		if !safeIdentifierPattern.MatchString(item.EvidenceSourceID) || seenSources[item.EvidenceSourceID] {
			return fmt.Errorf("items[%d].evidence_source_id is invalid", itemIndex)
		}
		seenSources[item.EvidenceSourceID] = true
		if strings.TrimSpace(item.AgentType) == "" || len(item.Events) == 0 {
			return fmt.Errorf("items[%d] requires agent_type and events", itemIndex)
		}
		for eventIndex, event := range item.Events {
			if event.EvidenceRef == "" || seenRefs[event.EvidenceRef] || !strings.HasPrefix(event.EvidenceRef, item.EvidenceSourceID+"/") {
				return fmt.Errorf("items[%d].events[%d].evidence_ref is invalid", itemIndex, eventIndex)
			}
			seenRefs[event.EvidenceRef] = true
			if event.OccurredAt.IsZero() || strings.TrimSpace(event.EventType) == "" || !json.Valid(event.Payload) {
				return fmt.Errorf("items[%d].events[%d] is incomplete", itemIndex, eventIndex)
			}
		}
	}
	return nil
}

func ValidateBaselineSourceRefs(baseline EvidenceBaseline, source SourceEvidence, reportDate string) error {
	if err := baseline.Validate("evidence_baseline"); err != nil {
		return err
	}
	if err := source.Validate(); err != nil {
		return err
	}
	if _, err := biztime.ParseDate(reportDate); err != nil {
		return fmt.Errorf("report date must be YYYY-MM-DD: %w", err)
	}
	available := map[string]SourceEvidenceEvent{}
	for _, item := range source.Items {
		for _, event := range item.Events {
			available[event.EvidenceRef] = event
		}
	}
	for _, item := range baseline.Items {
		for _, ref := range item.SourceRefs {
			event, ok := available[ref]
			if !ok {
				return fmt.Errorf("evidence item %s references unknown source %s", item.EvidenceID, ref)
			}
			// A daily-report baseline may retain adjacent-day evidence only to
			// document why it must be excluded. Required and optional facts must
			// be observable on the evaluated Shanghai business date.
			if item.Disposition != "exclude" && biztime.Date(event.OccurredAt) != reportDate {
				return fmt.Errorf(
					"evidence item %s references source %s on %s outside report date %s",
					item.EvidenceID, ref, biztime.Date(event.OccurredAt), reportDate,
				)
			}
		}
	}
	return nil
}

var evidenceUsageKeys = map[string]bool{
	"usage": true, "token_usage": true, "total_token_usage": true, "last_token_usage": true,
	"input_tokens": true, "output_tokens": true, "cached_input_tokens": true,
	"cache_read_input_tokens": true, "cache_creation_input_tokens": true,
	"cache_read_tokens": true, "cache_creation_tokens": true, "reasoning_output_tokens": true,
	"total_tokens": true,
}

func sanitizeEvidencePayload(payload json.RawMessage) (json.RawMessage, error) {
	if len(payload) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, errors.New("source event payload is invalid JSON")
	}
	value = sanitizeEvidenceValue(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func sanitizeEvidenceValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if evidenceUsageKeys[strings.ToLower(strings.TrimSpace(key))] {
				delete(typed, key)
				continue
			}
			typed[key] = sanitizeEvidenceValue(item)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = sanitizeEvidenceValue(item)
		}
		return typed
	case string:
		return sessiondigest.Redact(typed)
	default:
		return value
	}
}
