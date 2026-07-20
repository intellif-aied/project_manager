package handler

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/internal/reportcontext"
	"github.com/aidashboard/api/internal/reportsource"
	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/service"
	"github.com/go-chi/chi/v5"
)

type reportContextBuilderStub struct{}

func (reportContextBuilderStub) Build(context.Context, reportcontext.BuildRequest) (reportcontext.StoredContext, error) {
	return reportcontext.StoredContext{Payload: []byte(`{"schema_version":"report-context/v1"}`), Hash: strings.Repeat("a", 64), Bytes: 38}, nil
}

type jsonStringArg struct {
	require []string
	forbid  []string
}

func (m jsonStringArg) Match(v driver.Value) bool {
	var raw string
	switch value := v.(type) {
	case []byte:
		raw = string(value)
	case string:
		raw = value
	default:
		return false
	}
	for _, item := range m.require {
		if !strings.Contains(raw, item) {
			return false
		}
	}
	for _, item := range m.forbid {
		if strings.Contains(raw, item) {
			return false
		}
	}
	return true
}

func requestWithURLParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func requestWithURLParams(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for key, value := range params {
		rctx.URLParams.Add(key, value)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func testManagedAgentDefaults() ManagedAgentDefaults {
	return ManagedAgentDefaults{
		Engine:            "claude-code",
		ModelID:           "MiniMax-M2.5",
		ReportSkillOwner:  "100866",
		ReportMCPSlug:     "aida-report-mcp",
		ReportMCPVersion:  "report-v1",
		AIDAPublicBaseURL: "https://aida.example.com",
	}
}

func requireContainsAll(t *testing.T, value string, expected ...string) {
	t.Helper()
	for _, item := range expected {
		if !strings.Contains(value, item) {
			t.Fatalf("expected %q to contain %q", value, item)
		}
	}
}

func TestReportContextBuildRequestMapsAllReportTargets(t *testing.T) {
	tests := []struct {
		reportType string
		target     reportTarget
		wantID     string
	}{
		{reportTypePersonalDaily, reportTarget{Type: "self", UserID: "u1"}, "u1"},
		{reportTypePersonalWeekly, reportTarget{Type: "user", UserID: "u1"}, "u1"},
		{reportTypeTeamDaily, reportTarget{Type: "team", TeamID: "t1"}, "t1"},
		{reportTypeTeamWeekly, reportTarget{Type: "team", TeamID: "t1"}, "t1"},
		{reportTypeDepartmentDaily, reportTarget{Type: "department", DepartmentID: "d1"}, "d1"},
		{reportTypeDepartmentWeekly, reportTarget{Type: "department", DepartmentID: "d1"}, "d1"},
	}
	for _, test := range tests {
		t.Run(test.reportType, func(t *testing.T) {
			period := reportsource.Period{Start: "2026-07-13", End: "2026-07-19"}
			request := reportContextBuildRequest("runner", "run-1", test.reportType, period, "Asia/Shanghai", "manual", "model-1", test.target, "selection-1")
			gotID := request.Target.UserID + request.Target.TeamID + request.Target.DepartmentID
			if request.ReportType != test.reportType || gotID != test.wantID || request.Timezone != "Asia/Shanghai" || request.TriggerSource != "manual" {
				t.Fatalf("unexpected request: %+v", request)
			}
		})
	}
}

func TestBuildReportRunMessageIncludesSystemParams(t *testing.T) {
	message := buildReportRunMessage(map[string]string{
		"report_type":                "personal_daily",
		"period_json":                `{"date":"2026-07-01"}`,
		"calendar_context_json":      `{"type":"daily","day":{"date":"2026-07-01","weekday":"周三"}}`,
		"target_json":                `{"type":"self","user_id":"305"}`,
		"report_source_selection_id": "selection-report-source",
		"run_id":                     "run-report",
		"mcp_url":                    "https://aida.example.com/api/v1/mcp/reports",
	}, "请重点关注风险", reportMCPCredentialSlot)

	requireContainsAll(t, message,
		"/aida-report",
		"必须实际调用 Skill 工具",
		"文本本身不代表 Skill 已加载",
		"加载完成前不得调用报告 MCP",
		"report_type=personal_daily",
		`period={"date":"2026-07-01"}`,
		`calendar_context={"type":"daily","day":{"date":"2026-07-01","weekday":"周三"}}`,
		`target={"type":"self","user_id":"305"}`,
		"run_id=run-report",
		reportMCPCredentialSlot,
		"请重点关注风险",
		"report_source_selection_id=selection-report-source",
		"只传 run_id 调用一次 get_report_context",
		"不得再调用其他读取工具重新扫描 Session、任务或需求",
		"报告内容与格式遵循当前绑定 Skill",
		"最终动作：必须调用 write_report_result 回写",
		"最终动作：必须调用 write_report_result 回写",
	)
	if !strings.HasPrefix(message, "/aida-report\n") {
		t.Fatalf("run message must invoke the bound report skill first: %q", message)
	}
	if strings.Contains(message, "mcp_url=") {
		t.Fatalf("message should not expose mcp_url: %q", message)
	}
	if strings.Contains(message, "调用 get_sessions") {
		t.Fatalf("managed personal Context V1 message must not route back to get_sessions: %q", message)
	}
	for _, forbidden := range []string{"PRD/ADR", "TDD", "Top-K", "REPORT_CONTENT_INVALID", "验证记录", "文件变更"} {
		if strings.Contains(message, forbidden) {
			t.Fatalf("run protocol must not constrain report prose with %q: %q", forbidden, message)
		}
	}
}

func TestBuildReportRunMessagePinsDepartmentScope(t *testing.T) {
	message := buildReportRunMessage(map[string]string{
		"report_type":           "department_weekly",
		"period_json":           `{"week_start":"2026-07-13","week_end":"2026-07-19"}`,
		"calendar_context_json": `{}`,
		"target_json":           `{"type":"department"}`,
		"run_id":                "run-department-weekly",
	}, "", reportMCPCredentialSlot)

	requireContainsAll(t, message,
		"scope.type=department",
		"report_scope=team",
		"禁止改用 self 或 all",
	)
}

func TestValidateReportSourceRunInputRejectsOrganizationSourceParameters(t *testing.T) {
	if got := validateReportSourceRunInput(false, "", []string{"session:2026-07-14"}); got == nil || got.Code != "INVALID_REPORT_SOURCE" {
		t.Fatalf("organization legacy source validation=%+v", got)
	}
	if got := validateReportSourceRunInput(false, "selection-id", nil); got == nil || got.Code != "INVALID_REPORT_SOURCE" {
		t.Fatalf("organization selection validation=%+v", got)
	}
	if got := validateReportSourceRunInput(true, "selection-id", []string{"session:2026-07-14"}); got == nil || got.Code != "AMBIGUOUS_REPORT_SOURCE" {
		t.Fatalf("ambiguous personal source validation=%+v", got)
	}
	if got := validateReportSourceRunInput(true, "selection-id", nil); got != nil {
		t.Fatalf("valid personal selection rejected=%+v", got)
	}
}

func TestReportCalendarContextJSONUsesDeterministicWeekdays(t *testing.T) {
	daily := reportCalendarContextJSON(reportTypePersonalDaily, "2026-06-08", "", "")
	requireContainsAll(t, daily,
		`"timezone":"Asia/Shanghai"`,
		`"report_date":"2026-06-08"`,
		`"today_means":"period.date"`,
		`"date":"2026-06-08"`,
		`"weekday":"周一"`,
	)

	weekly := reportCalendarContextJSON(reportTypePersonalWeekly, "", "2026-06-08", "2026-06-14")
	requireContainsAll(t, weekly,
		`"timezone":"Asia/Shanghai"`,
		`"week_start":"2026-06-08"`,
		`"week_end":"2026-06-14"`,
		`"date":"2026-06-08"`,
		`"weekday":"周一"`,
		`"date":"2026-06-14"`,
		`"weekday":"周日"`,
	)
}

func TestDefaultReportAgentInstructionsContainProtocolOnly(t *testing.T) {
	instructions := defaultReportAgentInstructions(reportMCPCredentialSlot)
	requireContainsAll(t, instructions,
		defaultReportAssetsMarker,
		defaultReportAgentMarker,
		defaultReportAgentTypesPrefix,
		"报告内容、结构和写作风格由当前绑定的 Skill 决定",
		"每次报告运行必须先实际调用 Skill 工具加载 aida-report",
		"输入文本中出现 /aida-report 不代表已经加载",
		"不在 Prompt 中增加报告内容规则",
		"run_id、report_type、period、calendar_context、target",
		"report_source_selection_id",
		"get_report_context",
		"不得再调用 get_sessions、get_tasks 或 get_requirements",
		"兼容报告仍按当前 Skill 的旧工具路由",
		"team + report_scope=personal",
		"department + report_scope=team",
		"write_report_result",
		"write_report_failure",
		"不要用最终对话回复代替 MCP 回写",
	)
	for _, forbidden := range []string{"PRD/ADR", "TDD", "Top-K", "REPORT_CONTENT_INVALID", "验证记录", "文件变更"} {
		if strings.Contains(instructions, forbidden) {
			t.Fatalf("agent protocol must not constrain report prose with %q: %q", forbidden, instructions)
		}
	}
}

func TestReportMCPURLUsesExplicitOverride(t *testing.T) {
	h := NewManagedAgentHandlerWithDefaults(nil, nil, ManagedAgentDefaults{
		AIDAPublicBaseURL: "https://public-aida.example.com",
		ReportMCPURL:      "http://10.0.0.8/api/v1/mcp/reports/",
	})
	if got := h.reportMCPURL(); got != "http://10.0.0.8/api/v1/mcp/reports" {
		t.Fatalf("reportMCPURL = %q", got)
	}
}

func TestManagedAgentProxyReturnsNotConfiguredCode(t *testing.T) {
	h := NewManagedAgentHandler(nil, service.NewManagedAgentClient("", ""))
	req := httptest.NewRequest(http.MethodGet, "/ai-assets/skills", nil)
	rec := httptest.NewRecorder()

	h.ListSkills(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["code"] != service.ManagedAgentNotConfiguredCode {
		t.Fatalf("code = %q", got["code"])
	}
}

func TestManagedAgentProxyReturnsUpstreamErrorCode(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "platform down", http.StatusInternalServerError)
	}))
	defer platform.Close()

	h := NewManagedAgentHandler(nil, service.NewManagedAgentClient(platform.URL, "platform-token"))
	req := httptest.NewRequest(http.MethodGet, "/ai-assets/skills", nil)
	rec := httptest.NewRecorder()

	h.ListSkills(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["code"] != service.ManagedAgentUpstreamErrorCode {
		t.Fatalf("code = %q", got["code"])
	}
}

func TestManagedAgentProxyReturnsUnreachableCode(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	platformURL := platform.URL
	platform.Close()

	h := NewManagedAgentHandler(nil, service.NewManagedAgentClient(platformURL, "platform-token"))
	req := httptest.NewRequest(http.MethodGet, "/ai-assets/skills", nil)
	rec := httptest.NewRecorder()

	h.ListSkills(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["code"] != service.ManagedAgentUnreachableCode {
		t.Fatalf("code = %q", got["code"])
	}
}

func TestCreateSkillProxiesSkillMDMultipart(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/skill" {
			t.Fatalf("platform route = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer user-token" {
			t.Fatalf("authorization = %q", got)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if got := r.FormValue("slug"); got != "daily-summary" {
			t.Fatalf("slug = %q", got)
		}
		if got := r.FormValue("version"); got != "1.0.0" {
			t.Fatalf("version = %q", got)
		}
		if got := r.FormValue("skill_md"); got != "# Daily Summary\n\nUse session data." {
			t.Fatalf("skill_md = %q", got)
		}
		writeJSON(w, http.StatusOK, service.CreateManagedSkillResponse{
			SkillID: "skill-1",
			Owner:   "alice",
			Slug:    "daily-summary",
			Version: "1.0.0",
			SHA256:  "abc",
		})
	}))
	defer platform.Close()

	h := NewManagedAgentHandler(nil, service.NewManagedAgentClient(platform.URL, "platform-token"))
	body := `{"slug":"daily-summary","version":"1.0.0","name":"Daily Summary","description":"test","skill_md":"# Daily Summary\n\nUse session data."}`
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/skills", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer user-token")
	rec := httptest.NewRecorder()

	h.CreateSkill(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got service.CreateManagedSkillResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SkillID != "skill-1" || got.Owner != "alice" {
		t.Fatalf("response = %+v", got)
	}
}

