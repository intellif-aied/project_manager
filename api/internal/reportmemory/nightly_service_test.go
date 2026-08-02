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
			"user_id", "report_id", "report_date", "desired_source_fingerprint", "attempts",
		}).AddRow("305", "report-1", "2026-08-03", "fingerprint", 1))
	mock.ExpectExec(`(?s)UPDATE report_project_memory_jobs SET.*status = 'submitting'`).
		WithArgs(now, "305", "2026-08-03", "worker-1", now.Add(defaultMemoryLeaseTTL)).
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
