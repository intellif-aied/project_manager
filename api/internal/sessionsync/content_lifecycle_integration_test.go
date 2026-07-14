package sessionsync

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
)

func TestContentLifecycleClearRestoreAndRestoreUploadGateIntegration(t *testing.T) {
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
	userID := int64(990030)
	_, _ = database.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID)
	_, _ = database.Exec(`DELETE FROM users WHERE id = $1`, userID)
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, 'v2-content-lifecycle')`, userID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = database.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID)
		_, _ = database.Exec(`DELETE FROM users WHERE id = $1`, userID)
	}()

	var sessionID, sourceID, generationID string
	if err := database.QueryRow(`
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at, summary)
		VALUES ('content-lifecycle-e2e', $1, 'codex', now(), 'secret summary') RETURNING id`, userID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_sources (session_id, source_role, source_key)
		VALUES ($1, 'main', 'codex:content-lifecycle-e2e:main') RETURNING id`, sessionID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_source_generations (
			source_id, status, expected_cursor, prefix_checkpoint_hash,
			prefix_checkpoint_algorithm_version, prefix_checkpoint_state, prefix_checkpoint_state_format
		) VALUES ($1, 'active', 10, $2, $3, '\x01'::bytea, $4) RETURNING id`,
		sourceID, HashBytes([]byte("0123456789")), PrefixCheckpointAlgorithm, PrefixCheckpointStateFormat).Scan(&generationID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE session_sources SET active_generation_id = $1 WHERE id = $2`, generationID, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO session_upload_chunks (
			generation_id, start_cursor, end_cursor, start_line, end_line, content_sha256,
			content_epoch, raw_object_key, object_status
		) VALUES ($1, 0, 10, 1, 1, $2, 0, 'test/object', 'available')`, generationID, HashBytes([]byte("0123456789"))); err != nil {
		t.Fatal(err)
	}

	lifecycle, err := NewContentLifecycleService(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE sessions SET raw_log_url = 'legacy/untracked.jsonl' WHERE id = $1`, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.RequestClear(ctx, stringInt64(userID), sessionID, "must wait for migration"); !errors.Is(err, ErrLegacyContentMigration) {
		t.Fatalf("untracked legacy raw log clear err=%v", err)
	}
	var statusAfterBlockedClear string
	if err := database.QueryRow(`SELECT content_status FROM sessions WHERE id = $1`, sessionID).Scan(&statusAfterBlockedClear); err != nil {
		t.Fatal(err)
	}
	if statusAfterBlockedClear != "available" {
		t.Fatalf("status after blocked clear=%s", statusAfterBlockedClear)
	}
	if _, err := database.Exec(`UPDATE sessions SET raw_log_url = 'test/object' WHERE id = $1`, sessionID); err != nil {
		t.Fatal(err)
	}
	clear, err := lifecycle.RequestClear(ctx, stringInt64(userID), sessionID, "user requested content removal")
	if err != nil {
		t.Fatal(err)
	}
	if clear.ContentStatus != "clearing" || clear.ContentEpoch != 1 || clear.PendingJobs != 1 || clear.TombstoneID == "" {
		t.Fatalf("clear=%+v", clear)
	}
	secondClear, err := lifecycle.RequestClear(ctx, stringInt64(userID), sessionID, "ignored idempotent retry")
	if err != nil {
		t.Fatal(err)
	}
	if secondClear.TombstoneID != clear.TombstoneID || secondClear.PendingJobs != 1 {
		t.Fatalf("second clear=%+v", secondClear)
	}
	var jobCount int
	if err := database.QueryRow(`
		SELECT COUNT(*) FROM session_processing_jobs
		WHERE session_id = $1 AND job_type = 'build_metering_envelope'`, sessionID).Scan(&jobCount); err != nil || jobCount != 1 {
		t.Fatalf("metering jobs=%d err=%v", jobCount, err)
	}
	if _, err := lifecycle.RequestRestore(ctx, stringInt64(userID), sessionID); !errors.Is(err, ErrContentTransition) {
		t.Fatalf("restore while clearing err=%v", err)
	}

	// Simulate the Token-owned Metering Envelope and object deletion gate completing.
	if _, err := database.Exec(`UPDATE sessions SET content_status = 'cleared' WHERE id = $1`, sessionID); err != nil {
		t.Fatal(err)
	}
	restore, err := lifecycle.RequestRestore(ctx, stringInt64(userID), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if restore.ContentStatus != "cleared" || restore.ContentEpoch != 2 || restore.RestoreStatus != "waiting_upload" || !restore.RestoreExpiresAt.After(time.Now()) {
		t.Fatalf("restore=%+v", restore)
	}

	syncService, err := NewSyncService(database)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := syncService.Prepare(ctx, stringInt64(userID), PrepareSessionRequest{
		SessionRef: "content-lifecycle-e2e", AgentType: "codex",
		Sources: []PrepareSourceRequest{{
			SourceRole: "main", SourceKey: "codex:content-lifecycle-e2e:main", LocalSize: 10,
			PrefixCheckpointHash: HashBytes(nil), PrefixCheckpointAlgorithmVersion: PrefixCheckpointAlgorithm,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared) != 1 || prepared[0].Action != PrepareRestore || prepared[0].GenerationStatus != "staging" {
		t.Fatalf("prepared=%+v", prepared)
	}
	repository, err := NewPostgresChunkRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.InspectChunk(ctx, stringInt64(userID), prepared[0].GenerationID, ChunkMetadata{
		StartCursor: 0, EndCursor: 10, StartLine: 1, EndLine: 1, ContentSHA256: HashBytes([]byte("0123456789")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ContentStatus != ContentCleared || !snapshot.RestoreWritable || snapshot.ContentEpoch != 2 {
		t.Fatalf("restore upload snapshot=%+v", snapshot)
	}
	if _, err := lifecycle.RequestClear(ctx, "999999", sessionID, "not owner"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("non-owner clear err=%v", err)
	}
}

func stringInt64(value int64) string {
	return strconv.FormatInt(value, 10)
}