func TestArchiveSkillProxiesLifecycleRequest(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/skill/daily-summary/1.0.0/archive" {
			t.Fatalf("platform route = %s %s", r.Method, r.URL.Path)
		}
		var req service.ArchiveManagedSkillRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if !req.Archived {
			t.Fatalf("archived = false")
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))
	defer platform.Close()

	h := NewManagedAgentHandler(nil, service.NewManagedAgentClient(platform.URL, "platform-token"))
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/skills/daily-summary/1.0.0/archive", bytes.NewBufferString(`{"archived":true}`))
	req = requestWithURLParams(req, map[string]string{"slug": "daily-summary", "version": "1.0.0"})
	rec := httptest.NewRecorder()

	h.ArchiveSkill(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestDeleteSkillProxiesLifecycleRequest(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/skill/daily-summary/1.0.0" {
			t.Fatalf("platform route = %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}))
	defer platform.Close()

	h := NewManagedAgentHandler(nil, service.NewManagedAgentClient(platform.URL, "platform-token"))
	req := httptest.NewRequest(http.MethodDelete, "/ai-assets/skills/daily-summary/1.0.0", nil)
	req = requestWithURLParams(req, map[string]string{"slug": "daily-summary", "version": "1.0.0"})
	rec := httptest.NewRecorder()

	h.DeleteSkill(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetSkillMarkdownProxiesSkillFile(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/skill/alice/daily-summary/1.0.0/file" {
			t.Fatalf("platform route = %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("path"); got != "SKILL.md" {
			t.Fatalf("path query = %q", got)
		}
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte("# Daily Summary\n\nUse session data."))
	}))
	defer platform.Close()

	h := NewManagedAgentHandler(nil, service.NewManagedAgentClient(platform.URL, "platform-token"))
	req := httptest.NewRequest(http.MethodGet, "/ai-assets/skills/_mine/daily-summary/1.0.0/skill-md", nil)
	req = requestWithUser(req, &model.User{ID: "user-1", Username: "alice"})
	req = requestWithURLParams(req, map[string]string{"owner": "_mine", "slug": "daily-summary", "version": "1.0.0"})
	rec := httptest.NewRecorder()

	h.GetSkillMarkdown(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["content"] != "# Daily Summary\n\nUse session data." {
		t.Fatalf("content = %q", got["content"])
	}
}

func TestStartAgentRunAllowsDefaultModelAndRecordsManualRun(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var submitted service.SubmitManagedTaskRequest
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/my/agents" {
			writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{Agents: []model.ManagedAgent{{
				AgentID: "agent-1",
				Name:    "default model agent",
				Engine:  "claude-code",
			}}})
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/task/submit" {
			t.Fatalf("platform path = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer platform-token" {
			t.Fatalf("authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
			t.Fatal(err)
		}
		writeJSON(w, http.StatusOK, service.SubmitManagedTaskResponse{
			TaskID: "task-123",
			Status: "queued",
		})
	}))
	defer platform.Close()

	now := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("INSERT INTO ai_runs").
		WithArgs("user-1", "manual_agent_run", "agent-1", "task-123", nil, "running", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run-1"))
	mock.ExpectQuery("SELECT id::text").
		WithArgs("run-1", "user-1").
		WillReturnRows(sqlmock.NewRows(aiRunColumns()).
			AddRow("run-1", "user-1", "manual_agent_run", nil, "managed_task", "agent-1", nil, "task-123", nil, nil, "running", []byte(`{"message":"生成日报","params":{"report_date":"2026-06-26"},"trigger_source":"manual"}`), []byte(`{}`), nil, now, nil, now))

	h := NewManagedAgentHandler(db, service.NewManagedAgentClient(platform.URL, "platform-token"))
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/agents/agent-1/runs", bytes.NewBufferString(`{"message":"生成日报","params":{"report_date":"2026-06-26"}}`))
	req = requestWithUser(req, &model.User{ID: "user-1", Name: "张三", Role: "employee"})
	req = requestWithURLParam(req, "agentId", "agent-1")
	rec := httptest.NewRecorder()

	h.StartAgentRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if submitted.AgentID != "agent-1" {
		t.Fatalf("agent_id = %q", submitted.AgentID)
	}
	if submitted.ModelID != "" {
		t.Fatalf("model_id should be empty for default model, got %q", submitted.ModelID)
	}
	if submitted.Params["message"] != "生成日报" || submitted.Params["report_date"] != "2026-06-26" {
		t.Fatalf("params = %#v", submitted.Params)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestStartAgentRunDoesNotInjectReportMCPParams(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var submitted service.SubmitManagedTaskRequest
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/my/agents" {
			writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{Agents: []model.ManagedAgent{{
				AgentID: "agent-1",
				Name:    "generic report params",
				Engine:  "claude-code",
			}}})
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/task/submit" {
			t.Fatalf("platform path = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
			t.Fatal(err)
		}
		writeJSON(w, http.StatusOK, service.SubmitManagedTaskResponse{
			TaskID: "task-report",
			Status: "queued",
		})
	}))
	defer platform.Close()

	now := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("INSERT INTO ai_runs").
		WithArgs("user-1", "manual_agent_run", "agent-1", "task-report", nil, "running", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run-report"))
	mock.ExpectQuery("SELECT id::text").
		WithArgs("run-report", "user-1").
		WillReturnRows(sqlmock.NewRows(aiRunColumns()).
			AddRow("run-report", "user-1", "manual_agent_run", nil, "managed_task", "agent-1", nil, "task-report", nil, nil, "running", []byte(`{"message":"生成我的日报","params":{"period.date":"2026-06-26","report_type":"personal_daily"},"trigger_source":"manual"}`), []byte(`{}`), nil, now, nil, now))

	h := NewManagedAgentHandler(db, service.NewManagedAgentClient(platform.URL, "platform-token"))
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/agents/agent-1/runs", bytes.NewBufferString(`{"message":"生成我的日报","params":{"report_type":"personal_daily","period.date":"2026-06-26"}}`))
	req.Host = "aida.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Authorization", "Bearer user-token")
	req = requestWithUser(req, &model.User{ID: "user-1", Name: "张三", Role: "employee"})
	req = requestWithURLParam(req, "agentId", "agent-1")
	rec := httptest.NewRecorder()

	h.StartAgentRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if submitted.ModelID != "" {
		t.Fatalf("model_id = %q", submitted.ModelID)
	}
	if submitted.Params["report_type"] != "personal_daily" || submitted.Params["period.date"] != "2026-06-26" {
		t.Fatalf("report params = %#v", submitted.Params)
	}
	if _, ok := submitted.Params["run_id"]; ok {
		t.Fatalf("generic run should not inject run_id: %#v", submitted.Params)
	}
	if _, ok := submitted.Params["mcp_url"]; ok {
		t.Fatalf("generic run should not inject mcp_url: %#v", submitted.Params)
	}
	if _, ok := submitted.Params["mcp_"+"authorization"]; ok {
		t.Fatalf("generic run should not inject authorization: %#v", submitted.Params)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestStartAgentRunUsesCredentialedSessionForReportMCPBinding(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	reportMCPAgent := model.ManagedAgent{
		AgentID:             "agent-cred",
		Name:                "credentialed generic",
		Engine:              "claude-code",
		DefaultModelID:      "MiniMax-M2.5",
		StartPromptTemplate: "测试 {{ text }}",
		CredentialSlots: []model.ManagedCredentialSlot{{
			Name:     reportMCPCredentialSlot,
			Required: true,
		}},
		Skills: []model.ManagedSkillRef{{
			Owner: "alice", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion,
		}},
		MCPBindings: []model.ManagedMCPBinding{{
			Owner: "alice", Slug: service.ReportMCPSlug, Version: service.ReportMCPVersion, CredentialSlot: reportMCPCredentialSlot,
		}},
	}
	var createdCredential service.CreateManagedCredentialRequest
	var createdSession service.CreateManagedSessionRequest
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/my/agents":
			writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{Agents: []model.ManagedAgent{reportMCPAgent}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/credential":
			if err := json.NewDecoder(r.Body).Decode(&createdCredential); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, http.StatusOK, service.CreateManagedCredentialResponse{CredentialID: "cred-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			if err := json.NewDecoder(r.Body).Decode(&createdSession); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, http.StatusOK, service.CreateManagedSessionResponse{
				SessionID: "session-1",
				Status:    "running",
				ModelID:   "MiniMax-M2.5",
			})
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer platform.Close()

	now := time.Date(2026, 7, 2, 9, 20, 0, 0, time.UTC)
	safeInputRef := jsonStringArg{
		require: []string{"生成报告", "report_type", reportMCPCredentialSlot, "credential_override"},
		forbid:  []string{"user-token", "cred-1"},
	}
	mock.ExpectQuery("INSERT INTO ai_runs").
		WithArgs("user-1", "manual_agent_run", "agent-cred", "MiniMax-M2.5", safeInputRef).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run-cred"))
	mock.ExpectExec("UPDATE ai_runs SET external_session_id").
		WithArgs("session-1", "MiniMax-M2.5", "running", safeInputRef, "run-cred", "user-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT id::text").
		WithArgs("run-cred", "user-1").
		WillReturnRows(sqlmock.NewRows(aiRunColumns()).
			AddRow("run-cred", "user-1", "manual_agent_run", nil, "managed_session", "agent-cred", nil, nil, "session-1", "MiniMax-M2.5", "running", []byte(`{"message":"生成报告","params":{"text":"test","report_type":"personal_daily"},"trigger_source":"manual","credential_slots":["AIDA_REPORT_MCP_AUTH"],"credential_override":"redacted"}`), []byte(`{}`), nil, now, nil, now))

	h := NewManagedAgentHandlerWithDefaults(db, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/agents/agent-cred/runs", bytes.NewBufferString(`{"message":"生成报告","model_id":"MiniMax-M2.5","params":{"text":"test","report_type":"personal_daily"}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	req = requestWithUser(req, &model.User{ID: "user-1", Username: "alice", Role: "employee"})
	req = requestWithURLParam(req, "agentId", "agent-cred")
	rec := httptest.NewRecorder()

	h.StartAgentRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if createdCredential.Value != "user-token" {
		t.Fatalf("credential value = %q", createdCredential.Value)
	}
	if createdSession.CredentialOverrides[reportMCPCredentialSlot] != "cred-1" {
		t.Fatalf("credential overrides = %#v", createdSession.CredentialOverrides)
	}
	if createdSession.StartPromptValues["text"] != "test" || createdSession.StartPromptValues["message"] != "生成报告" {
		t.Fatalf("start prompt values = %#v", createdSession.StartPromptValues)
	}
	if strings.TrimSpace(createdSession.Message) != "" {
		t.Fatalf("message should stay empty so platform renders template, got %q", createdSession.Message)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestStartAgentRunUsesCredentialedSessionForDefaultBoundMCP(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	agent := model.ManagedAgent{
		AgentID:        "agent-github",
		Name:           "github agent",
		Engine:         "claude-code",
		DefaultModelID: "MiniMax-M2.5",
		CredentialSlots: []model.ManagedCredentialSlot{{
			Name:     "mcp-github",
			Required: true,
		}},
		DefaultBindings: map[string]string{"mcp-github": "cred-default"},
		MCPBindings: []model.ManagedMCPBinding{{
			Owner: "alice", Slug: "github", Version: "1.0.0", CredentialSlot: "mcp-github",
		}},
	}
	var createdSession service.CreateManagedSessionRequest
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/my/agents":
			writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{Agents: []model.ManagedAgent{agent}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			if err := json.NewDecoder(r.Body).Decode(&createdSession); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, http.StatusOK, service.CreateManagedSessionResponse{
				SessionID: "session-github",
				Status:    "running",
				ModelID:   "MiniMax-M2.5",
			})
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer platform.Close()

	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	inputRef := jsonStringArg{require: []string{"查 issue", "repository"}, forbid: []string{"cred-default"}}
	mock.ExpectQuery("INSERT INTO ai_runs").
		WithArgs("user-1", "manual_agent_run", "agent-github", "MiniMax-M2.5", inputRef).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run-github"))
	mock.ExpectExec("UPDATE ai_runs SET external_session_id").
		WithArgs("session-github", "MiniMax-M2.5", "running", inputRef, "run-github", "user-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT id::text").
		WithArgs("run-github", "user-1").
		WillReturnRows(sqlmock.NewRows(aiRunColumns()).
			AddRow("run-github", "user-1", "manual_agent_run", nil, "managed_session", "agent-github", nil, nil, "session-github", "MiniMax-M2.5", "running", []byte(`{"message":"查 issue","params":{"repository":"org/repo"},"trigger_source":"manual"}`), []byte(`{}`), nil, now, nil, now))

	h := NewManagedAgentHandler(db, service.NewManagedAgentClient(platform.URL, "platform-token"))
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/agents/agent-github/runs", bytes.NewBufferString(`{"message":"查 issue","model_id":"MiniMax-M2.5","params":{"repository":"org/repo"}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	req = requestWithUser(req, &model.User{ID: "user-1", Username: "alice", Role: "employee"})
	req = requestWithURLParam(req, "agentId", "agent-github")
	rec := httptest.NewRecorder()

	h.StartAgentRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(createdSession.CredentialOverrides) != 0 {
		t.Fatalf("custom MCP with default binding should not create overrides: %#v", createdSession.CredentialOverrides)
	}
	if createdSession.StartPromptValues["repository"] != "org/repo" || createdSession.StartPromptValues["message"] != "查 issue" {
		t.Fatalf("start prompt values = %#v", createdSession.StartPromptValues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestStartAgentRunUsesRuntimeCredentialOverride(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	agent := model.ManagedAgent{
		AgentID:        "agent-github",
		Name:           "github agent",
		Engine:         "claude-code",
		DefaultModelID: "MiniMax-M2.5",
		CredentialSlots: []model.ManagedCredentialSlot{{
			Name:     "mcp-github",
			Required: true,
		}},
		MCPBindings: []model.ManagedMCPBinding{{
			Owner: "alice", Slug: "github", Version: "1.0.0", CredentialSlot: "mcp-github",
		}},
	}
	var createdSession service.CreateManagedSessionRequest
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/my/agents":
			writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{Agents: []model.ManagedAgent{agent}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			if err := json.NewDecoder(r.Body).Decode(&createdSession); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, http.StatusOK, service.CreateManagedSessionResponse{
				SessionID: "session-github",
				Status:    "running",
				ModelID:   "MiniMax-M2.5",
			})
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer platform.Close()

	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	inputRef := jsonStringArg{require: []string{"查 issue", "repository", "credential_override_slots", "mcp-github"}, forbid: []string{"cred-runtime"}}
	mock.ExpectQuery("INSERT INTO ai_runs").
		WithArgs("user-1", "manual_agent_run", "agent-github", "MiniMax-M2.5", inputRef).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run-github"))
	mock.ExpectExec("UPDATE ai_runs SET external_session_id").
		WithArgs("session-github", "MiniMax-M2.5", "running", inputRef, "run-github", "user-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT id::text").
		WithArgs("run-github", "user-1").
		WillReturnRows(sqlmock.NewRows(aiRunColumns()).
			AddRow("run-github", "user-1", "manual_agent_run", nil, "managed_session", "agent-github", nil, nil, "session-github", "MiniMax-M2.5", "running", []byte(`{"message":"查 issue","params":{"repository":"org/repo"},"trigger_source":"manual","credential_override_slots":["mcp-github"],"credential_override":"redacted"}`), []byte(`{}`), nil, now, nil, now))

	h := NewManagedAgentHandler(db, service.NewManagedAgentClient(platform.URL, "platform-token"))
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/agents/agent-github/runs", bytes.NewBufferString(`{"message":"查 issue","model_id":"MiniMax-M2.5","params":{"repository":"org/repo"},"credential_overrides":{"mcp-github":"cred-runtime"}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	req = requestWithUser(req, &model.User{ID: "user-1", Username: "alice", Role: "employee"})
	req = requestWithURLParam(req, "agentId", "agent-github")
	rec := httptest.NewRecorder()

	h.StartAgentRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if createdSession.CredentialOverrides["mcp-github"] != "cred-runtime" {
		t.Fatalf("credential overrides = %#v", createdSession.CredentialOverrides)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestStartAgentRunRejectsMissingDefaultCredentialBinding(t *testing.T) {
	agent := model.ManagedAgent{
		AgentID: "agent-missing-credential",
		Name:    "missing credential",
		Engine:  "claude-code",
		CredentialSlots: []model.ManagedCredentialSlot{{
			Name:     "mcp-github",
			Required: true,
		}},
		MCPBindings: []model.ManagedMCPBinding{{
			Owner: "alice", Slug: "github", Version: "1.0.0", CredentialSlot: "mcp-github",
		}},
	}
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/my/agents" {
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{Agents: []model.ManagedAgent{agent}})
	}))
	defer platform.Close()

	h := NewManagedAgentHandler(nil, service.NewManagedAgentClient(platform.URL, "platform-token"))
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/agents/agent-missing-credential/runs", bytes.NewBufferString(`{"message":"查 issue","model_id":"MiniMax-M2.5"}`))
	req.Header.Set("Authorization", "Bearer user-token")
	req = requestWithUser(req, &model.User{ID: "user-1", Username: "alice", Role: "employee"})
	req = requestWithURLParam(req, "agentId", "agent-missing-credential")
	rec := httptest.NewRecorder()

	h.StartAgentRun(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["code"] != "CREDENTIAL_REQUIRED" || !strings.Contains(rec.Body.String(), "mcp-github") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestListSkillsFiltersReportSystemSkill(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/skill/list" {
			t.Fatalf("platform route = %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{Skills: []model.ManagedSkill{
			{SkillID: "system-skill", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion, Name: service.ReportSkillName},
			{SkillID: "user-skill", Slug: "daily-summary", Version: "1.0.0", Name: "Daily Summary"},
		}})
	}))
	defer platform.Close()

	h := NewManagedAgentHandler(nil, service.NewManagedAgentClient(platform.URL, "platform-token"))
	req := httptest.NewRequest(http.MethodGet, "/ai-assets/skills?scope=mine", nil)
	rec := httptest.NewRecorder()

	h.ListSkills(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.ListManagedSkillsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 1 || got.Skills[0].Slug != "daily-summary" {
		t.Fatalf("skills = %#v", got.Skills)
	}
}

func TestListSkillsIncludesReportSystemSkillForResourcePicker(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/skill/list" {
			t.Fatalf("platform route = %s %s", r.Method, r.URL.Path)
		}
		switch r.URL.Query().Get("scope") {
		case "mine":
			writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{Skills: []model.ManagedSkill{
				{SkillID: "duplicate-user-report", Owner: "alice", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion, Name: service.ReportSkillName},
				{SkillID: "user-skill", Owner: "alice", Slug: "daily-summary", Version: "1.0.0", Name: "Daily Summary"},
			}})
		case "public":
			writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{Skills: []model.ManagedSkill{
				{SkillID: "system-skill", Owner: "100866", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion, Name: service.ReportSkillName},
				{SkillID: "production-skill", Owner: "aida-system", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion, Name: service.ReportSkillName},
			}})
		default:
			t.Fatalf("unexpected scope %q", r.URL.Query().Get("scope"))
		}
	}))
	defer platform.Close()

	h := NewManagedAgentHandlerWithDefaults(nil, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	req := httptest.NewRequest(http.MethodGet, "/ai-assets/skills?scope=mine&include_system=true", nil)
	rec := httptest.NewRecorder()

	h.ListSkills(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.ListManagedSkillsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 2 {
		t.Fatalf("skills = %#v", got.Skills)
	}
	if got.Skills[0].Owner != "100866" || got.Skills[0].Slug != service.ReportSkillSlug || got.Skills[1].Slug != "daily-summary" {
		t.Fatalf("skills order/content = %#v", got.Skills)
	}
}

func TestResolveSystemReportSkillRequiresConfiguredOwner(t *testing.T) {
	platformCalled := false
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		platformCalled = true
		t.Fatalf("platform must not be called without configured owner")
	}))
	defer platform.Close()

	h := NewManagedAgentHandler(nil, service.NewManagedAgentClient(platform.URL, "platform-token"))
	_, err := h.resolveSystemReportSkill(context.Background(), h.client)
	if err == nil || !strings.Contains(err.Error(), "MANAGED_AGENT_REPORT_SKILL_OWNER") {
		t.Fatalf("error = %v", err)
	}
	if platformCalled {
		t.Fatal("platform was called")
	}
}

