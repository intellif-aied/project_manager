package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/model"
)

const (
	testRequirementID        = "11111111-1111-1111-1111-111111111111"
	testMissingRequirementID = "22222222-2222-2222-2222-222222222222"
	testTaskID               = "33333333-3333-3333-3333-333333333333"
)

func TestUpdateRequirementReturnsConflictForStaleBaseVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM requirements WHERE id = \$1\)`).
		WithArgs(testRequirementID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec(`UPDATE requirements SET`).
		WithArgs("B title", testRequirementID, int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT version FROM requirements WHERE id = \$1`).
		WithArgs(testRequirementID).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(int64(2)))

	h := NewRequirementHandler(db, nil)
	req := httptest.NewRequest(http.MethodPut, "/requirements/"+testRequirementID, bytes.NewBufferString(`{"title":"B title","base_version":1}`))
	req = requestWithUser(requestWithReportID(req, testRequirementID), &model.User{ID: "pm-1", Role: "pm"})
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["code"] != editConflictCode {
		t.Fatalf("code = %#v, want %s", payload["code"], editConflictCode)
	}
	if payload["current_version"] != float64(2) {
		t.Fatalf("current_version = %#v, want 2", payload["current_version"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUpdateTaskReturnsConflictForStaleBaseVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT id, requirement_id, assignee_id, creator_tl_id`).
		WithArgs(testTaskID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "requirement_id", "assignee_id", "creator_tl_id"}).
			AddRow(testTaskID, testRequirementID, nil, "tl-1"))
	mock.ExpectExec(`UPDATE tasks SET`).
		WithArgs("B task", testTaskID, int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT version FROM tasks WHERE id = \$1`).
		WithArgs(testTaskID).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(int64(2)))

	h := NewTaskHandler(db)
	req := httptest.NewRequest(http.MethodPut, "/tasks/"+testTaskID, bytes.NewBufferString(`{"title":"B task","base_version":1}`))
	req = requestWithUser(requestWithReportID(req, testTaskID), &model.User{ID: "pm-1", Role: "pm"})
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["code"] != editConflictCode {
		t.Fatalf("code = %#v, want %s", payload["code"], editConflictCode)
	}
	if payload["current_version"] != float64(2) {
		t.Fatalf("current_version = %#v, want 2", payload["current_version"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUpdateRequirementReturnsNotFoundWhenPermissionPrecheckMissesMissingRequirement(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM requirements WHERE id = \$1\)`).
		WithArgs(testMissingRequirementID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM requirements WHERE id = \$1\)`).
		WithArgs(testMissingRequirementID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	h := NewRequirementHandler(db, nil)
	req := httptest.NewRequest(http.MethodPut, "/requirements/"+testMissingRequirementID, bytes.NewBufferString(`{"title":"B title","base_version":1}`))
	req = requestWithUser(requestWithReportID(req, testMissingRequirementID), &model.User{ID: "pm-1", Role: "pm"})
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestDeleteRequirementReturnsNotFoundWhenPermissionPrecheckMissesMissingRequirement(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM requirements WHERE id = \$1\)`).
		WithArgs(testMissingRequirementID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM requirements WHERE id = \$1\)`).
		WithArgs(testMissingRequirementID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	h := NewRequirementHandler(db, nil)
	req := httptest.NewRequest(http.MethodDelete, "/requirements/"+testMissingRequirementID+"?base_version=1", nil)
	req = requestWithUser(requestWithReportID(req, testMissingRequirementID), &model.User{ID: "pm-1", Role: "pm"})
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestGetRequirementRejectsInvalidUUIDParam(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	h := NewRequirementHandler(db, nil)
	req := httptest.NewRequest(http.MethodGet, "/requirements/not-a-real-id", nil)
	req = requestWithUser(requestWithReportID(req, "not-a-real-id"), &model.User{ID: "pm-1", Role: "pm"})
	rec := httptest.NewRecorder()

	h.Get(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestGetTaskRejectsInvalidUUIDParam(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	h := NewTaskHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/tasks/not-a-real-id", nil)
	req = requestWithUser(requestWithReportID(req, "not-a-real-id"), &model.User{ID: "pm-1", Role: "pm"})
	rec := httptest.NewRecorder()

	h.Get(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
