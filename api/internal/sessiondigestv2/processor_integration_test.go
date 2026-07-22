package sessiondigestv2

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/contentreader"
	"github.com/aidashboard/api/internal/reportsourcecatalog"
	"github.com/aidashboard/api/internal/sessionsync"
)

func TestDigestV2ReconcilerAndProcessorIntegration(t *testing.T) {
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
	userID := time.Now().UnixNano()%100000000 + 890000000
	if _, err := database.ExecContext(
		ctx, `INSERT INTO users (id, username) VALUES ($1, $2)`,
		userID, "digest-v2-integration",
	); err != nil {
		t.Fatal(err)
	}
	defer database.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)

	bulk := strings.Repeat("MCP-BULK-MUST-NOT-LEAK-", 50000) +
		" process exited with code 0"
	baseTime := time.Date(2026, 7, 18, 1, 0, 0, 0, time.UTC)
	events := []struct {
		outerType string
		payload   map[string]any
	}{
		{"event_msg", map[string]any{
			"type": "user_message", "message": "实现结果优先的服务端 Digest v2",
		}},
		{"response_item", map[string]any{
			"type": "reasoning", "summary": strings.Repeat("private reasoning", 5000),
		}},
		{"response_item", map[string]any{
			"type": "function_call", "call_id": "validation-v2",
			"arguments": `{"cmd":"go test ./..."}`,
		}},
		{"response_item", map[string]any{
			"type": "function_call_output", "call_id": "validation-v2", "output": bulk,
		}},
		{"event_msg", map[string]any{
			"type": "agent_message", "phase": "final_answer",
			"message": "Digest v2 完成；Authorization: Bearer integration-secret",
		}},
	}
	var raw bytes.Buffer
	for index, event := range events {
		line, err := json.Marshal(map[string]any{
			"type":      event.outerType,
			"timestamp": baseTime.Add(time.Duration(index) * time.Second).Format(time.RFC3339Nano),
			"payload":   event.payload,
		})
		if err != nil {
			t.Fatal(err)
		}
		raw.Write(line)
		raw.WriteByte('\n')
	}
	content := raw.Bytes()
	endCursor := int64(len(content))
	objectKey := "digest-v2-object"
	store := memoryDigestContentStore{objectKey: append([]byte(nil), content...)}

	var sessionID, sourceID, generationID, projectionID, chunkID, sliceID string
	if err := database.QueryRowContext(ctx, `
		INSERT INTO sessions (
			session_ref, user_id, agent_type, started_at, content_status, content_epoch
		) VALUES ($1, $2, 'codex', now(), 'uploading', 0)
		RETURNING id::text`,
		"digest-v2-integration-session", userID,
	).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_sources (session_id, source_role, source_key)
		VALUES ($1, 'main', 'digest-v2-integration-source')
		RETURNING id::text`,
		sessionID,
	).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_source_generations (source_id, status, expected_cursor)
		VALUES ($1, 'active', 0) RETURNING id::text`,
		sourceID,
	).Scan(&generationID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_content_projection_revisions (
			generation_id, content_parser_version, status
		) VALUES ($1, $2, 'building')
		RETURNING id::text`,
		generationID, sessionsync.ContentParserVersion,
	).Scan(&projectionID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE session_sources
		SET active_generation_id = $2
		WHERE id = $1`,
		sourceID, generationID,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_upload_chunks (
			generation_id, start_cursor, end_cursor, start_line, end_line,
			content_sha256, content_epoch, raw_object_key, object_status
		) VALUES ($1, 0, $2, 1, $3, $4, 0, $5, 'available')
		RETURNING id::text`,
		generationID, endCursor, len(events), sessionsync.HashBytes(content), objectKey,
	).Scan(&chunkID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_content_slices (
			session_id, source_id, generation_id, start_cursor, end_cursor
		) VALUES ($1, $2, $3, 0, $4) RETURNING id::text`,
		sessionID, sourceID, generationID, endCursor,
	).Scan(&sliceID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE session_source_generations
		SET expected_cursor = $2, prefix_checkpoint_hash = $3,
			prefix_checkpoint_algorithm_version = $4,
			prefix_checkpoint_state = $5,
			prefix_checkpoint_state_format = $6
		WHERE id = $1`, generationID, endCursor, sessionsync.HashBytes(content),
		sessionsync.PrefixCheckpointAlgorithm, []byte{1}, sessionsync.PrefixCheckpointStateFormat); err != nil {
		t.Fatal(err)
	}
	if err := reportsourcecatalog.EnsureSlice(ctx, database, sliceID); err != nil {
		t.Fatal(err)
	}
	projectionProcessor, err := sessionsync.NewContentProjectionProcessor(database, store)
	if err != nil {
		t.Fatal(err)
	}
	indexJob := sessionsync.ProcessingJob{
		Type: sessionsync.JobIndexContentChunk, SessionID: sessionID,
		GenerationID:     sql.NullString{String: generationID, Valid: true},
		ChunkID:          sql.NullString{String: chunkID, Valid: true},
		TargetRevisionID: sql.NullString{String: projectionID, Valid: true},
		ContentEpoch:     sql.NullInt64{Int64: 0, Valid: true},
	}
	if err := projectionProcessor.Process(ctx, indexJob); err != nil {
		t.Fatal(err)
	}
	if err := projectionProcessor.Process(ctx, sessionsync.ProcessingJob{
		Type: sessionsync.JobRebuildContentRevision, SessionID: sessionID,
		GenerationID:     indexJob.GenerationID,
		TargetRevisionID: indexJob.TargetRevisionID,
		ContentEpoch:     indexJob.ContentEpoch,
	}); err != nil {
		t.Fatal(err)
	}
	var projectionStatus, catalogStatus string
	var indexedCursor, eventRows, nullPayloadRows int64
	if err := database.QueryRowContext(ctx, `
		SELECT revision.status, revision.content_indexed_cursor,
			(SELECT COUNT(*) FROM session_content_events event
				WHERE event.content_projection_revision_id = revision.id),
			(SELECT COUNT(*) FROM session_content_events event
				WHERE event.content_projection_revision_id = revision.id
					AND event.content_payload IS NULL),
			(SELECT status FROM report_source_slice_catalog
				WHERE content_projection_revision_id = revision.id)
		FROM session_content_projection_revisions revision WHERE revision.id = $1`, projectionID).Scan(
		&projectionStatus, &indexedCursor, &eventRows, &nullPayloadRows, &catalogStatus,
	); err != nil {
		t.Fatal(err)
	}
	if projectionStatus != "active" || indexedCursor != endCursor ||
		eventRows != int64(len(events)) || nullPayloadRows != eventRows || catalogStatus != "ready" {
		t.Fatalf("projection=%s cursor=%d/%d events=%d null=%d catalog=%s",
			projectionStatus, indexedCursor, endCursor, eventRows, nullPayloadRows, catalogStatus)
	}
	reader, err := contentreader.New(database, store)
	if err != nil {
		t.Fatal(err)
	}

	config := DefaultConfig()
	reconciler, err := NewReconciler(database, config)
	if err != nil {
		t.Fatal(err)
	}
	created, err := reconciler.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created < 1 {
		t.Fatalf("expected a v2 digest revision, created=%d", created)
	}
	var revisionID string
	if err := database.QueryRowContext(ctx, `
		SELECT id::text
		FROM session_slice_digest_revisions
		WHERE session_content_slice_id = $1
			AND digest_version = $2 AND redaction_version = $3`,
		sliceID, config.DigestVersion, config.RedactionVersion,
	).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}

	queue, err := sessionsync.NewPostgresJobRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := queue.ClaimTypes(
		ctx, "digest-v2-integration-worker", time.Now().UTC(),
		time.Minute, 10, []string{JobType},
	)
	if err != nil {
		t.Fatal(err)
	}
	var job *sessionsync.ProcessingJob
	for index := range jobs {
		if jobs[index].TargetDigestRevisionID.Valid &&
			jobs[index].TargetDigestRevisionID.String == revisionID {
			job = &jobs[index]
			break
		}
	}
	if job == nil {
		t.Fatalf("v2 digest job for %s was not claimable: %#v", revisionID, jobs)
	}
	processor, err := NewProcessor(database, reader, config)
	if err != nil {
		t.Fatal(err)
	}
	if err := processor.Process(ctx, *job); err != nil {
		t.Fatal(err)
	}
	if ok, err := queue.Complete(
		ctx, job.ID, "digest-v2-integration-worker", time.Now().UTC(),
	); err != nil || !ok {
		t.Fatalf("complete v2 digest job: ok=%v err=%v", ok, err)
	}
	if err := processor.Process(ctx, *job); err != nil {
		t.Fatalf("ready v2 digest replay must be idempotent: %v", err)
	}
	var status, digestText, digestHash string
	var sourceEvents, includedEvents, omittedEvents, sourceBytes, digestBytes int64
	if err := database.QueryRowContext(ctx, `
		SELECT status, digest_json::text, digest_sha256, source_event_count,
			included_event_count, omitted_event_count, source_bytes, digest_bytes
		FROM session_slice_digest_revisions WHERE id = $1`,
		revisionID,
	).Scan(
		&status, &digestText, &digestHash, &sourceEvents, &includedEvents,
		&omittedEvents, &sourceBytes, &digestBytes,
	); err != nil {
		t.Fatal(err)
	}
	if status != "ready" || sourceEvents != 5 ||
		includedEvents != 4 || omittedEvents != 1 || sourceBytes != endCursor {
		t.Fatalf(
			"unexpected v2 coverage: status=%s source=%d included=%d omitted=%d bytes=%d",
			status, sourceEvents, includedEvents, omittedEvents, sourceBytes,
		)
	}
	if digestBytes <= 0 {
		t.Fatalf("v2 digest bytes were not persisted: %d", digestBytes)
	}
	var digest Digest
	if err := json.Unmarshal([]byte(digestText), &digest); err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(digest)
	if int64(len(canonical)) != digestBytes {
		t.Fatalf("stored v2 digest bytes=%d want=%d", digestBytes, len(canonical))
	}
	if HashBytes(canonical) != digestHash {
		t.Fatal("stored v2 digest hash does not match canonical JSON")
	}
	if strings.Contains(digestText, "MCP-BULK-MUST-NOT-LEAK") ||
		strings.Contains(digestText, "private reasoning") ||
		strings.Contains(digestText, "integration-secret") {
		t.Fatalf("excluded content leaked into v2 digest: %s", digestText)
	}
	if len(digest.WorkUnits) != 1 ||
		len(digest.WorkUnits[0].Validations) != 1 ||
		digest.WorkUnits[0].Validations[0].LastStatus != "passed" ||
		len(digest.WorkUnits[0].ResultStatements) < 1 {
		t.Fatalf("unexpected v2 digest: %#v", digest)
	}
}

type memoryDigestContentStore map[string][]byte

func (s memoryDigestContentStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	content, ok := s[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}
