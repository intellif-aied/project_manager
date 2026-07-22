package openclaw

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aidashboard/daemon/internal/canonical"
	"github.com/aidashboard/daemon/internal/sessionadapter"
)

const maxTrajectoryBundleBytes = 64 << 20

type Runner interface {
	Run(context.Context, ...string) ([]byte, error)
}

type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "openclaw", args...).Output()
}

type Adapter struct {
	runner Runner
	spool  string
}

func New(spool string) *Adapter { return &Adapter{runner: commandRunner{}, spool: spool} }
func NewWithRunner(spool string, runner Runner) *Adapter {
	return &Adapter{runner: runner, spool: spool}
}
func (*Adapter) ID() sessionadapter.ClientType { return "openclaw" }

func (a *Adapter) Detect(ctx context.Context) sessionadapter.Detection {
	if _, err := exec.LookPath("openclaw"); err != nil {
		return sessionadapter.Detection{Diagnostic: "openclaw executable not found"}
	}
	out, err := a.runner.Run(ctx, "--version")
	if err != nil {
		return sessionadapter.Detection{Installed: true, Diagnostic: err.Error()}
	}
	return sessionadapter.Detection{Installed: true, NativeVersion: strings.TrimSpace(string(out))}
}

func (a *Adapter) Discover(ctx context.Context, options sessionadapter.DiscoverOptions) ([]sessionadapter.Descriptor, []sessionadapter.Diagnostic) {
	out, err := a.runner.Run(ctx, "sessions", "--all-agents", "--limit", "all", "--json")
	if err != nil {
		return nil, []sessionadapter.Diagnostic{{ClientType: a.ID(), Code: "LIST_FAILED", Message: err.Error()}}
	}
	var response struct {
		Sessions []map[string]any `json:"sessions"`
		HasMore  bool             `json:"hasMore"`
	}
	if json.Unmarshal(out, &response) != nil {
		return nil, []sessionadapter.Diagnostic{{ClientType: a.ID(), Code: "LIST_SCHEMA_UNSUPPORTED", Message: "openclaw sessions did not return the documented JSON object"}}
	}
	if response.HasMore {
		return nil, []sessionadapter.Diagnostic{{ClientType: a.ID(), Code: "LIST_TRUNCATED", Message: "openclaw session list was unexpectedly truncated"}}
	}
	refsByKey := make(map[string]string, len(response.Sessions))
	for _, row := range response.Sessions {
		key, agentID, sessionID := text(row, "key"), text(row, "agentId"), text(row, "sessionId")
		if key == "" || agentID == "" || sessionID == "" || backgroundSessionKey(key) {
			continue
		}
		refsByKey[key] = stableRef(agentID + "\n" + sessionID)
	}
	result := make([]sessionadapter.Descriptor, 0, len(response.Sessions))
	seenRef := make(map[string]bool, len(response.Sessions))
	for _, row := range response.Sessions {
		key, agentID, sessionID := text(row, "key"), text(row, "agentId"), text(row, "sessionId")
		ref, eligible := refsByKey[key]
		if !eligible || seenRef[ref] {
			continue
		}
		seenRef[ref] = true
		updated := instant(row, "lastInteractionAt", "updatedAt")
		if options.Since != nil && !updated.IsZero() && updated.Before(*options.Since) {
			continue
		}
		parentKey, spawnedBy := text(row, "parentSessionKey"), text(row, "spawnedBy")
		if parentKey == "" {
			parentKey = spawnedBy
		} else if spawnedBy != "" && spawnedBy != parentKey {
			parentKey = ""
		}
		parentRef := ""
		if candidate, exists := refsByKey[parentKey]; exists {
			parentRef = candidate
		}
		started := instant(row, "sessionStartedAt")
		summary := text(row, "label", "displayName")
		if summary == "" {
			summary = strings.TrimSpace(strings.Join([]string{agentID, text(row, "model")}, " "))
		}
		locator, _ := json.Marshal(opaqueLocator{SessionKey: key, SessionID: sessionID, AgentID: agentID})
		result = append(result, sessionadapter.Descriptor{
			ClientType: a.ID(), NativeSessionRef: ref, ParentRef: parentRef,
			ForkedAt: started, ForkSource: forkSource(parentKey), StartedAt: started, LastActivityAt: updated,
			Summary: summary, Capability: sessionadapter.Capability{Content: true, Usage: sessionadapter.UsageUnavailable},
			OpaqueLocator: string(locator),
		})
	}
	return result, nil
}

