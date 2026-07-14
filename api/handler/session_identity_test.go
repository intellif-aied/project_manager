package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/model"
)

func TestLegacyBatchUploadUsesV2SessionIdentity(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	mock.ExpectQuery(`SELECT s.id, \(SELECT COUNT\(\*\) FROM session_sources`).
		WithArgs("shared-ref", "42", "codex").
		WillReturnRows(sqlmock.NewRows([]string{"id", "v2_source_count"}).AddRow("session-id", 0))
	mock.ExpectExec(`UPDATE sessions`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM session_activity_slices`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO session_activity_slices`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("metadata", `{
		"sessions": [{
			"session_ref": "shared-ref",
			"agent_type": "codex",
			"started_at": "2026-07-14T01:00:00Z",
			"ended_at": "2026-07-14T01:10:00Z",
			"model": "gpt-fixture"
		}]
	}`); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/batch", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request = requestWithUser(request, &model.User{ID: "42", Role: "employee"})
	recorder := httptest.NewRecorder()

	NewSessionHandler(database, nil, nil).BatchUpload(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyBatchUploadRejectsV2ManagedSession(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	mock.ExpectQuery(`SELECT s.id, \(SELECT COUNT\(\*\) FROM session_sources`).
		WithArgs("v2-session", "42", "codex").
		WillReturnRows(sqlmock.NewRows([]string{"id", "v2_source_count"}).AddRow("session-id", 1))

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("metadata", `{
		"sessions": [{
			"session_ref": "v2-session",
			"agent_type": "codex",
			"started_at": "2026-07-14T01:00:00Z"
		}]
	}`); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/batch", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request = requestWithUser(request, &model.User{ID: "42", Role: "employee"})
	recorder := httptest.NewRecorder()

	NewSessionHandler(database, nil, nil).BatchUpload(recorder, request)
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte("CLI_UPGRADE_REQUIRED")) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
