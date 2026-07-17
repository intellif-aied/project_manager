package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/internal/reportsource"
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
		if strings.HasPrefix(name, "get_") {
			description, _ := m["description"].(string)
			if name != toolGetReportInventory && !strings.Contains(description, "Asia/Shanghai") {
				t.Fatalf("%s does not describe the business timezone: %q", name, description)
			}
		}
	}
}

func TestNormalizeReportSourcePageCursorAcceptsResponseAlias(t *testing.T) {
	for _, test := range []struct {
		name       string
		pageCursor string
		nextCursor string
		want       string
	}{
		{name: "page cursor", pageCursor: "page-1", want: "page-1"},
		{name: "response alias", nextCursor: "page-2", want: "page-2"},
		{name: "matching aliases", pageCursor: "page-3", nextCursor: "page-3", want: "page-3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeReportSourcePageCursor(test.pageCursor, test.nextCursor)
			if err != nil || got != test.want {
				t.Fatalf("cursor=%q err=%v, want %q", got, err, test.want)
			}
		})
	}
	if _, err := normalizeReportSourcePageCursor("page-1", "page-2"); err == nil {
		t.Fatal("different page_cursor and next_cursor must fail")
	}
}

func TestReportMCPInventorySchemaUsesRangeForSelectedKind(t *testing.T) {
	tools := reportMCPTools()
	for _, tool := range tools {
		if tool["name"] != toolGetReportInventory {
			continue
		}
		description, _ := tool["description"].(string)
		requireContainsAll(t, description, "report_kind=daily", "date_range only", "report_kind=weekly", "week_range only")
		schema := tool["inputSchema"].(map[string]any)
		required := schema["required"].([]string)
		if containsString(required, "date_range") || containsString(required, "week_range") {
			t.Fatalf("range must be conditionally selected by report_kind, required=%#v", required)
		}
		return
	}
	t.Fatal("get_report_inventory schema missing")
}

func TestReportMCPSessionSchemaSeparatesSnapshotFromLegacySliceKeys(t *testing.T) {
	for _, tool := range reportMCPTools() {
		if tool["name"] != toolGetSessions {
			continue
		}
		schema := tool["inputSchema"].(map[string]any)
		properties := schema["properties"].(map[string]any)
		selection := properties["report_source_selection_id"].(map[string]any)
		sliceKeys := properties["selected_session_slice_keys"].(map[string]any)
		requireContainsAll(t, selection["description"].(string),
			"send the value only in this field",
			"it is not a session slice key",
		)
		requireContainsAll(t, sliceKeys["description"].(string),
			"Never send this field when report_source_selection_id is present",
			"never copy a report_source_selection_id UUID",
		)
		return
	}
	t.Fatal("get_sessions schema missing")
}

func TestReportMCPReportSchemasRequireContentDecision(t *testing.T) {
	wanted := map[string]string{
		toolGetDailyReports:  "date_range",
		toolGetWeeklyReports: "week_range",
	}
	for _, tool := range reportMCPTools() {
		name, _ := tool["name"].(string)
		rangeField, ok := wanted[name]
		if !ok {
			continue
		}
		description, _ := tool["description"].(string)
		requireContainsAll(t, description, "include_content=true", "omitted values default to true")
		schema := tool["inputSchema"].(map[string]any)
		required := schema["required"].([]string)
		for _, field := range []string{"scope", rangeField, "include_content"} {
			if !containsString(required, field) {
				t.Fatalf("%s required=%#v, missing %s", name, required, field)
			}
		}
		delete(wanted, name)
	}
	if len(wanted) != 0 {
		t.Fatalf("report tools missing from schema: %#v", wanted)
	}
}

func TestReportContentDefaultsToIncludedUnlessExplicitlyDisabled(t *testing.T) {
	if !reportContentIncluded(nil) {
		t.Fatal("omitted include_content must default to true")
	}
	trueValue := true
	if !reportContentIncluded(&trueValue) {
		t.Fatal("include_content=true must include content")
	}
	falseValue := false
	if reportContentIncluded(&falseValue) {
		t.Fatal("include_content=false must omit content")
	}
}

