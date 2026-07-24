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
	UserID            string
	RunID             string
	ReportType        string
	Period            reportsource.Period
	Timezone          string
	TriggerSource     string
	ModelID           string
	Target            Target
	SourceSelectionID string
	Representation    string
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
// Session Digest. Repeated text is interned, while every report-facing result
// Work Unit and its evidence references remain explicit.
type WorkEvidence struct {
	SelectionID      string               `json:"selection_id"`
	Mode             string               `json:"mode"`
	Timezone         string               `json:"timezone,omitempty"`
	DigestVersion    string               `json:"digest_version"`
	RedactionVersion string               `json:"redaction_version"`
	ContentSnapshot  string               `json:"content_snapshot_at"`
	Completeness     string               `json:"completeness"`
	Coverage         WorkEvidenceCoverage `json:"coverage"`
	Period           WorkEvidencePeriod   `json:"period"`
	Categories       []WorkEvidenceLookup `json:"categories"`
	Statuses         []WorkEvidenceLookup `json:"statuses"`
	EvidenceGrades   []WorkEvidenceLookup `json:"evidence_grades"`
	ResultSources    []WorkEvidenceLookup `json:"result_sources"`
	ResultTexts      []WorkEvidenceLookup `json:"result_texts"`
	EvidenceByGoal   []ExactGoalEvidence  `json:"evidence_by_exact_goal"`
	Sources          []WorkEvidenceSource `json:"sources"`
}

type WorkEvidenceCoverage struct {
	Complete             bool  `json:"complete"`
	SourceItemCount      int   `json:"source_item_count"`
	RepresentedItemCount int   `json:"represented_item_count"`
	SourceEventCount     int64 `json:"source_event_count"`
	IncludedEventCount   int64 `json:"included_event_count"`
	OmittedEventCount    int64 `json:"omitted_event_count"`
	TruncatedItemCount   int   `json:"truncated_item_count"`
}

type WorkEvidencePeriod struct {
	StartDate           string            `json:"start_date"`
	EndDate             string            `json:"end_date"`
	WorkUnitCount       int               `json:"work_unit_count,omitempty"`
	ResultWorkUnitCount int               `json:"result_work_unit_count,omitempty"`
	PrimaryResultCount  int               `json:"primary_result_count,omitempty"`
	VerifiedResultCount int               `json:"verified_result_count,omitempty"`
	ChangeCount         int               `json:"change_count,omitempty"`
	ValidationCount     int               `json:"validation_count,omitempty"`
	UnresolvedCount     int               `json:"unresolved_count,omitempty"`
	Days                []WorkEvidenceDay `json:"days"`
}

type WorkEvidenceDay struct {
	Date                 string `json:"date"`
	WorkUnitCount        int    `json:"work_unit_count,omitempty"`
	ResultWorkUnitCount  int    `json:"result_work_unit_count,omitempty"`
	PrimaryResultCount   int    `json:"primary_result_count,omitempty"`
	VerifiedResultCount  int    `json:"verified_result_count,omitempty"`
	ChangeCount          int    `json:"change_count,omitempty"`
	ValidationCount      int    `json:"validation_count,omitempty"`
	UnresolvedCount      int    `json:"unresolved_count,omitempty"`
	SourceFactCount      int    `json:"source_fact_count"`
	RepresentedFactCount int    `json:"represented_fact_count"`
	Complete             bool   `json:"complete"`
	SourceTextCompacted  bool   `json:"source_text_compacted"`
	SourceFactsTruncated bool   `json:"source_facts_truncated"`
}

type WorkEvidenceLookup struct {
	ID    int
	Value string
}

type WorkEvidenceResult struct {
	TextRef      int
	SourceRef    int
	EvidenceRefs []string
}

type WorkEvidenceFact struct {
	WorkUnitRef      string
	Sequence         int
	DateRef          int
	ActivityEndAt    string
	CategoryRef      int
	StatusRef        int
	EvidenceGradeRef int
	Results          []WorkEvidenceResult
	Unresolved       []WorkEvidenceUnresolved
}

type WorkEvidenceUnresolved struct {
	Text        string
	EvidenceRef string
}

type ExactGoalEvidence struct {
	Goal  string             `json:"goal"`
	Facts []WorkEvidenceFact `json:"facts"`
}

type WorkEvidenceSource struct {
	SourceItemRef           string
	SessionRef              string
	AgentType               string
	ActivityStartAt         string
	ActivityEndAt           string
	DigestSHA256            string
	SourceEventCount        int64
	IncludedEventCount      int64
	OmittedEventCount       int64
	Truncated               bool
	SourceWorkUnitCount     int
	DetailedWorkUnitCount   int
	AggregatedWorkUnitCount int
}

// PresentationProfile is the immutable, report-type-specific presentation
// contract supplied to the Report Agent. It changes organization only; it
// never changes the frozen evidence set.
type PresentationProfile struct {
	SummaryFocus    string `json:"summary_focus"`
	ContentGrouping string `json:"content_grouping"`
}

type Payload struct {
	SchemaVersion       string               `json:"schema_version"`
	Run                 Run                  `json:"run"`
	Scope               Scope                `json:"scope"`
	Coverage            []CoverageItem       `json:"coverage"`
	SourceReports       []SourceReport       `json:"source_reports"`
	Requirements        []Requirement        `json:"requirements"`
	Tasks               []Task               `json:"tasks"`
	Sessions            []SessionSource      `json:"sessions,omitempty"`
	SourceIssues        []SourceIssue        `json:"source_issues"`
	SourceState         SourceState          `json:"source_state"`
	Sources             Sources              `json:"sources,omitzero"`
	WorkEvidence        *WorkEvidence        `json:"work_evidence,omitempty"`
	PresentationProfile *PresentationProfile `json:"presentation_profile,omitempty"`
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
