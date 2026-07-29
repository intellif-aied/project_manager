package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/model"
)

func TestTeamSyncPathCreateNormalizesAbsolutePath(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`)).
		WithArgs("team-sync-path:team-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("team-1", "42", "/workspace/alice", "").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("INSERT INTO team_sync_paths").
		WithArgs("team-1", "42", "/workspace/alice").
		WillReturnRows(sqlmock.NewRows([]string{"id", "normalized_path", "created_at", "updated_at"}).
			AddRow("19ab7264-10f5-4ba0-8e6d-ebfa77b86f21", "/workspace/alice", now, now))
	mock.ExpectCommit()

	handler := NewTeamSyncPathHandler(database)
	teamID := "team-1"
	request := requestWithUser(
		httptest.NewRequest(http.MethodPost, "/api/v1/me/team-sync-paths", bytes.NewBufferString(`{"path":"/workspace/alice/../alice/"}`)),
		&model.User{ID: "42", TeamID: &teamID},
	)
	recorder := httptest.NewRecorder()
	handler.Create(recorder, request)
	if recorder.Code != http.StatusCreated || !bytes.Contains(recorder.Body.Bytes(), []byte(`"normalized_path":"/workspace/alice"`)) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTeamSyncPathRejectsRootAndMissingTeam(t *testing.T) {
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	handler := NewTeamSyncPathHandler(database)
	teamID := "team-1"

	rootRequest := requestWithUser(
		httptest.NewRequest(http.MethodPost, "/api/v1/me/team-sync-paths", bytes.NewBufferString(`{"path":"/"}`)),
		&model.User{ID: "42", TeamID: &teamID},
	)
	rootRecorder := httptest.NewRecorder()
	handler.Create(rootRecorder, rootRequest)
	if rootRecorder.Code != http.StatusBadRequest || !bytes.Contains(rootRecorder.Body.Bytes(), []byte("INVALID_TEAM_DIRECTORY")) {
		t.Fatalf("root status=%d body=%s", rootRecorder.Code, rootRecorder.Body.String())
	}

	noTeamRequest := requestWithUser(
		httptest.NewRequest(http.MethodGet, "/api/v1/me/team-sync-paths", http.NoBody),
		&model.User{ID: "42"},
	)
	noTeamRecorder := httptest.NewRecorder()
	handler.List(noTeamRecorder, noTeamRequest)
	if noTeamRecorder.Code != http.StatusConflict || !bytes.Contains(noTeamRecorder.Body.Bytes(), []byte("TEAM_REQUIRED")) {
		t.Fatalf("no-team status=%d body=%s", noTeamRecorder.Code, noTeamRecorder.Body.String())
	}
}
