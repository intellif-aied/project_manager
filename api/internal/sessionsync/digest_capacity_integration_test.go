package sessionsync

import (
	"context"
	"os"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
)

func TestClaimDigestInteractiveIsolatedFromFiveThousandBackgroundJobs(t *testing.T) {
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
	userID := time.Now().UnixNano()%100000000 + 670000000
	if _, err := database.ExecContext(ctx, `INSERT INTO users (id, username) VALUES ($1, $2)`, userID, "digest-capacity"); err != nil {
		t.Fatal(err)
	}
	defer database.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	var sessionID string
	if err := database.QueryRowContext(ctx, `
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at)
		VALUES ($1, $2, 'codex', now()) RETURNING id::text`,
		"digest-capacity-session", userID,
	).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO session_processing_jobs (
			job_type, session_id, content_epoch, urgency, created_at
		)
		SELECT 'build_content_slice_digest_v2', $1, 0, 'background', now() - interval '1 hour'
		FROM generate_series(1, 5000)`, sessionID); err != nil {
		t.Fatal(err)
	}
	var interactiveID string
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_processing_jobs (
			job_type, session_id, content_epoch, urgency, urgency_raised_at
		) VALUES ('build_content_slice_digest_v2', $1, 0, 'interactive', now())
		RETURNING id::text`, sessionID,
	).Scan(&interactiveID); err != nil {
		t.Fatal(err)
	}
	repository, err := NewPostgresJobRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	jobs, err := repository.ClaimDigest(
		ctx, "capacity-test:interactive:1", "interactive", now, 5*time.Minute, 1,
		JobBuildContentSliceDigestV2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != interactiveID || jobs[0].Urgency != "interactive" {
		t.Fatalf("claimed jobs = %#v, interactive ID = %s", jobs, interactiveID)
	}
	var backgroundPending int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM session_processing_jobs
		WHERE session_id = $1 AND urgency = 'background' AND status = 'pending'`, sessionID,
	).Scan(&backgroundPending); err != nil {
		t.Fatal(err)
	}
	if backgroundPending != 5000 {
		t.Fatalf("background pending = %d, want 5000", backgroundPending)
	}
}
