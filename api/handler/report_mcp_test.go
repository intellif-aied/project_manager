package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/model"
)

func newReportMCPRequest(method string, body any) *http.Request {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/reports", bytes.NewReader(b))
	return req
}

func reportMCPBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code int `json:"code"`
			Data *struct {
				Code string `json:"code"`
			} `json:"data,omitempty"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v body=%s", err, rec.Body.String())
	}
	if resp.Error != nil {
		t.Fatalf("rpc error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v body=%s", err, rec.Body.String())
	}
	return result
}

func reportMCPTextPayload(t *testing.T, result map[string]any) map[string]any {
	t.Helper()
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("content missing: %#v", result["content"])
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] not object: %#v", content[0])
	}
	text, ok := first["text"].(string)
	if !ok {
		t.Fatalf("content[0].text not string: %#v", first["text"])
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal text payload: %v text=%s", err, text)
	}
	return payload
}

func reportMCPError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		Error *struct {
			Code int `json:"code"`
			Data *struct {
				Code string `json:"code"`
			} `json:"data,omitempty"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v body=%s", err, rec.Body.String())
	}
	if resp.Error == nil {
		t.Fatalf("expected rpc error, got nil. body=%s", rec.Body.String())
	}
	if resp.Error.Data == nil {
		t.Fatalf("expected structured error code, got nil. body=%s", rec.Body.String())
	}
	return resp.Error.Data.Code
}

func TestCopyReportRunMetadata(t *testing.T) {
	out := map[string]any{"report_type": reportTypePersonalDaily}
	copyReportRunMetadata(out, map[string]any{
		"trigger_source":       "scheduled",
		"scheduled_trigger_at": "2026-07-01T14:12:00Z",
		"schedule_id":          "schedule-1",
		"schedule_name":        "日报定时",
	})

	if out["trigger_source"] != "scheduled" {
		t.Fatalf("trigger_source = %#v", out["trigger_source"])
	}
	if out["scheduled_trigger_at"] != "2026-07-01T14:12:00Z" {
		t.Fatalf("scheduled_trigger_at = %#v", out["scheduled_trigger_at"])
	}
	if out["schedule_id"] != "schedule-1" {
		t.Fatalf("schedule_id = %#v", out["schedule_id"])
	}
	if out["schedule_name"] != "日报定时" {
		t.Fatalf("schedule_name = %#v", out["schedule_name"])
	}
}

func TestReportMCPToolsListReturns9AtomicTools(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := NewReportMCPHandler(db)
	req := newReportMCPRequest("tools/list", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	rec := httptest.NewRecorder()
	h.Serve(rec, req)

	result := reportMCPBody(t, rec)
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools not array: %#v", result["tools"])
	}
	if len(tools) != 9 {
		t.Fatalf("tools count = %d, want 9", len(tools))
	}
	expected := map[string]bool{
		"get_sessions":         true,
		"get_daily_reports":    true,
		"get_weekly_reports":   true,
		"get_tasks":            true,
		"get_requirements":     true,
		"get_existing_report":  true,
		"get_report_inventory": true,
		"write_report_result":  true,
		"write_report_failure": true,
	}
	for _, tool := range tools {
		m, ok := tool.(map[string]any)
		if !ok {
			t.Fatalf("tool not object: %#v", tool)
		}
		name, _ := m["name"].(string)
		if !expected[name] {
			t.Fatalf("unexpected tool %q in tools/list", name)
		}
	}
}

func TestReportMCPInitializeReturnsServerInfo(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()
	h := NewReportMCPHandler(db)
	req := newReportMCPRequest("initialize", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]any{"protocolVersion": "2024-11-05"},
	})
	rec := httptest.NewRecorder()
	h.Serve(rec, req)
	result := reportMCPBody(t, rec)
	info, _ := result["serverInfo"].(map[string]any)
	if info["name"] != "aida-report-mcp" {
		t.Fatalf("serverInfo.name = %#v", info["name"])
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Fatalf("protocolVersion = %#v", result["protocolVersion"])
	}
}

