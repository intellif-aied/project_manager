package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/internal/reportrun"
	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/service"
)

func TestReportRunSubmitterKeepsReportIdentityInCredential(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var submitted service.CreateManagedSessionRequest
	var createdCredential service.CreateManagedCredentialRequest
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer platform-token" {
			t.Fatalf("platform authorization = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/credential/list":
			writeJSON(w, http.StatusOK, model.ListManagedCredentialsResponse{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/credential":
			if err := json.NewDecoder(r.Body).Decode(&createdCredential); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, http.StatusOK, service.CreateManagedCredentialResponse{CredentialID: "credential-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
				t.Fatal(err)
			}
			writeJSON(w, http.StatusOK, service.CreateManagedSessionResponse{SessionID: "session-1"})
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer platform.Close()

	mock.ExpectQuery("(?s)SELECT id::text, username.*FROM users").
		WithArgs("305").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "nickname", "email", "name", "employee_id", "role"}).
			AddRow("305", "tester", "", "", "Tester", "", "employee"))
	mock.ExpectExec("(?s)UPDATE ai_runs.*external_submission_started_at").
		WithArgs("run-identity", "host:report-run:identity").
		WillReturnResult(sqlmock.NewResult(0, 1))

	submitter, err := NewReportRunSubmitter(
		database, service.NewManagedAgentClient(platform.URL, "platform-token"),
		ManagedAgentDefaults{AIHubSecret: "secret", AIDAPublicBaseURL: "https://aida.example.com"},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = submitter.Submit(t.Context(), reportrun.Run{
		ID: "run-identity", UserID: "305", AgentID: "agent-1", ModelID: "model-1",
		Stage: reportrun.StageSubmittingAgent, LeaseOwner: "host:report-run:identity",
		InputRef: map[string]any{
			"report_type": "team_daily", "period": map[string]any{"date": "2026-07-23"},
			"target":                     map[string]any{"type": "team", "team_id": "team-1"},
			"report_source_selection_id": "selection-1",
		},
		ExecutionInput: map[string]any{
			"initial_message":       "请强调已经确认的风险",
			"start_prompt_values":   map[string]any{"custom": "legacy-value"},
			"system_report_account": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(submitted.StartPromptValues) != 0 {
		t.Fatalf("start prompt values must not expose report identity: %#v", submitted.StartPromptValues)
	}
	identity, err := extractAIHubIdentityWithPolicy(createdCredential.Value, "secret", false)
	if err != nil {
		t.Fatal(err)
	}
	if identity.UID != 305 || identity.ReportRunID != "run-identity" {
		t.Fatalf("credential identity = %#v", identity)
	}
	if createdCredential.Metadata["ai_run_id"] != "run-identity" || createdCredential.Metadata["aida_user_id"] != "305" {
		t.Fatalf("credential metadata = %#v", createdCredential.Metadata)
	}
	for _, key := range []string{
		"report_type", "period_json", "calendar_context_json", "target_json",
		"report_source_selection_id", "selected_session_slice_keys_json", "report_date", "week_start", "week_end",
	} {
		if _, ok := submitted.StartPromptValues[key]; ok {
			t.Fatalf("legacy report identity %q leaked in start prompt values: %#v", key, submitted.StartPromptValues)
		}
	}
	for _, forbidden := range []string{"report_type=", "period=", "calendar_context=", "target=", "report_source_selection_id="} {
		if strings.Contains(submitted.Message, forbidden) {
			t.Fatalf("legacy report identity %q leaked in message: %s", forbidden, submitted.Message)
		}
	}
	if submitted.Message != "/aida-report\n\n用户补充说明：\n请强调已经确认的风险" {
		t.Fatalf("runtime message=%q", submitted.Message)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildReportRunMessageNeverCarriesRunID(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{name: "default", want: "/aida-report"},
		{name: "legacy fallback", message: fallbackReportRunMessage(), want: "/aida-report"},
		{
			name:    "user instruction",
			message: "请强调已经确认的风险",
			want:    "/aida-report\n\n用户补充说明：\n请强调已经确认的风险",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildReportRunMessage(reportAgentStartPromptValues("run-identity"), test.message, reportMCPCredentialSlot)
			if got != test.want {
				t.Fatalf("message=%q, want %q", got, test.want)
			}
		})
	}
}

func TestReportRunSubmitterFailsUnknownExternalStateWithoutRetry(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	sessionCalls := 0
	var platformAuthorization string
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		platformAuthorization = r.Header.Get("Authorization")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/credential/list":
			writeJSON(w, http.StatusOK, model.ListManagedCredentialsResponse{Credentials: []model.ManagedCredential{{
				CredentialID: "credential-1",
				Metadata:     map[string]string{"ai_run_id": "run-1", "purpose": "report_mcp_auth"},
			}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			sessionCalls++
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "upstream timeout"})
		default:
			t.Fatalf("unexpected platform request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer platform.Close()
	mock.ExpectQuery("(?s)SELECT id::text, username.*FROM users").
		WithArgs("305").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "nickname", "email", "name", "employee_id", "role"}).
			AddRow("305", "tester", "", "", "Tester", "", "employee"))
	mock.ExpectExec("(?s)UPDATE ai_runs.*external_submission_started_at").
		WithArgs("run-1", "host:report-run:1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	submitter, err := NewReportRunSubmitter(
		database, service.NewManagedAgentClient(platform.URL, "platform-token"),
		ManagedAgentDefaults{AIHubSecret: "secret", AIDAPublicBaseURL: "https://aida.example.com"},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = submitter.Submit(t.Context(), reportrun.Run{
		ID: "run-1", UserID: "305", AgentID: "agent-1", ModelID: "model-1",
		Stage: reportrun.StageSubmittingAgent, LeaseOwner: "host:report-run:1",
		InputRef: map[string]any{
			"report_type": "team_daily", "period": map[string]any{"date": "2026-07-22"},
			"target": map[string]any{"type": "team", "team_id": "team-1"},
		},
		ExecutionInput: map[string]any{"initial_message": "generate"},
	})
	var submissionErr *reportrun.SubmissionError
	if !errors.As(err, &submissionErr) || submissionErr.Code != "EXTERNAL_SUBMISSION_STATE_UNKNOWN" || submissionErr.Retryable {
		t.Fatalf("submission error = %#v", err)
	}
	if sessionCalls != 1 {
		t.Fatalf("session calls = %d", sessionCalls)
	}
	if platformAuthorization == "Bearer platform-token" || platformAuthorization == "" {
		t.Fatalf("custom report Agent must keep the request user's platform identity: %q", platformAuthorization)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReportRunSubmitterDoesNotSendAfterLeaseMarkerConflict(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	sessionCalls := 0
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/credential/list":
			writeJSON(w, http.StatusOK, model.ListManagedCredentialsResponse{Credentials: []model.ManagedCredential{{
				CredentialID: "credential-1", Metadata: map[string]string{"ai_run_id": "run-2", "purpose": "report_mcp_auth"},
			}}})
		case "/api/session":
			sessionCalls++
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer platform.Close()
	mock.ExpectQuery("(?s)SELECT id::text, username.*FROM users").WithArgs("305").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "nickname", "email", "name", "employee_id", "role"}).
			AddRow("305", "tester", "", "", "Tester", "", "employee"))
	mock.ExpectExec("(?s)UPDATE ai_runs.*external_submission_started_at").
		WithArgs("run-2", "host:report-run:2").
		WillReturnResult(sqlmock.NewResult(0, 0))
	submitter, _ := NewReportRunSubmitter(database, service.NewManagedAgentClient(platform.URL, "token"), ManagedAgentDefaults{AIHubSecret: "secret"})
	_, err = submitter.Submit(t.Context(), reportrun.Run{
		ID: "run-2", UserID: "305", AgentID: "agent-1", Stage: reportrun.StageSubmittingAgent,
		LeaseOwner: "host:report-run:2", InputRef: map[string]any{
			"report_type": "team_daily", "period": map[string]any{"date": "2026-07-22"},
		},
	})
	var submissionErr *reportrun.SubmissionError
	if !errors.As(err, &submissionErr) || submissionErr.Code != "EXTERNAL_SUBMISSION_STATE_UNKNOWN" {
		t.Fatalf("submission error = %#v", err)
	}
	if sessionCalls != 0 {
		t.Fatalf("session calls = %d", sessionCalls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
