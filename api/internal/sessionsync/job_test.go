package sessionsync

import (
	"testing"
	"time"
)

func TestSES018ExpiredLeaseCanBeReclaimed(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	job := ProcessingJobState{Status: JobPending, MaxAttempts: 3}
	if !job.Lease(now, "worker-a", time.Minute) {
		t.Fatal("first lease failed")
	}
	if job.Lease(now.Add(30*time.Second), "worker-b", time.Minute) {
		t.Fatal("active lease was stolen")
	}
	if !job.Lease(now.Add(time.Minute), "worker-b", time.Minute) {
		t.Fatal("expired lease was not reclaimed")
	}
	if job.Attempts != 2 || job.LeaseOwner != "worker-b" {
		t.Fatalf("job=%+v", job)
	}
}

func TestSES018RetryWaitAndDeadLetterAreDeterministic(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	job := ProcessingJobState{Status: JobPending, MaxAttempts: 2}
	if !job.Lease(now, "worker-a", time.Minute) || !job.Fail("worker-a", now.Add(10*time.Second), time.Minute, "parse failed") {
		t.Fatal("first failure transition failed")
	}
	if job.Status != JobRetryWait || job.NextRetryAt != now.Add(70*time.Second) {
		t.Fatalf("job=%+v", job)
	}
	if job.Lease(now.Add(60*time.Second), "worker-b", time.Minute) {
		t.Fatal("job leased before retry time")
	}
	if !job.Lease(now.Add(70*time.Second), "worker-b", time.Minute) ||
		!job.Fail("worker-b", now.Add(80*time.Second), time.Minute, "parse failed again") {
		t.Fatal("second failure transition failed")
	}
	if job.Status != JobDead || job.Attempts != 2 || job.LastError != "parse failed again" {
		t.Fatalf("job=%+v", job)
	}
	if job.Lease(now.Add(10*time.Minute), "worker-c", time.Minute) {
		t.Fatal("dead job was automatically leased")
	}
}

func TestSES018OnlyLeaseOwnerCanHeartbeatOrComplete(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	job := ProcessingJobState{Status: JobPending, MaxAttempts: 3}
	if !job.Lease(now, "worker-a", time.Minute) {
		t.Fatal("lease failed")
	}
	if job.Heartbeat(now.Add(10*time.Second), "worker-b", time.Minute) || job.Complete("worker-b", now.Add(20*time.Second)) {
		t.Fatal("non-owner changed leased job")
	}
	if !job.Heartbeat(now.Add(10*time.Second), "worker-a", time.Minute) || !job.Complete("worker-a", now.Add(20*time.Second)) {
		t.Fatal("owner could not finish job")
	}
	if job.Status != JobCompleted || job.LeaseOwner != "" {
		t.Fatalf("job=%+v", job)
	}
}
