package canonicalsync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidRequest = errors.New("invalid canonical sync request")

type IngestionMetadata struct {
	AdapterVersion      string `json:"adapter_version"`
	NativeClientVersion string `json:"native_client_version,omitempty"`
	UsageCapability     string `json:"usage_capability"`
}

type PrepareSource struct {
	SourceRole                       string            `json:"source_role"`
	SourceKey                        string            `json:"source_key"`
	LocalSize                        int64             `json:"local_size"`
	PrefixCheckpointHash             string            `json:"prefix_checkpoint_hash"`
	PrefixCheckpointAlgorithmVersion string            `json:"prefix_checkpoint_algorithm_version"`
	SourceFormat                     string            `json:"source_format"`
	IngestionMetadata                IngestionMetadata `json:"ingestion_metadata"`
}

type PrepareSession struct {
	SessionRef       string          `json:"session_ref"`
	AgentType        string          `json:"agent_type"`
	ParentSessionRef string          `json:"parent_session_ref,omitempty"`
	ForkedAt         *time.Time      `json:"forked_at,omitempty"`
	ForkSource       string          `json:"fork_source,omitempty"`
	Summary          string          `json:"summary,omitempty"`
	StartedAt        *time.Time      `json:"started_at,omitempty"`
	LastActivityAt   *time.Time      `json:"last_activity_at,omitempty"`
	CWD              string          `json:"cwd,omitempty"`
	ProjectName      string          `json:"project_name,omitempty"`
	Sources          []PrepareSource `json:"sources"`
}

type PrepareRequest struct {
	ClientVersion string           `json:"client_version"`
	Sessions      []PrepareSession `json:"sessions"`
}

type PrepareResult struct {
	SessionRef                string `json:"session_ref"`
	SourceKey                 string `json:"source_key"`
	GenerationID              string `json:"generation_id,omitempty"`
	GenerationStatus          string `json:"generation_status,omitempty"`
	ExpectedCursor            int64  `json:"expected_cursor"`
	PrefixCheckpointHash      string `json:"prefix_checkpoint_hash,omitempty"`
	PrefixCheckpointAlgorithm string `json:"prefix_checkpoint_algorithm_version,omitempty"`
	ContentStatus             string `json:"content_status,omitempty"`
	Action                    string `json:"action,omitempty"`
	ErrorCode                 string `json:"error_code,omitempty"`
	NextAction                string `json:"next_action,omitempty"`
}

type Preparer interface {
	PrepareFamily(context.Context, string, PrepareRequest) ([]PrepareResult, error)
}

func ValidatePrepare(request PrepareRequest) error {
	if strings.TrimSpace(request.ClientVersion) == "" || len(request.Sessions) == 0 || len(request.Sessions) > 100 {
		return fmt.Errorf("%w: client version and 1 to 100 sessions are required", ErrInvalidRequest)
	}
	seen := make(map[string]struct{}, len(request.Sessions))
	hasSource := false
	for _, session := range request.Sessions {
		sessionRef := strings.TrimSpace(session.SessionRef)
		agentType := strings.TrimSpace(session.AgentType)
		if sessionRef == "" || agentType == "" || agentType == "claude_code" || agentType == "codex" {
			return fmt.Errorf("%w: canonical session identity is required and legacy agent types are forbidden", ErrInvalidRequest)
		}
		identity := agentType + "\n" + sessionRef
		if _, exists := seen[identity]; exists {
			return fmt.Errorf("%w: duplicate session identity", ErrInvalidRequest)
		}
		seen[identity] = struct{}{}
		for _, source := range session.Sources {
			hasSource = true
			if source.SourceFormat != "aida_event_v1" || strings.TrimSpace(source.SourceRole) == "" ||
				strings.TrimSpace(source.SourceKey) == "" || source.LocalSize < 0 ||
				source.PrefixCheckpointAlgorithmVersion != "sha256-prefix-v1" {
				return fmt.Errorf("%w: source must use aida_event_v1 and sha256-prefix-v1", ErrInvalidRequest)
			}
			if strings.TrimSpace(source.IngestionMetadata.AdapterVersion) == "" || !validUsageCapability(source.IngestionMetadata.UsageCapability) {
				return fmt.Errorf("%w: adapter version and usage capability are required", ErrInvalidRequest)
			}
		}
	}
	if !hasSource {
		return fmt.Errorf("%w: at least one canonical source is required", ErrInvalidRequest)
	}
	return nil
}

func validUsageCapability(value string) bool {
	switch value {
	case "unavailable", "estimated", "exact":
		return true
	default:
		return false
	}
}

var releasedAdapters = map[string]map[string]string{
	"opencode":  {"opencode-v1": "unavailable"},
	"kimi_code": {"kimi-code-v1": "unavailable"},
}

// ValidateReleasedPrepare is the server-owned rollout gate. A client cannot
// promote itself to estimated/exact by changing request metadata.
func ValidateReleasedPrepare(request PrepareRequest) error {
	for _, session := range request.Sessions {
		adapters, ok := releasedAdapters[strings.TrimSpace(session.AgentType)]
		if !ok {
			return fmt.Errorf("%w: client type is not released for canonical upload", ErrInvalidRequest)
		}
		for _, source := range session.Sources {
			maximum, ok := adapters[strings.TrimSpace(source.IngestionMetadata.AdapterVersion)]
			if !ok || usageCapabilityRank(source.IngestionMetadata.UsageCapability) > usageCapabilityRank(maximum) {
				return fmt.Errorf("%w: adapter version or usage capability is not released", ErrInvalidRequest)
			}
		}
	}
	return nil
}

func usageCapabilityRank(value string) int {
	switch value {
	case "unavailable":
		return 0
	case "estimated":
		return 1
	case "exact":
		return 2
	default:
		return 99
	}
}
