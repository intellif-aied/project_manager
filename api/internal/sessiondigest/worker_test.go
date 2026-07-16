package sessiondigest

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/aidashboard/api/internal/sessionsync"
)

type digestQueueStub struct {
	jobs          []sessionsync.ProcessingJob
	claimedTypes  []string
	completed     []string
	failed        []string
	failureValues []string
	preserveFlags []bool
}

func (q *digestQueueStub) ClaimTypes(_ context.Context, _ string, _ time.Time, _ time.Duration, _ int, types []string) ([]sessionsync.ProcessingJob, error) {
	q.claimedTypes = append([]string(nil), types...)
	return q.jobs, nil
}
func (q *digestQueueStub) Heartbeat(context.Context, string, string, time.Time, time.Duration) (bool, error) {
	return true, nil
}
func (q *digestQueueStub) Complete(_ context.Context, id, _ string, _ time.Time) (bool, error) {
	q.completed = append(q.completed, id)
	return true, nil
}
func (q *digestQueueStub) Fail(_ context.Context, id, _ string, _ time.Time, _ time.Duration, preserve bool, failure string) (bool, error) {
	q.failed = append(q.failed, id)
	q.failureValues = append(q.failureValues, failure)
	q.preserveFlags = append(q.preserveFlags, preserve)
	return true, nil
}

func TestDigestWorkerPreservesAttemptWhenRevisionStateCannotBePersisted(t *testing.T) {
	queue := &digestQueueStub{jobs: []sessionsync.ProcessingJob{{
		ID: "persistence", Type: JobType, Attempts: 5, MaxAttempts: 5,
	}}}
	worker, err := NewWorker(queue, digestProcessorStub{failures: map[string]error{
		"persistence": ErrDigestStatePersistence,
	}}, "test-worker")
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if len(queue.preserveFlags) != 1 || !queue.preserveFlags[0] || queue.failureValues[0] != "digest_build_failed" {
		t.Fatalf("state-persistence retry consumed its final attempt: preserve=%#v failure=%#v", queue.preserveFlags, queue.failureValues)
	}
}

type digestProcessorStub struct{ failures map[string]error }

func (p digestProcessorStub) Process(_ context.Context, job sessionsync.ProcessingJob) error {
	return p.failures[job.ID]
}

func TestDigestWorkerClaimsOnlyDigestJobsAndSanitizesFailures(t *testing.T) {
	queue := &digestQueueStub{jobs: []sessionsync.ProcessingJob{
		{ID: "ready", Type: JobType, Attempts: 1, MaxAttempts: 5},
		{ID: "stale", Type: JobType, Attempts: 1, MaxAttempts: 5},
		{ID: "failed", Type: JobType, Attempts: 1, MaxAttempts: 5, TargetDigestRevisionID: sql.NullString{String: "revision", Valid: true}},
	}}
	processor := digestProcessorStub{failures: map[string]error{
		"stale":  ErrStaleDigestSource,
		"failed": errors.New("database error containing Authorization: Bearer super-secret"),
	}}
	worker, err := NewWorker(queue, processor, "test-worker")
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if len(queue.claimedTypes) != 1 || queue.claimedTypes[0] != JobType {
		t.Fatalf("unexpected claimed types: %#v", queue.claimedTypes)
	}
	if len(queue.completed) != 2 || queue.completed[0] != "ready" || queue.completed[1] != "stale" {
		t.Fatalf("unexpected completions: %#v", queue.completed)
	}
	if len(queue.failed) != 1 || queue.failed[0] != "failed" || len(queue.failureValues) != 1 || queue.failureValues[0] != "digest_build_failed" {
		t.Fatalf("failure was not sanitized: ids=%#v values=%#v", queue.failed, queue.failureValues)
	}
}
