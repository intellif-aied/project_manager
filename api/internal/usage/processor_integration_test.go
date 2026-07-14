package usage

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	appconfig "github.com/aidashboard/api/config"
	projectdb "github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/sessionsync"
	appstorage "github.com/aidashboard/api/storage"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type memoryUsageStore map[string][]byte

func (store memoryUsageStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	content, ok := store[key]
	if !ok {
		return nil, errors.New("usage object not found")
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func TestProcessorClaudeFoldAndActivationIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990021, "usage-claude-fold", "claude-code")
	defer fixture.cleanup(t)
	content := readUsageFixture(t, "claude_monotonic.jsonl")
	lines := completeLines(content)
	fixture.appendChunk(t, bytes.Join(lines[:3], nil))
	fixture.appendChunk(t, bytes.Join(lines[3:], nil))

	processor, err := NewProcessor(database, fixture.store, "5m")
	if err != nil {
		t.Fatal(err)
	}
	if err := processor.Process(context.Background(), fixture.jobs[1]); !errors.Is(err, ErrUsageOutOfOrder) {
		t.Fatalf("out-of-order error = %v", err)
	}
	for _, job := range fixture.jobs {
		if err := processor.Process(context.Background(), job); err != nil {
			t.Fatalf("process chunk: %v", err)
		}
	}
	if err := processor.Process(context.Background(), fixture.jobs[0]); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}

	var status, quality string
	var parsedCursor, observations, events, advances, duplicates int64
	if err := database.QueryRow(`
		SELECT status, quality_status, validated_through_cursor,
			usage_observation_count, usage_event_count,
			advanced_observation_count, duplicate_usage_event_count
		FROM session_metrics_revisions WHERE generation_id = $1`, fixture.generationID).Scan(
		&status, &quality, &parsedCursor, &observations, &events, &advances, &duplicates,
	); err != nil {
		t.Fatal(err)
	}
	if status != "active" || quality != "estimated" || parsedCursor != int64(len(content)) ||
		observations != 4 || events != 2 || advances != 1 || duplicates != 1 {
		t.Fatalf("status=%s quality=%s cursor=%d observations=%d events=%d advances=%d duplicates=%d",
			status, quality, parsedCursor, observations, events, advances, duplicates)
	}
	assertDailyUsage(t, database, fixture.sessionID, 2, 250, "estimated", 1)
}

func TestProcessorCodexCumulativeAcrossChunksIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990022, "usage-codex-cumulative", "codex")
	defer fixture.cleanup(t)
	content := readUsageFixture(t, "codex_cumulative.jsonl")
	lines := completeLines(content)
	fixture.appendChunk(t, bytes.Join(lines[:3], nil))
	fixture.appendChunk(t, bytes.Join(lines[3:], nil))
	processor, _ := NewProcessor(database, fixture.store, "")
	for _, job := range fixture.jobs {
		if err := processor.Process(context.Background(), job); err != nil {
			t.Fatalf("process Codex chunk: %v", err)
		}
	}
	var status, quality string
	var events int64
	if err := database.QueryRow(`
		SELECT status, quality_status, usage_event_count
		FROM session_metrics_revisions WHERE generation_id = $1`, fixture.generationID).Scan(&status, &quality, &events); err != nil {
		t.Fatal(err)
	}
	if status != "active" || quality != "estimated" || events != 2 {
		t.Fatalf("status=%s quality=%s events=%d", status, quality, events)
	}
	assertDailyUsage(t, database, fixture.sessionID, 2, 220, "estimated", 2)
}

func TestProcessorConflictDoesNotActivateIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990023, "usage-claude-conflict", "claude-code")
	defer fixture.cleanup(t)
	fixture.appendChunk(t, readUsageFixture(t, "claude_conflict.jsonl"))
	processor, _ := NewProcessor(database, fixture.store, "5m")
	if err := processor.Process(context.Background(), fixture.jobs[0]); err != nil {
		t.Fatal(err)
	}
	var revisionStatus, quality, sourceStatus string
	var activeRevision sql.NullString
	if err := database.QueryRow(`
		SELECT r.status, r.quality_status, state.status, state.active_revision_id
		FROM session_metrics_revisions r
		JOIN session_source_metrics_states state ON state.source_id = r.source_id
		WHERE r.generation_id = $1`, fixture.generationID).Scan(
		&revisionStatus, &quality, &sourceStatus, &activeRevision,
	); err != nil {
		t.Fatal(err)
	}
	if revisionStatus != "failed" || quality != "conflict" || sourceStatus != "error" || activeRevision.Valid {
		t.Fatalf("revision=%s quality=%s source=%s active=%v", revisionStatus, quality, sourceStatus, activeRevision)
	}
}

func TestProcessorActiveAppendConflictRollsBackIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990024, "usage-active-append-conflict", "claude-code")
	defer fixture.cleanup(t)
	lines := completeLines(readUsageFixture(t, "claude_conflict.jsonl"))
	fixture.appendChunk(t, lines[0])
	processor, _ := NewProcessor(database, fixture.store, "5m")
	if err := processor.Process(context.Background(), fixture.jobs[0]); err != nil {
		t.Fatal(err)
	}
	fixture.appendChunk(t, lines[1])
	if _, err := database.Exec(`
		UPDATE session_source_metrics_states
		SET source_high_water_cursor = $2, status = 'pending' WHERE source_id = $1`, fixture.sourceID, fixture.cursor); err != nil {
		t.Fatal(err)
	}
	if err := processor.Process(context.Background(), fixture.jobs[1]); !errors.Is(err, ErrUsageQualityGate) {
		t.Fatalf("active append conflict error = %v", err)
	}
	var status, quality string
	var parsedCursor, total int64
	if err := database.QueryRow(`
		SELECT status, quality_status, validated_through_cursor
		FROM session_metrics_revisions WHERE generation_id = $1`, fixture.generationID).Scan(&status, &quality, &parsedCursor); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		SELECT COALESCE(SUM(total_tokens), 0) FROM session_daily_usage
		WHERE session_id = $1 AND valid_to IS NULL`, fixture.sessionID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if status != "active" || quality != "estimated" || parsedCursor != int64(len(lines[0])) || total != 160 {
		t.Fatalf("status=%s quality=%s cursor=%d total=%d", status, quality, parsedCursor, total)
	}
}

func TestProcessorReplacementRequiresOldFactsIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990025, "usage-replacement-coverage", "claude-code")
	defer fixture.cleanup(t)
	lines := completeLines(readUsageFixture(t, "claude_monotonic.jsonl"))
	fixture.appendChunk(t, bytes.Join(lines, nil))
	processor, _ := NewProcessor(database, fixture.store, "5m")
	if err := processor.Process(context.Background(), fixture.jobs[0]); err != nil {
		t.Fatal(err)
	}
	oldRevisionID := activeMetricsRevision(t, database, fixture.sourceID)

	newGenerationID := fixture.replaceGeneration(t)
	fixture.appendChunkToGeneration(t, newGenerationID, lines[2])
	if err := processor.Process(context.Background(), fixture.jobs[len(fixture.jobs)-1]); err != nil {
		t.Fatal(err)
	}
	if got := activeMetricsRevision(t, database, fixture.sourceID); got != oldRevisionID {
		t.Fatalf("missing facts replaced active revision: old=%s got=%s", oldRevisionID, got)
	}
	var candidateStatus, stateStatus string
	if err := database.QueryRow(`
		SELECT r.status, state.status
		FROM session_metrics_revisions r
		JOIN session_source_metrics_states state ON state.source_id = r.source_id
		WHERE r.generation_id = $1`, newGenerationID).Scan(&candidateStatus, &stateStatus); err != nil {
		t.Fatal(err)
	}
	if candidateStatus != "building" || stateStatus != "rebuilding" {
		t.Fatalf("candidate=%s source=%s", candidateStatus, stateStatus)
	}

	fixture.appendChunkToGeneration(t, newGenerationID, lines[3])
	if err := processor.Process(context.Background(), fixture.jobs[len(fixture.jobs)-1]); err != nil {
		t.Fatal(err)
	}
	newRevisionID := activeMetricsRevision(t, database, fixture.sourceID)
	if newRevisionID == oldRevisionID {
		t.Fatal("complete replacement did not activate")
	}
	assertDailyUsage(t, database, fixture.sessionID, 2, 250, "estimated", 1)
}

func TestProcessorReadsVerifiedChunksFromRealMinIOIntegration(t *testing.T) {
	endpoint := os.Getenv("AIDA_TEST_MINIO_ENDPOINT")
	accessKey := os.Getenv("AIDA_TEST_MINIO_ACCESS_KEY")
	secretKey := os.Getenv("AIDA_TEST_MINIO_SECRET_KEY")
	if endpoint == "" || accessKey == "" || secretKey == "" {
		t.Skip("AIDA_TEST_MINIO_* is not configured")
	}
	bucket := fmt.Sprintf("aida-v2-usage-%d", time.Now().UnixNano())
	store, err := appstorage.NewMinioStorage(&appconfig.Config{
		MinioEndpoint: endpoint, MinioAccessKey: accessKey, MinioSecretKey: secretKey,
		MinioBucket: bucket, MinioUseSSL: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, "")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.RemoveBucket(context.Background(), bucket)
	})

	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990026, "usage-real-minio", "codex")
	defer fixture.cleanup(t)
	fixture.appendChunk(t, readUsageFixture(t, "codex_cumulative.jsonl"))
	for key, content := range fixture.store {
		if err := store.PutVerified(context.Background(), key, bytes.NewReader(content), int64(len(content)), sessionsync.HashBytes(content)); err != nil {
			t.Fatal(err)
		}
		objectKey := key
		t.Cleanup(func() { _ = store.Delete(context.Background(), objectKey) })
	}
	processor, err := NewProcessor(database, store, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := processor.Process(context.Background(), fixture.jobs[0]); err != nil {
		t.Fatal(err)
	}
	assertDailyUsage(t, database, fixture.sessionID, 2, 220, "estimated", 2)
}

func TestProcessorConcurrentHundredChunksCatchUpIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	database.SetMaxOpenConns(20)
	fixture := newUsageFixture(t, database, 990027, "usage-concurrent-100", "codex")
	defer fixture.cleanup(t)
	for index := 1; index <= 100; index++ {
		prefix := ""
		if index == 1 {
			prefix = `{"timestamp":"2026-07-10T00:00:00Z","type":"turn_context","payload":{"model":"gpt-concurrency"}}` + "\n"
		}
		line := fmt.Sprintf(
			`{"timestamp":"2026-07-10T00:%02d:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d,"reasoning_output_tokens":%d,"total_tokens":%d}}}}`+"\n",
			index%60, index*10, index*2, index*5, index, index*15,
		)
		fixture.appendChunk(t, []byte(prefix+line))
	}
	processor, _ := NewProcessor(database, fixture.store, "")
	pending := append([]sessionsync.ProcessingJob(nil), fixture.jobs...)
	for round := 0; len(pending) > 0 && round < 100; round++ {
		var wait sync.WaitGroup
		var lock sync.Mutex
		var retry []sessionsync.ProcessingJob
		var unexpected []error
		for index := len(pending) - 1; index >= 0; index-- {
			job := pending[index]
			wait.Add(1)
			go func() {
				defer wait.Done()
				err := processor.Process(context.Background(), job)
				lock.Lock()
				defer lock.Unlock()
				if errors.Is(err, ErrUsageOutOfOrder) {
					retry = append(retry, job)
				} else if err != nil {
					unexpected = append(unexpected, err)
				}
			}()
		}
		wait.Wait()
		if len(unexpected) > 0 {
			t.Fatalf("unexpected concurrent errors: %v", unexpected)
		}
		pending = retry
	}
	if len(pending) != 0 {
		t.Fatalf("%d chunks did not catch up", len(pending))
	}
	var status string
	var parsedCursor, highWater, observations, events, total int64
	if err := database.QueryRow(`
		SELECT r.status, r.validated_through_cursor, state.source_high_water_cursor,
			r.usage_observation_count, r.usage_event_count,
			(SELECT COALESCE(SUM(total_tokens), 0) FROM session_daily_usage WHERE revision_id = r.id AND valid_to IS NULL)
		FROM session_metrics_revisions r
		JOIN session_source_metrics_states state ON state.active_revision_id = r.id
		WHERE r.generation_id = $1`, fixture.generationID).Scan(
		&status, &parsedCursor, &highWater, &observations, &events, &total,
	); err != nil {
		t.Fatal(err)
	}
	if status != "active" || parsedCursor != fixture.cursor || highWater != fixture.cursor ||
		observations != 100 || events != 100 || total != 1500 {
		t.Fatalf("status=%s parsed=%d highwater=%d observations=%d events=%d total=%d",
			status, parsedCursor, highWater, observations, events, total)
	}
}

