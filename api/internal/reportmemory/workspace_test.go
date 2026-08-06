package reportmemory

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	_ "github.com/lib/pq"
)

func TestNormalizeWorkspacePath(t *testing.T) {
	tests := map[string]string{
		"/home/liule/CATP_Platform/":      `/home/liule/CATP_Platform`,
		`C:\\work\\chip_test\\`:           `c:/work/chip_test`,
		" /data1/chip_test/../chip_test ": `/data1/chip_test`,
		"":                                "",
		"relative":                        "",
		"/":                               "",
	}
	for input, want := range tests {
		if got := normalizeWorkspacePath(input); got != want {
			t.Errorf("normalizeWorkspacePath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWorkspaceHashIsUserScopedAndDeterministic(t *testing.T) {
	first := workspaceHash("identity", "21", "cwd", "/home/liule/CATP_Platform")
	again := workspaceHash("identity", "21", "cwd", "/home/liule/CATP_Platform")
	otherUser := workspaceHash("identity", "4", "cwd", "/home/liule/CATP_Platform")
	if first != again {
		t.Fatal("workspace identity hash is not deterministic")
	}
	if first == otherUser {
		t.Fatal("workspace identity hash must be scoped by AIDA user")
	}
	if len(first) != 64 {
		t.Fatalf("workspace identity hash length = %d, want 64", len(first))
	}
}

func TestMaterializeWorkspaceEvidenceForReportPersistsHashedIdentity(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	started := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	ended := started.Add(time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT selection.id::text")).
		WithArgs("report-1", "21").
		WillReturnRows(sqlmock.NewRows([]string{"selection_id"}).AddRow("selection-1"))
	mock.ExpectBegin()
	mock.ExpectExec("pg_advisory_xact_lock").
		WithArgs("report-workspace:21").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT selection.id::text, item.session_id::text,")).
		WithArgs("selection-1", "21").
		WillReturnRows(sqlmock.NewRows([]string{
			"selection_id", "session_id", "slice_id", "revision_id", "start_cursor", "end_cursor", "activity_start_at", "activity_end_at", "cwd", "repository_key",
		}).AddRow("selection-1", "session-1", "slice-1", "revision-1", 10, 20, started, ended, "/home/liule/CATP_Platform/", ""))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT workspace_id::text")).
		WithArgs("21", "cwd", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id"}))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO report_workspaces (")).
		WithArgs("21", started, ended, workspaceIdentityVersion).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("workspace-1"))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO report_workspace_keys (")).
		WithArgs("21", "workspace-1", "cwd", sqlmock.AnyArg(), started, ended).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO report_workspace_evidence (")).
		WithArgs("21", "workspace-1", sqlmock.AnyArg(), "cwd", "selection-1", "session-1", "slice-1", "revision-1", int64(10), int64(20), started, ended).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	stats, err := materializeWorkspaceEvidenceForReport(context.Background(), database, "21", "report-1")
	if err != nil {
		t.Fatal(err)
	}
	if stats.IdentitiesObserved != 1 || stats.EvidenceCreated != 1 {
		t.Fatalf("workspace evidence stats = %+v", stats)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestResolveOrCreateWorkspaceAttachesGitKeyToExistingCWDWorkspace(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	started := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	ended := started.Add(time.Hour)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT workspace_id::text")).
		WithArgs("21", "git_repository", "git-key").
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT workspace_id::text")).
		WithArgs("21", "cwd", "cwd-key").
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id"}).AddRow("workspace-1"))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE report_workspaces SET")).
		WithArgs("workspace-1", started, ended).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO report_workspace_keys (")).
		WithArgs("21", "workspace-1", "git_repository", "git-key", started, ended).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO report_workspace_keys (")).
		WithArgs("21", "workspace-1", "cwd", "cwd-key", started, ended).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID, err := resolveOrCreateWorkspace(context.Background(), tx, "21", []workspaceIdentityKey{
		{Kind: "git_repository", Hash: "git-key"},
		{Kind: "cwd", Hash: "cwd-key"},
	}, started, ended)
	if err != nil {
		t.Fatal(err)
	}
	if workspaceID != "workspace-1" {
		t.Fatalf("workspace id = %q, want workspace-1", workspaceID)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMaterializeWorkspaceEvidenceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("REPORT_WORKSPACE_INTEGRATION_DATABASE_URL")
	userID := os.Getenv("REPORT_WORKSPACE_INTEGRATION_USER_ID")
	reportID := os.Getenv("REPORT_WORKSPACE_INTEGRATION_REPORT_ID")
	if dsn == "" || userID == "" || reportID == "" {
		t.Skip("report workspace integration environment is not set")
	}
	database, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	stats, err := materializeWorkspaceEvidenceForReport(context.Background(), database, userID, reportID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.IdentitiesObserved == 0 {
		t.Fatal("expected at least one workspace identity")
	}
	t.Logf("workspaces=%d evidence_created=%d", stats.IdentitiesObserved, stats.EvidenceCreated)
}
