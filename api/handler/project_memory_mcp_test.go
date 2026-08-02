package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/internal/reportmemory"
	"github.com/aidashboard/api/model"
)

const testMemoryFingerprint = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestProjectMemoryMCPExposesOnlyDedicatedTools(t *testing.T) {
	tools := projectMemoryMCPTools()
	if len(tools) != 2 || tools[0]["name"] != toolGetProjectMemoryContext ||
		tools[1]["name"] != toolWriteProjectMemoryResult {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestProjectMemoryMCPReadsBoundJobContext(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	input := []byte(`{"schema_version":"project-memory-input/v1","current_themes":[{"theme_ref":"theme-001","title":"IF-Knowledge"}]}`)
	mock.ExpectQuery("SELECT input_json").
		WithArgs("305", "2026-08-01", testMemoryFingerprint).
		WillReturnRows(sqlmock.NewRows([]string{"input_json"}).AddRow(input))
	request := boundMemoryRequest(t)
	result, err := NewProjectMemoryMCPHandler(database).getContext(request, json.RawMessage(`{}`))
	if err != nil || result == nil {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProjectMemoryMCPValidatesAndWritesBoundProposal(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	input, err := json.Marshal(reportmemory.ConsolidationInput{
		SchemaVersion: "project-memory-input/v1",
		CurrentThemes: []reportmemory.InputTheme{{ThemeRef: "theme-001", Title: "IF-Knowledge"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT input_json, COALESCE(proposal_json, 'null'::jsonb)")).
		WithArgs("305", "2026-08-01", testMemoryFingerprint).
		WillReturnRows(sqlmock.NewRows([]string{"input_json", "proposal_json"}).AddRow(input, []byte("null")))
	mock.ExpectExec("UPDATE report_project_memory_jobs SET").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "305", "2026-08-01", testMemoryFingerprint).
		WillReturnResult(sqlmock.NewResult(0, 1))
	args := json.RawMessage(`{"proposal_json":{"schema_version":"project-memory-proposal/v1","decisions":[{"theme_ref":"theme-001","action":"create_new","canonical_name":"IF-Knowledge","confidence":0.9}]}}`)
	result, err := NewProjectMemoryMCPHandler(database).writeResult(boundMemoryRequest(t), args)
	if err != nil || result == nil {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProjectMemoryMCPRejectsMissingBoundJobIdentity(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/v1/mcp/project-memory", nil)
	request = request.WithContext(context.WithValue(request.Context(), userKey, &model.User{ID: "305"}))
	if _, _, _, err := boundProjectMemoryJob(request); err == nil {
		t.Fatal("expected missing project memory job identity")
	}
}

func boundMemoryRequest(t *testing.T) *http.Request {
	t.Helper()
	request := httptest.NewRequest("POST", "/api/v1/mcp/project-memory", nil)
	ctx := context.WithValue(request.Context(), userKey, &model.User{ID: "305"})
	ctx = context.WithValue(ctx, projectMemoryJobRefKey, "2026-08-01|"+testMemoryFingerprint)
	return request.WithContext(ctx)
}
