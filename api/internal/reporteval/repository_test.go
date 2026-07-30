package reporteval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRunArtifactRepositoryScopesRunToAuthenticatedUser(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository, err := NewRunArtifactRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	manifest := json.RawMessage(`{"pipeline_profile":"digest_context_brief_final"}`)
	var manifestValue any
	_ = json.Unmarshal(manifest, &manifestValue)
	hash, _ := CanonicalSHA256(manifestValue)
	mock.ExpectQuery("(?s)SELECT ar.status.*WHERE ar.id = \\$1 AND ar.user_id = \\$2").
		WithArgs("33333333-3333-4333-8333-333333333333", "305").
		WillReturnRows(sqlmock.NewRows([]string{
			"status", "failure_stage", "error_code", "created_at", "started_at", "finished_at",
			"source_identity_set_sha256", "manifest_json", "manifest_sha256", "digest", "context", "brief",
			"generated_content", "brief_invalid_attempts", "result_invalid_attempts",
		}).AddRow(
			"succeeded", nil, nil, now, now, now.Add(time.Second), strings.Repeat("a", 64), manifest, hash,
			[]byte(`{"work_units":[]}`), []byte(`{"schema_version":"report-context/v1"}`),
			[]byte(`{"schema_version":"report-brief/v1"}`), "1. 完成协议设计", 1, 2,
		))
	artifacts, err := repository.Load(context.Background(), "305", "33333333-3333-4333-8333-333333333333")
	if err != nil {
		t.Fatal(err)
	}
	if artifacts.RunID == "" || artifacts.GeneratedDraft == "" || artifacts.ResultInvalidAttempts != 2 {
		t.Fatalf("artifacts = %#v", artifacts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRunArtifactRepositoryDoesNotExposeOtherUsersRun(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository, _ := NewRunArtifactRepository(database)
	mock.ExpectQuery("(?s)SELECT ar.status.*WHERE ar.id = \\$1 AND ar.user_id = \\$2").
		WithArgs("33333333-3333-4333-8333-333333333333", "305").WillReturnError(sql.ErrNoRows)
	_, err = repository.Load(context.Background(), "305", "33333333-3333-4333-8333-333333333333")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}
