package sessionsync

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
)

func TestFailDigestFinalAttemptPersistsJobAndRevisionTerminalTogether(t *testing.T) {
	database := openDigestTerminalTestDatabase(t)
	ctx := context.Background()
	jobID, revisionID, cleanup := seedLeasedDigestJob(t, database, 5, 5, time.Now().UTC().Add(5*time.Minute))
	defer cleanup()

	repository, err := NewPostgresJobRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	finishedAt := time.Now().UTC()
	result, err := repository.FailDigest(
		ctx, jobID, "digest-terminal-test", finishedAt, time.Minute,
		false, "digest_v2_build_failed", "retryable",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.Terminal || result.RevisionID != revisionID {
		t.Fatalf("failure result = %#v, revision = %s", result, revisionID)
	}

	assertDigestTerminalState(t, database, jobID, revisionID)
}

func TestClaimDigestFinalExpiredLeasePersistsRevisionFailure(t *testing.T) {
	database := openDigestTerminalTestDatabase(t)
	ctx := context.Background()
	jobID, revisionID, cleanup := seedLeasedDigestJob(t, database, 5, 5, time.Now().UTC().Add(-time.Minute))
	defer cleanup()

	repository, err := NewPostgresJobRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := repository.ClaimDigest(
		ctx, "digest-reclaim-test", "interactive", time.Now().UTC(),
		5*time.Minute, 1, JobBuildContentSliceDigestV2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("claimed exhausted jobs = %#v", jobs)
	}

	assertDigestTerminalState(t, database, jobID, revisionID)
}

func TestClaimDigestFinalExpiredLeasePreservesCompletedRevision(t *testing.T) {
	database := openDigestTerminalTestDatabase(t)
	ctx := context.Background()
	jobID, revisionID, cleanup := seedLeasedDigestJob(t, database, 5, 5, time.Now().UTC().Add(-time.Minute))
	defer cleanup()
	if _, err := database.ExecContext(ctx, `
		UPDATE session_slice_digest_revisions
		SET status = 'superseded', superseded_at = now()
		WHERE id = $1`, revisionID,
	); err != nil {
		t.Fatal(err)
	}

	repository, err := NewPostgresJobRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ClaimDigest(
		ctx, "digest-completed-reclaim", "interactive", time.Now().UTC(),
		5*time.Minute, 1, JobBuildContentSliceDigestV2,
	); err != nil {
		t.Fatal(err)
	}

	var jobStatus, revisionStatus string
	if err := database.QueryRowContext(ctx, `
		SELECT j.status, d.status
		FROM session_processing_jobs j
		JOIN session_slice_digest_revisions d ON d.id = j.target_digest_revision_id
		WHERE j.id = $1 AND d.id = $2`, jobID, revisionID,
	).Scan(&jobStatus, &revisionStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "completed" || revisionStatus != "superseded" {
		t.Fatalf("job=%s revision=%s", jobStatus, revisionStatus)
	}
}

func openDigestTerminalTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("AIDA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AIDA_TEST_DATABASE_URL is not set")
	}
	database, err := projectdb.Connect(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := projectdb.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	return database
}

func seedLeasedDigestJob(
	t *testing.T,
	database *sql.DB,
	attempts, maxAttempts int,
	leaseUntil time.Time,
) (jobID, revisionID string, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	userID := time.Now().UnixNano()%100000000 + 760000000
	username := "digest-terminal-" + time.Now().UTC().Format("150405.000000000")
	if _, err := database.ExecContext(ctx, `INSERT INTO users (id, username) VALUES ($1, $2)`, userID, username); err != nil {
		t.Fatal(err)
	}
	cleanup = func() { _, _ = database.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID) }

	var sessionID, sourceID, generationID, projectionID, sliceID string
	if err := database.QueryRowContext(ctx, `
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at)
		VALUES ($1, $2, 'codex', now()) RETURNING id::text`, username, userID,
	).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_sources (session_id, source_role, source_key)
		VALUES ($1, 'main', $2) RETURNING id::text`, sessionID, username,
	).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_source_generations (source_id, status)
		VALUES ($1, 'active') RETURNING id::text`, sourceID,
	).Scan(&generationID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_content_projection_revisions (
			generation_id, content_parser_version, status, source_high_water_cursor
		) VALUES ($1, 'test', 'active', 1) RETURNING id::text`, generationID,
	).Scan(&projectionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_content_slices (
			session_id, source_id, generation_id, start_cursor, end_cursor
		) VALUES ($1, $2, $3, 0, 1) RETURNING id::text`, sessionID, sourceID, generationID,
	).Scan(&sliceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_slice_digest_revisions (
			session_content_slice_id, content_projection_revision_id, generation_id,
			content_epoch, digest_version, redaction_version, status, build_started_at
		) VALUES ($1, $2, $3, 0, 'session-digest/v2.10.0', 'report-redaction/v1', 'building', now())
		RETURNING id::text`, sliceID, projectionID, generationID,
	).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_processing_jobs (
			job_type, session_id, generation_id, target_digest_revision_id,
			content_epoch, status, attempts, max_attempts, lease_owner, lease_until,
			urgency, urgency_raised_at
		) VALUES ($1, $2, $3, $4, 0, 'leased', $5, $6, 'digest-terminal-test', $7,
			'interactive', now()) RETURNING id::text`,
		JobBuildContentSliceDigestV2, sessionID, generationID, revisionID,
		attempts, maxAttempts, leaseUntil,
	).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	return jobID, revisionID, cleanup
}

func assertDigestTerminalState(t *testing.T, database *sql.DB, jobID, revisionID string) {
	t.Helper()
	var jobStatus, revisionStatus, failureClass string
	var completedAt, failedAt sql.NullTime
	if err := database.QueryRow(`
		SELECT j.status, j.completed_at, d.status, d.failure_class, d.failed_at
		FROM session_processing_jobs j
		JOIN session_slice_digest_revisions d ON d.id = j.target_digest_revision_id
		WHERE j.id = $1 AND d.id = $2`, jobID, revisionID,
	).Scan(&jobStatus, &completedAt, &revisionStatus, &failureClass, &failedAt); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "dead" || !completedAt.Valid || revisionStatus != "failed" ||
		failureClass != "retryable" || !failedAt.Valid {
		t.Fatalf("job=%s completed=%v revision=%s class=%s failed=%v",
			jobStatus, completedAt.Valid, revisionStatus, failureClass, failedAt.Valid)
	}
}
