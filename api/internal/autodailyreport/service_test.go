package autodailyreport

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeRepository struct {
	config             Config
	snapshots          []SourceSnapshot
	jobs               []claimedJob
	eligibility        map[string]reportEligibility
	discoveredDate     string
	discoveredSince    time.Time
	observed           []SourceSnapshot
	observedQuiet      []time.Duration
	reconciled         int
	reconciledEnabled  bool
	suppressed         int
	running            []string
	blocked            []string
	failed             []string
	setEnabledOperator string
}

func (f *fakeRepository) GetConfig(context.Context) (Config, error) { return f.config, nil }

func (f *fakeRepository) SetEnabled(_ context.Context, enabled bool, operator string, at time.Time) (Config, error) {
	f.config.Enabled = enabled
	f.config.UpdatedAt = at
	f.setEnabledOperator = operator
	if enabled {
		f.config.EnabledSince = &at
	} else {
		f.config.EnabledSince = nil
	}
	return f.config, nil
}

func (f *fakeRepository) SuppressPending(context.Context) error {
	f.suppressed++
	return nil
}

func (f *fakeRepository) DiscoverSourceSnapshots(_ context.Context, date string, since time.Time) ([]SourceSnapshot, error) {
	f.discoveredDate = date
	f.discoveredSince = since
	return append([]SourceSnapshot(nil), f.snapshots...), nil
}

func (f *fakeRepository) ObserveSourceSnapshot(_ context.Context, snapshot SourceSnapshot, quiet time.Duration) error {
	f.observed = append(f.observed, snapshot)
	f.observedQuiet = append(f.observedQuiet, quiet)
	return nil
}

func (f *fakeRepository) ReconcileRuns(_ context.Context, enabled bool, _ time.Time, _ time.Duration) error {
	f.reconciled++
	f.reconciledEnabled = enabled
	return nil
}

func (f *fakeRepository) ClaimDue(_ context.Context, _ time.Time, _ string, _ time.Duration) (claimedJob, bool, error) {
	if len(f.jobs) == 0 {
		return claimedJob{}, false, nil
	}
	job := f.jobs[0]
	f.jobs = f.jobs[1:]
	return job, true, nil
}

func (f *fakeRepository) LoadReportEligibility(_ context.Context, job claimedJob) (reportEligibility, error) {
	if eligibility, found := f.eligibility[job.UserID]; found {
		return eligibility, nil
	}
	return reportEligibility{Allowed: true, Guard: ReportGuard{Mode: GuardModeAbsent}}, nil
}

func (f *fakeRepository) MarkRunning(_ context.Context, job claimedJob, runID string) error {
	f.running = append(f.running, job.UserID+":"+runID)
	return nil
}

func (f *fakeRepository) MarkBlocked(_ context.Context, job claimedJob, _ string) error {
	f.blocked = append(f.blocked, job.UserID)
	return nil
}

func (f *fakeRepository) MarkSubmissionFailed(_ context.Context, job claimedJob, _ string, _ time.Time, _ time.Duration) error {
	f.failed = append(f.failed, job.UserID)
	return nil
}

type fakeSubmitter struct {
	requests []SubmissionRequest
	errFor   map[string]error
}

func (f *fakeSubmitter) SubmitAutoDailyReport(_ context.Context, request SubmissionRequest) (string, error) {
	f.requests = append(f.requests, request)
	if err := f.errFor[request.UserID]; err != nil {
		return "", err
	}
	return "run-" + request.UserID, nil
}

func enabledConfig(since time.Time) Config {
	return Config{Enabled: true, EnabledSince: &since, UpdatedAt: since}
}