func TestReportMCPGetExistingReportRequiresAuth(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()
	h := NewReportMCPHandler(db)
	req := newReportMCPRequest("tools/call", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "get_existing_report",
			"arguments": map[string]any{"report_type": "personal_daily", "period": map[string]any{"date": "2026-06-29"}},
		},
	})
	rec := httptest.NewRecorder()
	h.Serve(rec, req)
	if code := reportMCPError(t, rec); code != "UNAUTHORIZED" {
		t.Fatalf("expected UNAUTHORIZED, got %s", code)
	}
}

func TestReportMCPGetExistingReportForbiddenTargetForEmployee(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()
	h := NewReportMCPHandler(db)
	req := newReportMCPRequest("tools/call", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "get_existing_report",
			"arguments": map[string]any{
				"report_type": "personal_daily",
				"period":      map[string]any{"date": "2026-06-29"},
				"target":      map[string]any{"type": "team", "team_id": "t-1"},
			},
		},
	})
	req = requestWithUser(req, &model.User{ID: "u-1", Role: "employee"})
	rec := httptest.NewRecorder()
	h.Serve(rec, req)
	if code := reportMCPError(t, rec); code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %s body=%s", code, rec.Body.String())
	}
}

func TestReportMCPWriteReportResultUnsupportedType(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()
	h := NewReportMCPHandler(db)
	req := newReportMCPRequest("tools/call", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "write_report_result",
			"arguments": map[string]any{
				"report_type": "invalid_type",
				"period":      map[string]any{"date": "2026-06-29"},
				"run_id":      "r-1",
				"content":     "x",
			},
		},
	})
	req = requestWithUser(req, &model.User{ID: "u-1", Role: "employee"})
	rec := httptest.NewRecorder()
	h.Serve(rec, req)
	if code := reportMCPError(t, rec); code != "REPORT_TYPE_NOT_SUPPORTED" {
		t.Fatalf("expected REPORT_TYPE_NOT_SUPPORTED, got %s body=%s", code, rec.Body.String())
	}
}

func TestReportMCPWriteReportFailureMissingRunID(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()
	h := NewReportMCPHandler(db)
	req := newReportMCPRequest("tools/call", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "write_report_failure",
			"arguments": map[string]any{
				"report_type":   "personal_daily",
				"period":        map[string]any{"date": "2026-06-29"},
				"error_message": "boom",
			},
		},
	})
	req = requestWithUser(req, &model.User{ID: "u-1", Role: "employee"})
	rec := httptest.NewRecorder()
	h.Serve(rec, req)
	if code := reportMCPError(t, rec); code != "INVALID_ARGUMENT" {
		t.Fatalf("expected INVALID_ARGUMENT, got %s body=%s", code, rec.Body.String())
	}
}

