package reportmemory

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestClaimPendingRecoversExpiredSubmittingLease(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, 8, 3, 18, 5, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM report_project_memory_jobs.*status = 'submitting'.*lease_until.*FOR UPDATE SKIP LOCKED`).
		WithArgs(now, maxMemoryAttempts).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "report_id", "report_date", "dirty_from_date", "desired_source_fingerprint", "desired_evidence_watermark", "rebuild_required", "attempts",
		}).AddRow("305", "report-1", "2026-08-03", "2026-08-01", "fingerprint", 7, false, 1))
	mock.ExpectExec(`(?s)UPDATE report_project_memory_jobs SET.*status = 'submitting'`).
		WithArgs(now, "305", "worker-1", now.Add(defaultMemoryLeaseTTL)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	service := &NightlyService{db: database, config: normalizedNightlyConfig(NightlyConfig{
		Enabled: true, WorkerID: "worker-1", StartHour: 2, EndHour: 6,
	})}
	job, found, err := service.claimPending(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !found || job.Attempts != 2 || job.ClaimedSourceFingerprint != "fingerprint" {
		t.Fatalf("recovered job = %#v, found=%v", job, found)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimPendingRebuildsV2FromBoundedHistory(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, 8, 8, 18, 5, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM report_project_memory_jobs.*FOR UPDATE SKIP LOCKED`).
		WithArgs(now, maxMemoryAttempts).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "report_id", "report_date", "dirty_from_date", "desired_source_fingerprint", "desired_evidence_watermark", "rebuild_required", "attempts",
		}).AddRow("305", "report-8", "2026-08-08", "2026-08-03", "fingerprint", 7, true, 0))
	mock.ExpectExec("UPDATE report_project_memory_jobs SET snapshot_id = NULL").
		WithArgs("305").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM report_project_memory_snapshots").
		WithArgs("305", ResolverVersion).WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectExec("DELETE FROM report_projects").
		WithArgs("305").WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectQuery("SELECT min\\(report_date\\)::text").
		WithArgs("305", "2026-08-08", maxMemorySnapshotDepth).
		WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow("2026-07-20"))
	mock.ExpectExec("SET dirty_from_date").
		WithArgs("305", "2026-07-20").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE report_project_memory_jobs SET.*status = 'submitting'`).
		WithArgs(now, "305", "worker-1", now.Add(defaultMemoryLeaseTTL)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	service := &NightlyService{db: database, config: normalizedNightlyConfig(NightlyConfig{
		Enabled: true, WorkerID: "worker-1", StartHour: 2, EndHour: 6,
	})}
	job, found, err := service.claimPending(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !found || job.DirtyFromDate != "2026-07-20" || job.RebuildRequired {
		t.Fatalf("rebuilt job = %#v, found=%v", job, found)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
