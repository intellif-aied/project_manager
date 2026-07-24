package sessiondigestv2

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/aidashboard/api/internal/sessionsync"
)

const (
	Version          = "session-digest/v2.10.0"
	LegacyVersion    = "session-digest/v2.9.0"
	RedactionVersion = "report-redaction/v1"
	JobType          = sessionsync.JobBuildContentSliceDigestV2
)

var (
	ErrDigestUnavailable      = errors.New("session digest v2 revision is unavailable")
	ErrStaleDigestSource      = errors.New("session digest v2 source is stale")
	ErrDigestStatePersistence = errors.New("session digest v2 state update must be retried")
)

type Goal struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

type ResultStatement struct {
	Text         string   `json:"text"`
	Source       string   `json:"source"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type AgentClaim struct {
	Text    string `json:"text"`
	Support string `json:"support"`
}

type Evidence struct {
	Ref           string `json:"ref"`
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	Summary       string `json:"summary"`
	CommandFamily string `json:"command_family,omitempty"`
	ExitCode      *int   `json:"exit_code,omitempty"`
}

type Change struct {
	Path        string `json:"path"`
	Operation   string `json:"operation"`
	EvidenceRef string `json:"evidence_ref"`
}

type Validation struct {
	Name           string `json:"name"`
	Attempts       int    `json:"attempts"`
	LastStatus     string `json:"last_status"`
	LastOccurredAt string `json:"last_occurred_at,omitempty"`
	Summary        string `json:"summary,omitempty"`
	EvidenceRef    string `json:"evidence_ref"`
}

type Unresolved struct {
	Text        string `json:"text"`
	EvidenceRef string `json:"evidence_ref,omitempty"`
}

type WorkUnit struct {
	WorkUnitRef      string            `json:"work_unit_ref"`
	Sequence         int               `json:"sequence"`
	ActivityStartAt  string            `json:"activity_start_at,omitempty"`
	ActivityEndAt    string            `json:"activity_end_at,omitempty"`
	PeriodRelation   string            `json:"period_relation"`
	Goal             Goal              `json:"goal"`
	Category         string            `json:"category"`
	Status           string            `json:"status"`
	EvidenceGrade    string            `json:"evidence_grade"`
	ResultStatements []ResultStatement `json:"result_statements"`
	AgentClaims      []AgentClaim      `json:"agent_claims"`
	Evidence         []Evidence        `json:"evidence"`
	Changes          []Change          `json:"changes"`
	Validations      []Validation      `json:"validations"`
	Unresolved       []Unresolved      `json:"unresolved"`
}

type DiscussionAggregate struct {
	Topic                string `json:"topic"`
	WorkUnitCount        int    `json:"work_unit_count"`
	ActivityStartAt      string `json:"activity_start_at,omitempty"`
	ActivityEndAt        string `json:"activity_end_at,omitempty"`
	PendingQuestionCount int    `json:"pending_question_count"`
}

type StatusCounts struct {
	Completed int `json:"completed"`
	Partial   int `json:"partial"`
	Blocked   int `json:"blocked"`
	Failed    int `json:"failed"`
	Pending   int `json:"pending"`
	Unknown   int `json:"unknown"`
}

type SessionSummary struct {
	PrimaryResultCount  int          `json:"primary_result_count"`
	VerifiedResultCount int          `json:"verified_result_count"`
	DecisionCount       int          `json:"decision_count"`
	UnresolvedCount     int          `json:"unresolved_count"`
	StatusCounts        StatusCounts `json:"status_counts"`
}

type DailyHighlight struct {
	SourceRef        string            `json:"source_ref,omitempty"`
	WorkUnitRef      string            `json:"work_unit_ref"`
	Sequence         int               `json:"sequence"`
	ActivityEndAt    string            `json:"activity_end_at,omitempty"`
	Category         string            `json:"category"`
	Status           string            `json:"status"`
	EvidenceGrade    string            `json:"evidence_grade"`
	Goal             string            `json:"goal,omitempty"`
	ResultStatements []ResultStatement `json:"result_statements"`
	Unresolved       []Unresolved      `json:"unresolved"`
}

// OutcomeCoverage makes result preservation explicit. A complete report view
// retains one entry for every distinct result-bearing Work Unit.
type OutcomeCoverage struct {
	SourceCount      int  `json:"source_count"`
	RepresentedCount int  `json:"represented_count"`
	Complete         bool `json:"complete"`
	TextCompacted    bool `json:"text_compacted"`
}

type DailySummary struct {
	Date                string           `json:"date"`
	WorkUnitCount       int              `json:"work_unit_count,omitzero"`
	ResultWorkUnitCount int              `json:"result_work_unit_count,omitzero"`
	PrimaryResultCount  int              `json:"primary_result_count,omitzero"`
	VerifiedResultCount int              `json:"verified_result_count,omitzero"`
	ChangeCount         int              `json:"change_count,omitzero"`
	ValidationCount     int              `json:"validation_count,omitzero"`
	UnresolvedCount     int              `json:"unresolved_count,omitzero"`
	StatusCounts        StatusCounts     `json:"status_counts,omitzero"`
	Highlights          []DailyHighlight `json:"highlights"`
	HighlightsTruncated bool             `json:"highlights_truncated"`
	OutcomeCoverage     OutcomeCoverage  `json:"outcome_coverage"`
}

type ReportPeriodSummary struct {
	StartDate           string         `json:"start_date"`
	EndDate             string         `json:"end_date"`
	WorkUnitCount       int            `json:"work_unit_count,omitzero"`
	ResultWorkUnitCount int            `json:"result_work_unit_count,omitzero"`
	PrimaryResultCount  int            `json:"primary_result_count,omitzero"`
	VerifiedResultCount int            `json:"verified_result_count,omitzero"`
	ChangeCount         int            `json:"change_count,omitzero"`
	ValidationCount     int            `json:"validation_count,omitzero"`
	UnresolvedCount     int            `json:"unresolved_count,omitzero"`
	StatusCounts        StatusCounts   `json:"status_counts,omitzero"`
	Days                []DailySummary `json:"days"`
}

type Coverage struct {
	SourceEventCount        int64  `json:"source_event_count"`
	IncludedEventCount      int64  `json:"included_event_count"`
	OmittedEventCount       int64  `json:"omitted_event_count"`
	SourceWorkUnitCount     int    `json:"source_work_unit_count"`
	DetailedWorkUnitCount   int    `json:"detailed_work_unit_count"`
	AggregatedWorkUnitCount int    `json:"aggregated_work_unit_count"`
	Truncated               bool   `json:"truncated"`
	Representation          string `json:"representation"`
}

type Digest struct {
	SchemaVersion        string                `json:"schema_version"`
	SessionSummary       SessionSummary        `json:"session_summary,omitzero"`
	DailySummaries       []DailySummary        `json:"daily_summaries"`
	ReportPeriodSummary  *ReportPeriodSummary  `json:"report_period_summary,omitempty"`
	WorkUnits            []WorkUnit            `json:"work_units"`
	DiscussionAggregates []DiscussionAggregate `json:"discussion_aggregates"`
	Coverage             Coverage              `json:"coverage"`
}

func EmptyDigest() Digest {
	return Digest{
		SchemaVersion:        Version,
		DailySummaries:       []DailySummary{},
		WorkUnits:            []WorkUnit{},
		DiscussionAggregates: []DiscussionAggregate{},
		Coverage: Coverage{
			Representation: "result_focused",
		},
	}
}

type Event struct {
	StartCursor  int64
	EndCursor    int64
	OccurredAt   time.Time
	EventType    string
	Summary      string
	Excerpt      string
	Payload      json.RawMessage
	ContentSHA   string
	PayloadBytes int64
}

type BuildResult struct {
	Digest             Digest
	SourceEventCount   int64
	IncludedEventCount int64
	OmittedEventCount  int64
	SourceBytes        int64
	DigestBytes        int
	Truncated          bool
	SourceSHA256       string
	DigestSHA256       string
	DigestJSON         []byte
}

type Revision struct {
	ID                   string
	SliceID              string
	SessionID            string
	ProjectionRevisionID string
	GenerationID         string
	ContentEpoch         int64
	StartCursor          int64
	EndCursor            int64
	Status               string
	DigestVersion        string
	RedactionVersion     string
}

type ProcessingTarget struct {
	Revision Revision
	Ready    bool
}

type Config struct {
	DigestVersion    string
	RedactionVersion string
	ReconcileBatch   int
	WorkerBatch      int
}

func DefaultConfig() Config {
	return Config{
		DigestVersion:    Version,
		RedactionVersion: RedactionVersion,
		ReconcileBatch:   10,
		WorkerBatch:      1,
	}
}

func (c Config) Normalized() (Config, error) {
	if c.DigestVersion == "" {
		c.DigestVersion = Version
	}
	if c.RedactionVersion == "" {
		c.RedactionVersion = RedactionVersion
	}
	if c.ReconcileBatch == 0 {
		c.ReconcileBatch = 10
	}
	if c.WorkerBatch == 0 {
		c.WorkerBatch = 1
	}
	if c.DigestVersion != Version || c.RedactionVersion != RedactionVersion {
		return Config{}, errors.New("unsupported session digest v2 or redaction version")
	}
	if c.ReconcileBatch < 1 || c.ReconcileBatch > 25 || c.WorkerBatch != 1 {
		return Config{}, errors.New("invalid session digest v2 limits")
	}
	return c, nil
}

func HashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
