package usage

import (
	"context"
	"testing"
	"time"

	"github.com/aidashboard/api/internal/sessionsync"
)

type fakeUsageQueue struct {
	jobs         []sessionsync.ProcessingJob
	claimedTypes []string
	completed    []string
	failed       []string
	retryDelays  []time.Duration
}

func (queue *fakeUsageQueue) ClaimTypes(_ context.Context, _ string, _ time.Time, _ time.Duration, _ int, types []string) ([]sessionsync.ProcessingJob, error) {
	queue.claimedTypes = append([]string(nil), types...)
	return queue.jobs, nil
}

func (queue *fakeUsageQueue) Heartbeat(_ context.Context, _, _ string, _ time.Time, _ time.Duration) (bool, error) {
	return true, nil
}

func (queue *fakeUsageQueue) Complete(_ context.Context, jobID, _ string, _ time.Time) (bool, error) {
	queue.completed = append(queue.completed, jobID)
	return true, nil
}

func (queue *fakeUsageQueue) Fail(_ context.Context, jobID, _ string, _ time.Time, retry time.Duration, _ string) (bool, error) {
	queue.failed = append(queue.failed, jobID)
	queue.retryDelays = append(queue.retryDelays, retry)
	return true, nil
}

type fakeUsageProcessor map[string]error

func (processor fakeUsageProcessor) Process(_ context.Context, job sessionsync.ProcessingJob) error {
	return processor[job.ID]
}

func TestUsageWorkerOnlyClaimsUsageJobs(t *testing.T) {
	queue := &fakeUsageQueue{jobs: []sessionsync.ProcessingJob{
		{ID: "parsed", Type: sessionsync.JobParseUsageChunk},
		{ID: "waiting", Type: sessionsync.JobRebuildMetricsRevision, Attempts: 2},
	}}
	worker, err := NewWorker(queue, fakeUsageProcessor{"waiting": ErrUsageOutOfOrder}, "usage-worker-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	wantTypes := []string{sessionsync.JobParseUsageChunk, sessionsync.JobRebuildMetricsRevision}
	if len(queue.claimedTypes) != len(wantTypes) || queue.claimedTypes[0] != wantTypes[0] || queue.claimedTypes[1] != wantTypes[1] {
		t.Fatalf("claimed types=%v", queue.claimedTypes)
	}
	if len(queue.completed) != 1 || queue.completed[0] != "parsed" ||
		len(queue.failed) != 1 || queue.failed[0] != "waiting" || queue.retryDelays[0] != 2*time.Second {
		t.Fatalf("completed=%v failed=%v delays=%v", queue.completed, queue.failed, queue.retryDelays)
	}
}

func TestMeteringWorkerOnlyClaimsLifecycleJobs(t *testing.T) {
	queue := &fakeUsageQueue{jobs: []sessionsync.ProcessingJob{
		{ID: "envelope", Type: sessionsync.JobBuildMeteringEnvelope},
		{ID: "delete", Type: sessionsync.JobDeleteObject},
	}}
	worker, err := NewMeteringWorker(queue, fakeUsageProcessor{}, "metering-worker-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	wantTypes := []string{sessionsync.JobBuildMeteringEnvelope, sessionsync.JobDeleteObject}
	if len(queue.claimedTypes) != 2 || queue.claimedTypes[0] != wantTypes[0] || queue.claimedTypes[1] != wantTypes[1] {
		t.Fatalf("claimed types=%v", queue.claimedTypes)
	}
	if len(queue.completed) != 2 || len(queue.failed) != 0 {
		t.Fatalf("completed=%v failed=%v", queue.completed, queue.failed)
	}
}
