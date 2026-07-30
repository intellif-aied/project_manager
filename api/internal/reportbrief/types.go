package reportbrief

import "errors"

const (
	SchemaVersion            = "report-brief/v1"
	MaxPayloadBytes          = 128 * 1024
	MaxWorkstreams           = 8
	MaxDeliverables          = 8
	MaxBriefInvalidAttempts  = 2
	MaxResultInvalidAttempts = 1
	FactRefJSONPattern       = `^fact-[0-9]{3,}$`
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
	Title        string        `json:"title"`
	Objective    string        `json:"objective"`
	Deliverables []Deliverable `json:"deliverables"`
}

type Deliverable struct {
	Result      string   `json:"result"`
	State       string   `json:"state"`
	Environment string   `json:"environment"`
	Validation  string   `json:"validation,omitempty"`
	NextAction  string   `json:"next_action,omitempty"`
	FactRefs    []string `json:"fact_refs"`
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
	return Accepted{
		Status: "accepted", SchemaVersion: s.Payload.SchemaVersion,
		BriefHash: s.BriefHash, ReportType: s.Payload.ReportType,
		Period: s.Payload.Period, Workstreams: s.Payload.Workstreams,
		ExcludedFacts:    s.Payload.ExcludedFacts,
		NoReportableWork: s.Payload.NoReportableWork,
	}
}
