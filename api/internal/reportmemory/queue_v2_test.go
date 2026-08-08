package reportmemory

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestQueueReportChangeCoalescesToOneUserMaintenanceJob(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("report-1", "305", "2026-08-08").
		WillReturnRows(sqlmock.NewRows([]string{"content", "run_id", "brief_signature"}).
			AddRow("1. AIDA", "run-1", "brief-1"))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("project-memory-queue:305").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT desired_source_fingerprint, COALESCE\\(last_event_fingerprint").
		WithArgs("305").WillReturnRows(sqlmock.NewRows([]string{"desired_source_fingerprint", "last_event_fingerprint", "desired_evidence_watermark"}))
	mock.ExpectExec("INSERT INTO report_project_memory_jobs").
		WithArgs("305", "2026-08-08", "report-1", sqlmock.AnyArg(), nextNightlyWindow(now), ResolverVersion, maxMemorySnapshotDepth).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := QueueReportChange(context.Background(), tx, "report-1", "305", "2026-08-08", now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestQueueReportChangeBuildsAggregateUserFingerprint(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC)
	oldFingerprint := memorySourceFingerprint("old")
	eventFingerprint := memorySourceFingerprint("report-2", "2026-08-07", "2. 修复", "run-2", "brief-2")
	wantFingerprint := memorySourceFingerprint(oldFingerprint, eventFingerprint, "8")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("report-2", "305", "2026-08-07").
		WillReturnRows(sqlmock.NewRows([]string{"content", "run_id", "brief_signature"}).
			AddRow("2. 修复", "run-2", "brief-2"))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("project-memory-queue:305").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT desired_source_fingerprint, COALESCE\\(last_event_fingerprint").
		WithArgs("305").WillReturnRows(sqlmock.NewRows([]string{"desired_source_fingerprint", "last_event_fingerprint", "desired_evidence_watermark"}).
		AddRow(oldFingerprint, memorySourceFingerprint("older-event"), 7))
	mock.ExpectExec("UPDATE report_project_memory_jobs SET").
		WithArgs("305", "2026-08-07", "report-2", wantFingerprint, int64(8), nextNightlyWindow(now), ResolverVersion, eventFingerprint).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := QueueReportChange(context.Background(), tx, "report-2", "305", "2026-08-07", now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestQueueReportChangeIgnoresRepeatedEvent(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC)
	eventFingerprint := memorySourceFingerprint("report-2", "2026-08-07", "2. 修复", "run-2", "brief-2")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("report-2", "305", "2026-08-07").
		WillReturnRows(sqlmock.NewRows([]string{"content", "run_id", "brief_signature"}).
			AddRow("2. 修复", "run-2", "brief-2"))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("project-memory-queue:305").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT desired_source_fingerprint, COALESCE\\(last_event_fingerprint").
		WithArgs("305").WillReturnRows(sqlmock.NewRows([]string{"desired_source_fingerprint", "last_event_fingerprint", "desired_evidence_watermark"}).
		AddRow(memorySourceFingerprint("aggregate"), eventFingerprint, 8))
	mock.ExpectCommit()
	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := QueueReportChange(context.Background(), tx, "report-2", "305", "2026-08-07", now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
