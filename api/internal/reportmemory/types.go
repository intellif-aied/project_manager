package reportmemory

import (
	"context"
	"database/sql"
)

const AlgorithmVersion = "project-memory-shadow/v7"

type FactInput struct {
	FactRef     string
	Text        string
	ThreadGoals []string
}

type ResolveRequest struct {
	RunID      string
	UserID     string
	ReportDate string
	Facts      []FactInput
}

type ResolutionSnapshot struct {
	AlgorithmVersion string           `json:"algorithm_version"`
	Mode             string           `json:"mode"`
	Facts            []FactResolution `json:"facts"`
}

type FactResolution struct {
	FactRef       string      `json:"fact_ref"`
	Decision      string      `json:"decision"`
	ProjectRef    string      `json:"project_ref,omitempty"`
	Confidence    float64     `json:"confidence"`
	CandidateList []Candidate `json:"candidates"`
}

type Candidate struct {
	ProjectRef    string   `json:"project_ref"`
	CanonicalName string   `json:"canonical_name"`
	Score         float64  `json:"score"`
	Signals       []string `json:"signals"`
	LastSeenOn    string   `json:"last_seen_on"`
}

// ResolveShadow is the Module interface used by report generation. Its
// implementation synchronizes user-final reports, resolves candidates, and
// stores a reproducible snapshot without changing the Agent-facing Context.
func ResolveShadow(ctx context.Context, tx *sql.Tx, request ResolveRequest) (ResolutionSnapshot, error) {
	return resolveShadow(ctx, tx, request)
}
