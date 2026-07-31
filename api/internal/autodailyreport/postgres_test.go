package autodailyreport

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func newPostgresRepositoryTest(t *testing.T) (*postgresRepository, sqlmock.Sqlmock) {
	t.Helper()
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return newPostgresRepository(database), mock
}

func TestSetEnabledStartsNewObservationBoundary(t *testing.T) {
	repository, mock := newPostgresRepositoryTest(t)
	changedAt := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT enabled, enabled_since`).
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "enabled_since"}).AddRow(false, nil))
	mock.ExpectQuery(`UPDATE auto_daily_report_config`).
		WithArgs(true, changedAt, "42", changedAt).
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "enabled_since", "updated_by", "updated_at"}).
			AddRow(true, changedAt, "42", changedAt))
	mock.ExpectExec(`INSERT INTO auto_daily_report_config_events`).
		WithArgs(true, changedAt, "42", changedAt).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	config, err := repository.SetEnabled(context.Background(), true, "42", changedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !config.Enabled || config.EnabledSince == nil || !config.EnabledSince.Equal(changedAt) || config.UpdatedBy == nil || *config.UpdatedBy != "42" {
		t.Fatalf("unexpected config: %#v", config)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSetEnabledKeepsBoundaryForIdempotentEnable(t *testing.T) {
	repository, mock := newPostgresRepositoryTest(t)
	originalBoundary := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	changedAt := originalBoundary.Add(time.Hour)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT enabled, enabled_since`).
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "enabled_since"}).AddRow(true, originalBoundary))
	mock.ExpectQuery(`UPDATE auto_daily_report_config`).
		WithArgs(true, originalBoundary, "42", changedAt).
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "enabled_since", "updated_by", "updated_at"}).
			AddRow(true, originalBoundary, "42", changedAt))
	mock.ExpectExec(`INSERT INTO auto_daily_report_config_events`).
		WithArgs(true, originalBoundary, "42", changedAt).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	config, err := repository.SetEnabled(context.Background(), true, "42", changedAt)
	if err != nil {
		t.Fatal(err)
	}
	if config.EnabledSince == nil || !config.EnabledSince.Equal(originalBoundary) {
		t.Fatalf("idempotent enable reset enabled_since: %#v", config)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSetEnabledDisablesPendingButLeavesClaimedAndRunningWork(t *testing.T) {
	repository, mock := newPostgresRepositoryTest(t)
	previousBoundary := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	changedAt := previousBoundary.Add(time.Hour)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT enabled, enabled_since`).
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "enabled_since"}).AddRow(true, previousBoundary))
	mock.ExpectQuery(`UPDATE auto_daily_report_config`).
		WithArgs(false, nil, "42", changedAt).
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "enabled_since", "updated_by", "updated_at"}).
			AddRow(false, nil, "42", changedAt))
	mock.ExpectExec(`INSERT INTO auto_daily_report_config_events`).
		WithArgs(false, nil, "42", changedAt).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`(?s)UPDATE auto_daily_report_states.*status IN \('pending', 'failed'\)`).
		WithArgs(changedAt).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	config, err := repository.SetEnabled(context.Background(), false, "42", changedAt)
	if err != nil {
		t.Fatal(err)
	}
	if config.Enabled || config.EnabledSince != nil {
		t.Fatalf("unexpected disabled config: %#v", config)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverSourceSnapshotsGroupsByActualOwner(t *testing.T) {
	repository, mock := newPostgresRepositoryTest(t)
	enabledSince := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	readyAt := enabledSince.Add(time.Minute)
	fingerprint := strings.Repeat("a", 64)
	keys := `{00000000-0000-4000-8000-000000000001,00000000-0000-4000-8000-000000000002}`
	mock.ExpectQuery(`(?s)FROM report_source_slice_catalog catalog.*catalog.activity_end_at >= \$1.*catalog.activity_start_at < \$2.*GROUP BY catalog.user_id.*HAVING max\(catalog.ready_at\) >= \$3`).
		WithArgs(
			time.Date(2026, 7, 30, 16, 0, 0, 0, time.UTC),
			time.Date(2026, 7, 31, 16, 0, 0, 0, time.UTC),
			enabledSince,
		).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "fingerprint", "slice_keys", "ready_at"}).
			AddRow("101", fingerprint, keys, readyAt))

	snapshots, err := repository.DiscoverSourceSnapshots(context.Background(), "2026-07-31", enabledSince)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 || snapshots[0].UserID != "101" || snapshots[0].ReportDate != "2026-07-31" ||
		snapshots[0].Fingerprint != fingerprint || len(snapshots[0].SourceSliceKeys) != 2 || !snapshots[0].LatestReadyAt.Equal(readyAt) {
		t.Fatalf("unexpected snapshots: %#v", snapshots)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestObserveSourceSnapshotCastsReadyTimestamp(t *testing.T) {
	repository, mock := newPostgresRepositoryTest(t)
	readyAt := time.Date(2026, 7, 31, 9, 17, 27, 0, time.UTC)
	fingerprint := strings.Repeat("b", 64)
	key := "00000000-0000-4000-8000-000000000001"
	mock.ExpectExec(`(?s)INSERT INTO auto_daily_report_states.*\$5::timestamptz.*\$5::timestamptz \+ make_interval`).
		WithArgs("305", "2026-07-31", fingerprint, sqlmock.AnyArg(), readyAt, 120).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repository.ObserveSourceSnapshot(context.Background(), SourceSnapshot{
		UserID: "305", ReportDate: "2026-07-31", Fingerprint: fingerprint,
		SourceSliceKeys: []string{key}, LatestReadyAt: readyAt,
	}, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSuppressPendingRecoversAlreadyCreatedRunBeforeDroppingExpiredClaim(t *testing.T) {
	repository, mock := newPostgresRepositoryTest(t)
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE auto_daily_report_states state.*FROM ai_runs run.*run.idempotency_key = 'auto-daily:'`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE auto_daily_report_states.*status = 'submitting' AND lease_until <= now\(\)`).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	if err := repository.SuppressPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimDueReturnsFrozenClaimAfterLease(t *testing.T) {
	repository, mock := newPostgresRepositoryTest(t)
	now := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	fingerprint := strings.Repeat("b", 64)
	key := "00000000-0000-4000-8000-000000000001"
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT state.user_id::text, state.report_date::text.*FOR UPDATE SKIP LOCKED`).
		WithArgs(now).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "report_date", "status", "desired_fingerprint", "desired_keys", "claimed_fingerprint", "claimed_keys",
		}).AddRow("101", "2026-07-31", "pending", fingerprint, "{"+key+"}", nil, nil))
	mock.ExpectExec(`UPDATE auto_daily_report_states`).
		WithArgs(now, "101", fingerprint, sqlmock.AnyArg(), "worker-1", now.Add(2*time.Minute), "2026-07-31").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	job, found, err := repository.ClaimDue(context.Background(), now, "worker-1", 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !found || job.UserID != "101" || job.ReportDate != "2026-07-31" || job.SourceFingerprint != fingerprint ||
		len(job.SourceSliceKeys) != 1 || job.SourceSliceKeys[0] != key || job.LeaseOwner != "worker-1" {
		t.Fatalf("unexpected job: found=%v job=%#v", found, job)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReportEligibilityTreatsAnyUserSaveAsOwnership(t *testing.T) {
	repository, mock := newPostgresRepositoryTest(t)
	updatedAt := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)report_user_outcome_events.*FROM daily_reports report`).
		WithArgs("101", "2026-07-31").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "edited", "status", "generation_mode", "trigger_source", "has_user_outcome", "updated_at",
		}).AddRow("report-1", false, "saved", "managed_agent", TriggerSource, true, updatedAt))

	eligibility, err := repository.LoadReportEligibility(context.Background(), claimedJob{UserID: "101", ReportDate: "2026-07-31"})
	if err != nil {
		t.Fatal(err)
	}
	if eligibility.Allowed || eligibility.Reason == "" {
		t.Fatalf("user-owned report was eligible: %#v", eligibility)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
