package opencode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aidashboard/daemon/internal/canonical"
	"github.com/aidashboard/daemon/internal/sessionadapter"
)

type Runner interface {
	Run(context.Context, ...string) ([]byte, error)
}
type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "opencode", args...).Output()
}

type Adapter struct {
	runner Runner
	spool  string
}

func New(spool string) *Adapter { return &Adapter{runner: commandRunner{}, spool: spool} }
func NewWithRunner(spool string, runner Runner) *Adapter {
	return &Adapter{runner: runner, spool: spool}
}
func (*Adapter) ID() sessionadapter.ClientType { return "opencode" }
func (a *Adapter) Detect(ctx context.Context) sessionadapter.Detection {
	if _, err := exec.LookPath("opencode"); err != nil {
		return sessionadapter.Detection{Diagnostic: "opencode executable not found"}
	}
	out, err := a.runner.Run(ctx, "--version")
	if err != nil {
		return sessionadapter.Detection{Installed: true, Diagnostic: err.Error()}
	}
	return sessionadapter.Detection{Installed: true, NativeVersion: strings.TrimSpace(string(out))}
}

func (a *Adapter) Discover(ctx context.Context, options sessionadapter.DiscoverOptions) ([]sessionadapter.Descriptor, []sessionadapter.Diagnostic) {
	out, err := a.runner.Run(ctx, "session", "list", "--format", "json")
	if err != nil {
		return nil, []sessionadapter.Diagnostic{{ClientType: a.ID(), Code: "LIST_FAILED", Message: err.Error()}}
	}
	var rows []map[string]any
	if json.Unmarshal(out, &rows) != nil {
		return nil, []sessionadapter.Diagnostic{{ClientType: a.ID(), Code: "LIST_SCHEMA_UNSUPPORTED", Message: "opencode session list did not return a JSON array"}}
	}
	result := make([]sessionadapter.Descriptor, 0, len(rows))
	for _, row := range rows {
		id := text(row, "id", "sessionID", "session_id")
		if id == "" {
			continue
		}
		updated := instant(row, "updatedAt", "updated_at", "timeUpdated")
		if options.Since != nil && !updated.IsZero() && updated.Before(*options.Since) {
			continue
		}
		cwd := text(row, "directory", "cwd", "path")
		result = append(result, sessionadapter.Descriptor{ClientType: a.ID(), NativeSessionRef: id, ParentRef: text(row, "parentID", "parent_id", "parentSessionID"), StartedAt: instant(row, "createdAt", "created_at", "timeCreated"), LastActivityAt: updated, CWD: cwd, ProjectName: filepath.Base(cwd), Summary: text(row, "title", "summary"), Capability: sessionadapter.Capability{Content: true, Usage: sessionadapter.UsageUnavailable}, OpaqueLocator: id})
	}
	return result, nil
}

func (a *Adapter) Materialize(ctx context.Context, d sessionadapter.Descriptor) (sessionadapter.MaterializedSession, error) {
	if d.ClientType != a.ID() || d.NativeSessionRef == "" {
		return sessionadapter.MaterializedSession{}, errors.New("invalid opencode descriptor")
	}
	out, err := a.runner.Run(ctx, "export", d.NativeSessionRef)
	if err != nil {
		return sessionadapter.MaterializedSession{}, err
	}
	var exported any
	if json.Unmarshal(out, &exported) != nil {
		return sessionadapter.MaterializedSession{}, errors.New("opencode export is not JSON")
	}
	at := d.LastActivityAt
	if at.IsZero() {
		at = d.StartedAt
	}
	if at.IsZero() {
		return sessionadapter.MaterializedSession{}, errors.New("opencode export has no stable session timestamp")
	}
	events, err := canonicalExportEvents(exported, at)
	if err != nil {
		return sessionadapter.MaterializedSession{}, err
	}
	path := filepath.Join(a.spool, "opencode", safe(d.NativeSessionRef)+".jsonl")
	if err := canonical.WriteFile(path, events); err != nil {
		return sessionadapter.MaterializedSession{}, err
	}
	return sessionadapter.MaterializedSession{Descriptor: d, SourceFormat: "aida_event_v1", CanonicalPath: path, AdapterVersion: "opencode-v1", UsageCapability: sessionadapter.UsageUnavailable}, nil
}

func canonicalExportEvents(exported any, fallback time.Time) ([]canonical.Event, error) {
	root, _ := exported.(map[string]any)
	messages, _ := root["messages"].([]any)
	if len(messages) == 0 {
		messages = []any{exported}
	}
	events := make([]canonical.Event, 0, len(messages))
	seen := map[string]bool{}
	for _, message := range messages {
		raw, err := json.Marshal(message)
		if err != nil {
			return nil, err
		}
		id := nestedText(message, "id")
		if id == "" {
			sum := sha256.Sum256(raw)
			id = hex.EncodeToString(sum[:])
		}
		if seen[id] {
			return nil, fmt.Errorf("opencode export contains duplicate message id %q", id)
		}
		seen[id] = true
		at := nestedInstant(message)
		if at.IsZero() {
			at = fallback
		}
		payload, _ := json.Marshal(map[string]any{
			"role": canonicalMessageRole(nestedText(message, "role")), "phase": "unknown",
			"message": extractText(message), "native": message,
		})
		events = append(events, canonical.Event{Schema: canonical.SchemaV1, EventID: "opencode-message-" + id, Timestamp: at.UTC(), Type: canonical.EventMessage, Payload: payload})
	}
	return events, nil
}

func canonicalMessageRole(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "user":
		return "user"
	case "assistant":
		return "assistant"
	default:
		return "unknown"
	}
}

func nestedText(value any, key string) string {
	row, _ := value.(map[string]any)
	if result := text(row, key); result != "" {
		return result
	}
	if info, ok := row["info"].(map[string]any); ok {
		return text(info, key)
	}
	return ""
}

func nestedInstant(value any) time.Time {
	row, _ := value.(map[string]any)
	if result := instant(row, "createdAt", "created_at", "timeCreated", "timestamp"); !result.IsZero() {
		return result
	}
	if info, ok := row["info"].(map[string]any); ok {
		return instant(info, "createdAt", "created_at", "timeCreated", "timestamp")
	}
	return time.Time{}
}

func text(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func instant(row map[string]any, keys ...string) time.Time {
	for _, key := range keys {
		switch value := row[key].(type) {
		case string:
			if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
				return parsed.UTC()
			}
		case float64:
			if value > 1e12 {
				return time.UnixMilli(int64(value)).UTC()
			}
			if value > 0 {
				return time.Unix(int64(value), 0).UTC()
			}
		}
	}
	return time.Time{}
}
func safe(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}
func extractText(value any) string {
	var parts []string
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				parts = append(parts, strings.TrimSpace(typed))
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for _, key := range []string{"text", "content", "message", "summary", "title"} {
				if item, ok := typed[key]; ok {
					walk(item)
				}
			}
		}
	}
	walk(value)
	return strings.Join(parts, "\n")
}

var _ sessionadapter.Adapter = (*Adapter)(nil)
