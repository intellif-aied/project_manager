package reportbrief

import (
	"errors"
	"fmt"
	"strings"
)

const (
	SchemaVersion            = "report-brief/v1"
	MaxPayloadBytes          = 128 * 1024
	MaxWorkstreams           = 5
	MaxDeliverables          = 8
	MaxBriefInvalidAttempts  = 2
	MaxResultInvalidAttempts = 1
	FactRefJSONPattern       = `^fact-[0-9]{3,}$`
	automaticExclusionReason = "not_selected"
)

var (
	ErrInvalid              = errors.New("report brief is invalid")
	ErrNotFound             = errors.New("report brief not found")
	ErrConflict             = errors.New("report brief conflicts with the accepted brief")
	ErrMismatch             = errors.New("report brief does not match the report run")
	ErrRunNotWritable       = errors.New("report run does not accept a report brief")
	ErrBriefRetryExhausted  = errors.New("report brief retry limit exhausted")
	ErrResultInvalid        = errors.New("report result is invalid")
	ErrResultRetryExhausted = errors.New("report result retry limit exhausted")
)

func ValidStates() []string {
	return []string{"released", "validated", "completed", "in_progress", "blocked"}
}

func ValidEnvironments() []string {
	return []string{"production", "test", "development", "none"}
}

func ValidExclusionReasons() []string {
	return []string{"preparation", "discussion", "trace", "duplicate", "low_reader_value", "secondary_activity"}
}

type Draft struct {
	Workstreams      []Workstream   `json:"workstreams"`
	ExcludedFacts    []ExcludedFact `json:"excluded_facts"`
	NoReportableWork bool           `json:"no_reportable_work"`
}

type Workstream struct {
	Subject string `json:"subject,omitempty"`
	Title   string `json:"title"`
	// Objective is accepted only for a legacy subject-less Brief. The subject
	// subject contract intentionally keeps it out of normalized system Briefs.
	Objective    string        `json:"objective,omitempty"`
	Deliverables []Deliverable `json:"deliverables"`
}

type Deliverable struct {
	Result   string   `json:"result"`
	FactRefs []string `json:"fact_refs"`
	// Legacy fields remain decodable during a rolling Skill/API update. They
	// are discarded by the project-outcome subject contract and never reach Pass 2.
	State       string `json:"state,omitempty"`
	Environment string `json:"environment,omitempty"`
	Validation  string `json:"validation,omitempty"`
	NextAction  string `json:"next_action,omitempty"`
}

type ExcludedFact struct {
	FactRef string `json:"fact_ref"`
	Reason  string `json:"reason"`
}

type Period struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type Payload struct {
	SchemaVersion    string         `json:"schema_version"`
	ReportType       string         `json:"report_type"`
	Period           Period         `json:"period"`
	Workstreams      []Workstream   `json:"workstreams"`
	ExcludedFacts    []ExcludedFact `json:"excluded_facts"`
	NoReportableWork bool           `json:"no_reportable_work"`
}

type Stored struct {
	Payload     Payload
	BriefHash   string
	ContextHash string
}

// ReaderSummary returns the deterministic reader-facing summary for Briefs
// that use the subject contract. The accepted workstream title is the sole
// headline; deliverables remain supporting detail and cannot be promoted here.
func (s Stored) ReaderSummary() (string, bool) {
	if s.Payload.NoReportableWork {
		return noReportableResultText, true
	}
	if len(s.Payload.Workstreams) == 0 {
		return "", false
	}
	items := make([]string, 0, len(s.Payload.Workstreams))
	for index, workstream := range s.Payload.Workstreams {
		if strings.TrimSpace(workstream.Subject) == "" {
			return "", false
		}
		title := strings.Join(strings.Fields(workstream.Title), " ")
		if title == "" {
			return "", false
		}
		items = append(items, fmt.Sprintf("%d. %s", index+1, title))
	}
	return strings.Join(items, "\n"), true
}

type Accepted struct {
	Status           string         `json:"status"`
	SchemaVersion    string         `json:"schema_version"`
	BriefHash        string         `json:"brief_hash"`
	ReportType       string         `json:"report_type"`
	Period           Period         `json:"period"`
	Workstreams      []Workstream   `json:"workstreams"`
	ExcludedFacts    []ExcludedFact `json:"excluded_facts"`
	NoReportableWork bool           `json:"no_reportable_work"`
}

func (s Stored) Accepted() Accepted {
	explicitExclusions := make([]ExcludedFact, 0, len(s.Payload.ExcludedFacts))
	for _, item := range s.Payload.ExcludedFacts {
		if item.Reason != automaticExclusionReason {
			explicitExclusions = append(explicitExclusions, item)
		}
	}
	return Accepted{
		Status: "accepted", SchemaVersion: s.Payload.SchemaVersion,
		BriefHash: s.BriefHash, ReportType: s.Payload.ReportType,
		Period: s.Payload.Period, Workstreams: s.Payload.Workstreams,
		ExcludedFacts:    explicitExclusions,
		NoReportableWork: s.Payload.NoReportableWork,
	}
}
