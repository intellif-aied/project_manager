package reportreview

import (
	"context"
	"encoding/json"
	"time"

	"github.com/aidashboard/api/internal/reportbrief"
)

const (
	ResolverVersion    = "report-review/v3"
	maxReviewFacts     = 36
	maxReviewFactRunes = 800
)

type Fact struct {
	FactRef    string   `json:"fact_ref"`
	Text       string   `json:"text"`
	Source     string   `json:"source,omitempty"`
	ThreadRefs []string `json:"thread_refs,omitempty"`
}

type ProjectCandidate struct {
	ProjectRef      string   `json:"project_ref,omitempty"`
	CanonicalName   string   `json:"canonical_name"`
	RelatedFactRefs []string `json:"related_fact_refs"`
	MatchBasis      string   `json:"match_basis,omitempty"`
	Confidence      float64  `json:"confidence,omitempty"`
	CandidateOnly   bool     `json:"candidate_only,omitempty"`
	Aliases         []string `json:"aliases,omitempty"`
	WorkstreamCues  []string `json:"workstream_cues,omitempty"`
	IdentityUsage   string   `json:"identity_usage,omitempty"`
	ProposedTargets []string `json:"proposed_targets,omitempty"`
}

type Input struct {
	SchemaVersion     string              `json:"schema_version"`
	RunID             string              `json:"run_id"`
	BriefHash         string              `json:"brief_hash"`
	ContextHash       string              `json:"context_hash"`
	Candidate         reportbrief.Payload `json:"candidate_brief"`
	SelectedFacts     []Fact              `json:"selected_facts"`
	ReviewCandidates  []Fact              `json:"review_candidates,omitempty"`
	ProjectCandidates []ProjectCandidate  `json:"project_candidates,omitempty"`
	AllowedFactRefs   []string            `json:"allowed_fact_refs"`
}

type QueueResult struct {
	RunID     string
	JobRef    string
	BriefHash string
	Status    string
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

type Resolver interface {
	Submit(context.Context, ResolverRequest) (ResolverSubmission, error)
	Status(context.Context, string) (ResolverTask, error)
}

type Finalizer interface {
	FinalizeReviewedReport(context.Context, string, string, reportbrief.ReviewFinalized, json.RawMessage) error
}

type Config struct {
	Enabled      bool
	AgentID      string
	ModelID      string
	WorkerID     string
	PollInterval time.Duration
	LeaseTTL     time.Duration
	ClaimBatch   int
}
