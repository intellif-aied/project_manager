package kimicode

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aidashboard/daemon/internal/canonical"
	"github.com/aidashboard/daemon/internal/sessionadapter"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Adapter struct{ home, spool string }

func New(home, spool string) *Adapter {
	if home == "" {
		home = os.Getenv("KIMI_CODE_HOME")
	}
	if home == "" {
		if user, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(user, ".kimi-code")
		}
	}
	return &Adapter{home: home, spool: spool}
}
func (*Adapter) ID() sessionadapter.ClientType { return "kimi_code" }
func (a *Adapter) Detect(ctx context.Context) sessionadapter.Detection {
	_, pathErr := os.Stat(filepath.Join(a.home, "session_index.jsonl"))
	binary, binErr := exec.LookPath("kimi")
	if pathErr != nil && binErr != nil {
		return sessionadapter.Detection{Diagnostic: "Kimi Code index and executable not found"}
	}
	version := ""
	if binErr == nil {
		if out, err := exec.CommandContext(ctx, binary, "--version").Output(); err == nil {
			version = strings.TrimSpace(string(out))
		}
	}
	return sessionadapter.Detection{Installed: true, NativeVersion: version}
}
func (a *Adapter) Discover(_ context.Context, options sessionadapter.DiscoverOptions) ([]sessionadapter.Descriptor, []sessionadapter.Diagnostic) {
	file, err := os.Open(filepath.Join(a.home, "session_index.jsonl"))
	if err != nil {
		return nil, []sessionadapter.Diagnostic{{ClientType: a.ID(), Code: "INDEX_UNAVAILABLE", Message: err.Error()}}
	}
	defer file.Close()
	var result []sessionadapter.Descriptor
	var diagnostics []sessionadapter.Diagnostic
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		var row map[string]any
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			continue
		}
		id := stringField(row, "sessionId", "session_id")
		dir := stringField(row, "sessionDir", "session_dir")
		work := stringField(row, "workDir", "work_dir")
		if id == "" || dir == "" {
			continue
		}
		trustedDir, trustErr := a.trustedSessionPath(dir)
		if trustErr != nil {
			diagnostics = append(diagnostics, sessionadapter.Diagnostic{ClientType: a.ID(), Code: "SESSION_PATH_OUTSIDE_ROOT", Message: trustErr.Error()})
			continue
		}
		dir = trustedDir
		trustedState, stateErr := a.trustedSessionPath(filepath.Join(dir, "state.json"))
		if stateErr != nil {
			diagnostics = append(diagnostics, sessionadapter.Diagnostic{ClientType: a.ID(), Code: "STATE_PATH_OUTSIDE_ROOT", Message: stateErr.Error()})
			continue
		}
		state := readObject(trustedState)
		updated := timeField(state, "updatedAt", "updated_at")
		if options.Since != nil && !updated.IsZero() && updated.Before(*options.Since) {
			continue
		}
		wires, _ := filepath.Glob(filepath.Join(dir, "agents", "*", "wire.jsonl"))
		sort.Strings(wires)
		for _, wire := range wires {
			trustedWire, wireErr := a.trustedSessionPath(wire)
			if wireErr != nil {
				diagnostics = append(diagnostics, sessionadapter.Diagnostic{ClientType: a.ID(), Code: "WIRE_PATH_OUTSIDE_ROOT", Message: wireErr.Error()})
				continue
			}
			agentID := filepath.Base(filepath.Dir(trustedWire))
			ref, parent, forkSource, summary := id, stringField(state, "forkedFrom", "forked_from"), "state.forkedFrom", stringField(state, "title", "lastPrompt")
			if agentID != "main" {
				ref = id + ":" + agentID
				parent = id
				forkSource = "agents/" + agentID
				summary = strings.TrimSpace(summary + " [" + agentID + "]")
			}
			result = append(result, sessionadapter.Descriptor{ClientType: a.ID(), NativeSessionRef: ref, ParentRef: parent, ForkSource: forkSource, StartedAt: timeField(state, "createdAt", "created_at"), LastActivityAt: updated, CWD: work, ProjectName: filepath.Base(work), Summary: summary, Capability: sessionadapter.Capability{Content: true, Usage: sessionadapter.UsageUnavailable}, OpaqueLocator: trustedWire})
		}
	}
	if err := scanner.Err(); err != nil {
		diagnostics = append(diagnostics, sessionadapter.Diagnostic{ClientType: a.ID(), Code: "INDEX_READ_FAILED", Message: err.Error()})
	}
	return result, diagnostics
}
func (a *Adapter) Materialize(_ context.Context, d sessionadapter.Descriptor) (sessionadapter.MaterializedSession, error) {
	if d.ClientType != a.ID() || d.OpaqueLocator == "" {
		return sessionadapter.MaterializedSession{}, errors.New("invalid Kimi Code descriptor")
	}
	wire, err := a.trustedSessionPath(d.OpaqueLocator)
	if err != nil || filepath.Base(wire) != "wire.jsonl" {
		return sessionadapter.MaterializedSession{}, errors.New("Kimi Code wire is outside the session root")
	}
	paths := []string{wire}
	var events []canonical.Event
	for _, path := range paths {
		file, openErr := os.Open(path)
		if openErr != nil {
			return sessionadapter.MaterializedSession{}, openErr
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64<<10), canonical.MaxEventLineBytes)
		var cursor int64
		for scanner.Scan() {
			raw := append([]byte(nil), scanner.Bytes()...)
			var native any
			if json.Unmarshal(raw, &native) != nil {
				_ = file.Close()
				return sessionadapter.MaterializedSession{}, errors.New("Kimi Code wire contains invalid JSON")
			}
			at := wireTime(native)
			if at.IsZero() {
				at = d.LastActivityAt
			}
			if at.IsZero() {
				_ = file.Close()
				return sessionadapter.MaterializedSession{}, errors.New("Kimi Code wire event has no stable timestamp")
			}
			payload, _ := json.Marshal(map[string]any{"agent": filepath.Base(filepath.Dir(path)), "message": wireText(native), "native": native})
			identity := fmt.Sprintf("%s:%d:", filepath.Base(filepath.Dir(path)), cursor)
			sum := sha256.Sum256(append([]byte(identity), raw...))
			events = append(events, canonical.Event{Schema: canonical.SchemaV1, EventID: "kimi-wire-" + hex.EncodeToString(sum[:]), Timestamp: at.UTC(), Type: canonical.EventMessage, Payload: payload})
			cursor += int64(len(raw)) + 1
		}
		scanErr := scanner.Err()
		_ = file.Close()
		if scanErr != nil {
			return sessionadapter.MaterializedSession{}, scanErr
		}
	}
	out := filepath.Join(a.spool, "kimi_code", safeName(d.NativeSessionRef)+".jsonl")
	if err := canonical.WriteFile(out, events); err != nil {
		return sessionadapter.MaterializedSession{}, err
	}
	return sessionadapter.MaterializedSession{Descriptor: d, SourceFormat: "aida_event_v1", CanonicalPath: out, AdapterVersion: "kimi-code-v1", UsageCapability: sessionadapter.UsageUnavailable}, nil
}

func (a *Adapter) trustedSessionPath(value string) (string, error) {
	root, err := filepath.EvalSymlinks(filepath.Join(a.home, "sessions"))
	if err != nil {
		return "", err
	}
	candidate := value
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(a.home, candidate)
	}
	candidate, err = filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside Kimi sessions root", value)
	}
	return candidate, nil
}
func readObject(path string) map[string]any {
	content, _ := os.ReadFile(path)
	var value map[string]any
	_ = json.Unmarshal(content, &value)
	return value
}
func stringField(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func timeField(row map[string]any, keys ...string) time.Time {
	for _, key := range keys {
		if value, ok := row[key].(string); ok {
			if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
				return parsed.UTC()
			}
		}
	}
	return time.Time{}
}
func wireTime(value any) time.Time {
	row, _ := value.(map[string]any)
	return timeField(row, "timestamp", "createdAt", "created_at")
}
func wireText(value any) string {
	row, _ := value.(map[string]any)
	for _, key := range []string{"content", "message", "text", "summary"} {
		if text, ok := row[key].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	content, _ := json.Marshal(value)
	return string(content)
}
func safeName(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

var _ sessionadapter.Adapter = (*Adapter)(nil)