func TestProcessorStableProviderClaimPreventsCrossSourceDoubleCountIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	userID := int64(990028)
	first := newUsageFixture(t, database, userID, "usage-claim-first", "claude-code")
	defer first.cleanup(t)
	second := newUsageFixtureForExistingUser(t, database, userID, "usage-claim-second", "claude-code")
	content := readUsageFixture(t, "claude_monotonic.jsonl")
	first.appendChunk(t, content)
	second.appendChunk(t, content)
	firstProcessor, _ := NewProcessor(database, first.store, "5m")
	if err := firstProcessor.Process(context.Background(), first.jobs[0]); err != nil {
		t.Fatal(err)
	}
	secondProcessor, _ := NewProcessor(database, second.store, "5m")
	if err := secondProcessor.Process(context.Background(), second.jobs[0]); err != nil {
		t.Fatal(err)
	}
	var firstStatus, secondStatus, secondQuality string
	if err := database.QueryRow(`
		SELECT
			(SELECT status FROM session_metrics_revisions WHERE generation_id = $1),
			(SELECT status FROM session_metrics_revisions WHERE generation_id = $2),
			(SELECT quality_status FROM session_metrics_revisions WHERE generation_id = $2)`,
		first.generationID, second.generationID).Scan(&firstStatus, &secondStatus, &secondQuality); err != nil {
		t.Fatal(err)
	}
	var total int64
	if err := database.QueryRow(`
		SELECT COALESCE(SUM(total_tokens), 0) FROM session_daily_usage
		WHERE user_id = $1 AND valid_to IS NULL`, userID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if firstStatus != "active" || secondStatus != "failed" || secondQuality != "conflict" || total != 250 {
		t.Fatalf("first=%s second=%s secondQuality=%s total=%d", firstStatus, secondStatus, secondQuality, total)
	}
}

