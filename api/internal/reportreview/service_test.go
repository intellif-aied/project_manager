package reportreview

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/internal/reportbrief"
)

type stubResolver struct {
	statusCalls int
	submission  ResolverSubmission
	submitErr   error
}

func (resolver *stubResolver) Submit(context.Context, ResolverRequest) (ResolverSubmission, error) {
	if resolver.submitErr != nil {
		return ResolverSubmission{}, resolver.submitErr
	}
	return resolver.submission, nil
}

func (resolver *stubResolver) Status(context.Context, string) (ResolverTask, error) {
	resolver.statusCalls++
	return ResolverTask{Status: "running"}, nil
}

type recordingFinalizer struct {
	err       error
	called    int
	finalized reportbrief.ReviewFinalized
}

func (finalizer *recordingFinalizer) FinalizeReviewedReport(
	_ context.Context,
	_, _ string,
	finalized reportbrief.ReviewFinalized,
	_ json.RawMessage,
) error {
	finalizer.called++
	finalizer.finalized = finalized
	return finalizer.err
}

func reviewServiceInput(t *testing.T) ([]byte, reportbrief.Payload) {
	t.Helper()
	payload := reportbrief.Payload{
		SchemaVersion: "report-brief/v1",
		ReportType:    "personal_daily",
		Period:        reportbrief.Period{Start: "2026-08-07", End: "2026-08-07"},
		Workstreams: []reportbrief.Workstream{{
			Subject: "AIDA", Title: "完善日报语义审核",
			Deliverables: []reportbrief.Deliverable{{Result: "接入独立审核步骤", FactRefs: []string{"fact-001"}}},
		}},
	}
	input, err := json.Marshal(Input{
		SchemaVersion: "report-review-input/v1",
		RunID:         "11111111-1111-1111-1111-111111111111", BriefHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ContextHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Candidate:   payload, AllowedFactRefs: []string{"fact-001"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return input, payload
}

func TestRefreshRunningTimesOutToCandidateWithoutPollingResolver(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	input, payload := reviewServiceInput(t)
	resolver := &stubResolver{}
	finalizer := &recordingFinalizer{}
	service := &Service{
		db: database, resolver: resolver, finalizer: finalizer,
		config: normalizeConfig(Config{Enabled: true, WorkerID: "review-worker"}),
	}
	runID := "11111111-1111-1111-1111-111111111111"
	mock.ExpectExec("UPDATE report_review_jobs SET status = 'finalizing'").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "review_timeout", "review-worker", sqlmock.AnyArg(), runID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE report_review_jobs SET status = 'written'").
		WithArgs(runID).WillReturnResult(sqlmock.NewResult(0, 1))
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	err = service.refreshRunning(context.Background(), queuedJob{
		RunID: runID, UserID: "198", InputJSON: input, StartedAt: now.Add(-maxReviewDuration),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.statusCalls != 0 {
		t.Fatalf("resolver was polled %d times after timeout", resolver.statusCalls)
	}
	if finalizer.called != 1 || finalizer.finalized.Stored.Payload.Workstreams[0].Title != payload.Workstreams[0].Title {
		t.Fatalf("candidate fallback was not finalized: %#v", finalizer.finalized)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestResumeFinalizingWritesStoredReviewedBrief(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, payload := reviewServiceInput(t)
	finalBrief, _ := json.Marshal(payload)
	finalizer := &recordingFinalizer{}
	service := &Service{db: database, finalizer: finalizer}
	runID := "11111111-1111-1111-1111-111111111111"
	mock.ExpectExec("UPDATE report_review_jobs SET status = 'written'").
		WithArgs(runID).WillReturnResult(sqlmock.NewResult(0, 1))
	err = service.resumeFinalizing(context.Background(), queuedJob{
		RunID: runID, UserID: "198", ContextHash: "context-hash",
		FinalBriefJSON: finalBrief, FinalMode: reportbrief.ReviewModeRepaired,
		DecisionJSON: []byte(`{"decision":"repair"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if finalizer.called != 1 || finalizer.finalized.Mode != reportbrief.ReviewModeRepaired ||
		finalizer.finalized.Stored.BriefHash == "" || finalizer.finalized.Stored.ContextHash != "context-hash" {
		t.Fatalf("reviewed finalization was not restored: %#v", finalizer.finalized)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestResumeFinalizingReleasesLeaseAfterWriteFailure(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, payload := reviewServiceInput(t)
	finalBrief, _ := json.Marshal(payload)
	finalizer := &recordingFinalizer{err: errors.New("temporary report write failure")}
	service := &Service{db: database, finalizer: finalizer}
	runID := "11111111-1111-1111-1111-111111111111"
	mock.ExpectExec("UPDATE report_review_jobs SET lease_owner = NULL").
		WithArgs(sqlmock.AnyArg(), "temporary report write failure", runID).WillReturnResult(sqlmock.NewResult(0, 1))
	err = service.resumeFinalizing(context.Background(), queuedJob{
		RunID: runID, UserID: "198", FinalBriefJSON: finalBrief,
		FinalMode: reportbrief.ReviewModeAccepted,
	})
	if err == nil {
		t.Fatal("expected finalizer error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitAcceptsFastReviewerWriteback(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	resolver := &stubResolver{submission: ResolverSubmission{TaskID: "review-session-1", Status: "running"}}
	service := &Service{
		db: database, resolver: resolver,
		config: normalizeConfig(Config{Enabled: true, AgentID: "review-agent", ModelID: "MiniMax-M2.5"}),
	}
	runID := "11111111-1111-1111-1111-111111111111"
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE report_review_jobs SET status = 'running'").
		WithArgs("review-session-1", "MiniMax-M2.5", runID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT status FROM report_review_jobs").
		WithArgs(runID).WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("written"))
	mock.ExpectRollback()
	err = service.submit(context.Background(), queuedJob{
		RunID: runID, UserID: "198",
		BriefHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestExpandProjectAttachmentsCreatesValidatedSubjectPatches(t *testing.T) {
	input := Input{
		Candidate: reportbrief.Payload{Workstreams: []reportbrief.Workstream{
			{Subject: "GLM-5.2", Title: "数据生成"}, {Subject: "任务书", Title: "文档更新"},
		}},
		ProjectCandidates: []ProjectCandidate{{
			CanonicalName: "Ai coding 提效支撑", IdentityUsage: "parent_label_for_matching_cues",
			RelatedFactRefs: []string{"fact-001", "fact-002"},
		}},
	}
	decision, err := expandProjectAttachments(reportbrief.ReviewDecision{
		Decision: reportbrief.ReviewDecisionAccept,
		ProjectAttachments: []reportbrief.ReviewProjectAttachment{{
			CanonicalName: "Ai coding 提效支撑", Targets: []string{"w1", "w2"}, FactRefs: []string{"fact-001", "fact-002"},
		}},
	}, input)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != reportbrief.ReviewDecisionRepair || len(decision.Patches) != 2 || decision.Patches[1].Target != "w2" {
		t.Fatalf("project attachments were not expanded: %#v", decision)
	}
}

func TestExpandProjectAttachmentsRejectsUnrelatedEvidence(t *testing.T) {
	_, err := expandProjectAttachments(reportbrief.ReviewDecision{
		Decision: reportbrief.ReviewDecisionAccept,
		ProjectAttachments: []reportbrief.ReviewProjectAttachment{{
			CanonicalName: "AIDA", Targets: []string{"w1"}, FactRefs: []string{"fact-other"},
		}},
	}, Input{
		Candidate:         reportbrief.Payload{Workstreams: []reportbrief.Workstream{{Subject: "CLI"}}},
		ProjectCandidates: []ProjectCandidate{{CanonicalName: "AIDA", IdentityUsage: "parent_label_for_matching_cues", RelatedFactRefs: []string{"fact-aida"}}},
	})
	if err == nil {
		t.Fatal("unrelated project evidence must be rejected")
	}
}

func TestExpandProjectAttachmentsUsesAcceptedResolverProposal(t *testing.T) {
	decision, err := expandProjectAttachments(reportbrief.ReviewDecision{Decision: reportbrief.ReviewDecisionAccept}, Input{
		Candidate: reportbrief.Payload{Workstreams: []reportbrief.Workstream{{Subject: "OSCAR"}, {Subject: "Qwen3"}}},
		ProjectCandidates: []ProjectCandidate{{
			CanonicalName: "KV Cache 压缩算法", IdentityUsage: "parent_label_for_matching_cues",
			ProposedTargets: []string{"w1", "w2"}, RelatedFactRefs: []string{"fact-001"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != reportbrief.ReviewDecisionRepair || len(decision.Patches) != 2 {
		t.Fatalf("accepted resolver proposal was not applied: %#v", decision)
	}
}

func TestExpandProjectAttachmentsRunsDuringUnrelatedRepair(t *testing.T) {
	decision, err := expandProjectAttachments(reportbrief.ReviewDecision{
		Decision: reportbrief.ReviewDecisionRepair,
		Patches:  []reportbrief.ReviewPatch{{Op: "replace_title", Target: "w1", Value: "修正标题", SupportingFactRefs: []string{"fact-001"}}},
	}, Input{
		Candidate: reportbrief.Payload{Workstreams: []reportbrief.Workstream{{Subject: "OSCAR"}, {Subject: "Qwen3"}}},
		ProjectCandidates: []ProjectCandidate{{
			CanonicalName: "KV Cache 压缩算法", IdentityUsage: "parent_label_for_matching_cues",
			ProposedTargets: []string{"w1", "w2"}, RelatedFactRefs: []string{"fact-001"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.ProjectAttachments) != 1 || len(decision.Patches) != 3 {
		t.Fatalf("project grouping was skipped during repair: %#v", decision)
	}
}
