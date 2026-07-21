package canonical

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	SchemaV1          = "aida.session.event.v1"
	MaxEventLineBytes = 4 << 20
)

type EventType string

const (
	EventSessionMeta EventType = "session_meta"
	EventMessage     EventType = "message"
	EventToolCall    EventType = "tool_call"
	EventToolResult  EventType = "tool_result"
	EventUsage       EventType = "usage"
	EventResult      EventType = "result"
	EventError       EventType = "error"
)

type Event struct {
	Schema    string
	EventID   string
	Timestamp time.Time
	Type      EventType
	Payload   json.RawMessage
}

func WriteFile(path string, events []Event) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("canonical path is required")
	}
	seen := make(map[string]struct{}, len(events))
	var content bytes.Buffer
	for _, event := range events {
		line, err := marshalEvent(event, seen)
		if err != nil {
			return err
		}
		if len(line)+1 > MaxEventLineBytes {
			return fmt.Errorf("canonical event %q exceeds %d bytes", event.EventID, MaxEventLineBytes)
		}
		content.Write(line)
		content.WriteByte('\n')
	}

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".canonical-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content.Bytes()); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	if directoryHandle, err := os.Open(directory); err == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return nil
}

func marshalEvent(event Event, seen map[string]struct{}) ([]byte, error) {
	if event.Schema != SchemaV1 {
		return nil, fmt.Errorf("unsupported canonical schema %q", event.Schema)
	}
	event.EventID = strings.TrimSpace(event.EventID)
	if event.EventID == "" {
		return nil, errors.New("canonical event ID is required")
	}
	if _, exists := seen[event.EventID]; exists {
		return nil, fmt.Errorf("duplicate canonical event ID %q", event.EventID)
	}
	seen[event.EventID] = struct{}{}
	if event.Timestamp.IsZero() {
		return nil, fmt.Errorf("canonical event %q has no timestamp", event.EventID)
	}
	_, offset := event.Timestamp.Zone()
	if offset != 0 {
		return nil, fmt.Errorf("canonical event %q timestamp must be UTC", event.EventID)
	}
	if !supportedEventType(event.Type) {
		return nil, fmt.Errorf("unsupported canonical event type %q", event.Type)
	}
	var payload map[string]any
	if len(event.Payload) == 0 || json.Unmarshal(event.Payload, &payload) != nil || payload == nil {
		return nil, fmt.Errorf("canonical event %q payload must be an object", event.EventID)
	}
	wire := struct {
		Schema    string         `json:"schema"`
		EventID   string         `json:"event_id"`
		Timestamp time.Time      `json:"timestamp"`
		Type      EventType      `json:"type"`
		Payload   map[string]any `json:"payload"`
	}{event.Schema, event.EventID, event.Timestamp.UTC(), event.Type, payload}
	return json.Marshal(wire)
}

func supportedEventType(eventType EventType) bool {
	switch eventType {
	case EventSessionMeta, EventMessage, EventToolCall, EventToolResult, EventUsage, EventResult, EventError:
		return true
	default:
		return false
	}
}