func TestProcessorTargetedNormalizerRevisionAtomicallyReplacesOldIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990029, "usage-targeted-normalizer", "claude-code")
	defer fixture.cleanup(t)
	fixture.appendChunk(t, readUsageFixture(t, "claude_monotonic.jsonl"))
	fiveMinuteProcessor, _ := NewProcessor(database, fixture.store, "5m")
	if err := fiveMinuteProcessor.Process(context.Background(), fixture.jobs[0]); err != nil {
		t.Fatal(err)
	}
	oldRevisionID := activeMetricsRevision(t, database, fixture.sourceID)

	oneHourProcessor, _ := NewProcessor(database, fixture.store, "1h")
	var newRevisionID string
	if err := database.QueryRow(`
		INSERT INTO session_metrics_revisions (
			source_id, generation_id, parser_version, normalizer_version,
			status, build_start_cursor, source_high_water_cursor
		) VALUES ($1, $2, $3, $4, 'building', $5, $5)
		RETURNING id`, fixture.sourceID, fixture.generationID, ParserVersion,
		oneHourProcessor.normalizerVersion, fixture.cursor).Scan(&newRevisionID); err != nil {
		t.Fatal(err)
	}
	targetedJob := fixture.jobs[0]
	targetedJob.TargetMetricsRevisionID = sql.NullString{String: newRevisionID, Valid: true}
	if err := oneHourProcessor.Process(context.Background(), targetedJob); err != nil {
		t.Fatal(err)
	}
	if got := activeMetricsRevision(t, database, fixture.sourceID); got != newRevisionID {
		t.Fatalf("active revision=%s want=%s", got, newRevisionID)
	}
	var oldStatus, newStatus string
	var cache5m, cache1h, total int64
	if err := database.QueryRow(`
		SELECT
			(SELECT status FROM session_metrics_revisions WHERE id = $1),
			(SELECT status FROM session_metrics_revisions WHERE id = $2),
			COALESCE(SUM(cache_write_5m_tokens), 0),
			COALESCE(SUM(cache_write_1h_tokens), 0),
			COALESCE(SUM(total_tokens), 0)
		FROM session_daily_usage WHERE revision_id = $2 AND valid_to IS NULL`,
		oldRevisionID, newRevisionID).Scan(&oldStatus, &newStatus, &cache5m, &cache1h, &total); err != nil {
		t.Fatal(err)
	}
	if oldStatus != "superseded" || newStatus != "active" || cache5m != 0 || cache1h != 10 || total != 250 {
		t.Fatalf("old=%s new=%s cache5m=%d cache1h=%d total=%d", oldStatus, newStatus, cache5m, cache1h, total)
	}
}

