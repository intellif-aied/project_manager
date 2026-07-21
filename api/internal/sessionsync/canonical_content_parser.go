package sessionsync

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	canonicalContentSchemaV1     = "aida.session.event.v1"
	maxCanonicalContentLineBytes = 4 << 20
)

var ErrInvalidCanonicalContent = errors.New("invalid canonical content event")

type canonicalContentEnvelope struct {
	Schema    string          `json:"schema"`
	EventID   string          `json:"event_id"`
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// ParseCanonicalContentChunk validates the canonical wire contract and only
// projects events that can contribute readable report content. Control events
// still advance the cursor but never become report-source rows.
func ParseCanonicalContentChunk(reader io.Reader, startCursor int64) (ContentParseResult, error) {
	if reader == nil || startCursor < 0 {
		return ContentParseResult{}, errors.New("reader and non-negative start cursor are required")
	}
	buffered := bufio.NewReaderSize(reader, 64<<10)
	result := ContentParseResult{EndCursor: startCursor}
	seen := map[string]struct{}{}
	for {
		line, err := readCompleteJSONLLine(buffered)
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return ContentParseResult{}, err
		}
		if len(line) > maxCanonicalContentLineBytes {
			return ContentParseResult{}, fmt.Errorf("%w: event line exceeds %d bytes", ErrInvalidCanonicalContent, maxCanonicalContentLineBytes)
		}
		lineStart := result.EndCursor
		result.EndCursor += int64(len(line))
		event, project, malformed, err := projectCanonicalContentLine(line, lineStart, result.EndCursor, seen)
		if err != nil {
			return ContentParseResult{}, err
		}
		if malformed {
			result.MalformedEventCount++
			continue
		}
		if project {
			result.Events = append(result.Events, event)
		}
	}
}

func projectCanonicalContentLine(rawLine []byte, startCursor, endCursor int64, seen map[string]struct{}) (ProjectedContentEvent, bool, bool, error) {
	trimmed := bytes.TrimSuffix(rawLine, []byte{'\n'})
	trimmed = bytes.TrimSuffix(trimmed, []byte{'\r'})
	if len(trimmed) == 0 || !utf8.Valid(trimmed) {
		return ProjectedContentEvent{}, false, true, nil
	}
	var envelope canonicalContentEnvelope
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return ProjectedContentEvent{}, false, true, nil
	}
	if envelope.Schema != canonicalContentSchemaV1 {
		return ProjectedContentEvent{}, false, false, fmt.Errorf("%w: unsupported schema %q", ErrInvalidCanonicalContent, envelope.Schema)
	}
	eventID := strings.TrimSpace(envelope.EventID)
	if eventID == "" {
		return ProjectedContentEvent{}, false, false, fmt.Errorf("%w: event_id is required", ErrInvalidCanonicalContent)
	}
	if _, exists := seen[eventID]; exists {
		return ProjectedContentEvent{}, false, false, fmt.Errorf("%w: duplicate event_id %q", ErrInvalidCanonicalContent, eventID)
	}
	seen[eventID] = struct{}{}
	if !canonicalContentTypeSupported(envelope.Type) {
		return ProjectedContentEvent{}, false, false, fmt.Errorf("%w: unsupported type %q", ErrInvalidCanonicalContent, envelope.Type)
	}
	var payload map[string]any
	if len(envelope.Payload) == 0 || json.Unmarshal(envelope.Payload, &payload) != nil || payload == nil {
		return ProjectedContentEvent{}, false, false, fmt.Errorf("%w: payload must be an object", ErrInvalidCanonicalContent)
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, envelope.Timestamp)
	if err != nil {
		return ProjectedContentEvent{}, false, true, nil
	}
	_, offset := occurredAt.Zone()
	if offset != 0 {
		return ProjectedContentEvent{}, false, true, nil
	}
	if envelope.Type == "usage" || envelope.Type == "session_meta" {
		return ProjectedContentEvent{}, false, false, nil
	}
	summary := canonicalReadableSummary(payload)
	if summary == "" {
		return ProjectedContentEvent{}, false, false, nil
	}
	sanitizedPayload, err := sanitizeProjectionJSON(trimmed)
	if err != nil {
		return ProjectedContentEvent{}, false, true, nil
	}
	sum := sha256.Sum256(rawLine)
	return ProjectedContentEvent{
		SourceStartCursor: startCursor,
		SourceEndCursor:   endCursor,
		OccurredAt:        occurredAt.UTC(),
		EventType:         "canonical." + envelope.Type,
		Summary:           truncateProjectionText(sanitizeProjectionText(summary), 500),
		Excerpt:           truncateProjectionText(sanitizeProjectionText(summary), 2000),
		Payload:           sanitizedPayload,
		ContentSHA256:     hex.EncodeToString(sum[:]),
	}, true, false, nil
}

func canonicalContentTypeSupported(value string) bool {
	switch value {
	case "session_meta", "message", "tool_call", "tool_result", "usage", "result", "error":
		return true
	default:
		return false
	}
}

func canonicalReadableSummary(payload map[string]any) string {
	for _, key := range []string{"message", "text", "content", "summary", "name", "result", "error"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