func TestResolveSystemReportSkillRejectsOwnerlessPlatformResult(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{Skills: []model.ManagedSkill{{
			SkillID: "ownerless-skill", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion,
		}}})
	}))
	defer platform.Close()

	h := NewManagedAgentHandlerWithDefaults(nil, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	_, err := h.resolveSystemReportSkill(context.Background(), h.client)
	if err == nil || !strings.Contains(err.Error(), "owner-less") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveSystemReportSkillRejectsArchivedTarget(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{Skills: []model.ManagedSkill{{
			SkillID: "archived-system-skill", Owner: "100866", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion, Archived: true,
		}}})
	}))
	defer platform.Close()

	h := NewManagedAgentHandlerWithDefaults(nil, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	_, err := h.resolveSystemReportSkill(context.Background(), h.client)
	if err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestCreateDefaultReportAgentMissingSystemSkillDoesNotWritePlatform(t *testing.T) {
	writeCalled := false
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/skill/list" {
			writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{})
			return
		}
		writeCalled = true
		t.Fatalf("unexpected platform write/request: %s %s", r.Method, r.URL.Path)
	}))
	defer platform.Close()

	h := NewManagedAgentHandlerWithDefaults(nil, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/report-agents/default", nil)
	req.Header.Set("Authorization", "Bearer user-token")
	req = requestWithUser(req, &model.User{ID: "307", Username: "local-user-name", Role: "employee"})
	rec := httptest.NewRecorder()

	h.CreateDefaultReportAgent(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if writeCalled {
		t.Fatal("platform was modified after system Skill resolution failed")
	}
	if !strings.Contains(rec.Body.String(), "100866/aida-report@1.0.0") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestResolveAndRepairReportAgentDependenciesFailureDoesNotUpdateAgent(t *testing.T) {
	putCalled := false
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/skill/list" {
			writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{})
			return
		}
		if r.Method == http.MethodPut {
			putCalled = true
		}
		t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
	}))
	defer platform.Close()

	h := NewManagedAgentHandlerWithDefaults(nil, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	agent := model.ManagedAgent{
		AgentID: "agent-report",
		Skills:  []model.ManagedSkillRef{{Owner: "001898", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion}},
	}
	err := h.resolveAndRepairReportAgentDependencies(context.Background(), h.client, &agent)
	if err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("error = %v", err)
	}
	if putCalled {
		t.Fatal("Agent PUT was sent before system Skill resolution succeeded")
	}
	if len(agent.Skills) != 1 || agent.Skills[0].Owner != "001898" {
		t.Fatalf("agent was mutated after resolution failure: %#v", agent.Skills)
	}
}

func TestListSkillsHidesArchivedByDefault(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{Skills: []model.ManagedSkill{
			{SkillID: "skill-active", Slug: "daily-summary", Version: "1.0.0", Name: "Daily Summary", Archived: false},
			{SkillID: "skill-archived", Slug: "legacy-summary", Version: "0.9.0", Name: "Legacy Summary", Archived: true},
		}})
	}))
	defer platform.Close()

	h := NewManagedAgentHandler(nil, service.NewManagedAgentClient(platform.URL, "platform-token"))
	req := httptest.NewRequest(http.MethodGet, "/ai-assets/skills?scope=mine", nil)
	rec := httptest.NewRecorder()

	h.ListSkills(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.ListManagedSkillsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 1 || got.Skills[0].Slug != "daily-summary" {
		t.Fatalf("skills = %#v", got.Skills)
	}
}

