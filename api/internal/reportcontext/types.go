package reportcontext

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aidashboard/api/internal/reportsource"
)

const (
	ReportTypePersonalDaily    = "personal_daily"
	ReportTypePersonalWeekly   = "personal_weekly"
	ReportTypeTeamDaily        = "team_daily"
	ReportTypeTeamWeekly       = "team_weekly"
	ReportTypeDepartmentDaily  = "department_daily"
	ReportTypeDepartmentWeekly = "department_weekly"
	RepresentationWorkEvidence = "work_evidence"
)

type Target struct {
	Type         string `json:"type"`
	UserID       string `json:"user_id,omitempty"`
	TeamID       string `json:"team_id,omitempty"`
	DepartmentID string `json:"department_id,omitempty"`
}

type BuildRequest struct {
	UserID                  string
	RunID                   string
	ReportType              string
	Period                  reportsource.Period
	Timezone                string
	TriggerSource           string
	ModelID                 string
	Target                  Target
	SourceSelectionID       string
	Representation          string
	IncludeWorkThreads      bool
	EnableMemoryShadow      bool
	EnableWorkspaceMemory   bool
	EnableContinuityContext bool
}

func (r BuildRequest) validate() error {
	if strings.TrimSpace(r.UserID) == "" || strings.TrimSpace(r.RunID) == "" ||
		strings.TrimSpace(r.ReportType) == "" || strings.TrimSpace(r.Period.Start) == "" ||
		strings.TrimSpace(r.Period.End) == "" || strings.TrimSpace(r.Timezone) == "" {
		return ErrInvalidRequest
	}
	if r.Representation != "" && r.Representation != RepresentationWorkEvidence {
		return ErrInvalidRequest
	}
	switch r.ReportType {
	case ReportTypePersonalDaily:
		if r.Target.UserID == "" || r.SourceSelectionID == "" || r.Period.Start != r.Period.End {
			return ErrInvalidRequest
		}
	case ReportTypePersonalWeekly:
		if r.Target.UserID == "" {
			return ErrInvalidRequest
		}
	case ReportTypeTeamDaily:
		if r.Target.TeamID == "" || r.Period.Start != r.Period.End {
			return ErrInvalidRequest
		}
	case ReportTypeTeamWeekly:
		if r.Target.TeamID == "" {
			return ErrInvalidRequest
		}
	case ReportTypeDepartmentDaily:
		if r.Target.DepartmentID == "" || r.Period.Start != r.Period.End {
			return ErrInvalidRequest
		}
	case ReportTypeDepartmentWeekly:
		if r.Target.DepartmentID == "" {
			return ErrInvalidRequest
		}
	default:
		return ErrInvalidRequest
	}
	return nil
}

type Run struct {
	ID            string              `json:"run_id"`
	ReportType    string              `json:"report_type"`
	Period        reportsource.Period `json:"period"`
	Timezone      string              `json:"timezone"`
	TriggerSource string              `json:"trigger_source"`
	ModelID       string              `json:"model_id,omitempty"`
	Target        Target              `json:"target"`
}

type Actor struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TeamID   string `json:"team_id,omitempty"`
	TeamName string `json:"team_name,omitempty"`
}

type TeamScope struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	LeaderID   string   `json:"leader_id,omitempty"`
	LeaderName string   `json:"leader_name,omitempty"`
	MemberIDs  []string `json:"member_ids"`
}

type Scope struct {
	EffectiveUserID string      `json:"effective_user_id"`
	Type            string      `json:"type"`
	UserIDs         []string    `json:"user_ids"`
	TeamID          string      `json:"team_id,omitempty"`
	TeamName        string      `json:"team_name,omitempty"`
	DepartmentID    string      `json:"department_id,omitempty"`
	DepartmentName  string      `json:"department_name,omitempty"`
	Members         []Actor     `json:"members"`
	Teams           []TeamScope `json:"teams"`
}

type CoverageItem struct {
	Type               string   `json:"type"`
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	MemberIDs          []string `json:"member_ids,omitempty"`
	ExpectedReportType string   `json:"expected_report_type"`
	SourceStatus       string   `json:"source_status"`
	ReportID           string   `json:"report_id,omitempty"`
	InvalidReason      string   `json:"invalid_reason,omitempty"`
}

