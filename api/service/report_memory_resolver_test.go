package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/internal/reportmemory"
	"github.com/aidashboard/api/model"
)

func TestManagedProjectMemoryResolverSubmitAndStatus(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	mock.ExpectQuery("SELECT id::text, username").
		WithArgs("305").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "username", "nickname", "email", "name", "employee_id", "role",
		}).AddRow("305", "t03", "", "", "", "", "member"))

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/api/credential":
			var payload CreateManagedCredentialRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.Value != "bound-token" || payload.Metadata["aida_user_id"] != "305" {
				t.Fatalf("credential = %#v", payload)
			}
			_ = json.NewEncoder(writer).Encode(CreateManagedCredentialResponse{CredentialID: "credential-1"})
		case "/api/session":
			var payload CreateManagedSessionRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.AgentID != "memory-agent" || payload.Message != "/aida-project-memory" ||
				payload.CredentialOverrides[ProjectMemoryMCPCredentialSlot] != "credential-1" {
				t.Fatalf("submission = %#v", payload)
			}
			_ = json.NewEncoder(writer).Encode(CreateManagedSessionResponse{SessionID: "session-1", Status: "queued"})
		case "/api/task/session-1/status":
			_ = json.NewEncoder(writer).Encode(ManagedTaskStatus{
				TaskID: "session-1", Status: "succeeded", StartedAt: 100, FinishedAt: 105,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	resolver := NewManagedProjectMemoryResolver(
		database, NewManagedAgentClient(server.URL, "test-token"),
		func(user *model.User, jobRef string) (string, error) {
			if user.ID != "305" || jobRef != "2026-08-01|fingerprint" {
				t.Fatalf("identity = %#v, %q", user, jobRef)
			}
			return "bound-token", nil
		}, ProjectMemoryMCPCredentialSlot,
	)
	submission, err := resolver.Submit(context.Background(), reportmemory.ResolverRequest{
		AgentID: "memory-agent", ModelID: "deepseek-v4-flash",
		UserID: "305", JobRef: "2026-08-01|fingerprint",
	})
	if err != nil || submission.TaskID != "session-1" {
		t.Fatalf("submission = %#v, %v", submission, err)
	}
	status, err := resolver.Status(context.Background(), submission.TaskID)
	if err != nil || status.Status != "succeeded" || status.EndedAt.Sub(status.StartedAt).Seconds() != 5 {
		t.Fatalf("status = %#v, %v", status, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
