package service

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

type outputRefWithoutResult struct {
	status        string
	taskID        string
	sessionID     string
	reportID      string
	errText       string
	forbiddenText string
}

func (m outputRefWithoutResult) Match(value driver.Value) bool {
	raw, ok := value.([]byte)
	if !ok {
		text, ok := value.(string)
		if !ok {
			return false
		}
		raw = []byte(text)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	if _, ok := payload["result"]; ok {
		return false
	}
	if m.status != "" && payload["status"] != m.status {
		return false
	}
	if m.taskID != "" && payload["task_id"] != m.taskID {
		return false
	}
	if m.sessionID != "" && payload["session_id"] != m.sessionID {
		return false
	}
	if m.reportID != "" && payload["report_id"] != m.reportID {
		return false
	}
	if m.errText != "" && !strings.Contains(managedStringFromAny(payload["error"]), m.errText) {
		return false
	}
	if m.forbiddenText != "" && strings.Contains(string(raw), m.forbiddenText) {
		return false
	}
	return true
}

const managedRunSyncQueryPattern = `(?s)SELECT id::text, COALESCE\(external_task_id, ''\), COALESCE\(external_session_id, ''\), status,.*business_type.*business_id::text.*output_ref_json.*FROM ai_runs.*external_task_id IS NOT NULL OR external_session_id IS NOT NULL OR business_type <> 'report_agent_run' OR execution_stage IS NULL`

func managedRunStatusRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"external_task_id",
		"external_session_id",
		"status",
		"business_type",
		"business_id",
		"output_ref_json",
		"started_at",
	})
}

