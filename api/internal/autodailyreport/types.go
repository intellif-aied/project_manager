package autodailyreport

import (
	"context"
	"time"
)

const (
	TriggerSource    = "session_upload_auto"
	GuardModeAbsent  = "absent"
	GuardModeReplace = "replace"

	QuietPeriod = 2 * time.Minute
)

type Config struct {
	Enabled            bool       `json:"enabled"`
	EnabledSince       *time.Time `json:"enabled_since,omitempty"`
	UpdatedBy          *string    `json:"updated_by,omitempty"`
	UpdatedAt          time.Time  `json:"updated_at"`
	QuietPeriodSeconds int        `json:"quiet_period_seconds"`
}

type SourceSnapshot struct {
	UserID          string
	ReportDate      string
	Fingerprint     string
	SourceSliceKeys []string
	LatestReadyAt   time.Time
}

type ReportGuard struct {
	Mode      string     `json:"mode"`
	ReportID  string     `json:"report_id,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type SubmissionRequest struct {
	UserID            string
	ReportDate        string
	SourceFingerprint string
	SourceSliceKeys   []string
	Guard             ReportGuard
}

type Submitter interface {
	SubmitAutoDailyReport(context.Context, SubmissionRequest) (string, error)
}

type claimedJob struct {
	UserID            string
	ReportDate        string
	SourceFingerprint string
	SourceSliceKeys   []string
	LeaseOwner        string
}

type reportEligibility struct {
	Allowed bool
	Guard   ReportGuard
	Reason  string
}

type repository interface {
	GetConfig(context.Context) (Config, error)
	SetEnabled(context.Context, bool, string, time.Time) (Config, error)
	SuppressPending(context.Context) error
	DiscoverSourceSnapshots(context.Context, string, time.Time) ([]SourceSnapshot, error)
	ObserveSourceSnapshot(context.Context, SourceSnapshot, time.Duration) error
	ReconcileRuns(context.Context, bool, time.Time, time.Duration) error
	ClaimDue(context.Context, time.Time, string, time.Duration) (claimedJob, bool, error)
	LoadReportEligibility(context.Context, claimedJob) (reportEligibility, error)
	MarkRunning(context.Context, claimedJob, string) error
	MarkBlocked(context.Context, claimedJob, string) error
	MarkSubmissionFailed(context.Context, claimedJob, string, time.Time, time.Duration) error
}
