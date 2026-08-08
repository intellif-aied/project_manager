package reportmemory

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestBuildConsolidationInputUsesSelectedReportWorkspaceEvidence(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	job := queuedJob{
		UserID: "305", ReportID: "latest-report", ReportDate: "2026-08-08", DirtyFromDate: "2026-08-03",
	}
	mock.ExpectQuery("WITH next_report AS").
		WithArgs("305", "2026-08-08", "2026-08-03").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "report_date", "content", "generation_mode", "edited", "generated_hash", "brief", "has_outcome",
		}).AddRow("selected-report", "2026-08-03", "1. 芯片验证平台", "manual", false, "", "", false))
	mock.ExpectQuery("SELECT selection.id::text").
		WithArgs("selected-report", "305").
		WillReturnRows(sqlmock.NewRows([]string{"selection_id"}))
	mock.ExpectQuery("SELECT DISTINCT source.fact_ref").
		WithArgs("selected-report", "305").
		WillReturnRows(sqlmock.NewRows([]string{"fact_ref", "workspace_id"}))
	mock.ExpectQuery("SELECT r.id::text, r.report_date::text").
		WithArgs("305", "2026-08-03", "", maxRecentReports+maxHistoricalAnchors).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "report_date", "content", "generation_mode", "edited", "generated_hash", "brief", "has_outcome",
		}))
	mock.ExpectQuery("SELECT p.id::text, p.canonical_name").
		WithArgs("305", "2026-08-03", maxWorkstreamCues, maxMemorySnapshotDepth, ResolverVersion, maxAliasesPerProject).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "canonical_name", "last_seen_on", "source_type", "source_weight", "aliases", "cues", "workspaces",
		}))

	input, _, _, err := buildConsolidationInput(context.Background(), database, job)
	if err != nil {
		t.Fatal(err)
	}
	if input.ReportRef != "selected-report" || input.ReportDate != "2026-08-03" {
		t.Fatalf("selected input = %#v", input)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
