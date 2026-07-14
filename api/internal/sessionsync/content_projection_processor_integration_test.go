package sessionsync

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"
)

type memoryContentStore map[string][]byte

func (s memoryContentStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	content, ok := s[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func TestContentProjectionProcessorOrdersAndActivatesIntegration(t *testing.T) {
	database := openSyncIntegrationDatabase(t)
	userID := int64(990014)
	fixture := createProjectionFixture(t, database, userID, "projection-order")
	defer cleanupSyncIntegrationUser(t, database, userID)
	processor, err := NewContentProjectionProcessor(database, fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	if err := processor.Process(context.Background(), fixture.jobs[1]); !errors.Is(err, ErrProjectionOutOfOrder) {
		t.Fatalf("out-of-order err=%v", err)
	}
	for _, job := range fixture.jobs {
		if err := processor.Process(context.Background(), job); err != nil {
			t.Fatalf("process chunk %s: %v", job.ChunkID.String, err)
		}
	}
	if err := processor.Process(context.Background(), fixture.jobs[0]); err != nil {
		t.Fatalf("duplicate process: %v", err)
	}
	if err := processor.Process(context.Background(), fixture.activationJob); err != nil {
		t.Fatalf("activate: %v", err)
	}
	var status string
	var cursor, eventCount, rows int64
	if err := database.QueryRow(`
		SELECT status, content_indexed_cursor, event_count,
			(SELECT COUNT(*) FROM session_content_events WHERE content_projection_revision_id = $1)
		FROM session_content_projection_revisions WHERE id = $1`, fixture.revisionID).Scan(
		&status, &cursor, &eventCount, &rows,
	); err != nil {
		t.Fatal(err)
	}
	if status != "active" || cursor != fixture.endCursor || eventCount != 2 || rows != 2 {
		t.Fatalf("status=%s cursor=%d eventCount=%d rows=%d", status, cursor, eventCount, rows)
	}
}

func TestContentProjectionProcessorRejectsStaleEpochIntegration(t *testing.T) {
	database := openSyncIntegrationDatabase(t)
	userID := int64(990015)
	fixture := createProjectionFixture(t, database, userID, "projection-stale")
	defer cleanupSyncIntegrationUser(t, database, userID)
	if _, err := database.Exec(`UPDATE sessions SET content_epoch = 1 WHERE id = $1`, fixture.sessionID); err != nil {
		t.Fatal(err)
	}
	processor, _ := NewContentProjectionProcessor(database, fixture.store)
	if err := processor.Process(context.Background(), fixture.jobs[0]); !errors.Is(err, ErrStaleContentEpoch) {
		t.Fatalf("stale epoch err=%v", err)
	}
	var cursor, eventCount int64
	if err := database.QueryRow(`
		SELECT content_indexed_cursor, event_count
		FROM session_content_projection_revisions WHERE id = $1`, fixture.revisionID).Scan(&cursor, &eventCount); err != nil {
		t.Fatal(err)
	}
	if cursor != 0 || eventCount != 0 {
		t.Fatalf("stale job wrote cursor=%d events=%d", cursor, eventCount)
	}
}

type projectionFixture struct {
	sessionID     string
	revisionID    string
	endCursor     int64
	store         memoryContentStore
	jobs          []ProcessingJob
	activationJob ProcessingJob
}

func createProjectionFixture(t *testing.T, database *sql.DB, userID int64, sessionRef string) projectionFixture {
	t.Helper()
	cleanupSyncIntegrationUser(t, database, userID)
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, $2)`, userID, sessionRef); err != nil {
		t.Fatal(err)
	}
	var fixture projectionFixture
	if err := database.QueryRow(`
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at)
		VALUES ($1, $2, 'codex', now()) RETURNING id`, sessionRef, userID).Scan(&fixture.sessionID); err != nil {
		t.Fatal(err)
	}
	var sourceID, generationID string
	if err := database.QueryRow(`
		INSERT INTO session_sources (session_id, source_role, source_key)
		VALUES ($1, 'main', $2) RETURNING id`, fixture.sessionID, "codex:"+sessionRef+":main").Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_source_generations (source_id, status)
		VALUES ($1, 'active') RETURNING id`, sourceID).Scan(&generationID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE session_sources SET active_generation_id = $1 WHERE id = $2`, generationID, sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_content_projection_revisions (
			generation_id, content_parser_version, status
		) VALUES ($1, $2, 'building') RETURNING id`, generationID, ContentParserVersion).Scan(&fixture.revisionID); err != nil {
		t.Fatal(err)
	}

	contents := [][]byte{
		[]byte("{\"type\":\"user\",\"timestamp\":\"2026-07-14T01:00:00Z\"}\n"),
		[]byte("{\"type\":\"assistant\",\"timestamp\":\"2026-07-14T01:01:00Z\"}\n"),
	}
	fixture.store = memoryContentStore{}
	cursor := int64(0)
	for index, content := range contents {
		objectKey := fmt.Sprintf("fixture/%s/%d", generationID, index)
		fixture.store[objectKey] = content
		var chunkID string
		end := cursor + int64(len(content))
		if err := database.QueryRow(`
			INSERT INTO session_upload_chunks (
				generation_id, start_cursor, end_cursor, start_line, end_line,
				content_sha256, content_epoch, event_start_at, event_end_at,
				raw_object_key, object_status
			) VALUES ($1, $2, $3, $4, $4, $5, 0, $6, $6, $7, 'available')
			RETURNING id`, generationID, cursor, end, index+1, HashBytes(content),
			time.Date(2026, 7, 14, 1, index, 0, 0, time.UTC), objectKey).Scan(&chunkID); err != nil {
			t.Fatal(err)
		}
		fixture.jobs = append(fixture.jobs, ProcessingJob{
			Type: JobIndexContentChunk, SessionID: fixture.sessionID,
			GenerationID:     sql.NullString{String: generationID, Valid: true},
			ChunkID:          sql.NullString{String: chunkID, Valid: true},
			TargetRevisionID: sql.NullString{String: fixture.revisionID, Valid: true},
			ContentEpoch:     sql.NullInt64{Int64: 0, Valid: true},
		})
		cursor = end
	}
	fixture.endCursor = cursor
	if _, err := database.Exec(`
		UPDATE session_source_generations
		SET expected_cursor = $1, prefix_checkpoint_hash = $2,
			prefix_checkpoint_state = $3, prefix_checkpoint_state_format = $4
		WHERE id = $5`, cursor, HashBytes(bytes.Join(contents, nil)), []byte{1}, PrefixCheckpointStateFormat, generationID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE session_content_projection_revisions SET source_high_water_cursor = $1 WHERE id = $2`, cursor, fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	fixture.activationJob = ProcessingJob{
		Type: JobRebuildContentRevision, SessionID: fixture.sessionID,
		GenerationID:     sql.NullString{String: generationID, Valid: true},
		TargetRevisionID: sql.NullString{String: fixture.revisionID, Valid: true},
		ContentEpoch:     sql.NullInt64{Int64: 0, Valid: true},
	}
	return fixture
}
