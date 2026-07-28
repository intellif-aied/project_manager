package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aidashboard/api/internal/reportrun"
	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/service"
)

type ReportRunSubmitter struct {
	db       *sql.DB
	client   *service.ManagedAgentClient
	defaults ManagedAgentDefaults
}

func NewReportRunSubmitter(
	database *sql.DB,
	client *service.ManagedAgentClient,
	defaults ManagedAgentDefaults,
) (*ReportRunSubmitter, error) {
	if database == nil || client == nil {
		return nil, errors.New("database and managed Agent client are required")
	}
	return &ReportRunSubmitter{
		db: database, client: client, defaults: normalizeManagedAgentDefaults(defaults),
	}, nil
}

func (s *ReportRunSubmitter) Submit(
	ctx context.Context,
	run reportrun.Run,
) (reportrun.SubmissionResult, error) {
	user, err := s.loadUser(ctx, run.UserID)
	if err != nil {
		return reportrun.SubmissionResult{}, prepareSubmissionError(err)
	}
	token, err := MintReportRunToken(user, s.defaults.AIHubSecret, run.ID)
	if err != nil {
		return reportrun.SubmissionResult{}, prepareSubmissionError(err)
	}
	client := s.client.WithToken(token)
	if runBool(run.ExecutionInput, "system_report_account") {
		// Default report credentials and Sessions belong to the configured
		// dedicated account. The current user's token is only the MCP secret value.
		client = s.client
	}
	credentialID, err := s.findOrCreateCredential(ctx, client, run, token)
	if err != nil {
		return reportrun.SubmissionResult{}, prepareSubmissionError(err)
	}

	systemValues := reportAgentStartPromptValues(run.ID)
	userValues := runStringMap(run.ExecutionInput, "start_prompt_values")
	message := runString(run.ExecutionInput, "initial_message")
	startValues, reserved, ok := mergeReportStartPromptValues(
		systemValues, userValues, message, s.defaults.ReportMCPCredentialSlot,
	)
	if !ok {
		return reportrun.SubmissionResult{}, prepareSubmissionError(
			fmt.Errorf("%s is managed by Aida", reserved),
		)
	}
	sessionMessage := buildReportRunMessage(startValues, message, s.defaults.ReportMCPCredentialSlot)
	overrides := runStringMap(run.ExecutionInput, "credential_overrides")
	for _, slot := range runStringSlice(run.ExecutionInput, "report_mcp_slots") {
		overrides[slot] = credentialID
	}
	if len(overrides) == 0 {
		overrides[s.defaults.ReportMCPCredentialSlot] = credentialID
	}
	marked, err := s.markSubmissionStarted(ctx, run)
	if err != nil {
		return reportrun.SubmissionResult{}, prepareSubmissionError(err)
	}
	if !marked {
		return reportrun.SubmissionResult{}, &reportrun.SubmissionError{
			Code: "EXTERNAL_SUBMISSION_STATE_UNKNOWN", Message: "report run lease was lost before AIHub submission",
		}
	}

	response, err := client.CreateSession(ctx, service.CreateManagedSessionRequest{
		AgentID: run.AgentID, ModelID: run.ModelID,
		StartPromptValues: startValues, Message: sessionMessage,
		CredentialOverrides: overrides,
	})
	if err != nil {
		var upstream *service.ManagedAgentError
		if errors.As(err, &upstream) && upstream.StatusCode >= http.StatusBadRequest &&
			upstream.StatusCode < http.StatusInternalServerError {
			return reportrun.SubmissionResult{}, &reportrun.SubmissionError{
				Code: "EXTERNAL_SUBMISSION_REJECTED", Message: upstream.Message,
			}
		}
		return reportrun.SubmissionResult{}, &reportrun.SubmissionError{
			Code: "EXTERNAL_SUBMISSION_STATE_UNKNOWN", Message: err.Error(),
		}
	}
	if response == nil || strings.TrimSpace(response.SessionID) == "" {
		return reportrun.SubmissionResult{}, &reportrun.SubmissionError{
			Code: "EXTERNAL_SUBMISSION_REJECTED", Message: "AIHub returned an empty session ID",
		}
	}
	return reportrun.SubmissionResult{
		SessionID: response.SessionID, ModelID: response.ModelID,
	}, nil
}

func (s *ReportRunSubmitter) markSubmissionStarted(ctx context.Context, run reportrun.Run) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE ai_runs
		SET execution_input_json = execution_input_json || jsonb_build_object(
			'external_submission_started_at', now())
		WHERE id = $1 AND status = 'pending' AND execution_stage = 'submitting_agent'
		  AND execution_lease_owner = $2 AND execution_lease_until > now()
		  AND external_session_id IS NULL
		  AND NOT (execution_input_json ? 'external_submission_started_at')`,
		run.ID, run.LeaseOwner,
	)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

func (s *ReportRunSubmitter) loadUser(ctx context.Context, userID string) (*model.User, error) {
	var user model.User
	err := s.db.QueryRowContext(ctx, `
		SELECT id::text, username, COALESCE(nickname, ''), COALESCE(email, ''),
			COALESCE(name, ''), COALESCE(employee_id, ''), COALESCE(role, '')
		FROM users WHERE id = $1`, userID,
	).Scan(
		&user.ID, &user.Username, &user.Nickname, &user.Email,
		&user.Name, &user.EmployeeID, &user.Role,
	)
	return &user, err
}

func (s *ReportRunSubmitter) findOrCreateCredential(
	ctx context.Context,
	client *service.ManagedAgentClient,
	run reportrun.Run,
	token string,
) (string, error) {
	listed, err := client.ListCredentials(ctx)
	if err != nil {
		return "", err
	}
	for _, credential := range listed.Credentials {
		if credential.Archived || credential.Metadata["ai_run_id"] != run.ID ||
			credential.Metadata["purpose"] != "report_mcp_auth" {
			continue
		}
		if strings.TrimSpace(credential.CredentialID) != "" {
			return credential.CredentialID, nil
		}
	}
	created, err := client.CreateCredential(ctx, service.CreateManagedCredentialRequest{
		Name: "Aida Report MCP Auth " + run.ID, Kind: "secret", Value: token,
		Metadata: map[string]string{
			"aida_user_id": run.UserID, "ai_run_id": run.ID, "purpose": "report_mcp_auth",
		},
	})
	if err != nil {
		return "", err
	}
	if created == nil || strings.TrimSpace(created.CredentialID) == "" {
		return "", errors.New("AIHub returned an empty credential ID")
	}
	return created.CredentialID, nil
}

func prepareSubmissionError(err error) error {
	return &reportrun.SubmissionError{
		Code: "EXTERNAL_SUBMISSION_PREPARE_FAILED", Message: err.Error(), Retryable: true,
	}
}

func runString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func runBool(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

func runStringMap(values map[string]any, key string) map[string]string {
	result := map[string]string{}
	raw, _ := values[key].(map[string]any)
	for name, value := range raw {
		if text, ok := value.(string); ok {
			result[name] = text
		}
	}
	return result
}

func runStringSlice(values map[string]any, key string) []string {
	raw, _ := values[key].([]any)
	result := make([]string, 0, len(raw))
	for _, value := range raw {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			result = append(result, strings.TrimSpace(text))
		}
	}
	return result
}