type SourceReport struct {
	ID         string              `json:"id"`
	ReportType string              `json:"report_type"`
	Owner      Actor               `json:"owner"`
	TeamID     string              `json:"team_id,omitempty"`
	TeamName   string              `json:"team_name,omitempty"`
	Period     reportsource.Period `json:"period"`
	Content    string              `json:"content"`
	UpdatedAt  string              `json:"updated_at"`
}

type Requirement struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Status       string   `json:"status"`
	Priority     string   `json:"priority"`
	Progress     int      `json:"progress"`
	Deadline     string   `json:"deadline,omitempty"`
	Creator      Actor    `json:"creator"`
	Responsibles []Actor  `json:"responsibles"`
	TeamIDs      []string `json:"team_ids"`
	UpdatedAt    string   `json:"updated_at"`
}

type Task struct {
	ID               string  `json:"id"`
	RequirementID    string  `json:"requirement_id"`
	RequirementTitle string  `json:"requirement_title"`
	Title            string  `json:"title"`
	Status           string  `json:"status"`
	Priority         string  `json:"priority"`
	Progress         int     `json:"progress"`
	DueDate          string  `json:"due_date,omitempty"`
	Creator          Actor   `json:"creator"`
	Responsibles     []Actor `json:"responsibles"`
	UpdatedAt        string  `json:"updated_at"`
}

type SessionSource struct {
	SelectionID string          `json:"selection_id"`
	Mode        string          `json:"mode"`
	Digest      json.RawMessage `json:"digest"`
}

type SourceIssue struct {
	Type       string `json:"type"`
	SourceType string `json:"source_type"`
	SourceID   string `json:"source_id"`
	SourceName string `json:"source_name,omitempty"`
	ReportID   string `json:"report_id,omitempty"`
	Reason     string `json:"reason"`
}

// SourceState and Sources preserve the first-stage personal Context contract
// until the default Skill is switched to the complete V1 fields.
type SourceState struct {
	Mode             string   `json:"mode,omitempty"`
	SourceMode       string   `json:"source_mode"`
	CoverageComplete bool     `json:"coverage_complete"`
	DependencyReady  bool     `json:"dependency_ready"`
	MissingNames     []string `json:"missing_names"`
}

type Sources struct {
	SessionDigest json.RawMessage `json:"session_digest,omitempty"`
}

// WorkEvidence is the deterministic Agent-facing projection of one frozen
// Session Digest. It exposes report facts as ordinary JSON objects and keeps
// Digest transport and raw source identities out of the model context.
type WorkEvidence struct {
	Mode     string               `json:"mode"`
	Timezone string               `json:"timezone,omitempty"`
	Period   WorkEvidencePeriod   `json:"period"`
	Threads  []WorkEvidenceThread `json:"threads,omitempty"`
	Facts    []WorkEvidenceFact   `json:"facts"`
}

type WorkEvidenceThread struct {
	ThreadRef string `json:"thread_ref"`
	Goal      string `json:"goal,omitempty"`
}

type WorkEvidencePeriod struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

type WorkEvidenceFact struct {
	FactRef      string                    `json:"fact_ref,omitempty"`
	Kind         string                    `json:"kind"`
	Text         string                    `json:"text"`
	Source       string                    `json:"source,omitempty"`
	ThreadRefs   []string                  `json:"thread_refs,omitempty"`
	Observations []WorkEvidenceObservation `json:"observations"`
	// SourceRefs are internal provenance used to connect an accepted Brief back
	// to frozen Session slices. They are stored separately and never exposed.
	SourceRefs []string `json:"-"`
}

type WorkEvidenceObservation struct {
	Date            string `json:"date"`
	FirstObservedAt string `json:"first_observed_at,omitempty"`
	ObservedAt      string `json:"observed_at,omitempty"`
	Category        string `json:"category,omitempty"`
	Status          string `json:"status"`
	OccurrenceCount int    `json:"occurrence_count"`
}

// PresentationProfile is the immutable, report-type-specific presentation
// contract supplied to the Report Agent. It changes organization only; it
// never changes the frozen evidence set.
type PresentationProfile struct {
	SummaryFocus    string `json:"summary_focus"`
	ContentGrouping string `json:"content_grouping"`
}