func TestProcessorMalformedAndUnknownUsageFailQualityGateIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990030, "usage-malformed-unknown", "claude-code")
	defer fixture.cleanup(t)
	content := []byte(
		"{not-json}\n" +
			`{"type":"assistant","timestamp":"2026-07-10T01:00:00Z","message":{"id":"missing-input","model":"claude-test","usage":{"output_tokens":2}}}` + "\n",
	)
	fixture.appendChunk(t, content)
	processor, _ := NewProcessor(database, fixture.store, "5m")
	if err := processor.Process(context.Background(), fixture.jobs[0]); err != nil {
		t.Fatal(err)
	}
	var status, quality string
	var malformed, unknown int64
	if err := database.QueryRow(`
		SELECT status, quality_status, malformed_event_count, unknown_usage_event_count
		FROM session_metrics_revisions WHERE generation_id = $1`, fixture.generationID).Scan(
		&status, &quality, &malformed, &unknown,
	); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || quality != "incomplete" || malformed != 1 || unknown != 1 {
		t.Fatalf("status=%s quality=%s malformed=%d unknown=%d", status, quality, malformed, unknown)
	}
}

type dbUsageFixture struct {
	database     *sql.DB
	userID       int64
	sessionID    string
	sourceID     string
	generationID string
	cursor       int64
	line         int64
	store        memoryUsageStore
	jobs         []sessionsync.ProcessingJob
}

func newUsageFixture(t *testing.T, database *sql.DB, userID int64, ref, provider string) *dbUsageFixture {
	t.Helper()
	cleanupUsageUser(t, database, userID)
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, $2)`, userID, ref); err != nil {
		t.Fatal(err)
	}
	return newUsageFixtureForExistingUser(t, database, userID, ref, provider)
}

func newUsageFixtureForExistingUser(t *testing.T, database *sql.DB, userID int64, ref, provider string) *dbUsageFixture {
	t.Helper()
	fixture := &dbUsageFixture{database: database, userID: userID, store: memoryUsageStore{}}
	if err := database.QueryRow(`
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at)
		VALUES ($1, $2, $3, now()) RETURNING id`, ref, userID, provider).Scan(&fixture.sessionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_sources (session_id, source_role, source_key)
		VALUES ($1, 'main', $2) RETURNING id`, fixture.sessionID, provider+":"+ref+":main").Scan(&fixture.sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_source_generations (source_id, status)
		VALUES ($1, 'active') RETURNING id`, fixture.sourceID).Scan(&fixture.generationID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE session_sources SET active_generation_id = $1 WHERE id = $2`, fixture.generationID, fixture.sourceID); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture *dbUsageFixture) appendChunk(t *testing.T, content []byte) {
	t.Helper()
	fixture.appendChunkToGeneration(t, fixture.generationID, content)
}