func (a *Adapter) Materialize(ctx context.Context, descriptor sessionadapter.Descriptor) (sessionadapter.MaterializedSession, error) {
	if descriptor.ClientType != a.ID() || descriptor.NativeSessionRef == "" || strings.TrimSpace(descriptor.OpaqueLocator) == "" {
		return sessionadapter.MaterializedSession{}, errors.New("invalid OpenClaw descriptor")
	}
	var locator opaqueLocator
	if json.Unmarshal([]byte(descriptor.OpaqueLocator), &locator) != nil || locator.SessionKey == "" || locator.SessionID == "" || locator.AgentID == "" || descriptor.NativeSessionRef != stableRef(locator.AgentID+"\n"+locator.SessionID) {
		return sessionadapter.MaterializedSession{}, errors.New("invalid OpenClaw private locator")
	}
	workspace, err := os.MkdirTemp("", "aida-openclaw-export-*")
	if err != nil {
		return sessionadapter.MaterializedSession{}, err
	}
	defer os.RemoveAll(workspace)
	if err := os.Chmod(workspace, 0700); err != nil {
		return sessionadapter.MaterializedSession{}, err
	}
	outputName := "aida-" + safeName(descriptor.NativeSessionRef)
	out, err := a.runner.Run(ctx, "sessions", "export-trajectory", "--session-key", locator.SessionKey, "--workspace", workspace, "--output", outputName, "--json")
	if err != nil {
		return sessionadapter.MaterializedSession{}, err
	}
	var exported struct {
		OutputDir string `json:"outputDir"`
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(out, &exported) != nil || strings.TrimSpace(exported.OutputDir) == "" {
		return sessionadapter.MaterializedSession{}, errors.New("OpenClaw trajectory export did not return outputDir")
	}
	exportDir, err := trustedExportPath(workspace, exported.OutputDir)
	if err != nil {
		return sessionadapter.MaterializedSession{}, err
	}
	manifestPath, err := trustedRegularFile(exportDir, filepath.Join(exportDir, "manifest.json"))
	if err != nil {
		return sessionadapter.MaterializedSession{}, err
	}
	manifestContent, err := os.ReadFile(manifestPath)
	if err != nil {
		return sessionadapter.MaterializedSession{}, err
	}
	var manifest struct {
		TraceSchema   string `json:"traceSchema"`
		SchemaVersion int    `json:"schemaVersion"`
		SessionID     string `json:"sessionId"`
		SessionKey    string `json:"sessionKey"`
	}
	if json.Unmarshal(manifestContent, &manifest) != nil || manifest.TraceSchema != "openclaw-trajectory" || manifest.SchemaVersion < 1 || manifest.SessionKey != locator.SessionKey || manifest.SessionID != locator.SessionID || (exported.SessionID != "" && exported.SessionID != manifest.SessionID) {
		return sessionadapter.MaterializedSession{}, errors.New("OpenClaw trajectory manifest identity or schema is invalid")
	}
	eventsPath, err := trustedRegularFile(exportDir, filepath.Join(exportDir, "events.jsonl"))
	if err != nil {
		return sessionadapter.MaterializedSession{}, err
	}
	info, err := os.Stat(eventsPath)
	if err != nil {
		return sessionadapter.MaterializedSession{}, err
	}
	if info.Size() > maxTrajectoryBundleBytes {
		return sessionadapter.MaterializedSession{}, errors.New("OpenClaw trajectory events exceed Aida size limit")
	}
	events, err := readTranscriptEvents(eventsPath, descriptor.NativeSessionRef, manifest.SessionID)
	if err != nil {
		return sessionadapter.MaterializedSession{}, err
	}
	if len(events) == 0 {
		return sessionadapter.MaterializedSession{}, errors.New("OpenClaw trajectory contains no readable transcript events")
	}
	canonicalPath := filepath.Join(a.spool, "openclaw", safeName(descriptor.NativeSessionRef)+".jsonl")
	if err := canonical.WriteFile(canonicalPath, events); err != nil {
		return sessionadapter.MaterializedSession{}, err
	}
	return sessionadapter.MaterializedSession{Descriptor: descriptor, SourceFormat: "aida_event_v1", CanonicalPath: canonicalPath, AdapterVersion: "openclaw-v1", UsageCapability: sessionadapter.UsageUnavailable}, nil
}

type trajectoryEvent struct {
	TraceSchema   string         `json:"traceSchema"`
	SchemaVersion int            `json:"schemaVersion"`
	TraceID       string         `json:"traceId"`
	Source        string         `json:"source"`
	Type          string         `json:"type"`
	Timestamp     string         `json:"ts"`
	SourceSeq     int64          `json:"sourceSeq"`
	SessionID     string         `json:"sessionId"`
	EntryID       string         `json:"entryId"`
	Data          map[string]any `json:"data"`
}

type opaqueLocator struct {
	SessionKey string `json:"session_key"`
	SessionID  string `json:"session_id"`
	AgentID    string `json:"agent_id"`
}

func readTranscriptEvents(path, sessionRef, sessionID string) ([]canonical.Event, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var result []canonical.Event
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), canonical.MaxEventLineBytes)
	for scanner.Scan() {
		var event trajectoryEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.TraceSchema != "openclaw-trajectory" || event.SchemaVersion < 1 || event.SessionID != sessionID || event.TraceID == "" || event.SourceSeq <= 0 {
			return nil, errors.New("OpenClaw trajectory contains an invalid event")
		}
		if !transcriptSource(event.Source) {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, event.Timestamp)
		if err != nil {
			return nil, errors.New("OpenClaw transcript event has no stable timestamp")
		}
		eventType, role, summary, include := projectTranscriptEvent(event)
		if !include {
			continue
		}
		canonicalPayload := map[string]any{"source_event_type": event.Type, "entry_id": event.EntryID}
		switch eventType {
		case canonical.EventMessage:
			canonicalPayload["message"] = summary
			canonicalPayload["role"] = role
			canonicalPayload["phase"] = "unknown"
		case canonical.EventToolCall:
			canonicalPayload["call_id"] = canonicalToolCallID(event.Data)
			canonicalPayload["name"] = summary
			canonicalPayload["command"] = text(event.Data, "commandSummary", "command_summary")
		case canonical.EventToolResult:
			canonicalPayload["call_id"] = canonicalToolCallID(event.Data)
			canonicalPayload["status"] = canonicalToolStatus(event.Data)
			canonicalPayload["output_summary"] = summary
		default:
			canonicalPayload["message"] = summary
		}
		payload, _ := json.Marshal(canonicalPayload)
		identity := fmt.Sprintf("%s|%s|%d|%s", sessionRef, event.EntryID, event.SourceSeq, event.Type)
		sum := sha256.Sum256([]byte(identity))
		result = append(result, canonical.Event{Schema: canonical.SchemaV1, EventID: "openclaw-" + hex.EncodeToString(sum[:]), Timestamp: at.UTC(), Type: eventType, Payload: payload})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func canonicalToolCallID(data map[string]any) string {
	return text(data, "callId", "call_id", "toolCallId", "tool_call_id")
}

func canonicalToolStatus(data map[string]any) string {
	if readableText(data["error"]) != "" {
		return "failure"
	}
	switch strings.ToLower(text(data, "status")) {
	case "success", "succeeded", "passed", "ok":
		return "success"
	case "failure", "failed", "error":
		return "failure"
	default:
		return "unknown"
	}
}

func projectTranscriptEvent(event trajectoryEvent) (canonical.EventType, string, string, bool) {
	message := event.Data["message"]
	if role := messageRole(message); role == "user" || role == "assistant" {
		summary := readableText(message)
		return canonical.EventMessage, role, summary, summary != ""
	}

	normalizedType := normalizeEventType(event.Type)
	switch {
	case normalizedType == "message.user", normalizedType == "user.message":
		summary := readableText(message)
		return canonical.EventMessage, "user", summary, summary != ""
	case normalizedType == "message.assistant", normalizedType == "assistant.message":
		summary := readableText(message)
		return canonical.EventMessage, "assistant", summary, summary != ""
	case normalizedType == "tool.call" || strings.HasSuffix(normalizedType, ".tool.call"):
		summary := text(event.Data, "name", "toolName")
		return canonical.EventToolCall, "", summary, summary != ""
	case normalizedType == "tool.result" || strings.HasSuffix(normalizedType, ".tool.result"):
		summary := readableText(message)
		if summary == "" {
			summary = readableText(event.Data["result"])
		}
		if summary == "" {
			summary = readableText(event.Data["error"])
		}
		return canonical.EventToolResult, "", summary, summary != ""
	default:
		return "", "", "", false
	}
}

func transcriptSource(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "transcript" || strings.HasPrefix(normalized, "transcript.") || strings.HasPrefix(normalized, "transcript-")
}

func messageRole(value any) string {
	message, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return strings.ToLower(text(message, "role"))
}

func normalizeEventType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer("_", ".", "-", ".").Replace(normalized)
	for strings.Contains(normalized, "..") {
		normalized = strings.ReplaceAll(normalized, "..", ".")
	}
	return strings.Trim(normalized, ".")
}

