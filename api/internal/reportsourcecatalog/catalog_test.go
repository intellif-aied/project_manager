package reportsourcecatalog

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestEnsureSliceCreatesOnlyLightweightBuildingRows(t *testing.T) {
	database, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlContainsAll(
		[]string{"INSERT INTO report_source_slice_catalog", "'building'"},
		[]string{"session_content_events", "content_payload"},
	)))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	mock.ExpectExec("ensure slice").WithArgs("slice-1").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := EnsureSlice(context.Background(), database, "slice-1"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineReconcileDoesNotDiscoverMissingHistoricalRows(t *testing.T) {
	database, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlContainsAll(
		[]string{
			"INSERT INTO report_source_slice_catalog",
			"session_content_events",
			"$3::boolean OR existing.slice_id IS NOT NULL",
		},
		[]string{"content_payload", "excerpt"},
	)))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	mock.ExpectExec("online reconcile").
		WithArgs(nil, 10, false).
		WillReturnResult(sqlmock.NewResult(0, 2))
	count, err := ReconcileRevision(context.Background(), database, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count=%d want=2", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBackfillDiscoversMissingRowsAndIsBounded(t *testing.T) {
	database, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlContainsAll(
		[]string{"LIMIT $2", "FOR UPDATE OF sl SKIP LOCKED"},
		[]string{"content_payload", "excerpt"},
	)))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	mock.ExpectExec("backfill").
		WithArgs(nil, 7, true).
		WillReturnResult(sqlmock.NewResult(0, 7))
	count, err := BackfillBatch(context.Background(), database, 7)
	if err != nil {
		t.Fatal(err)
	}
	if count != 7 {
		t.Fatalf("count=%d want=7", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestActivateAndClearOnlyChangeCatalogState(t *testing.T) {
	database, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlContainsAll(
		[]string{"UPDATE report_source_slice_catalog"},
		[]string{"session_content_events", "content_payload"},
	)))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	mock.ExpectExec("activate").WithArgs("source-1", "revision-1").WillReturnResult(sqlmock.NewResult(0, 2))
	if err := ActivateRevision(context.Background(), database, "source-1", "revision-1"); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec("clear").WithArgs("session-1").WillReturnResult(sqlmock.NewResult(0, 2))
	if err := MarkSessionCleared(context.Background(), database, "session-1"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogOperationsValidateInputs(t *testing.T) {
	if err := EnsureSlice(context.Background(), nil, "slice"); err == nil {
		t.Fatal("EnsureSlice accepted nil database")
	}
	if _, err := ReconcileRevision(context.Background(), nil, "", 1); err == nil {
		t.Fatal("ReconcileRevision accepted nil database")
	}
	if _, err := BackfillBatch(context.Background(), noOpExecer{}, 0); err == nil {
		t.Fatal("BackfillBatch accepted zero batch")
	}
	if err := ActivateRevision(context.Background(), noOpExecer{}, "", "revision"); err == nil {
		t.Fatal("ActivateRevision accepted empty source")
	}
	if err := MarkSessionCleared(context.Background(), noOpExecer{}, ""); err == nil {
		t.Fatal("MarkSessionCleared accepted empty session")
	}
}

type noOpExecer struct{}

func (noOpExecer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return sqlmock.NewResult(0, 0), nil
}

func sqlContainsAll(required, forbidden []string) sqlmock.QueryMatcher {
	return sqlmock.QueryMatcherFunc(func(_ string, actual string) error {
		for _, value := range required {
			if !strings.Contains(actual, value) {
				return fmt.Errorf("SQL is missing %q", value)
			}
		}
		for _, value := range forbidden {
			if strings.Contains(actual, value) {
				return fmt.Errorf("SQL contains forbidden %q", value)
			}
		}
		return nil
	})
}