func TestListSkillsIncludeArchivedReturnsArchivedItems(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{Skills: []model.ManagedSkill{
			{SkillID: "skill-active", Slug: "daily-summary", Version: "1.0.0", Name: "Daily Summary", Archived: false},
			{SkillID: "skill-archived", Slug: "legacy-summary", Version: "0.9.0", Name: "Legacy Summary", Archived: true},
		}})
	}))
	defer platform.Close()

	h := NewManagedAgentHandler(nil, service.NewManagedAgentClient(platform.URL, "platform-token"))
	req := httptest.NewRequest(http.MethodGet, "/ai-assets/skills?scope=mine&include_archived=true", nil)
	rec := httptest.NewRecorder()

	h.ListSkills(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.ListManagedSkillsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 2 {
		t.Fatalf("skills = %#v", got.Skills)
	}
}

func TestListMCPEntriesFiltersReportSystemMCP(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/mcp/list" {
			t.Fatalf("platform route = %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, model.ListManagedMCPEntriesResponse{Entries: []model.ManagedMCPEntry{
			{EntryID: "system-mcp", Slug: "aida-report-mcp", Version: "report-v1", Name: "Aida Report MCP"},
			{EntryID: "user-mcp", Slug: "user-tools", Version: "1.0.0", Name: "User Tools"},
		}})
	}))
	defer platform.Close()

	h := NewManagedAgentHandlerWithDefaults(nil, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	req := httptest.NewRequest(http.MethodGet, "/ai-assets/mcp?scope=mine", nil)
	rec := httptest.NewRecorder()

	h.ListMCPEntries(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.ListManagedMCPEntriesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Slug != "user-tools" {
		t.Fatalf("entries = %#v", got.Entries)
	}
}

func TestListMCPEntriesIncludesReportSystemMCPForResourcePicker(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/mcp/list" {
			t.Fatalf("platform route = %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, model.ListManagedMCPEntriesResponse{Entries: []model.ManagedMCPEntry{
			{EntryID: "system-mcp", Slug: "aida-report-mcp", Version: "report-v1", Name: "Aida Report MCP"},
			{EntryID: "user-mcp", Slug: "user-tools", Version: "1.0.0", Name: "User Tools"},
		}})
	}))
	defer platform.Close()

	h := NewManagedAgentHandlerWithDefaults(nil, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	req := httptest.NewRequest(http.MethodGet, "/ai-assets/mcp?scope=mine&include_system=true", nil)
	rec := httptest.NewRecorder()

	h.ListMCPEntries(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.ListManagedMCPEntriesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("entries = %#v", got.Entries)
	}
	if got.Entries[0].Slug != "aida-report-mcp" || got.Entries[1].Slug != "user-tools" {
		t.Fatalf("entries order/content = %#v", got.Entries)
	}
}

func TestListMCPEntriesHideArchivedByDefault(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, model.ListManagedMCPEntriesResponse{Entries: []model.ManagedMCPEntry{
			{EntryID: "mcp-active", Slug: "user-tools", Version: "1.0.0", Name: "User Tools", Archived: false},
			{EntryID: "mcp-archived", Slug: "legacy-tools", Version: "0.9.0", Name: "Legacy Tools", Archived: true},
		}})
	}))
	defer platform.Close()

	h := NewManagedAgentHandlerWithDefaults(nil, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	req := httptest.NewRequest(http.MethodGet, "/ai-assets/mcp?scope=mine", nil)
	rec := httptest.NewRecorder()

	h.ListMCPEntries(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.ListManagedMCPEntriesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Slug != "user-tools" {
		t.Fatalf("entries = %#v", got.Entries)
	}
}

func TestListMCPEntriesIncludeArchivedReturnsArchivedItems(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, model.ListManagedMCPEntriesResponse{Entries: []model.ManagedMCPEntry{
			{EntryID: "mcp-active", Slug: "user-tools", Version: "1.0.0", Name: "User Tools", Archived: false},
			{EntryID: "mcp-archived", Slug: "legacy-tools", Version: "0.9.0", Name: "Legacy Tools", Archived: true},
		}})
	}))
	defer platform.Close()

	h := NewManagedAgentHandlerWithDefaults(nil, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	req := httptest.NewRequest(http.MethodGet, "/ai-assets/mcp?scope=mine&include_archived=true", nil)
	rec := httptest.NewRecorder()

	h.ListMCPEntries(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.ListManagedMCPEntriesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("entries = %#v", got.Entries)
	}
}

func TestListMyAgentsHideArchivedByDefault(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/my/agents" {
			t.Fatalf("platform route = %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{Agents: []model.ManagedAgent{
			{AgentID: "agent-active", Name: "Active Agent", Engine: "codex", Archived: false},
			{AgentID: "agent-archived", Name: "Archived Agent", Engine: "codex", Archived: true},
		}})
	}))
	defer platform.Close()

	h := NewManagedAgentHandler(nil, service.NewManagedAgentClient(platform.URL, "platform-token"))
	req := httptest.NewRequest(http.MethodGet, "/ai-assets/agents", nil)
	req = requestWithUser(req, &model.User{ID: "user-1", Username: "alice"})
	rec := httptest.NewRecorder()

	h.ListMyAgents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.ListManagedAgentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Agents) != 1 || got.Agents[0].AgentID != "agent-active" {
		t.Fatalf("agents = %#v", got.Agents)
	}
}

func TestListMyAgentsIncludeArchivedReturnsArchivedItems(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{Agents: []model.ManagedAgent{
			{AgentID: "agent-active", Name: "Active Agent", Engine: "codex", Archived: false},
			{AgentID: "agent-archived", Name: "Archived Agent", Engine: "codex", Archived: true},
		}})
	}))
	defer platform.Close()

	h := NewManagedAgentHandler(nil, service.NewManagedAgentClient(platform.URL, "platform-token"))
	req := httptest.NewRequest(http.MethodGet, "/ai-assets/agents?include_archived=true", nil)
	req = requestWithUser(req, &model.User{ID: "user-1", Username: "alice"})
	rec := httptest.NewRecorder()

	h.ListMyAgents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.ListManagedAgentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Agents) != 2 {
		t.Fatalf("agents = %#v", got.Agents)
	}
}

func TestReportSkillProtectedLifecycle(t *testing.T) {
	h := NewManagedAgentHandler(nil, service.NewManagedAgentClient("https://managed.example.com", "platform-token"))
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{
			name:   "archive",
			method: http.MethodPost,
			path:   "/ai-assets/skills/aida-report/1.0.0/archive",
			body:   `{"archived":true}`,
			call:   h.ArchiveSkill,
		},
		{
			name:   "delete",
			method: http.MethodDelete,
			path:   "/ai-assets/skills/aida-report/1.0.0",
			call:   h.DeleteSkill,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req = requestWithURLParams(req, map[string]string{"slug": service.ReportSkillSlug, "version": service.ReportSkillVersion})
			rec := httptest.NewRecorder()
			tc.call(rec, req)
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			var got map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got["code"] != reportSkillProtectedCode {
				t.Fatalf("code = %q", got["code"])
			}
		})
	}
}

func TestReportMCPProtectedLifecycle(t *testing.T) {
	h := NewManagedAgentHandlerWithDefaults(nil, service.NewManagedAgentClient("https://managed.example.com", "platform-token"), testManagedAgentDefaults())
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{
			name:   "archive",
			method: http.MethodPost,
			path:   "/ai-assets/mcp/aida-report-mcp/report-v1/archive",
			body:   `{"archived":true}`,
			call:   h.ArchiveMCPEntry,
		},
		{
			name:   "delete",
			method: http.MethodDelete,
			path:   "/ai-assets/mcp/aida-report-mcp/report-v1",
			call:   h.DeleteMCPEntry,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req = requestWithURLParams(req, map[string]string{"slug": "aida-report-mcp", "version": "report-v1"})
			rec := httptest.NewRecorder()
			tc.call(rec, req)
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			var got map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got["code"] != reportMCPProtectedCode {
				t.Fatalf("code = %q", got["code"])
			}
		})
	}
}

