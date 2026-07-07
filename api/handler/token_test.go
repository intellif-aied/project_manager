package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/model"
)

func TestResolvePeriodAtUsesBusinessTimezone(t *testing.T) {
	now := time.Date(2026, 7, 5, 16, 30, 0, 0, time.UTC) // 2026-07-06 00:30 Asia/Shanghai.

	start, end, err := resolvePeriodAt("today", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if start != "2026-07-06" || end != "2026-07-06" {
		t.Fatalf("today = %s..%s, want 2026-07-06..2026-07-06", start, end)
	}

	start, end, err = resolvePeriodAt("week", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if start != "2026-07-06" || end != "2026-07-06" {
		t.Fatalf("week = %s..%s, want 2026-07-06..2026-07-06", start, end)
	}

	start, end, err = resolvePeriodAt("month", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if start != "2026-07-01" || end != "2026-07-06" {
		t.Fatalf("month = %s..%s, want 2026-07-01..2026-07-06", start, end)
	}
}

func TestListSessionTokensWithoutDateRangeDoesNotApplyDefaultDateFilter(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	startedAt := time.Date(2026, 6, 21, 14, 33, 2, 0, time.UTC)
	activityStart := time.Date(2026, 6, 21, 14, 33, 2, 0, time.UTC)
	activityEnd := time.Date(2026, 6, 22, 2, 40, 29, 0, time.UTC)

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM session_activity_slices sas WHERE sas\.user_id = \$1$`).
		WithArgs("u-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`FROM session_activity_slices sas\s+JOIN sessions s ON s\.id = sas\.session_id\s+LEFT JOIN users u ON u\.id = s\.user_id\s+WHERE sas\.user_id = \$1\s+ORDER BY`).
		WithArgs("u-1", 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "session_ref", "user_id", "user_name", "agent_type", "models", "summary",
			"started_at", "activity_date", "activity_start_at", "activity_end_at", "activity_dates",
			"slice_count", "source_has_raw_log", "is_estimated", "token_slice_strategy",
			"input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens",
		}).AddRow(
			"session-1", "local-1", "u-1", "Alice", "codex", "{MiniMax-M2.5}", "June work",
			startedAt, "2026-06-21", activityStart, activityEnd, "{2026-06-21}",
			1, true, false, "activity", int64(10), int64(20), int64(0), int64(0), int64(30),
		))

	h := NewTokenHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/tokens/sessions?scope=mine&page=1&page_size=20", nil)
	req = requestWithUser(req, &model.User{ID: "u-1", Role: "employee"})
	rec := httptest.NewRecorder()

	h.ListSessionTokens(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