func TestManagedAgentRunStatusSyncerRefreshesCompletedRun(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/task/task-123/status" {
			t.Fatalf("platform path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer platform-token" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"task_id":"task-123","status":"completed","agent_version_id":3,"model_id":"Kimi-K2.6","progress":"done","result":"SECRET_RESULT_SHOULD_NOT_BE_STORED"}`))
	}))
	defer platform.Close()

	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	startedAt := now.Add(-10 * time.Minute)
	mock.ExpectQuery(managedRunSyncQueryPattern).
		WithArgs(100).
		WillReturnRows(managedRunStatusRows().
			AddRow("run-1", "task-123", "", "pending", "scheduled_agent_run", "", []byte(`{}`), startedAt))
	mock.ExpectExec("UPDATE ai_runs SET").
		WithArgs("succeeded", outputRefWithoutResult{status: "completed", taskID: "task-123"}, 3, "Kimi-K2.6", now, "run-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	syncer := NewManagedAgentRunStatusSyncer(db, NewManagedAgentClient(platform.URL, "platform-token"))
	if err := syncer.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestManagedAgentRunStatusSyncerTimesOutOldPendingRun(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"task_id":"task-123","status":"pending","progress":"queued"}`))
	}))
	defer platform.Close()

	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	startedAt := now.Add(-3 * time.Hour)
	mock.ExpectQuery(managedRunSyncQueryPattern).
		WithArgs(100).
		WillReturnRows(managedRunStatusRows().
			AddRow("run-1", "task-123", "", "pending", "scheduled_agent_run", "", []byte(`{}`), startedAt))
	mock.ExpectExec("UPDATE ai_runs SET").
		WithArgs("timeout", outputRefWithoutResult{status: "pending", taskID: "task-123", errText: "managed agent run timed out after 2h"}, nil, "managed agent run timed out after 2h", now, "run-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	syncer := NewManagedAgentRunStatusSyncer(db, NewManagedAgentClient(platform.URL, "platform-token"))
	if err := syncer.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestManagedAgentRunStatusSyncerStillTimesOutLegacyPendingRunWithoutExternalID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	startedAt := now.Add(-ManagedAgentPendingTimeout - time.Second)
	mock.ExpectQuery(managedRunSyncQueryPattern).
		WithArgs(100).
		WillReturnRows(managedRunStatusRows().
			AddRow("legacy-pending", "", "", "pending", "manual_agent_run", "", []byte(`{}`), startedAt))
	mock.ExpectExec("UPDATE ai_runs SET").
		WithArgs(
			"timeout",
			outputRefWithoutResult{status: "timeout", errText: "pending submit timed out after 10m"},
			nil,
			"managed agent run pending submit timed out after 10m",
			now,
			"legacy-pending",
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	syncer := NewManagedAgentRunStatusSyncer(db, NewManagedAgentClient("", ""))
	if err := syncer.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestManagedAgentRunStatusSyncerRefreshesSessionRun(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/task/session-123/status" {
			t.Fatalf("platform path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"task_id":"session-123","status":"completed","agent_version_id":5,"model_id":"MiniMax-M2.5","progress":"done"}`))
	}))
	defer platform.Close()

	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	startedAt := now.Add(-10 * time.Minute)
	mock.ExpectQuery(managedRunSyncQueryPattern).
		WithArgs(100).
		WillReturnRows(managedRunStatusRows().
			AddRow("run-1", "", "session-123", "running", "scheduled_agent_run", "", []byte(`{}`), startedAt))
	mock.ExpectExec("UPDATE ai_runs SET").
		WithArgs("succeeded", outputRefWithoutResult{status: "completed", taskID: "session-123", sessionID: "session-123"}, 5, "MiniMax-M2.5", now, "run-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	syncer := NewManagedAgentRunStatusSyncer(db, NewManagedAgentClient(platform.URL, "platform-token"))
	if err := syncer.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestManagedAgentRunStatusSyncerFailsCompletedReportSessionWithoutWriteback(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	finishedAt := now.Add(-ManagedAgentReportWritebackGrace - time.Second).Unix()
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/task/session-report/result" {
			t.Fatal("report run without MCP writeback must not fetch or persist task final text")
		}
		if r.URL.Path != "/api/task/session-report/status" {
			t.Fatalf("platform path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"task_id":"session-report","status":"completed","result":"# 未经 MCP 回写的日报","finished_at":` + managedStringFromAny(finishedAt) + `}`))
	}))
	defer platform.Close()

	startedAt := now.Add(-10 * time.Minute)
	mock.ExpectQuery(managedRunSyncQueryPattern).
		WithArgs(100).
		WillReturnRows(managedRunStatusRows().
			AddRow("run-report", "", "session-report", "running", "report_agent_run", "", []byte(`{}`), startedAt))
	mock.ExpectExec("UPDATE ai_runs SET").
		WithArgs("failed", outputRefWithoutResult{status: "completed", taskID: "session-report", sessionID: "session-report", errText: reportAgentFailureErrorMessage}, nil, reportAgentFailureErrorMessage, now, "REPORT_WRITEBACK_MISSING", "run-report").
		WillReturnResult(sqlmock.NewResult(0, 1))

	syncer := NewManagedAgentRunStatusSyncer(db, NewManagedAgentClient(platform.URL, "platform-token"))
	if err := syncer.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

type recordingManagedReportFallback struct {
	runIDs []string
	err    error
}

func (f *recordingManagedReportFallback) WriteReportFallback(_ context.Context, runID string) error {
	f.runIDs = append(f.runIDs, runID)
	return f.err
}

func TestManagedAgentRunStatusSyncerUsesFallbackForCompletedReportWithoutWriteback(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	finishedAt := now.Add(-ManagedAgentReportWritebackGrace - time.Second).Unix()
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"task_id":"session-report","status":"completed","finished_at":` + managedStringFromAny(finishedAt) + `}`))
	}))
	defer platform.Close()

	startedAt := now.Add(-10 * time.Minute)
	mock.ExpectQuery(managedRunSyncQueryPattern).
		WithArgs(100).
		WillReturnRows(managedRunStatusRows().
			AddRow("run-report", "", "session-report", "running", "report_agent_run", "", []byte(`{}`), startedAt))

	fallback := &recordingManagedReportFallback{}
	syncer := NewManagedAgentRunStatusSyncer(db, NewManagedAgentClient(platform.URL, "platform-token")).
		ConfigureReportFallback(fallback)
	if err := syncer.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if len(fallback.runIDs) != 1 || fallback.runIDs[0] != "run-report" {
		t.Fatalf("fallback run ids = %#v", fallback.runIDs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestManagedAgentRunStatusSyncerKeepsCompletedReportSessionRunningDuringWritebackGrace(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	finishedAt := now.Add(-ManagedAgentReportWritebackGrace + time.Minute).Unix()
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"task_id":"session-report","status":"completed","finished_at":` + managedStringFromAny(finishedAt) + `}`))
	}))
	defer platform.Close()

	startedAt := now.Add(-10 * time.Minute)
	mock.ExpectQuery(managedRunSyncQueryPattern).
		WithArgs(100).
		WillReturnRows(managedRunStatusRows().
			AddRow("run-report", "", "session-report", "running", "report_agent_run", "", []byte(`{}`), startedAt))
	mock.ExpectExec("UPDATE ai_runs SET").
		WithArgs("running", outputRefWithoutResult{status: "completed", taskID: "session-report", sessionID: "session-report"}, nil, "run-report").
		WillReturnResult(sqlmock.NewResult(0, 1))

	syncer := NewManagedAgentRunStatusSyncer(db, NewManagedAgentClient(platform.URL, "platform-token"))
	if err := syncer.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestManagedAgentRunStatusSyncerAllowsCompletedReportSessionWithWriteback(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"task_id":"session-report","status":"completed","agent_version_id":7}`))
	}))
	defer platform.Close()

	startedAt := now.Add(-10 * time.Minute)
	mock.ExpectQuery(managedRunSyncQueryPattern).
		WithArgs(100).
		WillReturnRows(managedRunStatusRows().
			AddRow("run-report", "", "session-report", "running", "report_agent_run", "report-1", []byte(`{}`), startedAt))
	mock.ExpectExec("UPDATE ai_runs SET").
		WithArgs("succeeded", outputRefWithoutResult{status: "completed", taskID: "session-report", sessionID: "session-report", reportID: "report-1"}, 7, "report-1", now, "run-report").
		WillReturnResult(sqlmock.NewResult(0, 1))

	syncer := NewManagedAgentRunStatusSyncer(db, NewManagedAgentClient(platform.URL, "platform-token"))
	if err := syncer.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestManagedAgentRunStatusSyncerHidesPlatformFailedSessionDetails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"task_id":"session-failed","status":"failed","error":"tool execution failed"}`))
	}))
	defer platform.Close()

	startedAt := now.Add(-10 * time.Minute)
	mock.ExpectQuery(managedRunSyncQueryPattern).
		WithArgs(100).
		WillReturnRows(managedRunStatusRows().
			AddRow("run-failed", "", "session-failed", "running", "report_agent_run", "", []byte(`{}`), startedAt))
	mock.ExpectExec("UPDATE ai_runs SET").
		WithArgs("failed", outputRefWithoutResult{status: "failed", taskID: "session-failed", sessionID: "session-failed", errText: reportAgentFailureErrorMessage, forbiddenText: "tool execution failed"}, nil, reportAgentFailureErrorMessage, now, "AGENT_EXECUTION_FAILED", "run-failed").
		WillReturnResult(sqlmock.NewResult(0, 1))

	syncer := NewManagedAgentRunStatusSyncer(db, NewManagedAgentClient(platform.URL, "platform-token"))
	if err := syncer.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestManagedAgentRunStatusSyncerNormalizesInfrastructureTimeout(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 7, 24, 19, 39, 0, 0, time.UTC)
	startedAt := now.Add(-11 * time.Minute)
	platformStartedAt := now.Add(-9*time.Minute - 56*time.Second).Unix()
	platformFinishedAt := now.Unix()
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"task_id":"session-timeout","status":"failed","error":"infrastructure_failure","started_at":` + managedStringFromAny(platformStartedAt) + `,"finished_at":` + managedStringFromAny(platformFinishedAt) + `}`))
	}))
	defer platform.Close()

	mock.ExpectQuery(managedRunSyncQueryPattern).
		WithArgs(100).
		WillReturnRows(managedRunStatusRows().
			AddRow("run-timeout", "", "session-timeout", "running", "report_agent_run", "", []byte(`{}`), startedAt))
	mock.ExpectExec("UPDATE ai_runs SET").
		WithArgs(
			"timeout",
			outputRefWithoutResult{status: "failed", taskID: "session-timeout", sessionID: "session-timeout", errText: "报告生成超时，请稍后重试", forbiddenText: "infrastructure_failure"},
			nil,
			"报告生成超时，请稍后重试",
			now,
			"AGENT_EXECUTION_TIMEOUT",
			"run-timeout",
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	syncer := NewManagedAgentRunStatusSyncer(db, NewManagedAgentClient(platform.URL, "platform-token"))
	if err := syncer.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestManagedAgentRunStatusSyncerHidesEarlyInfrastructureFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 7, 24, 19, 39, 0, 0, time.UTC)
	startedAt := now.Add(-2 * time.Minute)
	platformStartedAt := now.Add(-time.Minute).Unix()
	platformFinishedAt := now.Unix()
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"task_id":"session-failed","status":"failed","error":"infrastructure_failure","started_at":` + managedStringFromAny(platformStartedAt) + `,"finished_at":` + managedStringFromAny(platformFinishedAt) + `}`))
	}))
	defer platform.Close()

	mock.ExpectQuery(managedRunSyncQueryPattern).
		WithArgs(100).
		WillReturnRows(managedRunStatusRows().
			AddRow("run-failed", "", "session-failed", "running", "report_agent_run", "", []byte(`{}`), startedAt))
	mock.ExpectExec("UPDATE ai_runs SET").
		WithArgs(
			"failed",
			outputRefWithoutResult{status: "failed", taskID: "session-failed", sessionID: "session-failed", errText: "报告生成失败，请稍后重试", forbiddenText: "infrastructure_failure"},
			nil,
			"报告生成失败，请稍后重试",
			now,
			"AGENT_EXECUTION_FAILED",
			"run-failed",
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	syncer := NewManagedAgentRunStatusSyncer(db, NewManagedAgentClient(platform.URL, "platform-token"))
	if err := syncer.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestManagedAgentRunStatusSyncerHidesUpstreamModelFailureDetails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	startedAt := now.Add(-5 * time.Minute)
	upstreamError := "API Error: 500 Internal Server Error. Received Model Group=anthropic/GLM-5.2-FP8; Available Model Group Fallbacks=None"
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := json.Marshal(map[string]any{
			"task_id": "session-model-failed", "status": "failed", "error": upstreamError,
			"started_at": startedAt.Unix(), "finished_at": now.Unix(),
		})
		_, _ = w.Write(payload)
	}))
	defer platform.Close()

	mock.ExpectQuery(managedRunSyncQueryPattern).
		WithArgs(100).
		WillReturnRows(managedRunStatusRows().
			AddRow("run-model-failed", "", "session-model-failed", "running", "report_agent_run", "", []byte(`{}`), startedAt))
	mock.ExpectExec("UPDATE ai_runs SET").
		WithArgs(
			"failed",
			outputRefWithoutResult{status: "failed", taskID: "session-model-failed", sessionID: "session-model-failed", errText: reportAgentFailureErrorMessage, forbiddenText: "GLM-5.2-FP8"},
			nil,
			reportAgentFailureErrorMessage,
			now,
			"AGENT_EXECUTION_FAILED",
			"run-model-failed",
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	syncer := NewManagedAgentRunStatusSyncer(db, NewManagedAgentClient(platform.URL, "platform-token"))
	if err := syncer.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestNormalizeManagedRunStatusIncludesTimeout(t *testing.T) {
	if got := NormalizeManagedRunStatus("timed_out"); got != "timeout" {
		t.Fatalf("status = %q", got)
	}
	if !IsTerminalManagedRunStatus("timeout") {
		t.Fatal("timeout should be terminal")
	}
	if strings.TrimSpace(NormalizeManagedRunStatus("")) != "pending" {
		t.Fatal("empty status should normalize to pending")
	}
}
