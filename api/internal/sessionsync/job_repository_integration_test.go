package sessionsync

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestPostgresJobRepositoryLeaseDependencyRetryAndDeadIntegration(t *testing.T) {
	database := openSyncIntegrationDatabase(t)
	userID := int64(990012)
	cleanupSyncIntegrationUser(t, database, userID)
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, 'v2-job-repository-test')`, userID); err != nil {
		t.Fatal(err)
	}
	defer cleanupSyncIntegrationUser(t, database, userID)

	var sessionID string
	if err := database.QueryRow(`
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at)
		VALUES ('job-repository', $1, 'codex', now()) RETURNING id`, userID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	var jobID string
	if err := database.QueryRow(`
		INSERT INTO session_processing_jobs (
			job_type, session_id, content_epoch, max_attempts
		) VALUES ('purge_session', $1, 0, 2) RETURNING id`, sessionID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}

	repository, err := NewPostgresJobRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	jobs, err := repository.ClaimTypes(context.Background(), "worker-a", now, time.Minute, 10, []string{"purge_session"})
	if err != nil || len(jobs) != 1 || jobs[0].ID != jobID || jobs[0].Attempts != 1 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	if ok, err := repository.Heartbeat(context.Background(), jobID, "worker-b", now.Add(10*time.Second), time.Minute); err != nil || ok {
		t.Fatalf("non-owner heartbeat ok=%v err=%v", ok, err)
	}
	if ok, err := repository.Fail(context.Background(), jobID, "worker-a", now.Add(10*time.Second), time.Minute, true, "dependency pending"); err != nil || !ok {
		t.Fatalf("dependency retry ok=%v err=%v", ok, err)
	}
	if jobs, err := repository.ClaimTypes(context.Background(), "worker-b", now.Add(30*time.Second), time.Minute, 10, []string{"purge_session"}); err != nil || len(jobs) != 0 {
		t.Fatalf("early retry jobs=%+v err=%v", jobs, err)
	}
	jobs, err = repository.ClaimTypes(context.Background(), "worker-b", now.Add(70*time.Second), time.Minute, 10, []string{"purge_session"})
	if err != nil || len(jobs) != 1 || jobs[0].Attempts != 1 {
		t.Fatalf("retry jobs=%+v err=%v", jobs, err)
	}
	if ok, err := repository.Fail(context.Background(), jobID, "worker-b", now.Add(80*time.Second), time.Minute, false, "first failure"); err != nil || !ok {
		t.Fatalf("first failure ok=%v err=%v", ok, err)
	}
	jobs, err = repository.ClaimTypes(context.Background(), "worker-c", now.Add(150*time.Second), time.Minute, 10, []string{"purge_session"})
	if err != nil || len(jobs) != 1 || jobs[0].Attempts != 2 {
		t.Fatalf("second claim jobs=%+v err=%v", jobs, err)
	}
	if ok, err := repository.Fail(context.Background(), jobID, "worker-c", now.Add(160*time.Second), time.Minute, false, "second failure"); err != nil || !ok {
		t.Fatalf("second failure ok=%v err=%v", ok, err)
	}
	var status, lastError string
	var attempts int
	if err := database.QueryRow(`SELECT status, attempts, last_error FROM session_processing_jobs WHERE id = $1`, jobID).Scan(&status, &attempts, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != "dead" || attempts != 2 || lastError != "second failure" {
		t.Fatalf("status=%s attempts=%d error=%s", status, attempts, lastError)
	}
	if jobs, err := repository.ClaimTypes(context.Background(), "worker-d", now.Add(time.Hour), time.Minute, 10, []string{"purge_session"}); err != nil || len(jobs) != 0 {
		t.Fatalf("dead jobs=%+v err=%v", jobs, err)
	}
}

func TestPostgresJobRepositoryExpiredLeaseIsReclaimedOnce(t *testing.T) {
	database := openSyncIntegrationDatabase(t)
	userID := int64(990013)
	cleanupSyncIntegrationUser(t, database, userID)
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, 'v2-job-reclaim-test')`, userID); err != nil {
		t.Fatal(err)
	}
	defer cleanupSyncIntegrationUser(t, database, userID)

	var sessionID string
	if err := database.QueryRow(`
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at)
		VALUES ('job-reclaim', $1, 'codex', now()) RETURNING id`, userID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO session_processing_jobs (job_type, session_id, content_epoch)
		VALUES ('purge_session', $1, 0)`, sessionID); err != nil {
		t.Fatal(err)
	}
	repository, _ := NewPostgresJobRepository(database)
	now := time.Date(2026, 7, 14, 13, 0, 0, 0, time.UTC)
	first, err := repository.Claim(context.Background(), "worker-a", now, time.Minute, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}

	results := make(chan []ProcessingJob, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(index int) {
			jobs, claimErr := repository.Claim(context.Background(), fmt.Sprintf("worker-%d", index), now.Add(time.Minute), time.Minute, 1)
			results <- jobs
			errs <- claimErr
		}(i)
	}
	claimed := 0
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		claimed += len(<-results)
	}
	if claimed != 1 {
		t.Fatalf("expired lease claimed %d times", claimed)
	}
}

func TestPostgresJobRepositoryReturnsClaimsInDeterministicOrder(t *testing.T) {
	database := openSyncIntegrationDatabase(t)
	userID := int64(990032)
	cleanupSyncIntegrationUser(t, database, userID)
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, 'v2-job-order-test')`, userID); err != nil {
		t.Fatal(err)
	}
	defer cleanupSyncIntegrationUser(t, database, userID)

	var sessionID string
	if err := database.QueryRow(`
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at)
		VALUES ('job-order', $1, 'codex', now()) RETURNING id`, userID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 14, 14, 0, 0, 0, time.UTC)
	want := make([]string, 0, 3)
	for index := 0; index < 3; index++ {
		var jobID string
		if err := database.QueryRow(`
			INSERT INTO session_processing_jobs (job_type, session_id, content_epoch, created_at)
			VALUES ('purge_session', $1, 0, $2) RETURNING id`, sessionID, base.Add(time.Duration(index)*time.Second)).Scan(&jobID); err != nil {
			t.Fatal(err)
		}
		want = append(want, jobID)
	}
	repository, _ := NewPostgresJobRepository(database)
	jobs, err := repository.Claim(context.Background(), "worker-order", base.Add(time.Minute), time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != len(want) {
		t.Fatalf("jobs=%d want=%d", len(jobs), len(want))
	}
	for index := range want {
		if jobs[index].ID != want[index] {
			t.Fatalf("job[%d]=%s want=%s", index, jobs[index].ID, want[index])
		}
	}
}
