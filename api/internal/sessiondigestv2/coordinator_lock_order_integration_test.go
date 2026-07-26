package sessiondigestv2

import (
	"context"
	"os"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/sessionsync"
)

func TestEnsureDigestLocksSessionBeforeDigestRevision(t *testing.T) {
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
	userID := time.Now().UnixNano()%100000000 + 790000000
	key := "digest-lock-order-" + time.Now().UTC().Format("150405.000000000")
	if _, err := database.ExecContext(ctx, `INSERT INTO users (id, username) VALUES ($1, $2)`, userID, key); err != nil {
		t.Fatal(err)
	}
	defer database.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)

	var sessionID, sourceID, generationID, projectionID, sliceID string
	if err := database.QueryRowContext(ctx, `
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at)
		VALUES ($1, $2, 'codex', now()) RETURNING id::text`, key, userID,
	).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_sources (session_id, source_role, source_key)
		VALUES ($1, 'main', $2) RETURNING id::text`, sessionID, key,
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

	blocker, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if err := sessionsync.LockSessionForUpdate(ctx, blocker, sessionID); err != nil {
		t.Fatal(err)
	}

	coordinator, err := NewCoordinator(database, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	type ensureOutcome struct {
		result EnsureResult
		err    error
	}
	outcomes := make(chan ensureOutcome, 1)
	go func() {
		result, ensureErr := coordinator.EnsureDigest(ctx, DigestIdentity{
			SliceID: sliceID, SessionID: sessionID, GenerationID: generationID,
			ProjectionRevisionID: projectionID, ContentEpoch: 0,
		}, UrgencyBackground)
		outcomes <- ensureOutcome{result: result, err: ensureErr}
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		if err := database.QueryRowContext(ctx, `
			SELECT count(*) FROM pg_stat_activity
			WHERE wait_event_type = 'Lock'
				AND query LIKE '%SELECT id FROM sessions%FOR UPDATE%'`,
		).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("EnsureDigest did not wait on the Session lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	var revisions int
	if err := database.QueryRowContext(ctx, `
		SELECT count(*) FROM session_slice_digest_revisions
		WHERE session_content_slice_id = $1 AND content_projection_revision_id = $2`,
		sliceID, projectionID,
	).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if revisions != 0 {
		t.Fatalf("Digest Revision was locked or created before the Session lock: %d", revisions)
	}

	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case outcome := <-outcomes:
		if outcome.err != nil {
			t.Fatal(outcome.err)
		}
		if outcome.result.State != EnsureWaiting {
			t.Fatalf("EnsureDigest result = %#v", outcome.result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("EnsureDigest did not finish after the Session lock was released")
	}

	var jobs int
	if err := database.QueryRowContext(ctx, `
		SELECT count(*)
		FROM session_processing_jobs j
		JOIN session_slice_digest_revisions d ON d.id = j.target_digest_revision_id
		WHERE d.session_content_slice_id = $1 AND d.content_projection_revision_id = $2`,
		sliceID, projectionID,
	).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 {
		t.Fatalf("Digest jobs = %d, want 1", jobs)
	}
}