func TestReportMCPReadResultRedactsPersistenceIdentifiers(t *testing.T) {
	result := mcpModelTextResult(map[string]any{
		"id":       "report-uuid",
		"user_id":  "308",
		"user_ids": []string{"308", "309"},
		"username": "test06",
		"items": []map[string]any{{
			"session_id":  "session-uuid",
			"session_ref": "local-session-ref",
			"content":     "work summary",
		}},
	})
	serialized, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var wireResult map[string]any
	if err := json.Unmarshal(serialized, &wireResult); err != nil {
		t.Fatal(err)
	}
	payload := reportMCPTextPayload(t, wireResult)
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"report-uuid", "\"user_id\"", "\"user_ids\"", "session-uuid"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("read result leaked %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{"test06", "local-session-ref", "work summary"} {
		if !strings.Contains(text, required) {
			t.Fatalf("read result lost %q: %s", required, text)
		}
	}
	if !strings.Contains(text, `"timezone":"Asia/Shanghai"`) {
		t.Fatalf("read result is missing the authoritative timezone: %s", text)
	}
}

func TestResolveAttachedReportSourceContractUsesRunAsAuthority(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT input_ref_json").
		WithArgs("run-1", "308", reportAgentRunBusinessType).
		WillReturnRows(sqlmock.NewRows([]string{"input_ref_json"}).AddRow([]byte(`{
			"report_source_selection_id":"selection-1",
			"report_type":"personal_weekly",
			"period":{"week_start":"2026-07-13","week_end":"2026-07-19"}
		}`)))
	handler := NewReportMCPHandler(db)
	selectionID, reportType, period, found, err := handler.resolveAttachedReportSourceContract(
		context.Background(), "308", sessionsArgs{RunID: "run-1"},
	)
	if err != nil || !found || selectionID != "selection-1" || reportType != "personal_weekly" ||
		period.Start != "2026-07-13" || period.End != "2026-07-19" {
		t.Fatalf("selection=%q type=%q period=%+v found=%v err=%v", selectionID, reportType, period, found, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportSourcePageKeepsCursorBeforeLargeItems(t *testing.T) {
	cursor := "cursor-2"
	start := time.Date(2026, 7, 15, 15, 40, 0, 0, time.UTC)
	result := reportSourceContentPageResult(reportsource.ContentPage{
		ContentSnapshot: start, HasMore: true, NextCursor: &cursor,
		Items: []reportsource.ContentItem{{
			ActivityStartAt: start, ActivityEndAt: start.Add(31 * time.Minute),
			Summary: strings.Repeat("x", 4096),
			Events:  []reportsource.ContentEvent{{OccurredAt: start, EventType: "message"}},
		}},
	})
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatal(err)
	}
	content := wire["content"].([]any)[0].(map[string]any)["text"].(string)
	cursorIndex := strings.Index(content, `"next_cursor":"cursor-2"`)
	itemsIndex := strings.Index(content, `"items"`)
	if cursorIndex < 0 || itemsIndex < 0 || cursorIndex > itemsIndex {
		t.Fatalf("cursor must precede items: %s", content[:min(len(content), 300)])
	}
	if strings.Contains(content, "selection_id") {
		t.Fatalf("selection id must not be model-visible: %s", content[:min(len(content), 300)])
	}
	for _, required := range []string{
		`"timezone":"Asia/Shanghai"`,
		`"activity_start_at":"2026-07-15T23:40:00+08:00"`,
		`"activity_end_at":"2026-07-16T00:11:00+08:00"`,
		`"activity_start_date":"2026-07-15"`,
		`"activity_end_date":"2026-07-16"`,
		`"occurred_at":"2026-07-15T23:40:00+08:00"`,
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("localized source response missing %s: %s", required, content[:min(len(content), 1000)])
		}
	}
}

func TestReportSourceDigestReturnsFrozenPayloadWithoutReassembly(t *testing.T) {
	frozen := json.RawMessage(`{"source_mode":"explicit","content_mode":"digest_v1","coverage":{"complete":true},"has_more":false,"items":[]}`)
	result := reportSourceContentPageResult(reportsource.ContentPage{FrozenPayload: frozen})
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatal(err)
	}
	content := wire["content"].([]any)[0].(map[string]any)["text"].(string)
	if content != string(frozen) {
		t.Fatalf("frozen digest was reassembled: got=%s want=%s", content, frozen)
	}
	for _, forbidden := range []string{`"events"`, `"payload"`, `"returned_event_count"`} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("full-source field leaked into digest response: %s", content)
		}
	}
}

