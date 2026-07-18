package reportcontext

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/internal/reportsource"
)

type sourceStub struct {
	page reportsource.ContentPage
	err  error
}

func (s sourceStub) ReadAttachedSelection(context.Context, string, string, string, string, reportsource.Period, string) (reportsource.ContentPage, error) {
	return s.page, s.err
}

func TestBuildPersonalStoresFrozenContext(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec("INSERT INTO report_run_contexts").
		WithArgs("run-1", SchemaVersion, "selection-1", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	svc := &Service{db: db, source: sourceStub{page: reportsource.ContentPage{
		FrozenPayload: json.RawMessage(`{"content_mode":"digest_v2","coverage":{"complete":true},"has_more":false,"items":[{"summary":"done"}]}`),
	}}}
	stored, err := svc.BuildPersonal(context.Background(), "7", "run-1", "selection-1", "personal_daily", reportsource.Period{Start: "2026-07-16", End: "2026-07-16"}, map[string]any{"type": "self"})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Bytes == 0 || len(stored.Hash) != 64 {
		t.Fatalf("unexpected stored context: %+v", stored)
	}
	var payload Payload
	if err := json.Unmarshal(stored.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SchemaVersion != SchemaVersion || !payload.SourceState.CoverageComplete {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildPersonalRejectsMissingFrozenPayload(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{db: db, source: sourceStub{page: reportsource.ContentPage{}}}
	_, err = svc.BuildPersonal(context.Background(), "7", "run-1", "selection-1", "personal_daily", reportsource.Period{}, nil)
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
