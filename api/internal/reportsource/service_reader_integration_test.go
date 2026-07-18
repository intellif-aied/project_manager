package reportsource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/contentreader"
	"github.com/aidashboard/api/internal/reportsourcecatalog"
	"github.com/aidashboard/api/internal/sessionsync"
)

func TestAttachedFullReadsNullPayloadIndexThroughContentReader(t *testing.T) {
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
	userID := int64(990042)
	_, _ = database.Exec(`DELETE FROM users WHERE id = $1`, userID)
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, 'reader-report-source')`, userID); err != nil {
		t.Fatal(err)
	}
	defer database.Exec(`DELETE FROM users WHERE id = $1`, userID)

	content, parsedEvents := buildReportReaderContent(t, 102)
	objectKey := "report-source/reader-integration.jsonl"
	store := &rangeTrackingObjectStore{objects: map[string][]byte{objectKey: content}}
	reader, err := contentreader.New(database, store)
	if err != nil {
		t.Fatal(err)
	}

	var sessionID, sourceID, generationID, chunkID, revisionID, sliceID string
	if err := database.QueryRow(`
		INSERT INTO sessions (
			session_ref, user_id, agent_type, started_at, last_activity_at,
			content_status, content_epoch
		) VALUES ('reader-report-source', $1, 'codex', $2, $3, 'available', 0)
		RETURNING id::text`, userID, parsedEvents[0].OccurredAt, parsedEvents[len(parsedEvents)-1].OccurredAt,
	).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_sources (session_id, source_role, source_key)
		VALUES ($1, 'main', 'reader-report-source:main') RETURNING id::text`, sessionID,
	).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_source_generations (
			source_id, status, expected_cursor, prefix_checkpoint_hash,
			prefix_checkpoint_state, prefix_checkpoint_state_format
		) VALUES ($1, 'active', $2, $3, '\x01'::bytea, 'sha256-state-v1')
		RETURNING id::text`, sourceID, len(content), sessionsync.HashBytes(content),
	).Scan(&generationID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_upload_chunks (
			generation_id, start_cursor, end_cursor, start_line, end_line,
			content_sha256, content_epoch, event_start_at, event_end_at,
			raw_object_key, object_status, content_index_status
		) VALUES ($1, 0, $2, 1, $3, $4, 0, $5, $6, $7, 'available', 'indexed')
		RETURNING id::text`, generationID, len(content), len(parsedEvents),
		sessionsync.HashBytes(content), parsedEvents[0].OccurredAt,
		parsedEvents[len(parsedEvents)-1].OccurredAt, objectKey,
	).Scan(&chunkID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_content_projection_revisions (
			generation_id, content_parser_version, status, build_start_cursor,
			content_indexed_cursor, source_high_water_cursor, event_count,
			validated_at, activated_at
		) VALUES ($1, $2, 'active', 0, $3, $3, $4, now(), now())
		RETURNING id::text`, generationID, sessionsync.ContentParserVersion, len(content), len(parsedEvents),
	).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE session_sources
		SET active_generation_id = $1, active_content_projection_revision_id = $2
		WHERE id = $3`, generationID, revisionID, sourceID,
	); err != nil {
		t.Fatal(err)
	}
	for _, event := range parsedEvents {
		if _, err := database.Exec(`
			INSERT INTO session_content_events (
				content_projection_revision_id, chunk_id,
				source_start_cursor, source_end_cursor, occurred_at,
				event_type, summary, excerpt, content_payload, content_sha256
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL, $9)`,
			revisionID, chunkID, event.SourceStartCursor, event.SourceEndCursor,
			event.OccurredAt, event.EventType, event.Summary, event.Excerpt, event.ContentSHA256,
		); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.QueryRow(`
		INSERT INTO session_content_slices (
			session_id, source_id, generation_id, start_cursor, end_cursor
		) VALUES ($1, $2, $3, 0, $4) RETURNING id::text`,
		sessionID, sourceID, generationID, len(content),
	).Scan(&sliceID); err != nil {
		t.Fatal(err)
	}
	if err := reportsourcecatalog.EnsureSlice(ctx, database, sliceID); err != nil {
		t.Fatal(err)
	}

	service, err := NewServiceWithReader(database, reader)
	if err != nil {
		t.Fatal(err)
	}
	period := Period{Start: "2026-07-18", End: "2026-07-18"}
	selection, err := service.CreateExplicit(ctx, "990042", "personal_daily", period, []SourceInput{{SliceKey: sliceID}})
	if err != nil {
		t.Fatal(err)
	}
	runID, selection, err := service.CreateAttachedRun(
		ctx, "990042", "personal_daily", period, selection.ID,
		"report_agent_run", "agent-test", "", false,
		map[string]any{"report_type": "personal_daily"},
	)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.ReadAttachedSelection(ctx, "990042", selection.ID, runID, "personal_daily", period, "")
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore || first.NextCursor == nil || first.ReturnedEvents != 100 || len(first.Items) != 1 {
		t.Fatalf("first page=%+v", first)
	}
	if strings.Contains(string(first.Items[0].Events[0].Payload), "usage") ||
		!strings.Contains(string(first.Items[0].Events[0].Payload), "message") {
		t.Fatalf("usage redaction/content changed: %s", first.Items[0].Events[0].Payload)
	}
	second, err := service.ReadAttachedSelection(
		ctx, "990042", selection.ID, runID, "personal_daily", period, *first.NextCursor,
	)
	if err != nil {
		t.Fatal(err)
	}
	if second.HasMore || second.ReturnedEvents != 2 || second.Completeness != "complete" {
		t.Fatalf("second page=%+v", second)
	}
	if len(store.reads) != 2 || store.reads[0].seekOffset != 0 || store.reads[1].seekOffset <= 0 {
		t.Fatalf("unexpected object range reads: %+v", store.reads)
	}
	if store.reads[0].bytesRead+store.reads[1].bytesRead != int64(len(content)) {
		t.Fatalf("pagination reread bytes: first=%d second=%d content=%d",
			store.reads[0].bytesRead, store.reads[1].bytesRead, len(content))
	}
	var nullPayloads int
	if err := database.QueryRow(`
		SELECT COUNT(*) FROM session_content_events
		WHERE content_projection_revision_id = $1 AND content_payload IS NULL`, revisionID,
	).Scan(&nullPayloads); err != nil || nullPayloads != len(parsedEvents) {
		t.Fatalf("NULL payload rows=%d err=%v", nullPayloads, err)
	}
}