func (fixture *dbUsageFixture) appendChunkToGeneration(t *testing.T, generationID string, content []byte) {
	t.Helper()
	start := int64(0)
	if generationID == fixture.generationID {
		start = fixture.cursor
	} else if err := fixture.database.QueryRow(`
		SELECT expected_cursor FROM session_source_generations WHERE id = $1`, generationID).Scan(&start); err != nil {
		t.Fatal(err)
	}
	end := start + int64(len(content))
	lineCount := int64(len(completeLines(content)))
	if lineCount == 0 {
		t.Fatal("chunk must contain complete lines")
	}
	objectKey := fmt.Sprintf("usage-fixture/%s/%d-%d", generationID, start, end)
	fixture.store[objectKey] = append([]byte(nil), content...)
	var chunkID string
	if err := fixture.database.QueryRow(`
		INSERT INTO session_upload_chunks (
			generation_id, start_cursor, end_cursor, start_line, end_line,
			content_sha256, content_epoch, raw_object_key, object_status,
			content_index_status, usage_parse_status
		) VALUES ($1, $2, $3, $4, $5, $6, 0, $7, 'available', 'pending', 'pending')
		RETURNING id`, generationID, start, end, fixture.line+1, fixture.line+lineCount,
		sessionsync.HashBytes(content), objectKey).Scan(&chunkID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.Exec(`
		UPDATE session_source_generations
		SET expected_cursor = $2,
			prefix_checkpoint_hash = $3,
			prefix_checkpoint_algorithm_version = $4,
			prefix_checkpoint_state = $5,
			prefix_checkpoint_state_format = $6
		WHERE id = $1`, generationID, end, sessionsync.HashBytes([]byte(fmt.Sprintf("%s:%d", generationID, end))),
		sessionsync.PrefixCheckpointAlgorithm, []byte{1}, sessionsync.PrefixCheckpointStateFormat); err != nil {
		t.Fatal(err)
	}
	fixture.jobs = append(fixture.jobs, sessionsync.ProcessingJob{
		Type: sessionsync.JobParseUsageChunk, SessionID: fixture.sessionID,
		GenerationID: sql.NullString{String: generationID, Valid: true},
		ChunkID:      sql.NullString{String: chunkID, Valid: true},
	})
	fixture.line += lineCount
	if generationID == fixture.generationID {
		fixture.cursor = end
	}
}

func (fixture *dbUsageFixture) replaceGeneration(t *testing.T) string {
	t.Helper()
	if _, err := fixture.database.Exec(`
		UPDATE session_source_generations SET status = 'superseded' WHERE id = $1`, fixture.generationID); err != nil {
		t.Fatal(err)
	}
	var generationID string
	if err := fixture.database.QueryRow(`
		INSERT INTO session_source_generations (source_id, status)
		VALUES ($1, 'active') RETURNING id`, fixture.sourceID).Scan(&generationID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.Exec(`
		UPDATE session_sources SET active_generation_id = $1 WHERE id = $2`, generationID, fixture.sourceID); err != nil {
		t.Fatal(err)
	}
	return generationID
}

func (fixture *dbUsageFixture) cleanup(t *testing.T) {
	t.Helper()
	cleanupUsageUser(t, fixture.database, fixture.userID)
}

func openUsageIntegrationDatabase(t *testing.T) *sql.DB {
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

func cleanupUsageUser(t *testing.T, database *sql.DB, userID int64) {
	t.Helper()
	if _, err := database.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}
}

func readUsageFixture(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile("../../../testdata/v2_usage/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func completeLines(content []byte) [][]byte {
	parts := bytes.SplitAfter(content, []byte("\n"))
	if len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func assertDailyUsage(t *testing.T, database *sql.DB, sessionID string, wantRows, wantTotal int64, wantQuality string, wantQualityRows int64) {
	t.Helper()
	var rows, total int64
	var qualities int64
	if err := database.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(total_tokens), 0),
			COUNT(*) FILTER (WHERE quality_status = $2)
		FROM session_daily_usage
		WHERE session_id = $1 AND valid_to IS NULL`, sessionID, wantQuality).Scan(&rows, &total, &qualities); err != nil {
		t.Fatal(err)
	}
	if rows != wantRows || total != wantTotal || qualities != wantQualityRows {
		t.Fatalf("daily rows=%d total=%d quality rows=%d", rows, total, qualities)
	}
}

func activeMetricsRevision(t *testing.T, database *sql.DB, sourceID string) string {
	t.Helper()
	var revisionID string
	if err := database.QueryRow(`
		SELECT active_revision_id FROM session_source_metrics_states WHERE source_id = $1`, sourceID).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	return revisionID
}
