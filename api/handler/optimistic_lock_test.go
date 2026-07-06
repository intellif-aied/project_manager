package handler

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestDeleteTaskClearsSessionActivitySlicesBeforeDeletingTask(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT requirement_id, creator_tl_id, assignee_id, version FROM tasks WHERE id = \$1 FOR UPDATE`).
		WithArgs(testTaskID).
		WillReturnRows(sqlmock.NewRows([]string{"requirement_id", "creator_tl_id", "assignee_id", "version"}).
			AddRow(testRequirementID, "tl-1", nil, int64(9)))
	mock.ExpectQuery(`SELECT requirement_id::text, title, COALESCE\(acceptance_criteria, ARRAY\[\]::text\[\]\),`).
		WithArgs(testTaskID).
		WillReturnRows(sqlmock.NewRows([]string{"requirement_id", "title", "acceptance_criteria", "assignee_id", "status", "priority", "progress", "due_date"}).
			AddRow(testRequirementID, "task title", "{AC}", nil, "todo", "medium", 0, nil))
	mock.ExpectExec(`SELECT id FROM requirements WHERE id = \$1 FOR UPDATE`).
		WithArgs(testRequirementID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE sessions SET task_id = NULL WHERE task_id = \$1`).
		WithArgs(testTaskID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE token_usage SET task_id = NULL WHERE task_id = \$1`).
		WithArgs(testTaskID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE documents SET task_id = NULL WHERE task_id = \$1`).
		WithArgs(testTaskID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE session_activity_slices\s+SET task_id = NULL, requirement_id = COALESCE\(requirement_id, \$2::uuid\)\s+WHERE task_id = \$1`).
		WithArgs(testTaskID, testRequirementID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM user_follows WHERE target_type = 'task' AND target_id = \$1`).
		WithArgs(testTaskID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM work_item_relations`).
		WithArgs(testTaskID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM tasks WHERE id = \$1`).
		WithArgs(testTaskID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE requirements\s+SET progress = COALESCE`).
		WithArgs(testRequirementID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	h := NewTaskHandler(db)
	req := httptest.NewRequest(http.MethodDelete, "/tasks/"+testTaskID+"?base_version=9", nil)
	req = requestWithUser(requestWithReportID(req, testTaskID), &model.User{ID: "303", Role: "pm"})
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestListTaskEventsFallsBackToEventHistoryWhenTaskWasDeleted(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT id, requirement_id, assignee_id, creator_tl_id\s+FROM tasks\s+WHERE id = \$1`).
		WithArgs(testTaskID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT requirement_id::text\s+FROM work_item_events`).
		WithArgs(testTaskID).
		WillReturnRows(sqlmock.NewRows([]string{"requirement_id"}).AddRow(testRequirementID))
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM requirements WHERE id = \$1\)`).
		WithArgs(testRequirementID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM work_item_events WHERE task_id = \$1`).
		WithArgs(testTaskID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT id::text, target_type, target_id::text,`).
		WithArgs(testTaskID, 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "target_type", "target_id", "requirement_id", "task_id", "actor_id",
			"actor_name", "actor_role", "event_type", "event_title",
			"before_data", "after_data", "metadata", "created_at",
		}).AddRow(
			"44444444-4444-4444-4444-444444444444", "task", testTaskID,
			testRequirementID, testTaskID, "303", "测试01", "pm", "task_deleted", "删除了任务",
			[]byte(`{"title":"测试任务"}`), []byte(`{}`), []byte(`{}`), time.Now(),
		))

	h := NewTaskHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/tasks/"+testTaskID+"/events", nil)
	req = requestWithUser(requestWithReportID(req, testTaskID), &model.User{ID: "303", Role: "pm"})
	rec := httptest.NewRecorder()

	h.ListEvents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"event_title":"删除了任务：测试任务"`)) {
		t.Fatalf("expected deleted task title in response, body=%s", rec.Body.String())
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
