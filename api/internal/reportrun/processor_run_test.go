package reportrun

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/internal/reportcontext"
	"github.com/aidashboard/api/internal/sessiondigestv2"
)

func TestStoreContextAndTransitionCastsContextBytes(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository, err := NewRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	run := Run{ID: "run-1", LeaseOwner: "worker-1", Stage: StageBuildingContext}
	query := regexp.QuoteMeta("'report_context_hash', $4::text") +
		"(?s).*" + regexp.QuoteMeta("'report_context_bytes', $5::integer") +
		"(?s).*" + regexp.QuoteMeta("CASE WHEN $5::integer > 1048576")
	mock.ExpectExec(query).
		WithArgs(run.ID, run.LeaseOwner, run.Stage, "context-hash", 6153).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repository.StoreContextAndTransition(t.Context(), run, "context-hash", 6153); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type coordinatorStub struct {
	result sessiondigestv2.EnsureResult
	err    error
	calls  int
}

func (s *coordinatorStub) EnsureRunDigests(context.Context, string, string) (sessiondigestv2.EnsureResult, error) {
	s.calls++
	return s.result, s.err
}

type freezerStub struct{ calls int }

func (s *freezerStub) FreezeRunDigests(context.Context, string) (string, error) {
	s.calls++
	return "", errors.New("freezer must not run in waiting_digest claim")
}

type contextStub struct{ calls int }

func (s *contextStub) Build(context.Context, reportcontext.BuildRequest) (reportcontext.StoredContext, error) {
	s.calls++
	return reportcontext.StoredContext{}, errors.New("context must not build in waiting_digest claim")
}

type submitterStub struct{ calls int }

func (s *submitterStub) Submit(context.Context, Run) (SubmissionResult, error) {
	s.calls++
	return SubmissionResult{}, errors.New("Agent must not submit in waiting_digest claim")
}

func TestProcessorClaimAdvancesOnlyOneStage(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository, err := NewRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	owner := "host:report-run:1"
	mock.ExpectBegin()
	mock.ExpectQuery("(?s)SELECT id::text.*FROM ai_runs.*FOR UPDATE SKIP LOCKED").
		WithArgs(now).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "business_type", "agent_id", "model_id", "status",
			"execution_stage", "stage_attempts", "external_session_id",
			"input_ref_json", "execution_input_json",
		}).AddRow(
			"run-1", "user-1", "report_agent_run", "agent-1", "model-1", "pending",
			StageWaitingDigest, 0, nil, []byte(`{"report_type":"personal_daily"}`), []byte(`{}`),
		))
	mock.ExpectExec("(?s)UPDATE ai_runs.*execution_lease_owner").
		WithArgs("run-1", owner, now.Add(reportRunLeaseTTL), StageWaitingDigest, now).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectExec("(?s)UPDATE ai_runs.*execution_stage = \\$4").
		WithArgs("run-1", owner, StageWaitingDigest, StageBuildingContext, "pending", false).
		WillReturnResult(sqlmock.NewResult(0, 1))

	coordinator := &coordinatorStub{result: sessiondigestv2.EnsureResult{State: sessiondigestv2.EnsureReady}}
	freezer := &freezerStub{}
	contextBuilder := &contextStub{}
	submitter := &submitterStub{}
	processor, err := NewProcessor(repository, coordinator, freezer, contextBuilder, submitter, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := processor.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if coordinator.calls != 1 || freezer.calls != 0 || contextBuilder.calls != 0 || submitter.calls != 0 {
		t.Fatalf("unexpected calls coordinator=%d freezer=%d context=%d submitter=%d",
			coordinator.calls, freezer.calls, contextBuilder.calls, submitter.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessorWaitingDigestReleasesLeaseWithoutPolling(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository, _ := NewRepository(database)
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	owner := "host:report-run:2"
	mock.ExpectBegin()
	mock.ExpectQuery("(?s)SELECT id::text.*FROM ai_runs.*FOR UPDATE SKIP LOCKED").
		WithArgs(now).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "business_type", "agent_id", "model_id", "status",
			"execution_stage", "stage_attempts", "external_session_id",
			"input_ref_json", "execution_input_json",
		}).AddRow("run-2", "user-1", "report_agent_run", "agent-1", nil, "pending",
			StageWaitingDigest, 0, nil, []byte(`{}`), []byte(`{}`)))
	mock.ExpectExec("(?s)UPDATE ai_runs.*execution_lease_owner").
		WithArgs("run-2", owner, now.Add(reportRunLeaseTTL), StageWaitingDigest, now).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectExec("(?s)UPDATE ai_runs.*SET next_attempt_at = NULL.*execution_lease_owner = NULL").
		WithArgs("run-2", owner, StageWaitingDigest).
		WillReturnResult(sqlmock.NewResult(0, 1))

	processor, err := NewProcessor(
		repository,
		&coordinatorStub{result: sessiondigestv2.EnsureResult{State: sessiondigestv2.EnsureWaiting}},
		&freezerStub{}, &contextStub{}, &submitterStub{}, owner,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := processor.RunOnce(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestContextBuildRequestUsesFrozenRepresentation(t *testing.T) {
	run := Run{
		ID: "run-1", UserID: "7", ModelID: "model-1",
		InputRef: map[string]any{
			"report_type": "personal_daily",
			"period":      map[string]any{"date": "2026-07-23"},
			"target":      map[string]any{"type": "self", "user_id": "7"},
		},
		ExecutionInput: map[string]any{
			"report_context_representation": reportcontext.RepresentationWorkEvidence,
			"report_agent_source":           "system",
		},
	}
	request, err := contextBuildRequest(run, "selection-1")
	if err != nil {
		t.Fatal(err)
	}
	if request.Representation != reportcontext.RepresentationWorkEvidence {
		t.Fatalf("representation = %q", request.Representation)
	}
	if !request.IncludeWorkThreads {
		t.Fatal("system personal daily must include work threads")
	}
	run.ExecutionInput["report_agent_source"] = "personal"
	request, err = contextBuildRequest(run, "selection-1")
	if err != nil {
		t.Fatal(err)
	}
	if request.IncludeWorkThreads {
		t.Fatal("personal report Agent must not include system work threads")
	}
	delete(run.ExecutionInput, "report_context_representation")
	request, err = contextBuildRequest(run, "selection-1")
	if err != nil {
		t.Fatal(err)
	}
	if request.Representation != "" {
		t.Fatalf("legacy run must keep the legacy representation, got %q", request.Representation)
	}
}

func TestHandleContextErrorClassifiesProjectionFailure(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository, err := NewRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	run := Run{ID: "run-1", LeaseOwner: "worker-1", Stage: StageBuildingContext}
	mock.ExpectExec("(?s)UPDATE ai_runs.*error_code = \\$5").
		WithArgs(
			run.ID, run.LeaseOwner, run.Stage, "failed",
			"REPORT_CONTEXT_BUILD_FAILED", reportcontext.ErrIncomplete.Error(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	processor := &Processor{repository: repository}
	if err := processor.handleContextError(t.Context(), run, reportcontext.ErrIncomplete); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
