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

const MaxContentProjectionLineBytes = 8 << 20

var (
	ErrContentLineTooLarge = errors.New("content projection line exceeds limit")
	ErrIncompleteJSONLLine = errors.New("content projection chunk ends with an incomplete JSONL line")
)

type ProjectedContentEvent struct {
	SourceStartCursor int64
	SourceEndCursor   int64
	OccurredAt        time.Time
	EventType         string
	Summary           string
	Excerpt           string
	Payload           json.RawMessage
	ContentSHA256     string
}

type ContentParseResult struct {
	Events              []ProjectedContentEvent
	MalformedEventCount int64
	EndCursor           int64
}

func ParseContentChunk(reader io.Reader, startCursor int64, fallbackTime *time.Time) (ContentParseResult, error) {
	if reader == nil || startCursor < 0 {
		return ContentParseResult{}, errors.New("reader and non-negative start cursor are required")
	}
	buffered := bufio.NewReaderSize(reader, 64<<10)
	result := ContentParseResult{EndCursor: startCursor}
	for {
		line, err := readCompleteJSONLLine(buffered)
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return ContentParseResult{}, err
		}
		lineStart := result.EndCursor
		result.EndCursor += int64(len(line))
		event, ok := projectContentLine(line, lineStart, result.EndCursor, fallbackTime)
		if !ok {
			result.MalformedEventCount++
			continue
		}
		result.Events = append(result.Events, event)
	}
}

func readCompleteJSONLLine(reader *bufio.Reader) ([]byte, error) {
	var line bytes.Buffer
	for {
		fragment, err := reader.ReadSlice('\n')
		if line.Len()+len(fragment) > MaxContentProjectionLineBytes {
			return nil, ErrContentLineTooLarge
		}
		line.Write(fragment)
		switch {
		case err == nil:
			return append([]byte(nil), line.Bytes()...), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && line.Len() == 0:
			return nil, io.EOF
		case errors.Is(err, io.EOF):
			return nil, ErrIncompleteJSONLLine
		default:
			return nil, err
		}
	}
}

type contentEnvelope struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
	Message   json.RawMessage `json:"message"`
}

func projectContentLine(
	rawLine []byte,
	startCursor, endCursor int64,
	fallbackTime *time.Time,
) (ProjectedContentEvent, bool) {
	trimmed := bytes.TrimSuffix(rawLine, []byte{'\n'})
	trimmed = bytes.TrimSuffix(trimmed, []byte{'\r'})
	if len(trimmed) == 0 || !utf8.Valid(trimmed) {
		return ProjectedContentEvent{}, false
	}
	var envelope contentEnvelope
	if err := json.Unmarshal(trimmed, &envelope); err != nil || strings.TrimSpace(envelope.Type) == "" {
		return ProjectedContentEvent{}, false
	}
	occurredAt, ok := eventTime(envelope.Timestamp, fallbackTime)
	if !ok {
		return ProjectedContentEvent{}, false
	}
	eventType, summary := eventTypeAndSummary(envelope)
	if eventType == "" {
		return ProjectedContentEvent{}, false
	}
	sum := sha256.Sum256(rawLine)
	return ProjectedContentEvent{
		SourceStartCursor: startCursor,
		SourceEndCursor:   endCursor,
		OccurredAt:        occurredAt,
		EventType:         eventType,
		Summary:           truncateProjectionText(summary, 500),
		Excerpt:           truncateProjectionText(summary, 2000),
		Payload:           append(json.RawMessage(nil), trimmed...),
		ContentSHA256:     hex.EncodeToString(sum[:]),
	}, true
}

func eventTime(value string, fallback *time.Time) (time.Time, bool) {
	if value != "" {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	if fallback != nil && !fallback.IsZero() {
		return fallback.UTC(), true
	}
	return time.Time{}, false
}

func eventTypeAndSummary(envelope contentEnvelope) (string, string) {
	eventType := strings.TrimSpace(envelope.Type)
	if len(envelope.Payload) > 0 {
		var payload struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Name    string `json:"name"`
		}
		if json.Unmarshal(envelope.Payload, &payload) == nil {
			if subtype := strings.TrimSpace(payload.Type); subtype != "" {
				eventType += "." + subtype
			}
			if text := strings.TrimSpace(payload.Message); text != "" {
				return eventType, text
			}
			if name := strings.TrimSpace(payload.Name); name != "" {
				return eventType, name
			}
		}
	}
	if len(envelope.Message) > 0 {
		var message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
				Name string `json:"name"`
			} `json:"content"`
		}
		if json.Unmarshal(envelope.Message, &message) == nil {
			parts := make([]string, 0, len(message.Content))
			for _, content := range message.Content {
				if text := strings.TrimSpace(content.Text); text != "" {
					parts = append(parts, text)
				} else if name := strings.TrimSpace(content.Name); name != "" {
					parts = append(parts, name)
				}
			}
			return eventType, strings.Join(parts, "\n")
		}
	}
	return eventType, ""
}

func truncateProjectionText(value string, limit int) string {
	if limit <= 0 || value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return fmt.Sprintf("%s...", string(runes[:limit-3]))
}