func TestReportMCPMethodsListNotExistent(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()
	h := NewReportMCPHandler(db)
	req := newReportMCPRequest("not_a_method", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "not_a_method",
	})
	rec := httptest.NewRecorder()
	h.Serve(rec, req)
	var resp struct {
		Error *struct {
			Code int `json:"code"`
			Data *struct {
				Code string `json:"code"`
			} `json:"data,omitempty"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("expected -32601 method not found, got body=%s", rec.Body.String())
	}
}

// Permission matrix tests (doc §3.5.2 / §3.3).
// resolveTarget is a pure function — the matrix is exhaustively testable without DB.

func strPtr(s string) *string { return &s }

func TestReportMCPScopeForbiddenForPM(t *testing.T) {
	// PM cannot expand read scope to team — only self is allowed.
	pm := &model.User{ID: "u-pm", Role: "pm"}
	_, err := resolveScope(nil, nil, pm, reportScope{Type: "team"})
	if err != errForbidden {
		t.Fatalf("PM scope=team: want FORBIDDEN, got %v", err)
	}
}

func TestReportMCPTargetMatrix(t *testing.T) {
	cases := []struct {
		name       string
		user       *model.User
		target     reportTarget
		reportType string
		write      bool
		wantErr    error
	}{
		// 1. employee 读别人 personal report → FORBIDDEN
		{
			name:       "employee read other personal_daily",
			user:       &model.User{ID: "u-emp", Role: "employee"},
			target:     reportTarget{Type: "user", UserID: "u-other"},
			reportType: "personal_daily",
			write:      false,
			wantErr:    errForbidden,
		},
		// 2. PM 读别人 session → FORBIDDEN (target.type=user narrowing)
		{
			name:    "pm read other user session via target=user",
			user:    &model.User{ID: "u-pm", Role: "pm"},
			target:  reportTarget{Type: "user", UserID: "u-other"},
			write:   false,
			wantErr: errForbidden,
		},
		// 3. TL 读小组成员 personal report → OK (defer membership check)
		{
			name:       "tl read team member personal_daily",
			user:       &model.User{ID: "u-tl", Role: "team_leader", TeamID: strPtr("t-1")},
			target:     reportTarget{Type: "user", UserID: "u-member"},
			reportType: "personal_daily",
			write:      false,
			wantErr:    nil,
		},
		// 4. TL 写小组成员 personal report → FORBIDDEN
		{
			name:       "tl write team member personal_daily",
			user:       &model.User{ID: "u-tl", Role: "team_leader", TeamID: strPtr("t-1")},
			target:     reportTarget{Type: "user", UserID: "u-member"},
			reportType: "personal_daily",
			write:      true,
			wantErr:    errForbidden,
		},
		// 5a. TL 写所属 team_daily (explicit team_id) → OK
		{
			name:       "tl write own team_daily explicit",
			user:       &model.User{ID: "u-tl", Role: "team_leader", TeamID: strPtr("t-1")},
			target:     reportTarget{Type: "team", TeamID: "t-1"},
			reportType: "team_daily",
			write:      true,
			wantErr:    nil,
		},
		// 5b. TL 写所属 team_daily (defaulted team_id) → OK
		{
			name:       "tl write own team_daily defaulted",
			user:       &model.User{ID: "u-tl", Role: "team_leader", TeamID: strPtr("t-1")},
			target:     reportTarget{Type: "team"},
			reportType: "team_daily",
			write:      true,
			wantErr:    nil,
		},
		// 5c. TL 写别组 team_daily → FORBIDDEN
		{
			name:       "tl write other team_daily",
			user:       &model.User{ID: "u-tl", Role: "team_leader", TeamID: strPtr("t-1")},
			target:     reportTarget{Type: "team", TeamID: "t-2"},
			reportType: "team_daily",
			write:      true,
			wantErr:    errForbidden,
		},
		// 6. Director 读部门员工 personal report → OK (defer membership check)
		{
			name:       "director read dept employee personal_daily",
			user:       &model.User{ID: "u-dir", Role: "director"},
			target:     reportTarget{Type: "user", UserID: "u-emp-dept"},
			reportType: "personal_daily",
			write:      false,
			wantErr:    nil,
		},
		// 7. Director 写部门员工 personal report → FORBIDDEN
		{
			name:       "director write dept employee personal_daily",
			user:       &model.User{ID: "u-dir", Role: "director"},
			target:     reportTarget{Type: "user", UserID: "u-emp-dept"},
			reportType: "personal_daily",
			write:      true,
			wantErr:    errForbidden,
		},
		// 8. Director 写 team_daily → FORBIDDEN
		{
			name:       "director write team_daily",
			user:       &model.User{ID: "u-dir", Role: "director"},
			target:     reportTarget{Type: "team", TeamID: "t-1"},
			reportType: "team_daily",
			write:      true,
			wantErr:    errForbidden,
		},
		// 9a. Director 写 department_daily (defaulted) → OK
		{
			name:       "director write own department_daily defaulted",
			user:       &model.User{ID: "u-dir", Role: "director"},
			target:     reportTarget{Type: "department"},
			reportType: "department_daily",
			write:      true,
			wantErr:    nil,
		},
		// 9b. Director 写别的 department_daily → FORBIDDEN
		{
			name:       "director write other department_daily",
			user:       &model.User{ID: "u-dir", Role: "director"},
			target:     reportTarget{Type: "department", DepartmentID: "u-other"},
			reportType: "department_daily",
			write:      true,
			wantErr:    errForbidden,
		},
		// 10. Admin global read/write
		{
			name:       "admin write other personal_daily",
			user:       &model.User{ID: "u-admin", Role: "admin"},
			target:     reportTarget{Type: "user", UserID: "u-anyone"},
			reportType: "personal_daily",
			write:      true,
			wantErr:    nil,
		},
		{
			name:       "admin write any team_daily",
			user:       &model.User{ID: "u-admin", Role: "admin"},
			target:     reportTarget{Type: "team", TeamID: "t-1"},
			reportType: "team_daily",
			write:      true,
			wantErr:    nil,
		},
		{
			name:       "admin write any department_daily",
			user:       &model.User{ID: "u-admin", Role: "admin"},
			target:     reportTarget{Type: "department", DepartmentID: "u-other"},
			reportType: "department_daily",
			write:      true,
			wantErr:    nil,
		},
		// target=self must not bypass the report_type write/read matrix.
		{
			name:       "employee write team_daily via self target",
			user:       &model.User{ID: "u-emp", Role: "employee", TeamID: strPtr("t-1")},
			target:     reportTarget{Type: "self"},
			reportType: "team_daily",
			write:      true,
			wantErr:    errForbidden,
		},
		{
			name:       "employee read team_daily via self target",
			user:       &model.User{ID: "u-emp", Role: "employee", TeamID: strPtr("t-1")},
			target:     reportTarget{Type: "self"},
			reportType: "team_daily",
			write:      false,
			wantErr:    errForbidden,
		},
		{
			name:       "tl write department_weekly via self target",
			user:       &model.User{ID: "u-tl", Role: "team_leader", TeamID: strPtr("t-1")},
			target:     reportTarget{Type: "self"},
			reportType: "department_weekly",
			write:      true,
			wantErr:    errForbidden,
		},
		{
			name:       "director write own department_weekly via self target",
			user:       &model.User{ID: "u-dir", Role: "director"},
			target:     reportTarget{Type: "self"},
			reportType: "department_weekly",
			write:      true,
			wantErr:    nil,
		},
		// Sanity: employee writing own personal_daily → OK
		{
			name:       "employee write own personal_daily",
			user:       &model.User{ID: "u-emp", Role: "employee"},
			target:     reportTarget{Type: "self"},
			reportType: "personal_daily",
			write:      true,
			wantErr:    nil,
		},
		// Sanity: employee reading own personal_daily → OK
		{
			name:       "employee read own personal_daily",
			user:       &model.User{ID: "u-emp", Role: "employee"},
			target:     reportTarget{Type: "self"},
			reportType: "personal_daily",
			write:      false,
			wantErr:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveTarget(tc.user, tc.target, tc.reportType, tc.write)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			if err != tc.wantErr {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestComputeDailyMissingNormalizesExistingReportDates(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT id::text, COALESCE").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "role", "team_id"}).
			AddRow("303", "user303", "pm", ""))

	missing := computeDailyMissing(context.Background(), db, "personal", []string{"303"}, "2026-07-01", "2026-07-03", []dailyReportItem{
		{Date: "2026-07-01T00:00:00Z", Owner: reportOwner{UserID: "303"}},
		{Date: "2026-07-02", Owner: reportOwner{UserID: "303"}},
	})

	if len(missing) != 1 {
		t.Fatalf("missing len = %d, want 1: %#v", len(missing), missing)
	}
	if got := missing[0]["date"]; got != "2026-07-03" {
		t.Fatalf("missing date = %#v, want 2026-07-03", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDailyInventoryExistingRequiresSavedReportContent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("(?s)FROM daily_reports.*status IS NOT NULL.*NULLIF\\(TRIM\\(COALESCE\\(content, ''\\)\\), ''\\) IS NOT NULL").
		WithArgs("2026-07-06", "2026-07-06", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "report_date", "generation_mode", "edited"}).
			AddRow("daily-1", "u-1", "2026-07-06", "managed_agent", false))

	got, err := loadDailyInventoryExisting(context.Background(), db, "personal", &resolvedScope{UserIDs: []string{"u-1", "u-2"}}, "2026-07-06", "2026-07-06")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("existing len = %d, want 1: %#v", len(got), got)
	}
	if got[0]["report_id"] != "daily-1" || got[0]["product_status"] != "ai_generated" {
		t.Fatalf("unexpected inventory row: %#v", got[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestComputeMissingNormalizesInventoryPeriodKeys(t *testing.T) {
	missing := computeMissing(
		[]map[string]any{
			{"owner_id": "u-1", "dates": []string{"2026-07-02", "2026-07-03"}},
		},
		[]map[string]any{
			{"owner_id": "u-1", "date": "2026-07-02T00:00:00Z", "report_id": "daily-1"},
		},
	)
	if len(missing) != 1 {
		t.Fatalf("missing len = %d, want 1: %#v", len(missing), missing)
	}
	if missing[0]["date"] != "2026-07-03" {
		t.Fatalf("missing date = %#v, want 2026-07-03", missing[0]["date"])
	}
}

func TestComputeMissingPreservesTeamMetadata(t *testing.T) {
	missing := computeMissing(
		[]map[string]any{
			{"owner_type": "team", "owner_id": "team-1", "team_name": "小组A", "dates": []string{"2026-07-02"}},
		},
		nil,
	)
	if len(missing) != 1 {
		t.Fatalf("missing len = %d, want 1: %#v", len(missing), missing)
	}
	if missing[0]["owner_type"] != "team" || missing[0]["team_name"] != "小组A" {
		t.Fatalf("team metadata was not preserved: %#v", missing[0])
	}
}

func TestLoadDailyInventoryExpectedIncludesScopedTeams(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT id::text, name FROM teams WHERE id = \\$1 ORDER BY name").
		WithArgs("team-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow("team-1", "小组A"))

	got, err := loadDailyInventoryExpected(context.Background(), db, "team", &resolvedScope{Type: "team", TeamID: "team-1"}, "2026-07-02", "2026-07-02")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected len = %d, want 1: %#v", len(got), got)
	}
	if got[0]["owner_type"] != "team" || got[0]["owner_id"] != "team-1" || got[0]["team_name"] != "小组A" {
		t.Fatalf("unexpected expected row: %#v", got[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportMCPGetSessionsReturnsScopeContextRoster(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := NewReportMCPHandler(db)

	mock.ExpectQuery("SELECT id::text FROM users WHERE team_id").
		WithArgs("team-a").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("305").AddRow("306").AddRow("307").AddRow("311"))

	now := time.Date(2026, 7, 6, 2, 0, 0, 0, time.UTC)
	sessionRows := sqlmock.NewRows([]string{
		"id", "user_id", "username", "role", "team_id", "team_name", "session_ref", "agent_type", "started_at", "ended_at",
		"activity_date", "activity_start_at", "activity_end_at", "activity_dates", "summary", "excerpt", "message_count", "source_event_count",
		"input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "slice_count", "source_has_raw_log",
		"token_slice_strategy", "summary_strategy", "is_estimated",
	}).
		AddRow("s-305", "305", "测试03", "team_leader", "team-a", "测试小组A", "prod-305", "codex", now, now.Add(10*time.Minute), "2026-07-06", now, now.Add(10*time.Minute), "{2026-07-06}", "日报验收", "", 6, 6, 100, 20, 0, 0, 120, 1, true, "actual", "summary", false).
		AddRow("s-306", "306", "测试04", "employee", "team-a", "测试小组A", "prod-306", "codex", now, now.Add(10*time.Minute), "2026-07-06", now, now.Add(10*time.Minute), "{2026-07-06}", "日报验收", "", 6, 6, 100, 20, 0, 0, 120, 1, true, "actual", "summary", false)
	mock.ExpectQuery("SELECT s.id::text, sas.user_id::text").
		WithArgs("2026-07-06", "2026-07-06", sqlmock.AnyArg(), sqlmock.AnyArg(), 100).
		WillReturnRows(sessionRows)

	mock.ExpectQuery("SELECT u.id::text,").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "role", "team_id", "team_name", "director_user_id", "director_name", "team_leader_id", "team_leader_name"}).
			AddRow("305", "测试03", "team_leader", "team-a", "测试小组A", "303", "测试01", "305", "测试03").
			AddRow("306", "测试04", "employee", "team-a", "测试小组A", "303", "测试01", "305", "测试03").
			AddRow("307", "测试05", "employee", "team-a", "测试小组A", "303", "测试01", "305", "测试03").
			AddRow("311", "测试09", "employee", "team-a", "测试小组A", "303", "测试01", "305", "测试03"))

	mock.ExpectQuery("SELECT id::text, COALESCE").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "role", "team_id"}).
			AddRow("305", "测试03", "team_leader", "team-a").
			AddRow("306", "测试04", "employee", "team-a").
			AddRow("307", "测试05", "employee", "team-a").
			AddRow("311", "测试09", "employee", "team-a"))

	req := newReportMCPRequest("tools/call", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "get_sessions",
			"arguments": map[string]any{
				"scope":           map[string]any{"type": "team"},
				"date_range":      map[string]any{"start": "2026-07-06", "end": "2026-07-06"},
				"include_summary": true,
			},
		},
	})
	req = requestWithUser(req, &model.User{ID: "305", Role: "team_leader", TeamID: strPtr("team-a")})
	rec := httptest.NewRecorder()
	h.Serve(rec, req)
	payload := reportMCPTextPayload(t, reportMCPBody(t, rec))
	scopeContext := payload["scope_context"].(map[string]any)
	if got := int(scopeContext["total_members"].(float64)); got != 4 {
		t.Fatalf("total_members=%d, want 4 payload=%#v", got, scopeContext)
	}
	if got := int(scopeContext["active_members"].(float64)); got != 2 {
		t.Fatalf("active_members=%d, want 2 payload=%#v", got, scopeContext)
	}
	members := scopeContext["members"].([]any)
	foundInactive := false
	foundLeaderRole := false
	for _, raw := range members {
		m := raw.(map[string]any)
		if m["user_id"] == "311" && m["active"] == false {
			foundInactive = true
		}
		if m["user_id"] == "305" && m["role_label"] == "小组组长" && m["is_team_leader"] == true {
			foundLeaderRole = true
		}
	}
	if !foundInactive {
		t.Fatalf("inactive member 311 missing: %#v", members)
	}
	if !foundLeaderRole {
		t.Fatalf("team leader semantic fields missing: %#v", members)
	}
	teams := scopeContext["teams"].([]any)
	if len(teams) != 1 {
		t.Fatalf("teams=%#v, want one team", teams)
	}
	team := teams[0].(map[string]any)
	if team["team_leader_name"] != "测试03" || team["department_director_name"] != "测试01" {
		t.Fatalf("team semantic owner fields = %#v", team)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportMCPInventoryTeamsRespectDepartmentScope(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := NewReportMCPHandler(db)

	mock.ExpectQuery("SELECT u.id::text").
		WithArgs("303").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("305").AddRow("306").AddRow("308"))
	mock.ExpectQuery("SELECT u.id::text,").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "role", "team_id", "team_name", "director_user_id", "director_name", "team_leader_id", "team_leader_name"}).
			AddRow("305", "测试03", "team_leader", "team-a", "测试小组A", "303", "测试01", "305", "测试03").
			AddRow("306", "测试04", "employee", "team-a", "测试小组A", "303", "测试01", "305", "测试03").
			AddRow("308", "测试06", "team_leader", "team-b", "测试小组B", "303", "测试01", "308", "测试06"))
	mock.ExpectQuery("SELECT id::text, team_id::text, week_start::text").
		WithArgs("2026-07-06", "2026-07-12", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "team_id", "week_start", "generation_mode", "edited"}).
			AddRow("r-a", "team-a", "2026-07-06", "managed_agent", false))
	mock.ExpectQuery("SELECT id::text, name FROM teams WHERE director_user_id").
		WithArgs("303").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow("team-a", "测试小组A").AddRow("team-b", "测试小组B"))

	req := newReportMCPRequest("tools/call", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "get_report_inventory",
			"arguments": map[string]any{
				"scope":        map[string]any{"type": "department"},
				"report_scope": "team",
				"report_kind":  "weekly",
				"week_range":   map[string]any{"week_start": "2026-07-06", "week_end": "2026-07-12"},
			},
		},
	})
	req = requestWithUser(req, &model.User{ID: "303", Role: "director"})
	rec := httptest.NewRecorder()
	h.Serve(rec, req)
	payload := reportMCPTextPayload(t, reportMCPBody(t, rec))
	inventory := payload["inventory"].(map[string]any)
	expected := inventory["expected"].([]any)
	missing := inventory["missing"].([]any)
	if len(expected) != 2 {
		t.Fatalf("expected teams=%d, want 2: %#v", len(expected), expected)
	}
	if len(missing) != 1 || missing[0].(map[string]any)["owner_id"] != "team-b" {
		t.Fatalf("missing=%#v, want only team-b", missing)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
