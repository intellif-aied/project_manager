package contentinventory

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/contentreader"
	"github.com/aidashboard/api/internal/sessionsync"
)

type inventoryObjectStore map[string][]byte

func (store inventoryObjectStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	content, ok := store[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func TestAuditorPostgresReaderIntegration(t *testing.T) {
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

	content := []byte(
		"{\"type\":\"user\",\"timestamp\":\"2026-07-18T01:00:00Z\",\"payload\":{\"message\":\"inventory\"}}\n",
	)
	parsed, err := sessionsync.ParseContentChunk(bytes.NewReader(content), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	userID := time.Now().UnixNano()%100000000 + 760000000
	revisionID := createInventoryFixture(t, database, userID, content, parsed)
	defer database.Exec(`DELETE FROM users WHERE id = $1`, userID)
	store := inventoryObjectStore{"inventory/chunk.jsonl": content}
	reader, err := contentreader.New(database, store)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := New(database, reader)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	plan, err := auditor.Run(ctx, Options{Action: ActionPlan})
	if err != nil {
		t.Fatal(err)
	}
	if plan.EligibleRevisions != 1 || plan.SnapshotThroughID != revisionID {
		t.Fatalf("plan=%+v", plan)
	}
	report, err := auditor.Run(ctx, Options{
		Action: ActionScan, SnapshotThroughID: plan.SnapshotThroughID,
		Limit: 1, PerRevisionTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Complete || report.SucceededRevisions != 1 || report.FailedRevisions != 0 ||
		report.ValidatedObjects != 1 || report.ValidatedEvents != int64(len(parsed.Events)) ||
		report.ValidatedBytes != int64(len(content)) {
		t.Fatalf("scan=%+v", report)
	}

	store["inventory/chunk.jsonl"] = []byte("corrupt\n")
	retry, err := auditor.Run(ctx, Options{
		Action: ActionScan, OnlyRevisionID: revisionID, PerRevisionTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if retry.Complete || retry.FailedRevisions != 1 || len(retry.Failures) != 1 {
		t.Fatalf("corrupt retry=%+v", retry)
	}
}

func createInventoryFixture(
	t *testing.T,
	database *sql.DB,
	userID int64,
	content []byte,
	parsed sessionsync.ContentParseResult,
) string {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, $2)`,
		userID, fmt.Sprintf("content-inventory-%d", userID)); err != nil {
		t.Fatal(err)
	}
	var sessionID, sourceID, generationID, chunkID, revisionID string
	if err := database.QueryRow(`
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at, content_status)
		VALUES ($1, $2, 'codex', now(), 'available') RETURNING id`,
		fmt.Sprintf("inventory-%d", userID), userID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_sources (session_id, source_role, source_key)
		VALUES ($1, 'main', $2) RETURNING id`, sessionID,
		fmt.Sprintf("codex:inventory-%d:main", userID)).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_source_generations (
			source_id, status, expected_cursor, prefix_checkpoint_hash,
			prefix_checkpoint_state, prefix_checkpoint_state_format
		) VALUES ($1, 'active', $2, $3, $4, $5) RETURNING id`,
		sourceID, len(content), sessionsync.HashBytes(content), []byte{1},
		sessionsync.PrefixCheckpointStateFormat).Scan(&generationID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_upload_chunks (
			generation_id, start_cursor, end_cursor, start_line, end_line,
			content_sha256, content_epoch, raw_object_key, object_status,
			content_index_status
		) VALUES ($1, 0, $2, 1, 1, $3, 0, 'inventory/chunk.jsonl',
			'available', 'indexed') RETURNING id`, generationID, len(content),
		sessionsync.HashBytes(content)).Scan(&chunkID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_content_projection_revisions (
			generation_id, content_parser_version, status, build_start_cursor,
			content_indexed_cursor, source_high_water_cursor, event_count, activated_at
		) VALUES ($1, $2, 'active', 0, $3, $3, $4, now()) RETURNING id`,
		generationID, sessionsync.ContentParserVersion, len(content), len(parsed.Events)).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	for _, event := range parsed.Events {
		if _, err := database.Exec(`
			INSERT INTO session_content_events (
				content_projection_revision_id, chunk_id, source_start_cursor,
				source_end_cursor, occurred_at, event_type, summary, excerpt,
				content_sha256
			) VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), $9)`,
			revisionID, chunkID, event.SourceStartCursor, event.SourceEndCursor,
			event.OccurredAt, event.EventType, event.Summary, event.Excerpt,
			event.ContentSHA256); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`
		UPDATE session_sources
		SET active_generation_id = $2, active_content_projection_revision_id = $3
		WHERE id = $1`, sourceID, generationID, revisionID); err != nil {
		t.Fatal(err)
	}
	return revisionID
}
