package sessionsync

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeContentJobQueue struct {
	jobs         []ProcessingJob
	claimedTypes []string
	completed    []string
	failed       []string
	retryDelays  []time.Duration
	preserved    []bool
}

func (q *fakeContentJobQueue) Heartbeat(_ context.Context, _, _ string, _ time.Time, _ time.Duration) (bool, error) {
	return true, nil
}

func (q *fakeContentJobQueue) ClaimTypes(_ context.Context, _ string, _ time.Time, _ time.Duration, _ int, types []string) ([]ProcessingJob, error) {
	q.claimedTypes = append([]string(nil), types...)
	return q.jobs, nil
}

func (q *fakeContentJobQueue) Complete(_ context.Context, jobID, _ string, _ time.Time) (bool, error) {
	q.completed = append(q.completed, jobID)
	return true, nil
}

func (q *fakeContentJobQueue) Fail(_ context.Context, jobID, _ string, _ time.Time, retry time.Duration, preserveAttempt bool, _ string) (bool, error) {
	q.failed = append(q.failed, jobID)
	q.retryDelays = append(q.retryDelays, retry)
	q.preserved = append(q.preserved, preserveAttempt)
	return true, nil
}

type fakeContentJobProcessor map[string]error

func (p fakeContentJobProcessor) Process(_ context.Context, job ProcessingJob) error {
	return p[job.ID]
}

func TestContentProjectionWorkerOnlyClaimsContentJobsAndHandlesStale(t *testing.T) {
	queue := &fakeContentJobQueue{jobs: []ProcessingJob{
		{ID: "success", Type: JobIndexContentChunk, Attempts: 1},
		{ID: "stale", Type: JobIndexContentChunk, Attempts: 1},
		{ID: "later", Type: JobRebuildContentRevision, Attempts: 2},
	}}
	processor := fakeContentJobProcessor{
		"stale": ErrStaleContentEpoch,
		"later": ErrProjectionOutOfOrder,
	}
	worker, err := NewContentProjectionWorker(queue, processor, "worker-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(queue.claimedTypes) != 2 || queue.claimedTypes[0] != JobIndexContentChunk || queue.claimedTypes[1] != JobRebuildContentRevision {
		t.Fatalf("claimed types=%v", queue.claimedTypes)
	}
	if len(queue.completed) != 2 || len(queue.failed) != 1 || queue.failed[0] != "later" ||
		queue.retryDelays[0] != 2*time.Second || !queue.preserved[0] {
		t.Fatalf("completed=%v failed=%v delays=%v preserved=%v", queue.completed, queue.failed, queue.retryDelays, queue.preserved)
	}
}

func TestContentJobRetryDelayIsBounded(t *testing.T) {
	if got := contentJobRetryDelay(100, errors.New("failed")); got != 64*time.Second {
		t.Fatalf("retry delay=%s", got)
	}
}