// ContinuityContext carries historical naming and hierarchy hints only. It is
// deliberately separated from WorkEvidence so prior reports can never satisfy
// the evidence requirement for a current-period outcome.
type ContinuityContext struct {
	Purpose       string                    `json:"purpose"`
	EvidenceRule  string                    `json:"evidence_rule"`
	GroupingRule  string                    `json:"grouping_rule"`
	RecentReports []ContinuityReportOutline `json:"recent_reports"`
}

type ContinuityReportOutline struct {
	Date   string            `json:"date"`
	Themes []ContinuityTheme `json:"themes"`
}

type ContinuityTheme struct {
	Title    string   `json:"title"`
	Children []string `json:"children,omitempty"`
}

// ProjectMemoryContext contains history-derived naming and grouping hints.
// It is not WorkEvidence and can never satisfy a current-period fact.
type ProjectMemoryContext struct {
	Purpose      string                  `json:"purpose"`
	EvidenceRule string                  `json:"evidence_rule"`
	GroupingRule string                  `json:"grouping_rule"`
	Hints        []HistoricalProjectHint `json:"hints"`
}

// WorkspaceContext preserves current-day source boundaries without exposing
// paths, repository identities, or persistent database IDs to the Agent.
type WorkspaceContext struct {
	Purpose      string               `json:"purpose"`
	GroupingRule string               `json:"grouping_rule"`
	Groups       []WorkspaceFactGroup `json:"groups"`
}

type WorkspaceFactGroup struct {
	WorkspaceRef string   `json:"workspace_ref"`
	FactRefs     []string `json:"fact_refs"`
}

type HistoricalProjectHint struct {
	ProjectRef        string   `json:"project_ref"`
	CanonicalName     string   `json:"canonical_name"`
	Aliases           []string `json:"aliases,omitempty"`
	WorkstreamCues    []string `json:"workstream_cues,omitempty"`
	SemanticFactRefs  []string `json:"semantic_fact_refs,omitempty"`
	WorkspaceFactRefs []string `json:"workspace_fact_refs,omitempty"`
	Confidence        float64  `json:"confidence"`
	CandidateOnly     bool     `json:"candidate_only,omitempty"`
	MatchBasis        string   `json:"match_basis,omitempty"`
	Instruction       string   `json:"instruction"`
}

type Payload struct {
	SchemaVersion        string                `json:"schema_version"`
	Run                  Run                   `json:"run"`
	Scope                Scope                 `json:"scope"`
	Coverage             []CoverageItem        `json:"coverage"`
	SourceReports        []SourceReport        `json:"source_reports"`
	Requirements         []Requirement         `json:"requirements"`
	Tasks                []Task                `json:"tasks"`
	Sessions             []SessionSource       `json:"sessions,omitempty"`
	SourceIssues         []SourceIssue         `json:"source_issues"`
	SourceState          SourceState           `json:"source_state"`
	Sources              Sources               `json:"sources,omitzero"`
	WorkEvidence         *WorkEvidence         `json:"work_evidence,omitempty"`
	PresentationProfile  *PresentationProfile  `json:"presentation_profile,omitempty"`
	ContinuityContext    *ContinuityContext    `json:"continuity_context,omitempty"`
	ProjectMemoryContext *ProjectMemoryContext `json:"project_memory_context,omitempty"`
	WorkspaceContext     *WorkspaceContext     `json:"workspace_context,omitempty"`
}

func targetFromAny(value any) (Target, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return Target{}, ErrInvalidRequest
	}
	var target Target
	if err := json.Unmarshal(raw, &target); err != nil {
		return Target{}, ErrInvalidRequest
	}
	if target.Type == "self" && target.UserID == "" {
		return Target{}, fmt.Errorf("%w: unresolved self target", ErrInvalidRequest)
	}
	return target, nil
}

func sourceModeForReport(reportType string, hasSessions bool) string {
	hasReports := reportType != ReportTypePersonalDaily
	switch {
	case hasReports && hasSessions:
		return "mixed"
	case hasReports:
		return "reports_only"
	case hasSessions:
		return "sessions_only"
	default:
		return "facts_only"
	}
}
