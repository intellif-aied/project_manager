package reportmemory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	ResolverVersion        = "project-memory-resolver/v12"
	maxInputTokens         = 8000
	maxOutputTokens        = 1500
	maxCurrentThemes       = 24
	maxCandidateProjects   = 8
	maxRecentReports       = 10
	maxHistoricalAnchors   = 10
	maxMemorySnapshotDepth = 20
	maxAliasesPerProject   = 5
	maxWorkstreamCues      = 8
	maxDecisionCues        = 5
	maxHistoricalExcerpt   = 2
	maxOverviewRunes       = 1200
	maxHistoryOverviewRune = 300
	maxHistoryAnchorRunes  = 120
)

type Resolver interface {
	Submit(context.Context, ResolverRequest) (ResolverSubmission, error)
	Status(context.Context, string) (ResolverTask, error)
}

type ResolverRequest struct {
	AgentID string
	ModelID string
	UserID  string
	JobRef  string
}

type ResolverSubmission struct {
	TaskID string
	Status string
}

type ResolverTask struct {
	TaskID    string
	Status    string
	Result    string
	Error     string
	StartedAt time.Time
	EndedAt   time.Time
}

type NightlyConfig struct {
	Enabled      bool
	AgentID      string
	ModelID      string
	WorkerID     string
	PollInterval time.Duration
	LeaseTTL     time.Duration
	ClaimBatch   int
	StartHour    int
	EndHour      int
}

type ConsolidationInput struct {
	SchemaVersion      string             `json:"schema_version"`
	ResolverVersion    string             `json:"resolver_version"`
	UserRef            string             `json:"user_ref"`
	ReportRef          string             `json:"report_ref"`
	ReportDate         string             `json:"report_date"`
	SourceType         string             `json:"source_type"`
	SourceWeight       float64            `json:"source_weight"`
	CurrentThemes      []InputTheme       `json:"current_themes"`
	BriefWorkstreams   []InputWorkstream  `json:"brief_workstreams,omitempty"`
	CandidateProjects  []InputProject     `json:"candidate_projects,omitempty"`
	RecentOverviews    []HistoricalReport `json:"recent_overviews,omitempty"`
	HistoricalAnchors  []HistoricalReport `json:"historical_project_anchors,omitempty"`
	CurrentMemory      []InputProject     `json:"current_memory,omitempty"`
	EvidenceConstraint string             `json:"evidence_constraint"`
	AllowedActions     []string           `json:"allowed_actions"`
}

type InputWorkstream struct {
	Subject      string   `json:"subject"`
	Deliverables []string `json:"deliverables,omitempty"`
	FactRefs     []string `json:"fact_refs,omitempty"`
}

type InputTheme struct {
	ThemeRef      string   `json:"theme_ref"`
	EvidenceRef   string   `json:"evidence_ref"`
	ReportRef     string   `json:"report_ref"`
	ReportDate    string   `json:"report_date"`
	SourceType    string   `json:"source_type"`
	SourceWeight  float64  `json:"source_weight"`
	Title         string   `json:"title"`
	FactRefs      []string `json:"fact_refs,omitempty"`
	WorkspaceRefs []string `json:"workspace_refs,omitempty"`
}

type InputProject struct {
	ProjectRef     string   `json:"project_ref"`
	CanonicalName  string   `json:"canonical_name"`
	Aliases        []string `json:"aliases,omitempty"`
	WorkstreamCues []string `json:"workstream_cues,omitempty"`
	WorkspaceRefs  []string `json:"workspace_refs,omitempty"`
	MatchedThemes  []string `json:"matched_theme_refs,omitempty"`
	LastSeenOn     string   `json:"last_seen_on"`
	SourceType     string   `json:"source_type"`
	SourceWeight   float64  `json:"source_weight"`
}

type HistoricalReport struct {
	Date         string  `json:"date"`
	Overview     string  `json:"overview"`
	SourceType   string  `json:"source_type"`
	SourceWeight float64 `json:"source_weight"`
}