func trustedExportPath(workspace, value string) (string, error) {
	root, err := filepath.EvalSymlinks(filepath.Join(workspace, ".openclaw", "trajectory-exports"))
	if err != nil {
		return "", err
	}
	candidate, err := filepath.EvalSymlinks(value)
	if err != nil {
		return "", err
	}
	if err := requireInside(root, candidate); err != nil {
		return "", err
	}
	return candidate, nil
}

func trustedRegularFile(root, value string) (string, error) {
	info, err := os.Lstat(value)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("OpenClaw export file must be a regular non-symlink file")
	}
	candidate, err := filepath.EvalSymlinks(value)
	if err != nil {
		return "", err
	}
	if err := requireInside(root, candidate); err != nil {
		return "", err
	}
	return candidate, nil
}

func requireInside(root, candidate string) error {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("OpenClaw export path is outside the private workspace")
	}
	return nil
}

func text(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key].(string); ok {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func instant(row map[string]any, keys ...string) time.Time {
	for _, key := range keys {
		switch value := row[key].(type) {
		case float64:
			if value > 0 {
				return time.UnixMilli(int64(value)).UTC()
			}
		case string:
			if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
				return parsed.UTC()
			}
		}
	}
	return time.Time{}
}

func readableText(value any) string {
	var parts []string
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case string:
			if trimmed := strings.TrimSpace(typed); trimmed != "" {
				parts = append(parts, trimmed)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for _, key := range []string{"text", "content", "message", "result", "error"} {
				if item, ok := typed[key]; ok {
					walk(item)
				}
			}
		}
	}
	walk(value)
	return strings.Join(parts, "\n")
}

func stableRef(identity string) string { return "openclaw-" + safeName(identity) }
func safeName(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}
func forkSource(parent string) string {
	if parent == "" {
		return ""
	}
	return "parentSessionKey"
}

func backgroundSessionKey(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"cron:", "hook:", "heartbeat:", "acp:", "model-run-"} {
		if strings.HasPrefix(lower, marker) || strings.Contains(lower, ":"+marker) {
			return true
		}
	}
	return false
}

var _ sessionadapter.Adapter = (*Adapter)(nil)
