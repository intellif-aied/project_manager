package reportcontext

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/reportsource"
)

type sourceStub struct {
	page  reportsource.ContentPage
	err   error
	calls int
}

func (s *sourceStub) ReadAttachedSelection(context.Context, string, string, string, string, reportsource.Period, string) (reportsource.ContentPage, error) {
	s.calls++
	return s.page, s.err
}

func TestBuildPersonalDailyStoresCompleteFrozenContext(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("FROM report_run_contexts c").WithArgs("run-1", "7").WillReturnRows(
		sqlmock.NewRows([]string{"context_payload", "context_hash", "context_bytes"}),
	)
	mock.ExpectBegin()
	mock.ExpectQuery("FROM ai_runs").WithArgs("run-1", "7").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("FROM users u LEFT JOIN teams").WithArgs("7").WillReturnRows(
		sqlmock.NewRows([]string{"id", "name", "team_id", "team_name"}).AddRow("7", "测试用户", "team-1", "研发一组"),
	)
	mock.ExpectQuery("FROM requirements r").WillReturnRows(emptyRequirementRows())
	mock.ExpectQuery("FROM tasks t").WillReturnRows(emptyTaskRows())
	mock.ExpectExec("INSERT INTO report_run_contexts").
		WithArgs("run-1", SchemaVersion, "selection-1", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	source := &sourceStub{page: reportsource.ContentPage{FrozenPayload: json.RawMessage(`{"content_mode":"digest_v2","coverage":{"complete":true},"items":[{"summary":"done"}]}`)}}
	svc := &Service{db: db, source: source}
	stored, err := svc.Build(context.Background(), BuildRequest{
		UserID: "7", RunID: "run-1", ReportType: ReportTypePersonalDaily,
		Period:   reportsource.Period{Start: "2026-07-16", End: "2026-07-16"},
		Timezone: biztime.Zone, TriggerSource: "manual", ModelID: "model-1",
		Target: Target{Type: "self", UserID: "7"}, SourceSelectionID: "selection-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 || stored.Bytes == 0 || len(stored.Hash) != 64 {
		t.Fatalf("unexpected stored context: calls=%d context=%+v", source.calls, stored)
	}
	var payload Payload
	if err := json.Unmarshal(stored.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SchemaVersion != SchemaVersion || !payload.SourceState.CoverageComplete || payload.SourceState.SourceMode != "sessions_only" || payload.SourceState.Mode != "digest_v2" {
		t.Fatalf("unexpected payload state: %+v", payload.SourceState)
	}
	if len(payload.Sessions) != 1 || len(payload.Requirements) != 0 || len(payload.Tasks) != 0 {
		t.Fatalf("unexpected payload facts: %+v", payload)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildReturnsExistingContextWithoutReadingSources(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	payload := []byte(`{"schema_version":"report-context/v1"}`)
	mock.ExpectQuery("FROM report_run_contexts c").WithArgs("run-1", "7").WillReturnRows(
		sqlmock.NewRows([]string{"context_payload", "context_hash", "context_bytes"}).AddRow(payload, "abc", len(payload)),
	)
	source := &sourceStub{}
	svc := &Service{db: db, source: source}
	stored, err := svc.Build(context.Background(), BuildRequest{
		UserID: "7", RunID: "run-1", ReportType: ReportTypePersonalDaily,
		Period: reportsource.Period{Start: "2026-07-16", End: "2026-07-16"}, Timezone: biztime.Zone,
		Target: Target{Type: "self", UserID: "7"}, SourceSelectionID: "selection-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Hash != "abc" || source.calls != 0 {
		t.Fatalf("existing context was not reused: %+v calls=%d", stored, source.calls)
	}
}

func TestPersonalWeeklyAllowsNoSessionSelection(t *testing.T) {
	request := BuildRequest{
		UserID: "7", RunID: "run-1", ReportType: ReportTypePersonalWeekly,
		Period: reportsource.Period{Start: "2026-07-13", End: "2026-07-19"}, Timezone: biztime.Zone,
		Target: Target{Type: "self", UserID: "7"},
	}
	if err := request.validate(); err != nil {
		t.Fatalf("personal weekly without selection must be valid: %v", err)
	}
	request.ReportType = ReportTypePersonalDaily
	if err := request.validate(); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("personal daily without selection must fail, got %v", err)
	}
}

func TestCollectMemberCoverageKeepsMissingAndInvalidMembers(t *testing.T) {
	members := []Actor{{ID: "1", Name: "甲"}, {ID: "2", Name: "乙"}, {ID: "3", Name: "丙"}}
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	rows := []reportRow{
		{ID: "r1", Owner: members[0], Period: reportsource.Period{Start: "2026-07-16", End: "2026-07-16"}, Content: "完成 A", UpdatedAt: now},
		{ID: "r3", Owner: members[2], Period: reportsource.Period{Start: "2026-07-16", End: "2026-07-16"}, Content: "  ", UpdatedAt: now},
	}
	reports, coverage, issues, err := collectMemberCoverage(members, rows, ReportTypePersonalDaily, "2026-07-16")
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || len(coverage) != 3 || len(issues) != 2 {
		t.Fatalf("unexpected coverage result: reports=%d coverage=%+v issues=%+v", len(reports), coverage, issues)
	}
	if coverage[1].SourceStatus != "missing" || coverage[2].SourceStatus != "invalid" || coverage[2].ReportID != "r3" {
		t.Fatalf("missing/invalid semantics lost: %+v", coverage)
	}
}

func TestCollectMemberCoverageRejectsDuplicateReports(t *testing.T) {
	member := Actor{ID: "1", Name: "甲"}
	rows := []reportRow{{ID: "r1", Owner: member, Content: "a"}, {ID: "r2", Owner: member, Content: "b"}}
	_, _, _, err := collectMemberCoverage([]Actor{member}, rows, ReportTypePersonalDaily, "")
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestWeeklyReportWithMismatchedEndIsInvalid(t *testing.T) {
	member := Actor{ID: "1", Name: "甲"}
	rows := []reportRow{{
		ID: "r1", Owner: member,
		Period:  reportsource.Period{Start: "2026-07-13", End: "2026-07-18"},
		Content: "周报正文",
	}}
	reports, coverage, issues, err := collectMemberCoverage([]Actor{member}, rows, ReportTypePersonalWeekly, "2026-07-19")
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 0 || len(issues) != 1 || coverage[0].SourceStatus != "invalid" || coverage[0].InvalidReason != "period_mismatch" {
		t.Fatalf("mismatched weekly period was not rejected: reports=%+v coverage=%+v issues=%+v", reports, coverage, issues)
	}
}

func TestBuildRejectsMissingFrozenPayload(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("FROM report_run_contexts c").WithArgs("run-1", "7").WillReturnRows(
		sqlmock.NewRows([]string{"context_payload", "context_hash", "context_bytes"}),
	)
	svc := &Service{db: db, source: &sourceStub{page: reportsource.ContentPage{}}}
	_, err = svc.Build(context.Background(), BuildRequest{
		UserID: "7", RunID: "run-1", ReportType: ReportTypePersonalDaily,
		Period: reportsource.Period{Start: "2026-07-16", End: "2026-07-16"}, Timezone: biztime.Zone,
		Target: Target{Type: "self", UserID: "7"}, SourceSelectionID: "selection-1",
	})
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("expected ErrIncomplete, got %v", err)
	}
}

func TestGetScopesContextToRunOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("FROM report_run_contexts c").
		WithArgs("run-1", "7").
		WillReturnRows(sqlmock.NewRows([]string{"context_payload", "context_hash", "context_bytes"}).AddRow([]byte(`{"schema_version":"report-context/v1"}`), "abc", 38))
	svc := &Service{db: db}
	stored, err := svc.Get(context.Background(), "7", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Hash != "abc" {
		t.Fatalf("unexpected hash %q", stored.Hash)
	}
}

func emptyRequirementRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "title", "description", "status", "priority", "progress", "deadline", "creator_id", "creator_name", "creator_team_id", "creator_team_name", "responsibles", "team_ids", "updated_at"})
}

func emptyTaskRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "requirement_id", "requirement_title", "title", "status", "priority", "progress", "due_date", "creator_id", "creator_name", "creator_team_id", "creator_team_name", "responsibles", "updated_at"})
}
