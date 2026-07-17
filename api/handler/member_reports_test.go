package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/model"
)

func TestResolveMemberScopeAllowsEmployeeTeam(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	teamID := "team-1"
	h := NewReportHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/reports/members/daily", nil)
	rec := httptest.NewRecorder()

	scope, ok := h.resolveMemberScope(rec, req, &model.User{
		ID:     "employee-1",
		Role:   "employee",
		TeamID: &teamID,
	})
	if !ok {
		t.Fatalf("employee team scope rejected: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if scope.teamID != teamID || scope.departmentID != "" {
		t.Fatalf("scope = %#v, want team %q", scope, teamID)
	}
}

func TestResolveMemberScopeRejectsEmployeeWithoutTeam(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	h := NewReportHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/reports/members/daily", nil)
	rec := httptest.NewRecorder()

	if _, ok := h.resolveMemberScope(rec, req, &model.User{ID: "employee-1", Role: "employee"}); ok {
		t.Fatal("employee without team unexpectedly received member report scope")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestEmployeeCanAccessOnlySameTeamMemberReports(t *testing.T) {
	for _, test := range []struct {
		name    string
		allowed bool
	}{
		{name: "same team", allowed: true},
		{name: "different team", allowed: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			teamID := "team-1"
			mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND team_id=$2)")).
				WithArgs("employee-2", teamID).
				WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(test.allowed))

			h := NewReportHandler(db)
			got := h.canAccessMemberUser(
				context.Background(),
				&model.User{ID: "employee-1", Role: "employee", TeamID: &teamID},
				"employee-2",
			)
			if got != test.allowed {
				t.Fatalf("access = %v, want %v", got, test.allowed)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEmployeeMemberReportListsUseOwnTeam(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		handler func(*ReportHandler, http.ResponseWriter, *http.Request)
		period  string
	}{
		{
			name:    "daily",
			url:     "/reports/members/daily?date=2026-07-17",
			handler: (*ReportHandler).ListMemberDailyReports,
			period:  "2026-07-17",
		},
		{
			name:    "weekly",
			url:     "/reports/members/weekly?week_start=2026-07-13",
			handler: (*ReportHandler).ListMemberWeeklyReports,
			period:  "2026-07-13",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			teamID := "team-1"
			mock.ExpectQuery(`(?s)SELECT u\.id::text.*WHERE u\.team_id::text = \$2 AND u\.local_enabled=true`).
				WithArgs(test.period, teamID).
				WillReturnRows(sqlmock.NewRows([]string{
					"user_id", "user_name", "role", "department_id", "department_name", "team_id", "team_name",
					"report_id", "status", "saved_at", "content_preview",
				}))

			h := NewReportHandler(db)
			req := httptest.NewRequest(http.MethodGet, test.url, nil)
			req = requestWithUser(req, &model.User{ID: "employee-1", Role: "employee", TeamID: &teamID})
			rec := httptest.NewRecorder()

			test.handler(h, rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGetMemberDailyReportRejectsEmployeeOutsideTeam(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	teamID := "team-1"
	mock.ExpectQuery(regexp.QuoteMeta("SELECT user_id::text FROM daily_reports WHERE id=$1")).
		WithArgs("report-1").
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow("employee-2"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND team_id=$2)")).
		WithArgs("employee-2", teamID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	h := NewReportHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/reports/members/daily/report-1", nil)
	req = requestWithUser(requestWithReportID(req, "report-1"), &model.User{
		ID:     "employee-1",
		Role:   "employee",
		TeamID: &teamID,
	})
	rec := httptest.NewRecorder()

	h.GetMemberDailyReport(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
