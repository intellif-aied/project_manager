package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/reportreview"
	"github.com/aidashboard/api/model"
)

const ReportReviewMCPCredentialSlot = "AIDA_REPORT_REVIEW_MCP_AUTH"

type ReportReviewTokenMinter func(*model.User, string) (string, error)

type ManagedReportReviewResolver struct {
	db             *sql.DB
	client         *ManagedAgentClient
	tokenMinter    ReportReviewTokenMinter
	credentialSlot string
}

func NewManagedReportReviewResolver(
	database *sql.DB,
	client *ManagedAgentClient,
	tokenMinter ReportReviewTokenMinter,
	credentialSlot string,
) *ManagedReportReviewResolver {
	if strings.TrimSpace(credentialSlot) == "" {
		credentialSlot = ReportReviewMCPCredentialSlot
	}
	return &ManagedReportReviewResolver{
		db: database, client: client, tokenMinter: tokenMinter,
		credentialSlot: strings.TrimSpace(credentialSlot),
	}
}

func (resolver *ManagedReportReviewResolver) Submit(ctx context.Context, request reportreview.ResolverRequest) (reportreview.ResolverSubmission, error) {
	if resolver == nil || resolver.db == nil || resolver.client == nil ||
		!resolver.client.Configured() || resolver.tokenMinter == nil {
		return reportreview.ResolverSubmission{}, errors.New("report review Agent platform is not configured")
	}
	user, err := resolver.loadUser(ctx, request.UserID)
	if err != nil {
		return reportreview.ResolverSubmission{}, err
	}
	token, err := resolver.tokenMinter(user, request.JobRef)
	if err != nil {
		return reportreview.ResolverSubmission{}, err
	}
	credential, err := resolver.client.CreateCredential(ctx, CreateManagedCredentialRequest{
		Name: "Aida Report Review MCP Auth " + time.Now().UTC().Format("20060102T150405"),
		Kind: "secret", Value: token,
		Metadata: map[string]string{
			"aida_user_id": request.UserID, "report_review_job_ref": request.JobRef,
			"purpose": "report_review_mcp_auth",
		},
	})
	if err != nil {
		return reportreview.ResolverSubmission{}, err
	}
	if credential == nil || strings.TrimSpace(credential.CredentialID) == "" {
		return reportreview.ResolverSubmission{}, errors.New("AIHub returned an empty report review credential ID")
	}
	response, err := resolver.client.CreateSession(ctx, CreateManagedSessionRequest{
		AgentID: request.AgentID, ModelID: request.ModelID,
		Message:             ReportReviewAgentStartPrompt(),
		CredentialOverrides: map[string]string{resolver.credentialSlot: credential.CredentialID},
	})
	if err != nil {
		return reportreview.ResolverSubmission{}, err
	}
	if response == nil || strings.TrimSpace(response.SessionID) == "" {
		return reportreview.ResolverSubmission{}, errors.New("report review resolver returned an empty Session")
	}
	return reportreview.ResolverSubmission{TaskID: response.SessionID, Status: response.Status}, nil
}

func (resolver *ManagedReportReviewResolver) Status(ctx context.Context, taskID string) (reportreview.ResolverTask, error) {
	if resolver == nil || resolver.client == nil || !resolver.client.Configured() {
		return reportreview.ResolverTask{}, errors.New("report review Agent platform is not configured")
	}
	result, err := resolver.client.GetTaskStatus(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return reportreview.ResolverTask{}, err
	}
	return reportreview.ResolverTask{
		TaskID: result.TaskID, Status: result.Status, Result: result.Result, Error: result.Error,
		StartedAt: unixTime(result.StartedAt), EndedAt: unixTime(result.FinishedAt),
	}, nil
}

func (resolver *ManagedReportReviewResolver) loadUser(ctx context.Context, userID string) (*model.User, error) {
	var user model.User
	err := resolver.db.QueryRowContext(ctx, `
		SELECT id::text, username, COALESCE(nickname, ''), COALESCE(email, ''),
		       COALESCE(name, ''), COALESCE(employee_id, ''), COALESCE(role, '')
		FROM users WHERE id = $1`, userID).Scan(
		&user.ID, &user.Username, &user.Nickname, &user.Email,
		&user.Name, &user.EmployeeID, &user.Role,
	)
	return &user, err
}
