package sessiondigest

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/contentreader"
	"github.com/aidashboard/api/internal/sessionsync"
)

func TestDigestReconcilerAndProcessorIntegration(t *testing.T) {
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
	userID := time.Now().UnixNano()%100000000 + 880000000
	if _, err := database.ExecContext(ctx, `INSERT INTO users (id, username) VALUES ($1, $2)`, userID, "digest-integration"); err != nil {
		t.Fatal(err)
	}
	defer database.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)

	var sessionID, sourceID, generationID, projectionID, chunkID, sliceID string
	if err := database.QueryRowContext(ctx, `
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at, content_status, content_epoch)
		VALUES ($1, $2, 'codex', now(), 'available', 0) RETURNING id::text`,
		"digest-integration-session", userID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_sources (session_id, source_role, source_key)
		VALUES ($1, 'main', 'digest-integration-source') RETURNING id::text`, sessionID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_source_generations (source_id, status, expected_cursor)
		VALUES ($1, 'active', 0) RETURNING id::text`, sourceID).Scan(&generationID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_content_projection_revisions (
			generation_id, content_parser_version, status, content_indexed_cursor,
			source_high_water_cursor, event_count, activated_at
		) VALUES ($1, $2, 'active', 100, 100, 5, now()) RETURNING id::text`,
		generationID, sessionsync.ContentParserVersion).Scan(&projectionID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE session_sources
		SET active_generation_id = $2, active_content_projection_revision_id = $3
		WHERE id = $1`, sourceID, generationID, projectionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_upload_chunks (
			generation_id, start_cursor, end_cursor, start_line, end_line,
			content_sha256, content_epoch, raw_object_key, content_index_status
		) VALUES ($1, 0, 100, 1, 5, $2, 0, 'digest-integration-object', 'indexed')
		RETURNING id::text`, generationID, strings.Repeat("a", 64)).Scan(&chunkID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO session_content_slices (session_id, source_id, generation_id, start_cursor, end_cursor)
		VALUES ($1, $2, $3, 0, 100) RETURNING id::text`, sessionID, sourceID, generationID).Scan(&sliceID); err != nil {
		t.Fatal(err)
	}

	bulk := strings.Repeat("MCP-BULK-MUST-NOT-LEAK-", 50000) + " process exited with code 0"
	events := []struct {
		start, end int
		typeName   string
		payload    any
	}{
		{0, 20, "event_msg.user_message", map[string]any{"payload": map[string]any{"message": "实现大 Session 的服务端 Digest"}}},
		{20, 40, "response_item.reasoning", map[string]any{"payload": map[string]any{"summary": strings.Repeat("private reasoning", 5000)}}},
		{40, 60, "response_item.function_call", map[string]any{"payload": map[string]any{"call_id": "validation-1", "arguments": `{"cmd":"go test ./..."}`}}},
		{60, 80, "response_item.function_call_output", map[string]any{"payload": map[string]any{"call_id": "validation-1", "output": bulk}}},
		{80, 100, "event_msg.agent_message", map[string]any{"payload": map[string]any{"phase": "final_answer", "message": "Digest 完成；Authorization: Bearer integration-secret"}}},
	}
	baseTime := time.Date(2026, 7, 18, 1, 0, 0, 0, time.UTC)
	reader := &integrationContentReader{}
	for index, event := range events {
		payload, _ := json.Marshal(event.payload)
		contentSHA := strings.Repeat(string(rune('b'+index)), 64)
		reader.events = append(reader.events, contentreader.Event{
			SourceStartCursor: int64(event.start),
			SourceEndCursor:   int64(event.end),
			OccurredAt:        baseTime.Add(time.Duration(index) * time.Second),
			EventType:         event.typeName,
			Payload:           payload,
			ContentSHA256:     contentSHA,
		})
		if _, err := database.ExecContext(ctx, `
			INSERT INTO session_content_events (
				content_projection_revision_id, chunk_id, source_start_cursor, source_end_cursor,
				occurred_at, event_type, summary, content_payload, content_sha256
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			projectionID, chunkID, event.start, event.end, baseTime.Add(time.Duration(index)*time.Second), event.typeName,
			"integration event", payload, contentSHA); err != nil {
			t.Fatal(err)
		}
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
		t.Fatalf("expected at least one digest revision, created=%d", created)
	}
	var revisionID string
	if err := database.QueryRowContext(ctx, `
		SELECT id::text FROM session_slice_digest_revisions
		WHERE session_content_slice_id = $1 AND digest_version = $2 AND redaction_version = $3`,
		sliceID, config.DigestVersion, config.RedactionVersion).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}

	queue, err := sessionsync.NewPostgresJobRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := queue.ClaimTypes(ctx, "digest-integration-worker", time.Now().UTC(), time.Minute, 10, []string{JobType})
	if err != nil {
		t.Fatal(err)
	}
	var job *sessionsync.ProcessingJob
	for index := range jobs {
		if jobs[index].TargetDigestRevisionID.Valid && jobs[index].TargetDigestRevisionID.String == revisionID {
			job = &jobs[index]
			break
		}
	}
	if job == nil {
		t.Fatalf("digest job for revision %s was not claimable: %#v", revisionID, jobs)
	}
	processor, err := NewProcessor(database, reader, config)
	if err != nil {
		t.Fatal(err)
	}
	if err := processor.Process(ctx, *job); err != nil {
		t.Fatal(err)
	}
	if ok, err := queue.Complete(ctx, job.ID, "digest-integration-worker", time.Now().UTC()); err != nil || !ok {
		t.Fatalf("complete digest job: ok=%v err=%v", ok, err)
	}
	if err := processor.Process(ctx, *job); err != nil {
		t.Fatalf("ready digest replay must be idempotent: %v", err)
	}
	if len(reader.requests) != 1 || reader.requests[0] != (contentreader.Request{
		RevisionID: projectionID, StartCursor: 0, EndCursor: 100,
	}) {
		t.Fatalf("unexpected content reader requests: %#v", reader.requests)
	}

	var status, digestText, digestHash string
	var sourceEvents, includedEvents, omittedEvents, sourceBytes, digestBytes int64
	if err := database.QueryRowContext(ctx, `
		SELECT status, digest_json::text, digest_sha256, source_event_count,
			included_event_count, omitted_event_count, source_bytes, digest_bytes
		FROM session_slice_digest_revisions WHERE id = $1`, revisionID).Scan(
		&status, &digestText, &digestHash, &sourceEvents, &includedEvents,
		&omittedEvents, &sourceBytes, &digestBytes); err != nil {
		t.Fatal(err)
	}
	if status != "ready" || sourceEvents != 5 || includedEvents != 4 || omittedEvents != 1 {
		t.Fatalf("unexpected revision coverage: status=%s source=%d included=%d omitted=%d", status, sourceEvents, includedEvents, omittedEvents)
	}
	if sourceBytes < int64(len(bulk)) || digestBytes > DefaultItemBytes {
		t.Fatalf("unexpected byte reduction: source=%d digest=%d", sourceBytes, digestBytes)
	}
	var digest Digest
	if err := json.Unmarshal([]byte(digestText), &digest); err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(digest)
	if HashBytes(canonical) != digestHash {
		t.Fatal("stored digest hash does not match canonical digest JSON")
	}
	if strings.Contains(digestText, "MCP-BULK-MUST-NOT-LEAK") || strings.Contains(digestText, "private reasoning") || strings.Contains(digestText, "integration-secret") {
		t.Fatalf("excluded or secret content leaked into digest: %s", digestText)
	}
	if len(digest.Validations) != 1 || digest.Validations[0].Status != "passed" {
		t.Fatalf("unexpected validation digest: %#v", digest.Validations)
	}
	if len(digest.Outcomes) != 1 || !strings.Contains(digest.Outcomes[0], "Digest 完成") {
		t.Fatalf("unexpected outcome digest: %#v", digest.Outcomes)
	}
}

type integrationContentReader struct {
	events   []contentreader.Event
	requests []contentreader.Request
}

func (r *integrationContentReader) Stream(
	ctx context.Context,
	request contentreader.Request,
	consume func(contentreader.Event) error,
) (contentreader.Result, error) {
	r.requests = append(r.requests, request)
	for _, event := range r.events {
		if err := ctx.Err(); err != nil {
			return contentreader.Result{}, err
		}
		if err := consume(event); err != nil {
			return contentreader.Result{}, err
		}
	}
	return contentreader.Result{
		StartCursor: request.StartCursor,
		EndCursor:   request.EndCursor,
		EventCount:  int64(len(r.events)),
	}, nil
}
