package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/model"
	"github.com/go-chi/chi/v5"
)

func TestDeleteOwnedReports(t *testing.T) {
	tests := []struct {
		name       string
		table      string
		user       *model.User
		deleteFunc func(*ReportHandler, http.ResponseWriter, *http.Request)
		ownerField string
		ownerID    string
	}{
		{"personal daily", "daily_reports", &model.User{ID: "user-1", Role: "employee"}, (*ReportHandler).DeletePersonalDailyReport, "user_id", "user-1"},
		{"personal weekly", "personal_weekly_reports", &model.User{ID: "user-1", Role: "employee"}, (*ReportHandler).DeletePersonalWeeklyReport, "user_id", "user-1"},
		{"team daily", "team_reports", &model.User{ID: "leader-1", Role: "team_leader"}, (*ReportHandler).DeleteTeamDailyReport, "leader_id", "leader-1"},
		{"team weekly", "team_weekly_reports", &model.User{ID: "leader-1", Role: "team_leader"}, (*ReportHandler).DeleteTeamWeeklyReport, "leader_id", "leader-1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			query := "DELETE FROM " + test.table + " WHERE id = $1 AND " + test.ownerField + " = $2"
			if test.table == "daily_reports" {
				mock.ExpectBegin()
				mock.ExpectQuery("SELECT report_date::text, managed_agent_run_id::text").
					WithArgs(testRequirementID, test.ownerID).
					WillReturnRows(sqlmock.NewRows([]string{"report_date", "managed_agent_run_id"}).AddRow("2026-07-23", nil))
				mock.ExpectExec("INSERT INTO report_user_outcome_events").
					WithArgs(testRequirementID, test.ownerID, "2026-07-23", nil, "deleted", nil, nil).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(regexp.QuoteMeta(query)).
					WithArgs(testRequirementID, test.ownerID).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			} else {
				mock.ExpectExec(regexp.QuoteMeta(query)).
					WithArgs(testRequirementID, test.ownerID).
					WillReturnResult(sqlmock.NewResult(0, 1))
			}

			h := NewReportHandler(db)
			req := httptest.NewRequest(http.MethodDelete, "/reports/"+testRequirementID, nil)
			req = requestWithUser(requestWithReportID(req, testRequirementID), test.user)
			rec := httptest.NewRecorder()

			test.deleteFunc(h, rec, req)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeleteDepartmentReportUsesDirectorDepartment(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id::text FROM departments WHERE director_user_id = $1")).
		WithArgs("director-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("department-1"))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM department_reports WHERE id = $1 AND department_id = $2")).
		WithArgs(testRequirementID, "department-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	h := NewReportHandler(db)
	req := httptest.NewRequest(http.MethodDelete, "/reports/department/"+testRequirementID, nil)
	req = requestWithUser(requestWithReportID(req, testRequirementID), &model.User{ID: "director-1", Role: "director"})
	rec := httptest.NewRecorder()
	h.DeleteDepartmentDailyReport(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func requestWithUser(req *http.Request, user *model.User) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), userKey, user))
}

func requestWithReportID(req *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestResolveReportDepartmentIDUsesExplicitDepartmentForAdmin(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT id::text FROM departments WHERE id::text = \$1`).
		WithArgs("department-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("department-1"))

	h := NewReportHandler(db)
	departmentID, err := h.resolveReportDepartmentID(
		&model.User{ID: "admin-1", Role: "admin"},
		"department-1",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if departmentID != "department-1" {
		t.Fatalf("department id = %q", departmentID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestGetDepartmentReportSourcesFiltersSelectedDepartment(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT id::text FROM departments WHERE id::text = \$1`).
		WithArgs("department-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("department-1"))
	mock.ExpectQuery(`(?s)SELECT t\.id::text.*WHERE t\.department_id = \$2.*ORDER BY t\.name`).
		WithArgs("2026-07-14", "department-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"team_id", "team_name", "leader_id", "leader_name", "report_id", "content", "submitted_at", "has_report",
		}).AddRow("team-1", "平台组", "leader-1", "组长", "report-1", "日报正文", time.Now(), true))

	h := NewReportHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/reports/department/sources?date=2026-07-14&department_id=department-1", nil)
	req = requestWithUser(req, &model.User{ID: "admin-1", Role: "admin"})
	rec := httptest.NewRecorder()

	h.GetDepartmentReportSources(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.DepartmentReportSources
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.TotalTeamCount != 1 || got.SubmittedTeamCount != 1 || len(got.SubmittedTeamReports) != 1 {
		t.Fatalf("unexpected department sources: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUpdateReportPersistsSessionIDsOnSave(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 6, 24, 19, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT COUNT").
		WithArgs("user-1", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE daily_reports SET").
		WithArgs("最终日报", sqlmock.AnyArg(), "report-1", "user-1").
		WillReturnRows(sqlmock.NewRows([]string{"report_date", "content", "managed_agent_run_id"}).
			AddRow("2026-06-24", "最终日报", nil))
	mock.ExpectExec("INSERT INTO report_user_outcome_events").
		WithArgs("report-1", "user-1", "2026-06-24", nil, "saved", "最终日报", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT dr.id").
		WithArgs("report-1").
		WillReturnRows(sqlmock.NewRows(dailyReportGetColumns()).
			AddRow("report-1", "user-1", "张三", nil, "2026-06-24", "最终日报", "", true, nil, "{session-1,session-2}", "saved", nil, now, nil, nil, "default", nil, nil, nil, nil, nil, now, now))

	h := NewReportHandler(db)
	req := httptest.NewRequest(http.MethodPut, "/reports/report-1", bytes.NewBufferString(`{"content":"最终日报","session_ids":["session-1","session-2"]}`))
	req = requestWithUser(requestWithReportID(req, "report-1"), &model.User{ID: "user-1", Name: "张三", Role: "employee"})
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestSubmitReportSavesAndPublishesSubmittedContent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 6, 24, 19, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT COUNT").
		WithArgs("user-1", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE daily_reports SET").
		WithArgs("发送版本", sqlmock.AnyArg(), "team_leader", "report-1", "user-1").
		WillReturnRows(sqlmock.NewRows([]string{"report_date", "content", "managed_agent_run_id"}).
			AddRow("2026-06-24", "发送版本", nil))
	mock.ExpectExec("INSERT INTO report_user_outcome_events").
		WithArgs("report-1", "user-1", "2026-06-24", nil, "submitted", "发送版本", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT dr.id").
		WithArgs("report-1").
		WillReturnRows(sqlmock.NewRows(dailyReportGetColumns()).
			AddRow("report-1", "user-1", "张三", nil, "2026-06-24", "发送版本", "", true, nil, "{session-1,session-2}", "submitted", "发送版本", now, now, "team_leader", "default", nil, nil, nil, nil, nil, now, now))

	h := NewReportHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/reports/report-1/submit", bytes.NewBufferString(`{"content":"发送版本","session_ids":["session-1","session-2"]}`))
	req = requestWithUser(requestWithReportID(req, "report-1"), &model.User{ID: "user-1", Name: "张三", Role: "employee"})
	rec := httptest.NewRecorder()

	h.SubmitReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestGetTeamReportSourcesUsesSavedReportsAsSources(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	teamID := "team-1"
	savedAt := time.Date(2026, 7, 6, 18, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT name FROM teams").
		WithArgs(teamID).
		WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("AI Coding平台开发组"))
	mock.ExpectQuery("(?s)SELECT u\\.id, COALESCE.*dr\\.status IS NOT NULL.*NULLIF\\(TRIM.*u\\.app_role IN \\('team_leader', 'employee'\\)").
		WithArgs("2026-07-06", teamID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "report_id", "content", "submitted_at", "has_report"}).
			AddRow("tl-1", "组长", "daily-tl", "组长保存的日报", savedAt, true).
			AddRow("emp-1", "成员", "daily-emp", "成员保存的日报", savedAt.Add(5*time.Minute), true).
			AddRow("emp-2", "未保存成员", nil, nil, nil, false))

	h := NewReportHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/reports/team/sources?date=2026-07-06", nil)
	req = requestWithUser(req, &model.User{ID: "tl-1", Role: "team_leader", TeamID: &teamID})
	rec := httptest.NewRecorder()

	h.GetTeamReportSources(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.TeamReportSources
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.TotalMemberCount != 3 || got.SubmittedCount != 2 || got.MissingCount != 1 {
		t.Fatalf("unexpected counts: total=%d submitted=%d missing=%d body=%s", got.TotalMemberCount, got.SubmittedCount, got.MissingCount, rec.Body.String())
	}
	if len(got.SubmittedReports) != 2 || got.SubmittedReports[0].UserID != "tl-1" || got.SubmittedReports[1].UserID != "emp-1" {
		t.Fatalf("saved reports not returned as sources: %#v", got.SubmittedReports)
	}
	if got.SubmittedReports[0].Content != "组长保存的日报" || got.SubmittedReports[1].Content != "成员保存的日报" {
		t.Fatalf("unexpected source content: %#v", got.SubmittedReports)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestListTeamMemberReportsAllowsManagedDirectorTeam(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("team-1", "director-1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("(?s)SELECT u\\.id, COALESCE.*u\\.team_id = \\$2").
		WithArgs("2026-07-14", "team-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "report_id", "content", "submitted_at", "has_report"}).
			AddRow("emp-1", "成员", "report-1", "日报正文", time.Now(), true))

	h := NewReportHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/reports/team/members?date=2026-07-14&team_id=team-1", nil)
	req = requestWithUser(req, &model.User{ID: "director-1", Role: "director"})
	rec := httptest.NewRecorder()

	h.ListTeamMemberReports(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestListTeamMemberReportsRejectsUnmanagedDirectorTeam(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("team-2", "director-1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	h := NewReportHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/reports/team/members?date=2026-07-14&team_id=team-2", nil)
	req = requestWithUser(req, &model.User{ID: "director-1", Role: "director"})
	rec := httptest.NewRecorder()

	h.ListTeamMemberReports(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestGetReportRejectsDirectorOutsideManagedTeam(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT dr.id").
		WithArgs("report-1").
		WillReturnRows(sqlmock.NewRows(dailyReportGetColumns()).
			AddRow("report-1", "emp-1", "成员", "team-2", "2026-07-14", "日报正文", "", false, nil, "{}", "saved", nil, now, nil, nil, "default", nil, nil, nil, nil, nil, now, now))
	mock.ExpectQuery("(?s)SELECT EXISTS.*departments").
		WithArgs("emp-1", "director-1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	h := NewReportHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/reports/report-1", nil)
	req = requestWithUser(requestWithReportID(req, "report-1"), &model.User{ID: "director-1", Role: "director"})
	rec := httptest.NewRecorder()

	h.Get(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func draftSessionColumns() []string {
	return []string{
		"id", "session_ref", "agent_type", "started_at", "ended_at", "duration_secs",
		"model", "summary", "tool_calls_json",
		"task_id", "task_title", "requirement_id", "requirement_title",
		"input_tokens", "output_tokens", "total_tokens",
	}
}

func dailyReportGetColumns() []string {
	return []string{
		"id", "user_id", "name", "team_id", "report_date", "content", "next_day_plan", "edited",
		"feishu_doc_url", "session_ids", "status", "submitted_content", "saved_at", "submitted_at", "submitted_to",
		"generation_mode", "managed_agent_run_id", "agent_id", "agent_version_id", "model_id", "finished_at",
		"created_at", "updated_at",
	}
}

func dailyReportByUserDateColumns() []string {
	return []string{
		"id", "user_id", "name", "report_date", "content", "next_day_plan", "edited",
		"feishu_doc_url", "session_ids", "status", "submitted_content", "saved_at", "submitted_at", "submitted_to",
		"created_at", "updated_at",
	}
}