func TestCreateDefaultReportAgentCreatesWhenOnlyOrdinaryAgentExists(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var createdAgent model.UpsertManagedAgentRequest
	ordinaryAgent := model.ManagedAgent{AgentID: "agent-generic", Name: "通用 Agent", Engine: "codex"}
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/my/agents":
			writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{Agents: []model.ManagedAgent{ordinaryAgent}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/skill/list":
			if r.URL.Query().Get("scope") != "public" {
				t.Fatalf("unexpected skill scope %q", r.URL.Query().Get("scope"))
			}
			writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{Skills: []model.ManagedSkill{{
				SkillID: "system-skill", Owner: "100866", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion, Name: service.ReportSkillName,
			}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/skill":
			t.Fatal("default Report Agent must not create a user-owned Report Skill")
		case r.Method == http.MethodPost && r.URL.Path == "/api/my/agents":
			if err := json.NewDecoder(r.Body).Decode(&createdAgent); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, http.StatusOK, model.UpsertManagedAgentResponse{AgentID: createdAgent.AgentID, ManagedVersion: 1})
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer platform.Close()

	mock.ExpectQuery("SELECT agent_id, business_type, report_types").
		WithArgs("307", "agent-generic").
		WillReturnRows(sqlmock.NewRows([]string{"agent_id", "business_type", "report_types", "is_default_report"}))
	mock.ExpectExec("UPDATE managed_agent_profiles").
		WithArgs("307", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO managed_agent_profiles").
		WithArgs(sqlmock.AnyArg(), "307", managedAgentBusinessReport, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	h := NewManagedAgentHandlerWithDefaults(db, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/report-agents/default", nil)
	req.Header.Set("Authorization", "Bearer user-token")
	req = requestWithUser(req, &model.User{ID: "307", Username: "t05", Role: "employee"})
	rec := httptest.NewRecorder()

	h.CreateDefaultReportAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if createdAgent.AgentID == "" || createdAgent.Name != defaultReportAgentName {
		t.Fatalf("created agent request=%#v", createdAgent)
	}
	if createdAgent.Engine != "claude-code" || createdAgent.DefaultModelID != "MiniMax-M2.5" {
		t.Fatalf("engine/model = %q/%q", createdAgent.Engine, createdAgent.DefaultModelID)
	}
	if createdAgent.Description != defaultReportAgentDescription {
		t.Fatalf("description = %q", createdAgent.Description)
	}
	if containsDefaultMarkers(createdAgent.Description) {
		t.Fatalf("description should not expose default markers: %q", createdAgent.Description)
	}
	if !containsDefaultMarkers(createdAgent.Instructions) {
		t.Fatalf("instructions missing default markers: %q", createdAgent.Instructions)
	}
	if !hasSkillRef(createdAgent.Skills, service.ReportSkillSlug, service.ReportSkillVersion) {
		t.Fatalf("skills = %#v", createdAgent.Skills)
	}
	if len(createdAgent.Skills) != 1 || createdAgent.Skills[0].Owner != "100866" {
		t.Fatalf("system report skill = %#v", createdAgent.Skills)
	}
	if len(createdAgent.MCPBindings) != 0 {
		t.Fatalf("report mcp should be inline, bindings = %#v", createdAgent.MCPBindings)
	}
	if len(createdAgent.MCPServers) != 1 || createdAgent.MCPServers[0].Name != service.ReportMCPSlug || createdAgent.MCPServers[0].CredentialSlot != reportMCPCredentialSlot || strings.Contains(fmt.Sprint(createdAgent.MCPServers), "user-token") {
		t.Fatalf("inline mcp wiring slots=%#v servers=%#v", createdAgent.CredentialSlots, createdAgent.MCPServers)
	}
	if !hasCredentialSlot(createdAgent.CredentialSlots, reportMCPCredentialSlot) {
		t.Fatalf("credential slots=%#v", createdAgent.CredentialSlots)
	}
	var got model.ManagedAgent
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.BusinessType != managedAgentBusinessReport || !containsString(got.ReportTypes, reportTypePersonalDaily) {
		t.Fatalf("response = %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestEnsureReportMCPEntryRejectsExistingDifferentURL(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/mcp/list":
			writeJSON(w, http.StatusOK, model.ListManagedMCPEntriesResponse{Entries: []model.ManagedMCPEntry{{
				Slug:    "aida-report-mcp",
				Version: "report-v1",
				URL:     "https://old-aida.example.com/api/v1/mcp/reports",
			}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/mcp":
			t.Fatalf("should not create over mismatched immutable mcp entry")
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer platform.Close()

	h := NewManagedAgentHandlerWithDefaults(nil, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	err := h.ensureReportMCPEntry(httptest.NewRequest(http.MethodPost, "/", nil), h.client)
	if err == nil || !strings.Contains(err.Error(), "publish a new MCP asset version in code") {
		t.Fatalf("expected explicit mcp version error, got %v", err)
	}
}

func TestRepairedReportAgentDependencyMovesReportMCPToInlineServer(t *testing.T) {
	h := NewManagedAgentHandlerWithDefaults(nil, nil, ManagedAgentDefaults{
		ReportMCPSlug:           service.ReportMCPSlug,
		ReportMCPVersion:        "report-prod-v1",
		ReportMCPCredentialSlot: reportMCPCredentialSlot,
		ReportSkillSlug:         service.ReportSkillSlug,
		ReportSkillVersion:      "1.0.1-prod",
	})
	agent := model.ManagedAgent{
		AgentID: "agent-report",
		MCPBindings: []model.ManagedMCPBinding{
			{Owner: "t03", Slug: service.ReportMCPSlug, Version: service.ReportMCPVersion, CredentialSlot: reportMCPCredentialSlot},
			{Owner: "t03", Slug: "custom-mcp", Version: "1.0.0", CredentialSlot: "custom-slot"},
		},
		Skills: []model.ManagedSkillRef{
			{Owner: "t03", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion},
			{Owner: "t03", Slug: "custom-skill", Version: "1.0.0"},
		},
	}

	req, changed := h.repairedReportAgentDependencyRequest(agent, model.ManagedSkillRef{Owner: "t03", Slug: service.ReportSkillSlug, Version: "1.0.1-prod"})
	if !changed {
		t.Fatal("expected stale system assets to be replaced")
	}
	if len(req.MCPBindings) != 1 || req.MCPBindings[0].Slug != "custom-mcp" {
		t.Fatalf("custom binding should be preserved and report binding removed, got %#v", req.MCPBindings)
	}
	if len(req.MCPServers) != 1 || req.MCPServers[0].Name != service.ReportMCPSlug || req.MCPServers[0].CredentialSlot != reportMCPCredentialSlot {
		t.Fatalf("current report mcp server = %#v", req.MCPServers)
	}
	if len(req.Skills) != 2 {
		t.Fatalf("skills = %#v", req.Skills)
	}
	if req.Skills[0].Slug != "custom-skill" {
		t.Fatalf("custom skill should be preserved, got %#v", req.Skills)
	}
	gotSkill := req.Skills[1]
	if gotSkill.Slug != service.ReportSkillSlug || gotSkill.Version != "1.0.1-prod" || gotSkill.Owner != "t03" {
		t.Fatalf("current report skill = %#v", gotSkill)
	}
}

func TestReportSkillRefOwnerOmitsOwnerWhenPlatformOmitsOwner(t *testing.T) {
	got := reportSkillRefOwner(model.ManagedSkill{SkillID: "skill-1", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion}, "001898")
	if got != "" {
		t.Fatalf("owner = %q, want empty owner for platform mine skill", got)
	}
	got = reportSkillRefOwner(model.ManagedSkill{SkillID: "skill-1", Owner: "1066", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion}, "001898")
	if got != "1066" {
		t.Fatalf("owner = %q, want platform owner", got)
	}
}

func TestRepairedReportAgentDependencyReplacesStaleReportSkillOwner(t *testing.T) {
	h := NewManagedAgentHandlerWithDefaults(nil, nil, testManagedAgentDefaults())
	agent := model.ManagedAgent{
		AgentID: "agent-report",
		Skills: []model.ManagedSkillRef{
			{Owner: "001898", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion},
		},
		MCPServers: []model.ManagedMCPServer{{
			Name:           service.ReportMCPSlug,
			URL:            h.reportMCPURL(),
			CredentialSlot: reportMCPCredentialSlot,
			AuthHeader:     "Authorization",
			AuthScheme:     "Bearer",
		}},
		CredentialSlots: []model.ManagedCredentialSlot{{Name: reportMCPCredentialSlot, Required: true}},
	}

	req, changed := h.repairedReportAgentDependencyRequest(agent, model.ManagedSkillRef{Owner: "100866", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion})
	if !changed {
		t.Fatal("expected stale report skill owner to be cleared")
	}
	if len(req.Skills) != 1 {
		t.Fatalf("skills = %#v", req.Skills)
	}
	if req.Skills[0].Owner != "100866" || req.Skills[0].Slug != service.ReportSkillSlug || req.Skills[0].Version != service.ReportSkillVersion {
		t.Fatalf("report skill = %#v", req.Skills[0])
	}
}

func TestCreateDefaultReportAgentReturnsExistingReportAgent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	existing := model.ManagedAgent{
		AgentID:         "agent-report",
		Name:            "我的报告 Agent",
		Description:     "custom report agent",
		Engine:          "codex",
		CredentialSlots: []model.ManagedCredentialSlot{{Name: reportMCPCredentialSlot, Required: true}},
		Skills:          []model.ManagedSkillRef{{Owner: "100866", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion}},
		MCPServers:      []model.ManagedMCPServer{{Name: service.ReportMCPSlug, URL: "https://aida.example.com/api/v1/mcp/reports", CredentialSlot: reportMCPCredentialSlot, AuthHeader: "Authorization", AuthScheme: "Bearer"}},
	}
	postCalled := false
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/my/agents":
			writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{Agents: []model.ManagedAgent{existing}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/skill/list":
			if r.URL.Query().Get("scope") != "public" {
				t.Fatalf("unexpected skill scope %q", r.URL.Query().Get("scope"))
			}
			writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{Skills: []model.ManagedSkill{{
				SkillID: "skill-1",
				Owner:   "100866",
				Slug:    service.ReportSkillSlug,
				Version: service.ReportSkillVersion,
				Name:    service.ReportSkillName,
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/mcp/list":
			writeJSON(w, http.StatusOK, model.ListManagedMCPEntriesResponse{Entries: []model.ManagedMCPEntry{{
				EntryID: "mcp-1",
				Slug:    "aida-report-mcp",
				Version: "report-v1",
			}}})
		case r.Method == http.MethodPost:
			postCalled = true
			t.Fatalf("idempotent create must not post to %s", r.URL.Path)
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer platform.Close()

	mock.ExpectQuery("SELECT agent_id, business_type, report_types").
		WithArgs("307", "agent-report").
		WillReturnRows(sqlmock.NewRows([]string{"agent_id", "business_type", "report_types", "is_default_report"}).
			AddRow("agent-report", managedAgentBusinessReport, []byte(`["personal_daily"]`), false))
	mock.ExpectExec("UPDATE managed_agent_profiles").
		WithArgs("307", "agent-report").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO managed_agent_profiles").
		WithArgs("agent-report", "307", managedAgentBusinessReport, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	h := NewManagedAgentHandlerWithDefaults(db, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/report-agents/default", nil)
	req.Header.Set("Authorization", "Bearer user-token")
	req = requestWithUser(req, &model.User{ID: "307", Username: "t05", Role: "employee"})
	rec := httptest.NewRecorder()

	h.CreateDefaultReportAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.ManagedAgent
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AgentID != "agent-report" || got.BusinessType != managedAgentBusinessReport || postCalled {
		t.Fatalf("response=%#v postCalled=%v", got, postCalled)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestReportAgentRepairRequestDoesNotOverwriteCustomInstructions(t *testing.T) {
	customInstructions := defaultReportAgentMarker + "\n" + defaultReportAgentTypesPrefix + strings.Join(supportedReportTypes, ",") + "\n" + defaultManagedAgentMarker + "\n" + "用户自定义报告写作风格"
	customStartPrompt := "请用表格输出 {{ report_type }}"
	existing := model.ManagedAgent{
		AgentID:             "agent-custom",
		Name:                defaultReportAgentName,
		Description:         defaultReportAgentMarker + "\n" + defaultReportAgentTypesPrefix + strings.Join(supportedReportTypes, ",") + "\n" + defaultManagedAgentMarker,
		Engine:              "codex",
		Instructions:        customInstructions,
		StartPromptTemplate: customStartPrompt,
	}
	h := NewManagedAgentHandlerWithDefaults(nil, nil, testManagedAgentDefaults())
	repairReq, changed := h.repairedDefaultReportAgentRequest(existing, model.ManagedSkillRef{Owner: "100866", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion})
	if !changed || repairReq.DefaultModelID != "MiniMax-M2.5" || !h.hasReportMCPServer(repairReq.MCPServers) || !hasCredentialSlot(repairReq.CredentialSlots, reportMCPCredentialSlot) {
		t.Fatalf("repair request = %#v changed=%v", repairReq, changed)
	}
	if repairReq.Engine != "codex" {
		t.Fatalf("custom engine should not be overwritten, got %q", repairReq.Engine)
	}
	if repairReq.Instructions != customInstructions {
		t.Fatalf("custom instructions overwritten: %q", repairReq.Instructions)
	}
	if repairReq.StartPromptTemplate != customStartPrompt {
		t.Fatalf("custom start prompt overwritten: %q", repairReq.StartPromptTemplate)
	}
}

func TestReportAgentRepairRequestRefreshesManagedStartPromptTemplate(t *testing.T) {
	defaults := testManagedAgentDefaults()
	h := NewManagedAgentHandlerWithDefaults(nil, nil, defaults)
	oldTemplate := strings.Replace(defaultReportAgentStartPromptTemplate(reportMCPCredentialSlot), "calendar_context={{ calendar_context_json }}\n", "", 1)
	existing := model.ManagedAgent{
		AgentID:             "agent-default",
		Name:                defaultReportAgentName,
		Description:         defaultReportAgentDescription,
		Engine:              "claude-code",
		DefaultModelID:      defaults.ModelID,
		Instructions:        defaultReportAgentInstructions(reportMCPCredentialSlot),
		StartPromptTemplate: oldTemplate,
		CredentialSlots:     []model.ManagedCredentialSlot{{Name: reportMCPCredentialSlot, Required: true}},
		Skills:              []model.ManagedSkillRef{{Owner: "100866", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion}},
		MCPServers:          []model.ManagedMCPServer{h.defaultReportMCPServer()},
	}

	repairReq, changed := h.repairedDefaultReportAgentRequest(existing, model.ManagedSkillRef{Owner: "100866", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion})
	if !changed {
		t.Fatal("managed default start prompt should be refreshed")
	}
	if repairReq.StartPromptTemplate != h.reportAgentStartPromptTemplate() {
		t.Fatalf("start prompt = %q", repairReq.StartPromptTemplate)
	}
	if !strings.Contains(repairReq.StartPromptTemplate, "calendar_context={{ calendar_context_json }}") {
		t.Fatalf("calendar context missing from refreshed start prompt: %q", repairReq.StartPromptTemplate)
	}
}

func TestResolveAndRepairDefaultReportAgentRefreshesManagedPromptBeforeRun(t *testing.T) {
	defaults := testManagedAgentDefaults()
	var updated model.UpsertManagedAgentRequest
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/skill/list":
			writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{Skills: []model.ManagedSkill{{
				SkillID: "skill-report",
				Owner:   "100866",
				Slug:    service.ReportSkillSlug,
				Version: service.ReportSkillVersion,
			}}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/my/agents/agent-default":
			if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, http.StatusOK, model.ManagedAgent{AgentID: "agent-default"})
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer platform.Close()

	h := NewManagedAgentHandlerWithDefaults(nil, service.NewManagedAgentClient(platform.URL, "platform-token"), defaults)
	agent := model.ManagedAgent{
		AgentID:             "agent-default",
		Name:                defaultReportAgentName,
		Description:         defaultReportAgentDescription,
		Engine:              "claude-code",
		DefaultModelID:      defaults.ModelID,
		Instructions:        defaultReportAgentInstructions(reportMCPCredentialSlot),
		StartPromptTemplate: strings.Replace(defaultReportAgentStartPromptTemplate(reportMCPCredentialSlot), "calendar_context={{ calendar_context_json }}\n", "", 1),
		CredentialSlots:     []model.ManagedCredentialSlot{{Name: reportMCPCredentialSlot, Required: true}},
		Skills:              []model.ManagedSkillRef{{Owner: "100866", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion}},
		MCPServers:          []model.ManagedMCPServer{h.defaultReportMCPServer()},
	}

	if err := h.resolveAndRepairReportAgent(context.Background(), h.client, &agent, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated.StartPromptTemplate, "calendar_context={{ calendar_context_json }}") {
		t.Fatalf("updated start prompt = %q", updated.StartPromptTemplate)
	}
	if agent.StartPromptTemplate != h.reportAgentStartPromptTemplate() {
		t.Fatalf("in-memory start prompt was not refreshed: %q", agent.StartPromptTemplate)
	}
}

func TestSelectReportAgentPrefersProfileBusinessType(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	agents := []model.ManagedAgent{
		{
			AgentID:   "agent-generic",
			Name:      "普通 Agent",
			Engine:    "codex",
			CreatedAt: 2,
		},
		{
			AgentID:   "agent-report",
			Name:      "自定义报告 Agent",
			Engine:    "codex",
			CreatedAt: 1,
		},
	}
	mock.ExpectQuery("SELECT agent_id, business_type, report_types").
		WithArgs("307", "agent-generic", "agent-report").
		WillReturnRows(sqlmock.NewRows([]string{"agent_id", "business_type", "report_types", "is_default_report"}).
			AddRow("agent-report", managedAgentBusinessReport, []byte(`["personal_daily","personal_weekly"]`), false))

	h := NewManagedAgentHandlerWithDefaults(db, nil, testManagedAgentDefaults())
	selected, found, err := h.selectReportAgentForUser(context.Background(), "307", agents)
	if err != nil {
		t.Fatal(err)
	}
	if !found || selected.AgentID != "agent-report" || selected.BusinessType != managedAgentBusinessReport {
		t.Fatalf("selected=%#v found=%v", selected, found)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestStartReportAgentRunUsesSessionCredentialOverrides(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	reportAgent := model.ManagedAgent{
		AgentID:             "agent-report",
		Name:                defaultReportAgentName,
		Description:         defaultReportAgentMarker + "\n" + defaultReportAgentTypesPrefix + strings.Join(supportedReportTypes, ",") + "\n" + defaultManagedAgentMarker,
		Engine:              "claude-code",
		DefaultModelID:      "MiniMax-M2.5",
		Instructions:        defaultReportAgentInstructions(reportMCPCredentialSlot),
		StartPromptTemplate: defaultReportAgentStartPromptTemplate(reportMCPCredentialSlot),
		CredentialSlots:     []model.ManagedCredentialSlot{{Name: "mcp-custom", Required: true}},
		Skills:              []model.ManagedSkillRef{{Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion}},
		MCPBindings: []model.ManagedMCPBinding{
			{Slug: "aida-report-mcp", Version: "report-v1"},
			{Owner: "alice", Slug: "custom-mcp", Version: "1.0.0", CredentialSlot: "mcp-custom"},
		},
	}
	var createdCredential service.CreateManagedCredentialRequest
	var createdSession service.CreateManagedSessionRequest
	var updatedAgent model.UpsertManagedAgentRequest
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/my/agents":
			writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{Agents: []model.ManagedAgent{reportAgent}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/skill/list":
			if r.URL.Query().Get("scope") != "public" {
				t.Fatalf("unexpected skill scope %q", r.URL.Query().Get("scope"))
			}
			writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{Skills: []model.ManagedSkill{{
				SkillID: "skill-1",
				Owner:   "100866",
				Slug:    service.ReportSkillSlug,
				Version: service.ReportSkillVersion,
				Name:    service.ReportSkillName,
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/mcp/list":
			writeJSON(w, http.StatusOK, model.ListManagedMCPEntriesResponse{Entries: []model.ManagedMCPEntry{{
				EntryID: "mcp-1",
				Slug:    "aida-report-mcp",
				Version: "report-v1",
				URL:     "https://aida.example.com/api/v1/mcp/reports",
			}}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/my/agents/agent-report":
			if err := json.NewDecoder(r.Body).Decode(&updatedAgent); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, http.StatusOK, model.UpsertManagedAgentResponse{AgentID: "agent-report", ManagedVersion: 2})
		case r.Method == http.MethodPost && r.URL.Path == "/api/credential":
			if err := json.NewDecoder(r.Body).Decode(&createdCredential); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, http.StatusOK, service.CreateManagedCredentialResponse{CredentialID: "cred-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			if err := json.NewDecoder(r.Body).Decode(&createdSession); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, http.StatusOK, service.CreateManagedSessionResponse{
				SessionID: "session-1",
				Status:    "running",
				ModelID:   "MiniMax-M2.5",
			})
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer platform.Close()

	now := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	safeInputRef := jsonStringArg{
		require: []string{"personal_daily", "2026-06-30", "aida-report-mcp", reportMCPCredentialSlot, "credential_override_slots", "mcp-custom"},
		forbid:  []string{"user-token", "cred-1", "cred-custom", "mcp_" + "authorization"},
	}
	mock.ExpectQuery("SELECT agent_id, business_type, report_types").
		WithArgs("user-1", "agent-report").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("INSERT INTO ai_runs").
		WithArgs("user-1", reportAgentRunBusinessType, "agent-report", "MiniMax-M2.5", safeInputRef).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run-report"))
	mock.ExpectExec("UPDATE ai_runs").
		WithArgs("session-1", "MiniMax-M2.5", "running", safeInputRef, "run-report", "user-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id::text").
		WithArgs("run-report", "user-1").
		WillReturnRows(sqlmock.NewRows(aiRunColumns()).
			AddRow("run-report", "user-1", reportAgentRunBusinessType, nil, "managed_session", "agent-report", nil, nil, "session-1", "MiniMax-M2.5", "running", []byte(`{"trigger_source":"manual","report_type":"personal_daily","period":{"date":"2026-06-30"},"target":{"type":"self","user_id":"user-1"},"model_id":"MiniMax-M2.5","mcp_url":"https://aida.example.com/api/v1/mcp/reports","credential_slot":"AIDA_REPORT_MCP_AUTH","start_prompt_values":{"report_type":"personal_daily","run_id":"run-report"},"credential_override":"redacted"}`), []byte(`{}`), nil, now, nil, now))

	h := NewManagedAgentHandlerWithDefaults(db, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	h.reportContext = reportContextBuilderStub{}
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/report-agents/agent-report/runs", bytes.NewBufferString(`{"report_type":"personal_daily","period":{"date":"2026-06-30"},"target":{"type":"self"},"model_id":"MiniMax-M2.5","credential_overrides":{"mcp-custom":"cred-custom"}}`))
	req.Host = "aida.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Authorization", "Bearer user-token")
	req = requestWithUser(req, &model.User{ID: "user-1", Name: "张三", Role: "employee"})
	req = requestWithURLParam(req, "agentId", "agent-report")
	rec := httptest.NewRecorder()

	h.StartReportAgentRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if createdCredential.Value != "user-token" {
		t.Fatalf("credential value should be current user token without Bearer prefix, got %q", createdCredential.Value)
	}
	if createdSession.AgentID != "agent-report" || createdSession.ModelID != "MiniMax-M2.5" {
		t.Fatalf("session request = %#v", createdSession)
	}
	if createdSession.CredentialOverrides[reportMCPCredentialSlot] != "cred-1" {
		t.Fatalf("credential overrides = %#v", createdSession.CredentialOverrides)
	}
	if createdSession.CredentialOverrides["mcp-custom"] != "cred-custom" {
		t.Fatalf("custom credential overrides = %#v", createdSession.CredentialOverrides)
	}
	if !hasCredentialSlot(updatedAgent.CredentialSlots, reportMCPCredentialSlot) || len(updatedAgent.MCPServers) != 1 || updatedAgent.MCPServers[0].CredentialSlot != reportMCPCredentialSlot {
		t.Fatalf("report dependency repair = %#v", updatedAgent)
	}
	if len(updatedAgent.MCPBindings) != 1 || updatedAgent.MCPBindings[0].Slug != "custom-mcp" {
		t.Fatalf("custom mcp binding should be preserved, got %#v", updatedAgent.MCPBindings)
	}
	if _, ok := createdSession.StartPromptValues["mcp_"+"authorization"]; ok {
		t.Fatalf("start prompt values should not contain authorization field: %#v", createdSession.StartPromptValues)
	}
	if createdSession.StartPromptValues["run_id"] != "run-report" {
		t.Fatalf("start prompt values = %#v", createdSession.StartPromptValues)
	}
	if _, ok := createdSession.StartPromptValues["mcp_url"]; ok {
		t.Fatalf("start prompt values should not expose mcp_url: %#v", createdSession.StartPromptValues)
	}
	requireContainsAll(t, createdSession.Message,
		"report_type=personal_daily",
		"run_id=run-report",
		"period=",
		"target=",
		reportMCPCredentialSlot,
	)
	if strings.Contains(createdSession.Message, "mcp_url=") {
		t.Fatalf("session message should not expose mcp_url: %q", createdSession.Message)
	}
	if strings.Contains(createdSession.Message, "user-token") || strings.Contains(createdSession.Message, "cred-1") {
		t.Fatalf("session message leaked credential material: %q", createdSession.Message)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestStartReportAgentRunFallsBackToMessageWhenTemplateMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	reportAgent := model.ManagedAgent{
		AgentID:        "agent-report",
		Name:           "自定义报告 Agent",
		Description:    "汇报用的",
		Engine:         "claude-code",
		DefaultModelID: "MiniMax-M2.5",
		CredentialSlots: []model.ManagedCredentialSlot{{
			Name:     reportMCPCredentialSlot,
			Required: true,
		}},
		Skills:     []model.ManagedSkillRef{{Owner: "100866", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion}},
		MCPServers: []model.ManagedMCPServer{{Name: service.ReportMCPSlug, URL: "https://aida.example.com/api/v1/mcp/reports", CredentialSlot: reportMCPCredentialSlot, AuthHeader: "Authorization", AuthScheme: "Bearer"}},
	}
	var createdSession service.CreateManagedSessionRequest
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/my/agents":
			writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{Agents: []model.ManagedAgent{reportAgent}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/skill/list":
			if r.URL.Query().Get("scope") != "public" {
				t.Fatalf("unexpected skill scope %q", r.URL.Query().Get("scope"))
			}
			writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{Skills: []model.ManagedSkill{{
				SkillID: "skill-1",
				Owner:   "100866",
				Slug:    service.ReportSkillSlug,
				Version: service.ReportSkillVersion,
				Name:    service.ReportSkillName,
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/mcp/list":
			writeJSON(w, http.StatusOK, model.ListManagedMCPEntriesResponse{Entries: []model.ManagedMCPEntry{{
				EntryID:            "mcp-1",
				Slug:               "aida-report-mcp",
				Version:            "report-v1",
				URL:                "https://aida.example.com/api/v1/mcp/reports",
				RequiresCredential: true,
				CredentialEnv:      reportMCPCredentialSlot,
				AuthHeader:         "Authorization",
				AuthScheme:         "Bearer",
			}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/credential":
			writeJSON(w, http.StatusOK, service.CreateManagedCredentialResponse{CredentialID: "cred-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			if err := json.NewDecoder(r.Body).Decode(&createdSession); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, http.StatusOK, service.CreateManagedSessionResponse{
				SessionID: "session-1",
				Status:    "running",
				ModelID:   "MiniMax-M2.5",
			})
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer platform.Close()

	now := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	safeInputRef := jsonStringArg{
		require: []string{"personal_daily", "2026-07-01"},
		forbid:  []string{"cred-1"},
	}
	mock.ExpectQuery("SELECT agent_id, business_type, report_types").
		WithArgs("305", "agent-report").
		WillReturnRows(sqlmock.NewRows([]string{"agent_id", "business_type", "report_types", "is_default_report"}).
			AddRow("agent-report", managedAgentBusinessReport, []byte(`["personal_daily"]`), false))
	mock.ExpectQuery("INSERT INTO ai_runs").
		WithArgs("305", reportAgentRunBusinessType, "agent-report", "MiniMax-M2.5", safeInputRef).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run-report"))
	mock.ExpectExec("UPDATE ai_runs SET external_session_id").
		WithArgs("session-1", "MiniMax-M2.5", "running", jsonStringArg{
			require: []string{"personal_daily", "2026-07-01", "请生成 Aida 报告。", "report_type=personal_daily", "run_id=run-report", "period=", "target="},
			forbid:  []string{"mcp_url=https://aida.example.com/api/v1/mcp/reports", "cred-1"},
		}, "run-report", "305").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT id::text").
		WithArgs("run-report", "305").
		WillReturnRows(sqlmock.NewRows(aiRunColumns()).
			AddRow("run-report", "305", reportAgentRunBusinessType, nil, "managed_session", "agent-report", nil, nil, "session-1", "MiniMax-M2.5", "running", []byte(`{"trigger_source":"manual","report_type":"personal_daily","period":{"date":"2026-07-01"},"target":{"type":"self","user_id":"305"},"model_id":"MiniMax-M2.5","mcp_url":"https://aida.example.com/api/v1/mcp/reports","credential_slot":"AIDA_REPORT_MCP_AUTH","start_prompt_values":{"report_type":"personal_daily","report_date":"2026-07-01"},"message":"请生成 Aida 报告。\nreport_type=personal_daily\ndate=2026-07-01\ntarget_type=self\ntarget_user_id=305","credential_override":"redacted"}`), []byte(`{}`), nil, now, nil, now))

	h := NewManagedAgentHandlerWithDefaults(db, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	h.reportContext = reportContextBuilderStub{}
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/report-agents/agent-report/runs", bytes.NewBufferString(`{"report_type":"personal_daily","period":{"date":"2026-07-01"},"target":{"type":"self"},"model_id":"MiniMax-M2.5"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer user-token")
	req = requestWithURLParam(req, "agentId", "agent-report")
	req = requestWithUser(req, &model.User{ID: "305", Username: "t03"})
	rec := httptest.NewRecorder()

	h.StartReportAgentRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(createdSession.Message) == "" {
		t.Fatalf("expected fallback message, got session=%#v", createdSession)
	}
	if !strings.Contains(createdSession.Message, "report_type=personal_daily") {
		t.Fatalf("unexpected fallback message: %q", createdSession.Message)
	}
	requireContainsAll(t, createdSession.Message,
		"run_id=run-report",
		"period=",
		"target=",
		reportMCPCredentialSlot,
	)
	if strings.Contains(createdSession.Message, "mcp_url=") {
		t.Fatalf("fallback message should not expose mcp_url: %q", createdSession.Message)
	}
	if createdSession.StartPromptValues["report_type"] != "personal_daily" {
		t.Fatalf("start prompt values = %#v", createdSession.StartPromptValues)
	}
	if !strings.Contains(createdSession.StartPromptValues["calendar_context_json"], `"weekday":"周三"`) {
		t.Fatalf("calendar context missing from start prompt values = %#v", createdSession.StartPromptValues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestExecuteManagedAgentScheduleRunUsesUserScopedClient(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	reportAgent := model.ManagedAgent{
		AgentID:             "agent-report",
		Name:                defaultReportAgentName,
		Engine:              "claude-code",
		Instructions:        defaultReportAgentInstructions(reportMCPCredentialSlot),
		StartPromptTemplate: defaultReportAgentStartPromptTemplate(reportMCPCredentialSlot),
		DefaultModelID:      "MiniMax-M2.5",
		BusinessType:        managedAgentBusinessReport,
		CredentialSlots:     []model.ManagedCredentialSlot{{Name: reportMCPCredentialSlot, Required: true}},
		Skills:              []model.ManagedSkillRef{{Owner: "100866", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion}},
		MCPServers:          []model.ManagedMCPServer{{Name: service.ReportMCPSlug, URL: "https://aida.example.com/api/v1/mcp/reports", CredentialSlot: reportMCPCredentialSlot, AuthHeader: "Authorization", AuthScheme: "Bearer"}},
	}
	schedule := model.ManagedAgentSchedule{
		ID:                "schedule-1",
		UserID:            "305",
		Name:              "daily report",
		AgentID:           "agent-report",
		RunKind:           scheduleRunKindReport,
		ModelID:           strPtr("MiniMax-M2.5"),
		ReportConfig:      map[string]string{"report_type": "personal_daily"},
		ScheduleType:      "daily",
		TimeOfDay:         "19:00",
		Timezone:          "Asia/Shanghai",
		StartPromptValues: map[string]string{},
	}

	var auths []string
	var createdSession service.CreateManagedSessionRequest
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/my/agents":
			writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{Agents: []model.ManagedAgent{reportAgent}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/skill/list":
			writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{Skills: []model.ManagedSkill{{
				SkillID: "system-skill", Owner: "100866", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion,
			}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/credential":
			writeJSON(w, http.StatusOK, service.CreateManagedCredentialResponse{CredentialID: "cred-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			if err := json.NewDecoder(r.Body).Decode(&createdSession); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, http.StatusOK, service.CreateManagedSessionResponse{
				SessionID: "session-1",
				Status:    "running",
				ModelID:   "MiniMax-M2.5",
			})
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer platform.Close()

	now := time.Date(2026, 7, 1, 10, 54, 7, 0, time.UTC)
	mock.ExpectQuery("SELECT agent_id, business_type, report_types").
		WithArgs("305", "agent-report").
		WillReturnRows(sqlmock.NewRows([]string{"agent_id", "business_type", "report_types", "is_default_report"}).
			AddRow("agent-report", managedAgentBusinessReport, []byte(`["personal_daily"]`), false))
	mock.ExpectQuery("INSERT INTO ai_runs").
		WithArgs("305", reportAgentRunBusinessType, "agent-report", "MiniMax-M2.5", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run-report"))
	mock.ExpectExec("UPDATE ai_runs SET external_session_id").
		WithArgs("session-1", "MiniMax-M2.5", "running", sqlmock.AnyArg(), "run-report", "305").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE managed_agent_schedules").
		WithArgs(now, "run-report", "", "schedule-1", "305").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT id::text").
		WithArgs("run-report", "305").
		WillReturnRows(sqlmock.NewRows(aiRunColumns()).
			AddRow("run-report", "305", reportAgentRunBusinessType, nil, "managed_session", "agent-report", nil, nil, "session-1", "MiniMax-M2.5", "running", []byte(`{"trigger_source":"save_and_run"}`), []byte(`{}`), nil, now, nil, now))

	h := NewManagedAgentHandlerWithDefaults(db, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	h.reportContext = reportContextBuilderStub{}
	run, err := h.executeManagedAgentScheduleRun(context.Background(), schedule, &model.User{ID: "305", Username: "t03"}, "user-token", "save_and_run", now, false)
	if err != nil {
		t.Fatalf("executeManagedAgentScheduleRun error = %v", err)
	}
	if run == nil || run.ID != "run-report" {
		t.Fatalf("run = %#v", run)
	}
	if len(auths) != 4 {
		t.Fatalf("auths = %#v", auths)
	}
	for _, auth := range auths {
		if auth != "Bearer user-token" {
			t.Fatalf("expected user-scoped auth header, got %#v", auths)
		}
	}
	if createdSession.AgentID != "agent-report" {
		t.Fatalf("createdSession = %#v", createdSession)
	}
	if strings.TrimSpace(createdSession.Message) == "" {
		t.Fatalf("expected fallback message, got session=%#v", createdSession)
	}
	if !strings.Contains(createdSession.Message, "report_type=personal_daily") {
		t.Fatalf("unexpected fallback message: %q", createdSession.Message)
	}
	requireContainsAll(t, createdSession.Message,
		"run_id=run-report",
		"period=",
		"target=",
		reportMCPCredentialSlot,
	)
	if strings.Contains(createdSession.Message, "mcp_url=") {
		t.Fatalf("fallback message should not expose mcp_url: %q", createdSession.Message)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestEnsureScheduleAgentRunnableUsesLocalReportProfile(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/my/agents":
			writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{Agents: []model.ManagedAgent{{
				AgentID:         "agent-report",
				Name:            "report",
				BusinessType:    managedAgentBusinessGeneric,
				CredentialSlots: []model.ManagedCredentialSlot{{Name: reportMCPCredentialSlot, Required: true}},
				Skills:          []model.ManagedSkillRef{{Owner: "100866", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion}},
				MCPServers:      []model.ManagedMCPServer{{Name: service.ReportMCPSlug, URL: "https://aida.example.com/api/v1/mcp/reports", CredentialSlot: reportMCPCredentialSlot, AuthHeader: "Authorization", AuthScheme: "Bearer"}},
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/skill/list":
			writeJSON(w, http.StatusOK, model.ListManagedSkillsResponse{Skills: []model.ManagedSkill{{
				SkillID: "system-skill", Owner: "100866", Slug: service.ReportSkillSlug, Version: service.ReportSkillVersion,
			}}})
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer platform.Close()

	mock.ExpectQuery("SELECT agent_id, business_type, report_types").
		WithArgs("305", "agent-report").
		WillReturnRows(sqlmock.NewRows([]string{"agent_id", "business_type", "report_types", "is_default_report"}).
			AddRow("agent-report", managedAgentBusinessReport, []byte(`["team_daily"]`), false))

	h := NewManagedAgentHandlerWithDefaults(db, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	if err := h.ensureScheduleAgentRunnable(context.Background(), h.client, "305", "agent-report", scheduleRunKindReport); err != nil {
		t.Fatalf("ensureScheduleAgentRunnable error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestStartReportAgentRunDefaultDoesNotCreateAssets(t *testing.T) {
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/my/agents":
			writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{})
		case r.Method == http.MethodPost:
			t.Fatalf("default report run must not create assets, got %s", r.URL.Path)
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer platform.Close()

	h := NewManagedAgentHandlerWithDefaults(nil, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/report-agents/default/runs", bytes.NewBufferString(`{"report_type":"personal_daily","period":{"date":"2026-06-30"},"target":{"type":"self"}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	req = requestWithUser(req, &model.User{ID: "user-1", Username: "t05", Role: "employee"})
	req = requestWithURLParam(req, "agentId", "default")
	rec := httptest.NewRecorder()

	h.StartReportAgentRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Available bool   `json:"available"`
		Code      string `json:"code"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Available || got.Code != "DEFAULT_REPORT_AGENT_NOT_CONFIGURED" || got.Message == "" {
		t.Fatalf("response = %#v", got)
	}
}

func TestDailyReportIntegrationReturnsMCPAndSkill(t *testing.T) {
	h := NewManagedAgentHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai-assets/daily-report-integration", nil)
	req.Host = "aida.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()

	h.DailyReportIntegration(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		MCP struct {
			URL   string   `json:"url"`
			Tools []string `json:"tools"`
		} `json:"mcp"`
		Skill struct {
			Slug    string `json:"slug"`
			Version string `json:"version"`
			SkillMD string `json:"skill_md"`
		} `json:"skill"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.MCP.URL != "https://aida.example.com/api/v1/mcp/reports" {
		t.Fatalf("mcp url = %q", got.MCP.URL)
	}
	if got.Skill.Slug != service.DailyReportSkillSlug || got.Skill.Version != service.DailyReportSkillVersion {
		t.Fatalf("skill ref = %s@%s", got.Skill.Slug, got.Skill.Version)
	}
	if strings.Contains(got.Skill.SkillMD, got.MCP.URL) {
		t.Fatalf("skill markdown should not include mcp url")
	}
	if len(got.MCP.Tools) != 9 {
		t.Fatalf("tools = %#v", got.MCP.Tools)
	}
}

func TestReportPromptDoesNotExposeResolvedTargetIDs(t *testing.T) {
	target := reportTarget{Type: "department", DepartmentID: "11111111-1111-4111-8111-111111111111"}
	values := reportAgentStartPromptValues(
		"run-1", reportTypeDepartmentDaily, "2026-07-15", "", "", target, nil, "selection-1", "",
	)
	if values["target_json"] != `{"type":"department"}` {
		t.Fatalf("target_json=%q", values["target_json"])
	}
	message := fallbackReportRunMessage(reportTypeDepartmentDaily, "2026-07-15", "", "", target)
	if strings.Contains(message, target.DepartmentID) || strings.Contains(message, "target_department_id") {
		t.Fatalf("fallback message leaked target id: %s", message)
	}
}

func TestStartReportRunSubmitsUrlsStartPromptValues(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var submitted service.SubmitManagedTaskRequest
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/task/submit" {
			t.Fatalf("platform path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
			t.Fatal(err)
		}
		writeJSON(w, http.StatusOK, service.SubmitManagedTaskResponse{
			TaskID: "task-urls",
			Status: "queued",
		})
	}))
	defer platform.Close()

	now := time.Date(2026, 6, 29, 10, 30, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT s.id::text").
		WithArgs("user-1", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(draftSessionColumns()).
			AddRow("session-1", "ref-1", "claude_code", now, now.Add(20*time.Minute), 1200, "sonnet", "完成日报", "{}", nil, "", nil, "", 100, 200, 300))
	mock.ExpectQuery("INSERT INTO ai_runs").
		WithArgs("user-1", "personal_daily", "legacy-source-agent", "task-urls", "MiniMax-M2.5", "running", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run-1"))
	mock.ExpectQuery("SELECT id::text").
		WithArgs("run-1", "user-1").
		WillReturnRows(sqlmock.NewRows(aiRunColumns()).
			AddRow("run-1", "user-1", "personal_daily", nil, "managed_task", "legacy-source-agent", nil, "task-urls", nil, "MiniMax-M2.5", "running", []byte(`{"report_date":"2026-06-29","session_ids":["session-1"],"urls":["https://aida.example.com/api/v1/sessions/session-1/log"]}`), []byte(`{}`), nil, now, nil, now))

	h := NewManagedAgentHandlerWithDefaults(db, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	req := httptest.NewRequest(http.MethodPost, "/reports/today/managed-agent-runs", bytes.NewBufferString(`{"report_date":"2026-06-29","session_ids":["session-1"],"agent_id":"legacy-source-agent"}`))
	req.Host = "aida.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	req = requestWithUser(req, &model.User{ID: "user-1", Name: "张三", Role: "employee"})
	rec := httptest.NewRecorder()

	h.StartReportRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if submitted.AgentID != "legacy-source-agent" {
		t.Fatalf("agent_id = %q", submitted.AgentID)
	}
	if submitted.ModelID != "MiniMax-M2.5" {
		t.Fatalf("model_id = %q", submitted.ModelID)
	}
	if submitted.Params["urls"] != `["https://aida.example.com/api/v1/sessions/session-1/log"]` {
		t.Fatalf("urls = %q", submitted.Params["urls"])
	}
	if _, ok := submitted.Params["message"]; ok {
		t.Fatalf("message param should not be submitted: %#v", submitted.Params)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestGetDailyReportRunReturnsLocalRunWhenRefreshFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "task result not ready", http.StatusBadGateway)
	}))
	defer platform.Close()

	now := time.Date(2026, 6, 30, 12, 25, 28, 0, time.UTC)
	mock.ExpectQuery("SELECT id::text").
		WithArgs("run-1", "user-1").
		WillReturnRows(sqlmock.NewRows(aiRunColumns()).
			AddRow("run-1", "user-1", "manual_agent_run", nil, "managed_task", "agent-1", nil, "task-1", nil, "MiniMax-M2.5", "pending", []byte(`{"report_type":"personal_daily"}`), []byte(`{}`), nil, now, nil, now))

	h := NewManagedAgentHandlerWithDefaults(db, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	req := httptest.NewRequest(http.MethodGet, "/reports/managed-agent-runs/run-1", nil)
	req = requestWithUser(req, &model.User{ID: "user-1", Name: "张三", Role: "employee"})
	req = requestWithURLParam(req, "runId", "run-1")
	rec := httptest.NewRecorder()

	h.GetDailyReportRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.AIRun
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "run-1" || got.Status != "pending" {
		t.Fatalf("unexpected run: %#v, body=%s", got, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateAgentScheduleValidatesAndReturnsSchedule(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT agent_id, business_type, report_types").
		WithArgs("user-1", "agent-1").
		WillReturnRows(sqlmock.NewRows([]string{"agent_id", "business_type", "report_types", "is_default_report"}))
	mock.ExpectQuery("INSERT INTO managed_agent_schedules").
		WithArgs("user-1", "日报定时", "agent-1", "generic_agent", "Kimi-K2.6", "生成日报", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "weekly", sqlmock.AnyArg(), "19:00", "Asia/Shanghai", true, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("schedule-1"))
	mock.ExpectQuery("SELECT s.id::text").
		WithArgs("schedule-1", "user-1").
		WillReturnRows(sqlmock.NewRows(agentScheduleColumns()).
			AddRow("schedule-1", "user-1", "日报定时", "agent-1", "generic_agent", "Kimi-K2.6", "生成日报", []byte(`{"report_date":"today"}`), []byte(`{"report_date":"today"}`), []byte(`{}`), "weekly", []byte(`[1,2,3,4,5]`), "19:00", "Asia/Shanghai", true, now, nil, nil, nil, nil, nil, nil, nil, now, now))

	h := NewManagedAgentHandler(db, nil)
	body := `{"name":"日报定时","agent_id":"agent-1","model_id":"Kimi-K2.6","message":"生成日报","params":{"report_date":"today"},"schedule_type":"weekly","weekdays":[1,2,3,4,5],"time_of_day":"19:00","timezone":"Asia/Shanghai","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/agent-schedules", bytes.NewBufferString(body))
	req = requestWithUser(req, &model.User{ID: "user-1", Name: "张三", Role: "employee"})
	rec := httptest.NewRecorder()

	h.CreateAgentSchedule(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.ManagedAgentSchedule
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "schedule-1" || got.ScheduleType != "weekly" || len(got.Weekdays) != 5 || got.ModelID == nil || *got.ModelID != "Kimi-K2.6" {
		t.Fatalf("schedule = %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateReportAgentScheduleUsesLocalProfileWhenPlatformTypeIsGeneric(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/my/agents" {
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, model.ListManagedAgentsResponse{Agents: []model.ManagedAgent{{
			AgentID:      "agent-report",
			Name:         "报告生成 Agent",
			BusinessType: "",
		}}})
	}))
	defer platform.Close()

	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT agent_id, business_type, report_types").
		WithArgs("305", "agent-report").
		WillReturnRows(sqlmock.NewRows([]string{"agent_id", "business_type", "report_types", "is_default_report"}).
			AddRow("agent-report", managedAgentBusinessReport, []byte(`["team_daily"]`), false))
	mock.ExpectQuery("INSERT INTO managed_agent_schedules").
		WithArgs("305", "团队日报定时", "agent-report", scheduleRunKindReport, "MiniMax-M2.5", "", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "daily", sqlmock.AnyArg(), "19:00", "Asia/Shanghai", false, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("schedule-report"))
	mock.ExpectQuery("SELECT s.id::text").
		WithArgs("schedule-report", "305").
		WillReturnRows(sqlmock.NewRows(agentScheduleColumns()).
			AddRow("schedule-report", "305", "团队日报定时", "agent-report", scheduleRunKindReport, "MiniMax-M2.5", "", []byte(`{}`), []byte(`{}`), []byte(`{"report_type":"team_daily"}`), "daily", []byte(`[]`), "19:00", "Asia/Shanghai", false, nil, nil, nil, nil, nil, nil, nil, nil, now, now))

	h := NewManagedAgentHandlerWithDefaults(db, service.NewManagedAgentClient(platform.URL, "platform-token"), testManagedAgentDefaults())
	body := `{"name":"团队日报定时","agent_id":"agent-report","run_kind":"report_agent","model_id":"MiniMax-M2.5","report_config":{"report_type":"team_daily"},"schedule_type":"daily","time_of_day":"19:00","timezone":"Asia/Shanghai","enabled":false}`
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/agent-schedules", bytes.NewBufferString(body))
	req = requestWithUser(req, &model.User{ID: "305", Name: "测试03", Role: "team_leader", TeamID: strPtr("team-1")})
	rec := httptest.NewRecorder()

	h.CreateAgentSchedule(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.ManagedAgentSchedule
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "schedule-report" || got.RunKind != scheduleRunKindReport || got.ReportConfig["report_type"] != "team_daily" {
		t.Fatalf("schedule = %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateReportAgentScheduleRejectsEmployeeTeamReportSelfTarget(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT agent_id, business_type, report_types").
		WithArgs("307", "agent-report").
		WillReturnRows(sqlmock.NewRows([]string{"agent_id", "business_type", "report_types", "is_default_report"}).
			AddRow("agent-report", managedAgentBusinessReport, []byte(`["team_daily"]`), false))

	h := NewManagedAgentHandlerWithDefaults(db, nil, testManagedAgentDefaults())
	body := `{"name":"团队日报定时","agent_id":"agent-report","run_kind":"report_agent","model_id":"MiniMax-M2.5","report_config":{"report_type":"team_daily"},"schedule_type":"daily","time_of_day":"19:00","timezone":"Asia/Shanghai","enabled":false}`
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/agent-schedules", bytes.NewBufferString(body))
	req = requestWithUser(req, &model.User{ID: "307", Name: "测试05", Role: "employee", TeamID: strPtr("team-1")})
	rec := httptest.NewRecorder()

	h.CreateAgentSchedule(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "无法推导报告对象") {
		t.Fatalf("body=%s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestPreviewAgentScheduleAcceptsReportConfigPayload(t *testing.T) {
	h := NewManagedAgentHandlerWithDefaults(nil, nil, testManagedAgentDefaults())
	body := `{"name":"个人日报定时","agent_id":"agent-report","run_kind":"report_agent","model_id":"MiniMax-M2.5","report_config":{"report_type":"personal_daily"},"schedule_type":"daily","time_of_day":"19:00","timezone":"Asia/Shanghai","enabled":false}`
	req := httptest.NewRequest(http.MethodPost, "/ai-assets/agent-schedules/preview", bytes.NewBufferString(body))
	req = requestWithUser(req, &model.User{ID: "309", Name: "测试07", Role: "employee", TeamID: strPtr("team-1")})
	rec := httptest.NewRecorder()

	h.PreviewAgentSchedule(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got model.PreviewManagedAgentScheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AgentType != managedAgentBusinessReport || got.ReportType != reportTypePersonalDaily || got.PeriodDisplay == "" {
		t.Fatalf("preview = %#v", got)
	}
}

func TestCreateAgentScheduleRejectsInvalidTime(t *testing.T) {
	for _, timeOfDay := range []string{"25:00", "9:00"} {
		t.Run(timeOfDay, func(t *testing.T) {
			h := NewManagedAgentHandler(nil, nil)
			body := fmt.Sprintf(`{"name":"日报定时","agent_id":"agent-1","message":"生成日报","schedule_type":"daily","time_of_day":%q}`, timeOfDay)
			req := httptest.NewRequest(http.MethodPost, "/ai-assets/agent-schedules", bytes.NewBufferString(body))
			req = requestWithUser(req, &model.User{ID: "user-1", Name: "张三", Role: "employee"})
			rec := httptest.NewRecorder()

			h.CreateAgentSchedule(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestNormalizeReportSourceSliceKeysAcceptsUUIDs(t *testing.T) {
	first := "fd46e4ca-9eef-4249-9184-d342d7754a4e"
	second := "8ee3bf8b-42d6-47f5-8e45-feadad44017d"
	got, err := normalizeReportSourceSliceKeys([]string{" " + first + " ", first, "", second})
	if err != nil {
		t.Fatalf("normalizeReportSourceSliceKeys() error = %v", err)
	}
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("normalizeReportSourceSliceKeys() = %#v", got)
	}
}

func TestNormalizeReportSourceSliceKeysRejectsLegacyDateKeys(t *testing.T) {
	if _, err := normalizeReportSourceSliceKeys([]string{"session-1:2026-07-15"}); err == nil {
		t.Fatal("normalizeReportSourceSliceKeys() accepted a legacy date slice key")
	}
}

func aiRunColumns() []string {
	return []string{
		"id", "user_id", "business_type", "business_id", "runtime_type",
		"agent_id", "agent_version_id", "external_task_id", "external_session_id", "model_id",
		"status", "input_ref_json", "output_ref_json", "error_message", "started_at", "finished_at", "created_at",
	}
}

func agentScheduleColumns() []string {
	return []string{
		"id", "user_id", "name", "agent_id", "run_kind", "model_id", "message",
		"start_prompt_values_json", "params_json", "report_config_json", "schedule_type",
		"weekdays_json", "time_of_day", "timezone", "enabled", "next_run_at",
		"last_run_at", "last_ai_run_id", "last_run_status", "last_error",
		"last_skip_reason", "last_skip_at", "last_skipped_trigger_at", "created_at", "updated_at",
	}
}