func TestRunOnceDisabledOnlyReconcilesAndSuppresses(t *testing.T) {
	repo := &fakeRepository{config: Config{Enabled: false}}
	submitter := &fakeSubmitter{}
	service, err := newService(repo, submitter, "worker-1")
	if err != nil {
		t.Fatal(err)
	}

	if err := service.RunOnce(context.Background(), time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if repo.reconciled != 1 || repo.reconciledEnabled || repo.suppressed != 1 {
		t.Fatalf("unexpected disabled calls: reconciled=%d enabled=%v suppressed=%d", repo.reconciled, repo.reconciledEnabled, repo.suppressed)
	}
	if repo.discoveredDate != "" || len(submitter.requests) != 0 {
		t.Fatalf("disabled scheduler discovered or submitted work: date=%q requests=%d", repo.discoveredDate, len(submitter.requests))
	}
}

func TestRunOnceUsesShanghaiDateAndSubmitsEachActualOwner(t *testing.T) {
	since := time.Date(2026, 7, 31, 15, 0, 0, 0, time.UTC)
	fingerprint1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fingerprint2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	repo := &fakeRepository{
		config: enabledConfig(since),
		snapshots: []SourceSnapshot{
			{UserID: "101", ReportDate: "2026-08-01", Fingerprint: fingerprint1, SourceSliceKeys: []string{"slice-a"}},
			{UserID: "202", ReportDate: "2026-08-01", Fingerprint: fingerprint2, SourceSliceKeys: []string{"slice-b", "slice-c"}},
		},
		jobs: []claimedJob{
			{UserID: "101", ReportDate: "2026-08-01", SourceFingerprint: fingerprint1, SourceSliceKeys: []string{"slice-a"}, LeaseOwner: "worker-1"},
			{UserID: "202", ReportDate: "2026-08-01", SourceFingerprint: fingerprint2, SourceSliceKeys: []string{"slice-b", "slice-c"}, LeaseOwner: "worker-1"},
		},
	}
	submitter := &fakeSubmitter{}
	service, err := newService(repo, submitter, "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 31, 16, 5, 0, 0, time.UTC)
	if err := service.RunOnce(context.Background(), now); err != nil {
		t.Fatal(err)
	}

	if repo.discoveredDate != "2026-08-01" || !repo.discoveredSince.Equal(since) {
		t.Fatalf("unexpected discovery boundary: date=%s since=%s", repo.discoveredDate, repo.discoveredSince)
	}
	if len(repo.observed) != 2 || !reflect.DeepEqual(repo.observedQuiet, []time.Duration{QuietPeriod, QuietPeriod}) {
		t.Fatalf("unexpected observations: %#v quiet=%#v", repo.observed, repo.observedQuiet)
	}
	if len(submitter.requests) != 2 {
		t.Fatalf("expected two owner requests, got %d", len(submitter.requests))
	}
	if submitter.requests[0].UserID != "101" || submitter.requests[1].UserID != "202" {
		t.Fatalf("requests were not grouped by actual owner: %#v", submitter.requests)
	}
	if !reflect.DeepEqual(submitter.requests[1].SourceSliceKeys, []string{"slice-b", "slice-c"}) {
		t.Fatalf("frozen multi-host sources changed: %#v", submitter.requests[1].SourceSliceKeys)
	}
	if !reflect.DeepEqual(repo.running, []string{"101:run-101", "202:run-202"}) {
		t.Fatalf("unexpected attached runs: %#v", repo.running)
	}
}

func TestRunOnceBlocksProtectedReportWithoutSubmission(t *testing.T) {
	since := time.Now().Add(-time.Hour)
	repo := &fakeRepository{
		config: enabledConfig(since),
		jobs:   []claimedJob{{UserID: "101", ReportDate: "2026-07-31", SourceFingerprint: "a", LeaseOwner: "worker-1"}},
		eligibility: map[string]reportEligibility{
			"101": {Allowed: false, Reason: "manual report"},
		},
	}
	submitter := &fakeSubmitter{}
	service, _ := newService(repo, submitter, "worker-1")
	if err := service.RunOnce(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(submitter.requests) != 0 || !reflect.DeepEqual(repo.blocked, []string{"101"}) {
		t.Fatalf("protected report was submitted: requests=%d blocked=%#v", len(submitter.requests), repo.blocked)
	}
}

func TestRunOnceRecordsSubmissionFailureWithoutImmediateRetry(t *testing.T) {
	since := time.Now().Add(-time.Hour)
	repo := &fakeRepository{
		config: enabledConfig(since),
		jobs:   []claimedJob{{UserID: "101", ReportDate: "2026-07-31", SourceFingerprint: "a", LeaseOwner: "worker-1"}},
	}
	submitter := &fakeSubmitter{errFor: map[string]error{"101": errors.New("agent unavailable")}}
	service, _ := newService(repo, submitter, "worker-1")
	if err := service.RunOnce(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(submitter.requests) != 1 || !reflect.DeepEqual(repo.failed, []string{"101"}) || len(repo.running) != 0 {
		t.Fatalf("unexpected failure handling: requests=%d failed=%#v running=%#v", len(submitter.requests), repo.failed, repo.running)
	}
}

func TestReportEligibilityOnlyAllowsUntouchedUploadAutoReport(t *testing.T) {
	updatedAt := time.Date(2026, 7, 31, 12, 0, 0, 123000, time.UTC)
	tests := []struct {
		name     string
		existing *existingDailyReport
		allowed  bool
		mode     string
	}{
		{name: "absent", allowed: true, mode: GuardModeAbsent},
		{name: "upload auto", existing: &existingDailyReport{ID: "r1", Status: "saved", GenerationMode: "managed_agent", TriggerSource: TriggerSource, UpdatedAt: updatedAt}, allowed: true, mode: GuardModeReplace},
		{name: "manual", existing: &existingDailyReport{ID: "r1", Status: "saved", GenerationMode: "default", UpdatedAt: updatedAt}},
		{name: "other AI trigger", existing: &existingDailyReport{ID: "r1", Status: "saved", GenerationMode: "managed_agent", TriggerSource: "manual", UpdatedAt: updatedAt}},
		{name: "edited", existing: &existingDailyReport{ID: "r1", Edited: true, Status: "saved", GenerationMode: "managed_agent", TriggerSource: TriggerSource, UpdatedAt: updatedAt}},
		{name: "submitted", existing: &existingDailyReport{ID: "r1", Status: "submitted", GenerationMode: "managed_agent", TriggerSource: TriggerSource, UpdatedAt: updatedAt}},
		{name: "user saved", existing: &existingDailyReport{ID: "r1", Status: "saved", GenerationMode: "managed_agent", TriggerSource: TriggerSource, HasUserOutcome: true, UpdatedAt: updatedAt}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := reportEligibilityFor(test.existing)
			if got.Allowed != test.allowed || got.Guard.Mode != test.mode {
				t.Fatalf("got allowed=%v mode=%q reason=%q", got.Allowed, got.Guard.Mode, got.Reason)
			}
			if test.mode == GuardModeReplace && (got.Guard.ReportID != "r1" || got.Guard.UpdatedAt == nil || !got.Guard.UpdatedAt.Equal(updatedAt)) {
				t.Fatalf("replace guard did not freeze the report snapshot: %#v", got.Guard)
			}
		})
	}
}

func TestSetEnabledReturnsRuntimeMetadata(t *testing.T) {
	repo := &fakeRepository{}
	service, _ := newService(repo, &fakeSubmitter{}, "worker-1")
	config, err := service.SetEnabled(context.Background(), true, "42")
	if err != nil {
		t.Fatal(err)
	}
	if !config.Enabled || config.EnabledSince == nil || config.QuietPeriodSeconds != 120 || repo.setEnabledOperator != "42" {
		t.Fatalf("unexpected config: %#v operator=%q", config, repo.setEnabledOperator)
	}
}
