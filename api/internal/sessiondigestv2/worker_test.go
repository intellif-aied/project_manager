package sessiondigestv2

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aidashboard/api/internal/sessionsync"
)

type queueStub struct {
	jobs         []sessionsync.ProcessingJob
	claimedTypes []string
	claimLimit   int
	completed    []string
	failed       []string
	failures     []string
}

func (q *queueStub) ClaimTypes(
	_ context.Context,
	_ string,
	_ time.Time,
	_ time.Duration,
	limit int,
	types []string,
) ([]sessionsync.ProcessingJob, error) {
	q.claimLimit = limit
	q.claimedTypes = append([]string(nil), types...)
	return q.jobs, nil
}

func (q *queueStub) Heartbeat(
	context.Context, string, string, time.Time, time.Duration,
) (bool, error) {
	return true, nil
}

func (q *queueStub) Complete(
	_ context.Context, id, _ string, _ time.Time,
) (bool, error) {
	q.completed = append(q.completed, id)
	return true, nil
}

func (q *queueStub) Fail(
	_ context.Context,
	id, _ string,
	_ time.Time,
	_ time.Duration,
	_ bool,
	failure string,
) (bool, error) {
	q.failed = append(q.failed, id)
	q.failures = append(q.failures, failure)
	return true, nil
}

type processorStub struct {
	failures map[string]error
}

func (p processorStub) Process(
	_ context.Context,
	job sessionsync.ProcessingJob,
) error {
	return p.failures[job.ID]
}

func TestWorkerClaimsOnlyOneDigestV2Job(t *testing.T) {
	queue := &queueStub{jobs: []sessionsync.ProcessingJob{
		{ID: "ready", Type: JobType, Attempts: 1, MaxAttempts: 5},
		{ID: "stale", Type: JobType, Attempts: 1, MaxAttempts: 5},
		{ID: "failed", Type: JobType, Attempts: 1, MaxAttempts: 5},
	}}
	worker, err := NewWorker(queue, processorStub{failures: map[string]error{
		"stale":  ErrStaleDigestSource,
		"failed": errors.New("Authorization: Bearer should-not-leak"),
	}}, "test-v2-worker", DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if queue.claimLimit != 1 ||
		len(queue.claimedTypes) != 1 || queue.claimedTypes[0] != JobType {
		t.Fatalf("unexpected claim: limit=%d types=%v", queue.claimLimit, queue.claimedTypes)
	}
	if len(queue.completed) != 2 ||
		queue.completed[0] != "ready" || queue.completed[1] != "stale" {
		t.Fatalf("unexpected completions: %v", queue.completed)
	}
	if len(queue.failed) != 1 || queue.failed[0] != "failed" ||
		len(queue.failures) != 1 || queue.failures[0] != "digest_v2_build_failed" {
		t.Fatalf("failure was not sanitized: ids=%v values=%v", queue.failed, queue.failures)
	}
}

func TestWorkerRejectsBatchLargerThanOne(t *testing.T) {
	config := DefaultConfig()
	config.WorkerBatch = 2
	if _, err := NewWorker(
		&queueStub{}, processorStub{}, "test-v2-worker", config,
	); err == nil {
		t.Fatal("worker batch larger than one must be rejected")
	}
}
