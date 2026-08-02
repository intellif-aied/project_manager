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
	ResolverVersion        = "project-memory-resolver/v4"
	maxInputTokens         = 8000
	maxOutputTokens        = 1500
	maxCurrentThemes       = 8
	maxCandidateProjects   = 8
	maxRecentReports       = 5
	maxAliasesPerProject   = 5
	maxHistoricalExcerpt   = 2
	maxOverviewRunes       = 1200
	maxHistoryOverviewRune = 400
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
	EvidenceConstraint string             `json:"evidence_constraint"`
	AllowedActions     []string           `json:"allowed_actions"`
}

type InputWorkstream struct {
	Subject      string   `json:"subject"`
	Deliverables []string `json:"deliverables,omitempty"`
}

type InputTheme struct {
	ThemeRef string `json:"theme_ref"`
	Title    string `json:"title"`
}

type InputProject struct {
	ProjectRef    string   `json:"project_ref"`
	CanonicalName string   `json:"canonical_name"`
	Aliases       []string `json:"aliases,omitempty"`
	LastSeenOn    string   `json:"last_seen_on"`
	SourceType    string   `json:"source_type"`
	SourceWeight  float64  `json:"source_weight"`
}

type HistoricalReport struct {
	Date         string  `json:"date"`
	Overview     string  `json:"overview"`
	SourceType   string  `json:"source_type"`
	SourceWeight float64 `json:"source_weight"`
}

type MemoryProposal struct {
	SchemaVersion string           `json:"schema_version"`
	Decisions     []MemoryDecision `json:"decisions"`
}

type MemoryDecision struct {
	ThemeRef      string   `json:"theme_ref"`
	Action        string   `json:"action"`
	ProjectRef    string   `json:"project_ref,omitempty"`
	CanonicalName string   `json:"canonical_name,omitempty"`
	Aliases       []string `json:"aliases,omitempty"`
	Confidence    float64  `json:"confidence"`
	Reason        string   `json:"reason,omitempty"`
}

func (decision *MemoryDecision) UnmarshalJSON(data []byte) error {
	type decisionFields struct {
		ThemeRef      string          `json:"theme_ref"`
		Action        string          `json:"action"`
		ProjectRef    string          `json:"project_ref,omitempty"`
		CanonicalName string          `json:"canonical_name,omitempty"`
		Aliases       []string        `json:"aliases,omitempty"`
		Confidence    json.RawMessage `json:"confidence"`
		Reason        string          `json:"reason,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var fields decisionFields
	if err := decoder.Decode(&fields); err != nil {
		return err
	}
	confidence, err := parseProposalConfidence(fields.Confidence)
	if err != nil {
		return err
	}
	*decision = MemoryDecision{
		ThemeRef: fields.ThemeRef, Action: fields.Action, ProjectRef: fields.ProjectRef,
		CanonicalName: fields.CanonicalName, Aliases: fields.Aliases,
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
	DesiredSourceFingerprint string
	ClaimedSourceFingerprint string
	ExternalTaskID           string
	Attempts                 int
	InputJSON                json.RawMessage
	ProposalJSON             json.RawMessage
	StartedAt                time.Time
}

func JobRef(reportDate, sourceFingerprint string) string {
	return strings.TrimSpace(reportDate) + "|" + strings.TrimSpace(sourceFingerprint)
}
