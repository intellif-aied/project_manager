package sessiondigestv2

import (
	"context"
	"os"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/sessionsync"
)

func TestControlledRebuildKeepsFailedRevisionWhilePreviousJobIsActive(t *testing.T) {
	databaseURL := os.Getenv("AIDA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AIDA_TEST_DATABASE_URL is not set")
	}
	database, err := projectdb.Connect(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := projectdb.RunMigrations(database); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	userID := time.Now().UnixNano()%100000000 + 780000000
	username := "digest-rebuild-active-" + time.Now().UTC().Format("150405.000000000")
	if _, err := database.ExecContext(ctx, `INSERT INTO users (id, username) VALUES ($1, $2)`, userID, username); err != nil {
		t.Fatal(err)
	}
	defer database.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)

	var sessionID, sourceID, generationID, projectionID, sliceID, revisionID, runID string
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
	failedAt := time.Now().UTC().Add(-time.Hour)
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_slice_digest_revisions (
			session_content_slice_id, content_projection_revision_id, generation_id,
			content_epoch, digest_version, redaction_version, status,
			error_code, failure_class, failed_at
		) VALUES ($1, $2, $3, 0, $4, $5, 'failed',
			'digest_v2_build_failed', 'retryable', $6) RETURNING id::text`,
		sliceID, projectionID, generationID, Version, RedactionVersion, failedAt,
	).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO session_processing_jobs (
			job_type, session_id, generation_id, target_digest_revision_id,
			content_epoch, status, attempts, max_attempts, lease_owner, lease_until,
			urgency
		) VALUES ($1, $2, $3, $4, 0, 'leased', 5, 5,
			'previous-worker', now() + interval '5 minutes', 'background')`,
		JobType, sessionID, generationID, revisionID,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO ai_runs (
			user_id, business_type, runtime_type, agent_id, status, input_ref_json
		) VALUES ($1, 'report_agent_run', 'managed_session', 'agent-test', 'pending', '{}')
		RETURNING id::text`, userID,
	).Scan(&runID); err != nil {
		t.Fatal(err)
	}

	coordinator, err := NewCoordinator(database, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.EnsureDigest(ctx, DigestIdentity{
		SliceID: sliceID, SessionID: sessionID, GenerationID: generationID,
		ProjectionRevisionID: projectionID, ContentEpoch: 0,
		RunID: runID, RunCreatedAt: failedAt.Add(time.Minute),
	}, UrgencyInteractive)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != EnsureWaiting {
		t.Fatalf("ensure result = %#v", result)
	}

	var revisionStatus, urgency string
	var rebuildCount int
	if err := database.QueryRowContext(ctx, `
		SELECT d.status, d.rebuild_count, j.urgency
		FROM session_slice_digest_revisions d
		JOIN session_processing_jobs j ON j.target_digest_revision_id = d.id
		WHERE d.id = $1 AND j.status = 'leased'`, revisionID,
	).Scan(&revisionStatus, &rebuildCount, &urgency); err != nil {
		t.Fatal(err)
	}
	if revisionStatus != "failed" || rebuildCount != 0 || urgency != UrgencyInteractive {
		t.Fatalf("revision=%s rebuild_count=%d urgency=%s", revisionStatus, rebuildCount, urgency)
	}

	repository, err := sessionsync.NewPostgresJobRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ClaimDigest(
		ctx, "reclaim-after-rebuild-check", UrgencyInteractive,
		time.Now().UTC().Add(10*time.Minute), 5*time.Minute, 1, JobType,
	); err != nil {
		t.Fatal(err)
	}
	var jobStatus string
	if err := database.QueryRowContext(ctx, `
		SELECT status FROM session_processing_jobs
		WHERE target_digest_revision_id = $1`, revisionID,
	).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "dead" {
		t.Fatalf("expired previous job status = %s", jobStatus)
	}
}