type MemoryProposal struct {
	SchemaVersion string              `json:"schema_version"`
	Operations    []MemoryOperation   `json:"operations"`
	Rejected      []RejectedOperation `json:"rejected_operations,omitempty"`
}

type MemoryOperation struct {
	OperationID   string   `json:"operation_id"`
	Operation     string   `json:"operation"`
	ThemeRef      string   `json:"theme_ref,omitempty"`
	ProjectRef    string   `json:"project_ref,omitempty"`
	TempRef       string   `json:"temp_ref,omitempty"`
	DependsOn     []string `json:"depends_on,omitempty"`
	EvidenceRefs  []string `json:"evidence_refs,omitempty"`
	CanonicalName string   `json:"canonical_name,omitempty"`
	SignalType    string   `json:"signal_type,omitempty"`
	Value         string   `json:"value,omitempty"`
	WorkspaceRef  string   `json:"workspace_ref,omitempty"`
	Confidence    float64  `json:"confidence"`
	Reason        string   `json:"reason,omitempty"`
}

type RejectedOperation struct {
	OperationID string `json:"operation_id"`
	Reason      string `json:"reason"`
}

func (operation *MemoryOperation) UnmarshalJSON(data []byte) error {
	type operationFields struct {
		OperationID   string          `json:"operation_id"`
		Operation     string          `json:"operation"`
		ThemeRef      string          `json:"theme_ref,omitempty"`
		ProjectRef    string          `json:"project_ref,omitempty"`
		TempRef       string          `json:"temp_ref,omitempty"`
		DependsOn     []string        `json:"depends_on,omitempty"`
		EvidenceRefs  []string        `json:"evidence_refs,omitempty"`
		CanonicalName string          `json:"canonical_name,omitempty"`
		SignalType    string          `json:"signal_type,omitempty"`
		Value         string          `json:"value,omitempty"`
		WorkspaceRef  string          `json:"workspace_ref,omitempty"`
		Confidence    json.RawMessage `json:"confidence"`
		Reason        string          `json:"reason,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var fields operationFields
	if err := decoder.Decode(&fields); err != nil {
		return err
	}
	confidence, err := parseProposalConfidence(fields.Confidence)
	if err != nil {
		return err
	}
	*operation = MemoryOperation{
		OperationID: fields.OperationID, Operation: fields.Operation,
		ThemeRef: fields.ThemeRef, ProjectRef: fields.ProjectRef, TempRef: fields.TempRef,
		DependsOn: fields.DependsOn, CanonicalName: fields.CanonicalName,
		EvidenceRefs: fields.EvidenceRefs,
		SignalType:   fields.SignalType, Value: fields.Value, WorkspaceRef: fields.WorkspaceRef,
		Confidence: confidence, Reason: fields.Reason,
	}
	return nil
}

func parseProposalConfidence(raw json.RawMessage) (float64, error) {
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, fmt.Errorf("confidence must be a number or confidence label")
	}
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "high":
		return 0.9, nil
	case "medium":
		return 0.6, nil
	case "low":
		return 0.3, nil
	default:
		value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return 0, fmt.Errorf("confidence label is invalid")
		}
		return value, nil
	}
}

type queuedJob struct {
	UserID                   string
	ReportID                 string
	ReportDate               string
	DirtyFromDate            string
	DesiredSourceFingerprint string
	ClaimedSourceFingerprint string
	DesiredEvidenceWatermark int64
	ClaimedEvidenceWatermark int64
	RebuildRequired          bool
	ExternalTaskID           string
	Attempts                 int
	InputJSON                json.RawMessage
	ProposalJSON             json.RawMessage
	StartedAt                time.Time
}

func JobRef(reportDate, sourceFingerprint string) string {
	return strings.TrimSpace(reportDate) + "|" + strings.TrimSpace(sourceFingerprint)
}
