package reportmemory

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestLoadHistoricalHintsKeepsAcceptedProjectAsUnanchoredCandidate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	mock.ExpectQuery("SELECT project_memory_json").
		WithArgs("5192", "2026-07-31").
		WillReturnRows(sqlmock.NewRows([]string{"project_memory_json"}).AddRow(
			`{"projects":[{"project_ref":"project-1"}]}`,
		))
	mock.ExpectQuery("FROM report_projects p").
		WithArgs("5192", "2026-07-31").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "canonical_name", "last_seen_on", "alias", "normalized_alias",
			"alias_type", "source_type", "source_weight",
		}).AddRow(
			"project-1", "芯片验证平台", "2026-07-30", "芯片验证平台", "芯片验证平台",
			"canonical", "manual_final", 1.0,
		))
	mock.ExpectQuery("SELECT alias FROM report_project_aliases").
		WithArgs("project-1", "5192", maxAliasesPerProject).
		WillReturnRows(sqlmock.NewRows([]string{"alias"}).AddRow("芯片验证平台"))

	hints, err := LoadHistoricalHints(context.Background(), tx, HintRequest{
		UserID: "5192", ReportDate: "2026-07-31",
		Facts: []FactInput{{FactRef: "fact-001", Text: "完成测试执行模块改造方案设计"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hints) != 1 {
		t.Fatalf("hints = %#v, want one accepted project candidate", hints)
	}
	if !hints[0].CandidateOnly || hints[0].CanonicalName != "芯片验证平台" || len(hints[0].MatchedFactRef) != 0 {
		t.Fatalf("candidate = %#v", hints[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
