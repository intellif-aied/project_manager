package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/reportmemory"
	"github.com/aidashboard/api/model"
)

const ProjectMemoryMCPCredentialSlot = "AIDA_PROJECT_MEMORY_MCP_AUTH"

type ProjectMemoryTokenMinter func(*model.User, string) (string, error)

type ManagedProjectMemoryResolver struct {
	db             *sql.DB
	client         *ManagedAgentClient
	tokenMinter    ProjectMemoryTokenMinter
	credentialSlot string
}

func NewManagedProjectMemoryResolver(
	database *sql.DB,
	client *ManagedAgentClient,
	tokenMinter ProjectMemoryTokenMinter,
	credentialSlot string,
) *ManagedProjectMemoryResolver {
	if strings.TrimSpace(credentialSlot) == "" {
		credentialSlot = ProjectMemoryMCPCredentialSlot
	}
	return &ManagedProjectMemoryResolver{
		db: database, client: client, tokenMinter: tokenMinter,
		credentialSlot: strings.TrimSpace(credentialSlot),
	}
}

func (resolver *ManagedProjectMemoryResolver) Submit(ctx context.Context, request reportmemory.ResolverRequest) (reportmemory.ResolverSubmission, error) {
	if resolver == nil || resolver.db == nil || resolver.client == nil ||
		!resolver.client.Configured() || resolver.tokenMinter == nil {
		return reportmemory.ResolverSubmission{}, errors.New("memory resolver Agent platform is not configured")
	}
	user, err := resolver.loadUser(ctx, request.UserID)
	if err != nil {
		return reportmemory.ResolverSubmission{}, err
	}
	token, err := resolver.tokenMinter(user, request.JobRef)
	if err != nil {
		return reportmemory.ResolverSubmission{}, err
	}
	credentialID, err := resolver.findOrCreateCredential(ctx, request, token)
	if err != nil {
		return reportmemory.ResolverSubmission{}, err
	}
	response, err := resolver.client.CreateSession(ctx, CreateManagedSessionRequest{
		AgentID: request.AgentID,
		ModelID: request.ModelID,
		Message: "/aida-project-memory",
		CredentialOverrides: map[string]string{
			resolver.credentialSlot: credentialID,
		},
	})
	if err != nil {
		return reportmemory.ResolverSubmission{}, err
	}
	if response == nil || strings.TrimSpace(response.SessionID) == "" {
		return reportmemory.ResolverSubmission{}, errors.New("memory resolver returned an empty Session")
	}
	return reportmemory.ResolverSubmission{TaskID: response.SessionID, Status: response.Status}, nil
}

func (resolver *ManagedProjectMemoryResolver) Status(ctx context.Context, taskID string) (reportmemory.ResolverTask, error) {
	if resolver == nil || resolver.client == nil || !resolver.client.Configured() {
		return reportmemory.ResolverTask{}, errors.New("memory resolver Agent platform is not configured")
	}
	result, err := resolver.client.GetTaskStatus(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return reportmemory.ResolverTask{}, err
	}
	return reportmemory.ResolverTask{
		TaskID: result.TaskID, Status: result.Status, Result: result.Result, Error: result.Error,
		StartedAt: unixTime(result.StartedAt), EndedAt: unixTime(result.FinishedAt),
	}, nil
}

func (resolver *ManagedProjectMemoryResolver) loadUser(ctx context.Context, userID string) (*model.User, error) {
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

func (resolver *ManagedProjectMemoryResolver) findOrCreateCredential(
	ctx context.Context,
	request reportmemory.ResolverRequest,
	token string,
) (string, error) {
	// A retry may happen in the next nightly window, after the previous bound
	// JWT has expired. Always create a fresh Credential for each submission.
	created, err := resolver.client.CreateCredential(ctx, CreateManagedCredentialRequest{
		Name: "Aida Project Memory MCP Auth " + time.Now().UTC().Format("20060102T150405"),
		Kind: "secret", Value: token,
		Metadata: map[string]string{
			"aida_user_id":           request.UserID,
			"project_memory_job_ref": request.JobRef,
			"purpose":                "project_memory_mcp_auth",
		},
	})
	if err != nil {
		return "", err
	}
	if created == nil || strings.TrimSpace(created.CredentialID) == "" {
		return "", errors.New("AIHub returned an empty project memory credential ID")
	}
	return created.CredentialID, nil
}

func unixTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.Unix(value, 0)
}
