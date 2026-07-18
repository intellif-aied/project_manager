package reportsourcecatalog

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
)

func TestBackfillBatchDiscoversHistoricalSliceAndIsIdempotentIntegration(t *testing.T) {
	database := openCatalogIntegrationDatabase(t)
	const userID int64 = 990041
	cleanupCatalogIntegrationUser(t, database, userID)
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, 'catalog-backfill-test')`, userID); err != nil {
		t.Fatal(err)
	}
	defer cleanupCatalogIntegrationUser(t, database, userID)

	var sessionID, sourceID, generationID, revisionID, chunkID, sliceID string
	if err := database.QueryRow(`
		INSERT INTO sessions (
			session_ref, user_id, agent_type, started_at, last_activity_at,
			cwd, models, content_status
		) VALUES (
			'catalog-history', $1, 'codex', '2026-07-17T01:00:00Z',
			'2026-07-17T01:01:00Z', '/workspace/catalog', ARRAY['gpt-test'], 'available'
		) RETURNING id`, userID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_sources (session_id, source_role, source_key)
		VALUES ($1, 'main', 'codex:catalog-history:main') RETURNING id`, sessionID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_source_generations (source_id, status, finalized_at)
		VALUES ($1, 'active', now()) RETURNING id`, sourceID).Scan(&generationID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_content_projection_revisions (
			generation_id, content_parser_version, status, content_indexed_cursor,
			source_high_water_cursor, event_count, validated_at, activated_at
		) VALUES ($1, 'session-content-v2', 'active', 10, 10, 2, now(), now())
		RETURNING id`, generationID).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE session_sources
		SET active_generation_id = $1, active_content_projection_revision_id = $2
		WHERE id = $3`, generationID, revisionID, sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_upload_chunks (
			generation_id, start_cursor, end_cursor, start_line, end_line,
			content_sha256, content_epoch, raw_object_key, object_status,
			content_index_status
		) VALUES ($1, 0, 10, 1, 2, repeat('0', 64), 0, 'catalog/test', 'available', 'indexed')
		RETURNING id`, generationID).Scan(&chunkID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO session_content_events (
			content_projection_revision_id, chunk_id, source_start_cursor,
			source_end_cursor, occurred_at, event_type, summary, content_sha256
		) VALUES
			($1, $2, 0, 5, '2026-07-17T01:00:00Z', 'event_msg.user_message', 'first user', repeat('1', 64)),
			($1, $2, 5, 10, '2026-07-17T01:01:00Z', 'event_msg.agent_message', 'assistant', repeat('2', 64))`,
		revisionID, chunkID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_content_slices (
			session_id, source_id, generation_id, start_cursor, end_cursor
		) VALUES ($1, $2, $3, 0, 10) RETURNING id`,
		sessionID, sourceID, generationID).Scan(&sliceID); err != nil {
		t.Fatal(err)
	}

	if count, err := ReconcileRevision(context.Background(), database, "", 10); err != nil || count != 0 {
		t.Fatalf("online reconcile discovered historical row: count=%d err=%v", count, err)
	}
	before, err := InspectBackfill(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	if before.Missing != 1 || before.Ready != 0 {
		t.Fatalf("before=%+v", before)
	}
	if count, err := RunBackfillBatch(context.Background(), database, 1); err != nil || count != 1 {
		t.Fatalf("backfill count=%d err=%v", count, err)
	}
	var status, summary string
	var eventCount int64
	var start, end time.Time
	if err := database.QueryRow(`
		SELECT status, event_count, activity_start_at, activity_end_at, summary
		FROM report_source_slice_catalog
		WHERE slice_id = $1 AND content_projection_revision_id = $2`, sliceID, revisionID).Scan(
		&status, &eventCount, &start, &end, &summary,
	); err != nil {
		t.Fatal(err)
	}
	if status != StatusReady || eventCount != 2 || summary != "first user" ||
		!start.Equal(time.Date(2026, 7, 17, 1, 0, 0, 0, time.UTC)) ||
		!end.Equal(time.Date(2026, 7, 17, 1, 1, 0, 0, time.UTC)) {
		t.Fatalf("catalog status=%s events=%d range=%s..%s summary=%q", status, eventCount, start, end, summary)
	}
	if count, err := RunBackfillBatch(context.Background(), database, 1); err != nil || count != 0 {
		t.Fatalf("repeated backfill count=%d err=%v", count, err)
	}
	after, err := InspectBackfill(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	if after.Missing != 0 || after.Building != 0 || after.Ready != 1 || after.Empty != 0 || after.Failed != 0 {
		t.Fatalf("after=%+v", after)
	}
}

func openCatalogIntegrationDatabase(t *testing.T) *sql.DB {
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

func cleanupCatalogIntegrationUser(t *testing.T, database *sql.DB, userID int64) {
	t.Helper()
	_, _ = database.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID)
	_, _ = database.Exec(`DELETE FROM users WHERE id = $1`, userID)
}