func TestReportSourceConsistencyRejectsFalseZeroCoverage(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)").
		WithArgs("2026-05-18", "312").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	issues, err := reportSourceConsistencyIssues(
		context.Background(), db, reportTypeDepartmentDaily, "2026-05-18", "", "",
		reportTarget{Type: "department", DepartmentID: "312"},
		"本部门当日无小组日报记录。小组日报：缺失 1 个小组。",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || !strings.Contains(issues[0], "实际包含 3 份小组日报") {
		t.Fatalf("issues=%#v", issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportSourceConsistencySkipsContentWithoutZeroClaim(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	issues, err := reportSourceConsistencyIssues(
		context.Background(), db, reportTypeDepartmentDaily, "2026-05-18", "", "",
		reportTarget{Type: "department", DepartmentID: "312"},
		"小组日报已提交 3 份，缺失 1 个小组。",
	)
	if err != nil || len(issues) != 0 {
		t.Fatalf("issues=%#v err=%v", issues, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportSourceConsistencyRejectsUnsupportedFullParticipation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)").
		WithArgs("2026-05-18", "312").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM teams").
		WithArgs("312").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))

	issues, err := reportSourceConsistencyIssues(
		context.Background(), db, reportTypeDepartmentDaily, "2026-05-18", "", "",
		reportTarget{Type: "department", DepartmentID: "312"},
		"小组全员参与生产发布回归测试。小组日报提交 3 份，缺失 1 个小组。",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || !strings.Contains(issues[0], "范围共有 4个小组") {
		t.Fatalf("issues=%#v", issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportSourceConsistencyAllowsPartialMissingWeeklyReports(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	issues, err := reportSourceConsistencyIssues(
		context.Background(), db, reportTypeTeamWeekly, "", "2026-05-18", "2026-05-24",
		reportTarget{Type: "team", TeamID: "team-1"},
		"个人周报已提交 2 份。缺失成员：测试08暂无个人周报记录，测试09暂无个人周报记录。",
	)
	if err != nil || len(issues) != 0 {
		t.Fatalf("issues=%#v err=%v", issues, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportSourceConsistencyRejectsWeeklyCoverageInDailyReport(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	issues, err := reportSourceConsistencyIssues(
		context.Background(), db, reportTypeDepartmentDaily, "2026-05-18", "", "",
		reportTarget{Type: "department", DepartmentID: "312"},
		"个人日报提交 3 人，缺失 1 人（测试09）。个人周报：缺失 4 人。",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || !strings.Contains(issues[0], "日报正文不应统计") {
		t.Fatalf("issues=%#v", issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
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

func TestReportContentValidationRejectsWrongWeekdayAndInternalIDs(t *testing.T) {
	tests := []struct {
		name       string
		reportType string
		content    string
		want       int
	}{
		{name: "valid weekday", content: "# 周报\n- 2026-06-08（周一）：完成回归\n- 部门共 4 位成员", want: 0},
		{name: "valid range with later weekday", content: "本周（2026-06-08 至 2026-06-14）有 1 天活动，周一（2026-06-08）完成回归", want: 0},
		{name: "wrong ISO weekday", content: "# 周报\n- 2026-06-08（周日）：完成回归", want: 1},
		{name: "wrong ISO weekday without brackets", content: "# 周报\n- 2026-06-08 周日：完成回归", want: 1},
		{name: "wrong partial weekday", content: "# 周报\n- 6 月 8 日（周日）：完成回归", want: 1},
		{name: "internal ID label", content: "# 日报\n- 用户ID：310", want: 1},
		{name: "director ID label", content: "# 部门日报\n- 总监ID：304", want: 1},
		{name: "raw ID field", content: "# 日报\n- user_id: 310", want: 1},
		{name: "UUID", content: "# 小组日报\n- 团队：b44b1277-db0f-4bd3-bc53-0a24160704c6", want: 1},
		{name: "engineering section", content: "# 日报\n### 验证状态\n- go test 通过", want: 1},
		{name: "empty followup section", content: "# 日报\n### 进行中与待跟进\n\n（无）", want: 1},
		{name: "inline empty followup section", content: "# 日报\n**进行中与待跟进**：无", want: 1},
		{name: "generation time is forbidden", content: "# 日报\n- 完成交付\n\n*报告生成时间：2026-07-16*", want: 1},
		{name: "nonempty followup section", content: "# 日报\n### 进行中与待跟进\n\n1. 复制按钮兼容性仍待修复", want: 0},
		{name: "file count", content: "# 日报\n- 今日共产生 190 项文件变更", want: 1},
		{name: "validation attempts", content: "# 日报\n- go test：40 次尝试后通过", want: 1},
		{name: "work item aggregate", content: "# 日报\n今日有 17 个有结果工作项", want: 1},
		{name: "internal asset label", content: "# 日报\n- Registry owner：100866", want: 1},
		{name: "skill version", content: "# 日报\n- 最新 Skill 1.0.12 已部署", want: 1},
		{name: "test account", content: "# 日报\n- 使用测试账号发起运行", want: 1},
		{name: "internal host", content: "# 日报\n- 14.157 API 已切换", want: 1},
		{name: "operational validation", content: "# 日报\n- 健康检查正常，E2E 已通过", want: 1},
		{name: "validation status", content: "# 日报\n- 真实数据链验证通过", want: 1},
		{name: "raw URL", content: "# 日报\n- 调研 https://github.com/rtk-ai/rtk", want: 1},
		{name: "technical file", content: "# 日报\n- 合并 `018_report_digest.sql` 并更新 `15点.md`", want: 1},
		{name: "technical path", content: "# 日报\n- 更新 /doc/v2/agent优化/第二阶段/ 方案", want: 1},
		{name: "relative technical path", content: "# 日报\n- 更新 doc/v2/agent优化/ 方案", want: 1},
		{name: "document count", content: "# 日报\n- 完成 10 份方案文档", want: 1},
		{name: "document lines", content: "# 日报\n- 方案共 2,411 行文档", want: 1},
		{name: "standalone full flow test", content: "# 日报\n- 完成真实全流程测试", want: 1},
		{name: "preflight is not outcome", content: "# 日报\n- 确认开发任务与其他开发无冲突，可以开始", want: 1},
		{name: "code commit is not outcome", content: "# 日报\n- 完成代码提交，修复日报时区", want: 1},
		{
			name:       "personal report may retain all material outcomes",
			reportType: reportTypePersonalDaily,
			content:    "# 日报\n1. 结果一\n2. 结果二\n3. 结果三\n4. 结果四\n5. 结果五\n6. 结果六\n7. 结果七",
			want:       0,
		},
		{
			name:       "organization report may list more owners",
			reportType: reportTypeTeamDaily,
			content:    "# 小组日报\n1. 成员一\n2. 成员二\n3. 成员三\n4. 成员四\n5. 成员五\n6. 成员六\n7. 成员七",
			want:       0,
		},
		{name: "conflicting asset versions", content: "# 日报\n- aida-report@1.0.6\n- aida-report@1.0.11", want: 1},
		{name: "conflicting digest versions", content: "# 日报\n- Digest v2.2\n- Session Digest v2.3", want: 1},
		{name: "single current asset version", content: "# 日报\n- 已发布 aida-report@1.0.11", want: 0},
		{name: "personal single asset version", reportType: reportTypePersonalDaily, content: "# 日报\n- 已发布 aida-report@1.0.11", want: 1},
		{name: "personal digest version", reportType: reportTypePersonalDaily, content: "# 日报\n- 完成 Session Digest v2.1 开发", want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := reportContentValidationIssues(
				tt.content,
				tt.reportType,
				"",
				"2026-06-08",
				"2026-06-14",
			)
			if len(issues) != tt.want {
				t.Fatalf("issues=%#v, want count=%d", issues, tt.want)
			}
		})
	}
}

func TestReportContentValidationRejectsConflictingDailyDateMetadata(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{name: "matching metadata", content: "报告日期：2026-07-16\n今日（2026-07-16）完成回归\n昨日（2026-07-15）完成开发", want: 0},
		{name: "wrong report date", content: "报告日期：2026-07-15", want: 1},
		{name: "wrong today", content: "今日（2026-07-15）完成回归", want: 1},
		{name: "wrong yesterday", content: "昨日（2026-07-14）完成开发", want: 1},
		{name: "historical task date is allowed", content: "修复 2026-07-01 创建的历史任务", want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			issues := reportContentValidationIssues(
				test.content,
				reportTypePersonalDaily,
				"2026-07-16",
				"",
				"",
			)
			if len(issues) != test.want {
				t.Fatalf("issues=%#v want=%d", issues, test.want)
			}
		})
	}
}

func TestReportPersonalSourceActivityIssuesRejectsFalseNoActivityClaim(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("selection-1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	issues, err := reportPersonalSourceActivityIssues(context.Background(), db,
		map[string]any{"report_source_selection_id": "selection-1"}, reportTypePersonalDaily,
		"# 个人日报\n今日无活动记录。")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || !strings.Contains(issues[0], "来源快照包含工作记录") {
		t.Fatalf("issues = %#v", issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportPersonalSourceActivityIssuesUsesDigestCoverage(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("session_slice_digest_revisions").
		WithArgs("selection-1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	issues, err := reportPersonalSourceActivityIssues(context.Background(), db,
		map[string]any{
			"report_source_selection_id": "selection-1",
			"report_source_read_mode":    "digest_v1",
		}, reportTypePersonalDaily, "# 个人日报\n今日无活动记录。")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues = %#v", issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportPersonalSourceActivityIssuesAllowsFactualContentWithoutQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	issues, err := reportPersonalSourceActivityIssues(context.Background(), db,
		map[string]any{"report_source_selection_id": "selection-1"}, reportTypePersonalDaily,
		"# 个人日报\n完成跨日来源设计与接口核对。")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("issues = %#v", issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportPersonalDigestOutcomeCoverageIssuesRejectsDroppedHighlights(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("COUNT\\(DISTINCT highlight").
		WithArgs("selection-1", "2026-07-16").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(18))

	issues, err := reportPersonalDigestOutcomeCoverageIssues(
		context.Background(), db,
		map[string]any{
			"report_source_selection_id": "selection-1",
			"report_source_read_mode":    "digest_v2",
		},
		reportTypePersonalDaily,
		"2026-07-16",
		"## 今日完成\n- 成果一\n- 成果二",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || !strings.Contains(issues[0], "18 个成果 highlight") {
		t.Fatalf("issues = %#v", issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportPersonalDigestOutcomeCoverageIssuesAcceptsOneItemPerHighlight(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("COUNT\\(DISTINCT highlight").
		WithArgs("selection-1", "2026-07-16").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	issues, err := reportPersonalDigestOutcomeCoverageIssues(
		context.Background(), db,
		map[string]any{
			"report_source_selection_id": "selection-1",
			"report_source_read_mode":    "digest_v2",
		},
		reportTypePersonalDaily,
		"2026-07-16",
		"## 今日完成\n1. 成果一\n2. 成果二\n3. 成果三",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("issues = %#v", issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportPersonalDigestOutcomeCoverageIssuesSkipsNonDigestV2(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	issues, err := reportPersonalDigestOutcomeCoverageIssues(
		context.Background(), db,
		map[string]any{
			"report_source_selection_id": "selection-1",
			"report_source_read_mode":    "digest_v1",
		},
		reportTypePersonalDaily,
		"2026-07-16",
		"## 今日完成\n- 成果一",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("issues = %#v", issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportPersonalDigestFollowupEvidenceIssuesRejectsUnfoundedFollowup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("selection-1", "2026-07-16").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	issues, err := reportPersonalDigestFollowupEvidenceIssues(
		context.Background(), db,
		map[string]any{
			"report_source_selection_id": "selection-1",
			"report_source_read_mode":    "digest_v2",
		},
		reportTypePersonalDaily,
		"2026-07-16",
		"## 今日完成\n- 成果\n\n## 进行中与待跟进\n- 待重启",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || !strings.Contains(issues[0], "status=partial") {
		t.Fatalf("issues = %#v", issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportPersonalDigestFollowupEvidenceIssuesAllowsConcreteFollowup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("selection-1", "2026-07-16").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	issues, err := reportPersonalDigestFollowupEvidenceIssues(
		context.Background(), db,
		map[string]any{
			"report_source_selection_id": "selection-1",
			"report_source_read_mode":    "digest_v2",
		},
		reportTypePersonalDaily,
		"2026-07-16",
		"## 今日完成\n- 成果\n\n## 进行中与待跟进\n- 阻塞事项",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("issues = %#v", issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportPersonalDigestFollowupEvidenceIssuesSkipsWithoutSection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	issues, err := reportPersonalDigestFollowupEvidenceIssues(
		context.Background(), db,
		map[string]any{
			"report_source_selection_id": "selection-1",
			"report_source_read_mode":    "digest_v2",
		},
		reportTypePersonalDaily,
		"2026-07-16",
		"## 今日完成\n- 成果",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("issues = %#v", issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportMCPWriteReportResultRejectsInvalidContentBeforeWrite(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := NewReportMCPHandler(db)
	now := time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT id::text, business_type").
		WithArgs("run-1", "310").
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_type", "agent_id", "model_id", "status", "input_ref_json", "output_ref_json", "created_at"}).
			AddRow("run-1", reportAgentRunBusinessType, "agent-1", "MiniMax-M2.5", "running", []byte(`{}`), []byte(`{}`), now))

	req := newReportMCPRequest("tools/call", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "write_report_result",
			"arguments": map[string]any{
				"report_type": "personal_weekly",
				"period":      map[string]any{"week_start": "2026-06-08", "week_end": "2026-06-14"},
				"target":      map[string]any{"type": "self"},
				"run_id":      "run-1",
				"content":     "# 周报\n6 月 8 日（周日）完成回归，用户ID：310",
			},
		},
	})
	req = requestWithUser(req, &model.User{ID: "310", Role: "employee"})
	rec := httptest.NewRecorder()
	h.Serve(rec, req)
	if code := reportMCPError(t, rec); code != "REPORT_CONTENT_INVALID" {
		t.Fatalf("expected REPORT_CONTENT_INVALID, got %s body=%s", code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
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

func TestUserIDsForDepartmentUsesDepartmentIDAsAuthority(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)JOIN departments d ON d\.id = COALESCE\(u\.department_id, t\.department_id\)\s+WHERE d\.id = \$1\s*$`).
		WithArgs("department-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("304").AddRow("305"))

	ids, err := userIDsForDepartment(context.Background(), db, "department-1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "304,305" {
		t.Fatalf("ids = %v", ids)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
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
			user:       &model.User{ID: "u-dir", Role: "director", DepartmentID: strPtr("d-1")},
			target:     reportTarget{Type: "user", UserID: "u-emp-dept"},
			reportType: "personal_daily",
			write:      false,
			wantErr:    nil,
		},
		// 7. Director 写部门员工 personal report → FORBIDDEN
		{
			name:       "director write dept employee personal_daily",
			user:       &model.User{ID: "u-dir", Role: "director", DepartmentID: strPtr("d-1")},
			target:     reportTarget{Type: "user", UserID: "u-emp-dept"},
			reportType: "personal_daily",
			write:      true,
			wantErr:    errForbidden,
		},
		// 8. Director 写 team_daily → FORBIDDEN
		{
			name:       "director write team_daily",
			user:       &model.User{ID: "u-dir", Role: "director", DepartmentID: strPtr("d-1")},
			target:     reportTarget{Type: "team", TeamID: "t-1"},
			reportType: "team_daily",
			write:      true,
			wantErr:    errForbidden,
		},
		// 9a. Director 写 department_daily (defaulted) → OK
		{
			name:       "director write own department_daily defaulted",
			user:       &model.User{ID: "u-dir", Role: "director", DepartmentID: strPtr("d-1")},
			target:     reportTarget{Type: "department"},
			reportType: "department_daily",
			write:      true,
			wantErr:    nil,
		},
		// 9b. Director 写别的 department_daily → FORBIDDEN
		{
			name:       "director write other department_daily",
			user:       &model.User{ID: "u-dir", Role: "director", DepartmentID: strPtr("d-1")},
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
			user:       &model.User{ID: "u-dir", Role: "director", DepartmentID: strPtr("d-1")},
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
		if m["username"] == "测试05" && m["active"] == false {
			foundInactive = true
		}
		if m["username"] == "测试03" && m["role_label"] == "小组组长" && m["is_team_leader"] == true {
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

func TestReportMCPGetSessionsSelectedSliceKeysOverrideDateRange(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := NewReportMCPHandler(db)

	now := time.Date(2026, 7, 1, 2, 0, 0, 0, time.UTC)
	sessionRows := sqlmock.NewRows([]string{
		"id", "user_id", "username", "role", "team_id", "team_name", "session_ref", "agent_type", "started_at", "ended_at",
		"activity_date", "activity_start_at", "activity_end_at", "activity_dates", "summary", "excerpt", "message_count", "source_event_count",
		"input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "slice_count", "source_has_raw_log",
		"token_slice_strategy", "summary_strategy", "is_estimated",
	}).
		AddRow("s-old", "305", "测试03", "employee", "team-a", "测试小组A", "prod-old", "codex", now, now.Add(10*time.Minute), "2026-07-01", now, now.Add(10*time.Minute), "{2026-07-01}", "用户显式选择的旧切片", "", 6, 6, 100, 20, 0, 0, 120, 1, true, "actual", "summary", false)
	mock.ExpectQuery("SELECT s.id::text, sas.user_id::text").
		WithArgs("2026-07-06", "2026-07-06", sqlmock.AnyArg(), sqlmock.AnyArg(), 100).
		WillReturnRows(sessionRows)
	mock.ExpectQuery("SELECT u.id::text,").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "role", "team_id", "team_name", "director_user_id", "director_name", "team_leader_id", "team_leader_name"}).
			AddRow("305", "测试03", "employee", "team-a", "测试小组A", "303", "测试01", "306", "测试04"))

	req := newReportMCPRequest("tools/call", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "get_sessions",
			"arguments": map[string]any{
				"scope":                       map[string]any{"type": "self"},
				"date_range":                  map[string]any{"start": "2026-07-06", "end": "2026-07-06"},
				"selected_session_slice_keys": []string{"s-old:2026-07-01"},
			},
		},
	})
	req = requestWithUser(req, &model.User{ID: "305", Role: "employee", TeamID: strPtr("team-a")})
	rec := httptest.NewRecorder()
	h.Serve(rec, req)

	payload := reportMCPTextPayload(t, reportMCPBody(t, rec))
	sessions := payload["sessions"].([]any)
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1 payload=%#v", len(sessions), payload)
	}
	got := sessions[0].(map[string]any)
	if got["slice_key"] != "s-old:2026-07-01" {
		t.Fatalf("slice_key = %#v, want explicit selected slice outside date_range", got["slice_key"])
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
		WithArgs("department-1").
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
	mock.ExpectQuery("SELECT id::text, name FROM teams WHERE department_id").
		WithArgs("department-1").
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
	req = requestWithUser(req, &model.User{ID: "303", Role: "director", DepartmentID: strPtr("department-1")})
	rec := httptest.NewRecorder()
	h.Serve(rec, req)
	payload := reportMCPTextPayload(t, reportMCPBody(t, rec))
	inventory := payload["inventory"].(map[string]any)
	expected := inventory["expected"].([]any)
	missing := inventory["missing"].([]any)
	if len(expected) != 2 {
		t.Fatalf("expected teams=%d, want 2: %#v", len(expected), expected)
	}
	if len(missing) != 1 || missing[0].(map[string]any)["team_name"] != "测试小组B" {
		t.Fatalf("missing=%#v, want only team-b", missing)
	}
	scopeContext := payload["scope_context"].(map[string]any)
	if scopeContext["department_name"] != "部门" {
		t.Fatalf("department display name = %#v, want 部门", scopeContext["department_name"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
