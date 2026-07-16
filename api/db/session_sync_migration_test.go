package db

import (
	"database/sql"
	"os"
	"strings"
	"testing"
)

func TestSessionSyncMigrationContract(t *testing.T) {
	databaseURL := os.Getenv("AIDA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AIDA_TEST_DATABASE_URL is not set")
	}
	database, err := Connect(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := RunMigrations(database); err != nil {
		t.Fatal(err)
	}

	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO users (id, username) VALUES (990001, 'v2-schema-test')`); err != nil {
		t.Fatal(err)
	}
	var claudeSessionID string
	if err := tx.QueryRow(`
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at)
		VALUES ('same-ref', 990001, 'claude_code', now()) RETURNING id`).Scan(&claudeSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at)
		VALUES ('same-ref', 990001, 'codex', now())`); err != nil {
		t.Fatalf("same session_ref for another agent must be allowed: %v", err)
	}
	expectConstraintViolation(t, tx, `
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at)
		VALUES ('same-ref', 990001, 'claude_code', now())`)

	var secondSessionID string
	if err := tx.QueryRow(`
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at)
		VALUES ('other-ref', 990001, 'claude_code', now()) RETURNING id`).Scan(&secondSessionID); err != nil {
		t.Fatal(err)
	}
	var sourceID string
	if err := tx.QueryRow(`
		INSERT INTO session_sources (session_id, source_role, source_key)
		VALUES ($1, 'main', 'logical:shared-key') RETURNING id`, claudeSessionID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`
		INSERT INTO session_sources (session_id, source_role, source_key)
		VALUES ($1, 'main', 'logical:shared-key')`, secondSessionID); err != nil {
		t.Fatalf("source_key must not be globally unique: %v", err)
	}

	var generationID string
	if err := tx.QueryRow(`
		INSERT INTO session_source_generations (source_id, status)
		VALUES ($1, 'active') RETURNING id`, sourceID).Scan(&generationID); err != nil {
		t.Fatal(err)
	}
	expectConstraintViolation(t, tx, `
		INSERT INTO session_source_generations (source_id, status)
		VALUES ('`+sourceID+`', 'active')`)

	hash := strings.Repeat("a", 64)
	var firstChunkID string
	if err := tx.QueryRow(`
		INSERT INTO session_upload_chunks (
			generation_id, start_cursor, end_cursor, start_line, end_line,
			content_sha256, content_epoch, raw_object_key
		) VALUES ($1, 0, 10, 1, 1, $2, 0, 'object-1') RETURNING id`, generationID, hash).Scan(&firstChunkID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`
		INSERT INTO session_upload_chunks (
			generation_id, start_cursor, end_cursor, start_line, end_line,
			content_sha256, content_epoch, raw_object_key
		) VALUES ($1, 10, 20, 2, 2, $2, 0, 'object-2')`, generationID, hash); err != nil {
		t.Fatalf("same hash at another range must be allowed: %v", err)
	}
	expectConstraintViolation(t, tx, `
		INSERT INTO session_upload_chunks (
			generation_id, start_cursor, end_cursor, start_line, end_line,
			content_sha256, content_epoch, raw_object_key
		) VALUES ('`+generationID+`', 0, 10, 1, 1, '`+strings.Repeat("b", 64)+`', 0, 'object-conflict')`)

	if _, err := tx.Exec(`
		INSERT INTO session_processing_jobs (job_type, session_id, generation_id, chunk_id, content_epoch)
		VALUES ('index_content_chunk', $1, $2, $3, 0)`, claudeSessionID, generationID, firstChunkID); err != nil {
		t.Fatal(err)
	}
	expectConstraintViolation(t, tx, `
		INSERT INTO session_processing_jobs (job_type, session_id, generation_id, chunk_id, content_epoch)
		VALUES ('index_content_chunk', '`+claudeSessionID+`', '`+generationID+`', '`+firstChunkID+`', 0)`)
	if _, err := tx.Exec(`
		INSERT INTO session_processing_jobs (job_type, session_id, generation_id, chunk_id)
		VALUES ('parse_usage_chunk', $1, $2, $3)`, claudeSessionID, generationID, firstChunkID); err != nil {
		t.Fatalf("usage job must not require content_epoch: %v", err)
	}

	var revisionID string
	if err := tx.QueryRow(`
		INSERT INTO session_content_projection_revisions (
			generation_id, content_parser_version, status, source_high_water_cursor
		) VALUES ($1, 'fixture-v1', 'building', 20) RETURNING id`, generationID).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`
		INSERT INTO session_content_events (
			content_projection_revision_id, chunk_id, source_start_cursor, source_end_cursor,
			occurred_at, event_type, content_sha256
		) VALUES ($1, $2, 0, 10, now(), 'message', $3)`, revisionID, firstChunkID, hash); err != nil {
		t.Fatal(err)
	}
	expectConstraintViolation(t, tx, `
		INSERT INTO session_content_events (
			content_projection_revision_id, chunk_id, source_start_cursor, source_end_cursor,
			occurred_at, event_type, content_sha256
		) VALUES ('`+revisionID+`', '`+firstChunkID+`', 0, 10, now(), 'message', '`+hash+`')`)
	if _, err := tx.Exec(`
		INSERT INTO session_processing_jobs (
			job_type, session_id, generation_id, target_revision_id, content_epoch
		) VALUES ('rebuild_content_revision', $1, $2, $3, 0)`, claudeSessionID, generationID, revisionID); err != nil {
		t.Fatal(err)
	}
	expectConstraintViolation(t, tx, `
		INSERT INTO session_processing_jobs (
			job_type, session_id, generation_id, target_revision_id, content_epoch
		) VALUES ('rebuild_content_revision', '`+claudeSessionID+`', '`+generationID+`', '`+revisionID+`', 0)`)

	var sliceID string
	if err := tx.QueryRow(`
		INSERT INTO session_content_slices (
			session_id, source_id, generation_id, start_cursor, end_cursor
		) VALUES ($1, $2, $3, 0, 10) RETURNING id`, claudeSessionID, sourceID, generationID).Scan(&sliceID); err != nil {
		t.Fatal(err)
	}
	var digestRevisionID string
	if err := tx.QueryRow(`
		INSERT INTO session_slice_digest_revisions (
			session_content_slice_id, content_projection_revision_id, generation_id,
			content_epoch, digest_version, redaction_version
		) VALUES ($1, $2, $3, 0, 'session-digest/v1', 'report-redaction/v1')
		RETURNING id`, sliceID, revisionID, generationID).Scan(&digestRevisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`
		INSERT INTO session_processing_jobs (
			job_type, session_id, generation_id, target_digest_revision_id, content_epoch
		) VALUES ('build_content_slice_digest', $1, $2, $3, 0)`, claudeSessionID, generationID, digestRevisionID); err != nil {
		t.Fatalf("digest job must be accepted: %v", err)
	}
	expectConstraintViolation(t, tx, `
		INSERT INTO session_processing_jobs (
			job_type, session_id, generation_id, target_digest_revision_id, content_epoch
		) VALUES ('build_content_slice_digest', '`+claudeSessionID+`', '`+generationID+`', '`+digestRevisionID+`', 0)`)
	if _, err := tx.Exec(`
		UPDATE session_slice_digest_revisions
		SET status = 'ready',
			digest_json = '{"goals":[],"outcomes":[],"files_changed":[],"validations":[],"blockers":[]}',
			source_event_count = 1, included_event_count = 1, omitted_event_count = 0,
			source_bytes = 10, digest_bytes = 80, source_sha256 = $2,
			digest_sha256 = $2, ready_at = now()
		WHERE id = $1`, digestRevisionID, hash); err != nil {
		t.Fatalf("ready digest must satisfy the schema contract: %v", err)
	}
	expectConstraintViolation(t, tx, `
		UPDATE session_slice_digest_revisions
		SET digest_json = '{"goals":["mutated"],"outcomes":[],"files_changed":[],"validations":[],"blockers":[]}'
		WHERE id = '`+digestRevisionID+`'`)
}

func expectConstraintViolation(t *testing.T, tx *sql.Tx, statement string) {
	t.Helper()
	if _, err := tx.Exec("SAVEPOINT expected_constraint"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(statement); err == nil {
		t.Fatal("expected constraint violation")
	}
	if _, err := tx.Exec("ROLLBACK TO SAVEPOINT expected_constraint"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("RELEASE SAVEPOINT expected_constraint"); err != nil {
		t.Fatal(err)
	}
}