func buildReportReaderContent(t *testing.T, count int) ([]byte, []contentreader.Event) {
	t.Helper()
	var content bytes.Buffer
	start := time.Date(2026, 7, 18, 1, 0, 0, 0, time.UTC)
	for index := 0; index < count; index++ {
		line := map[string]any{
			"type":      "event_msg",
			"timestamp": start.Add(time.Duration(index) * time.Second).Format(time.RFC3339Nano),
			"payload": map[string]any{
				"type": "user_message", "message": "event-" + strconv.Itoa(index),
				"usage": map[string]any{"input_tokens": index + 1},
			},
		}
		encoded, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		content.Write(encoded)
		content.WriteByte('\n')
	}
	parsed, err := sessionsync.ParseContentChunk(bytes.NewReader(content.Bytes()), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Events) != count {
		t.Fatalf("parsed events=%d want=%d", len(parsed.Events), count)
	}
	return content.Bytes(), parsed.Events
}

type rangeTrackingObjectStore struct {
	objects map[string][]byte
	reads   []*rangeTrackingReadCloser
}

func (s *rangeTrackingObjectStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	content, ok := s.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	reader := &rangeTrackingReadCloser{reader: bytes.NewReader(content), seekOffset: -1}
	s.reads = append(s.reads, reader)
	return reader, nil
}

type rangeTrackingReadCloser struct {
	reader     *bytes.Reader
	seekOffset int64
	bytesRead  int64
}

func (r *rangeTrackingReadCloser) Read(buffer []byte) (int, error) {
	count, err := r.reader.Read(buffer)
	r.bytesRead += int64(count)
	return count, err
}

func (r *rangeTrackingReadCloser) Seek(offset int64, whence int) (int64, error) {
	position, err := r.reader.Seek(offset, whence)
	if err == nil && whence == io.SeekStart {
		r.seekOffset = position
	}
	return position, err
}

func (r *rangeTrackingReadCloser) Close() error { return nil }

var _ contentreader.ObjectStore = (*rangeTrackingObjectStore)(nil)
