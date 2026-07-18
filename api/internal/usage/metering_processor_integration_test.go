package usage

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

	appconfig "github.com/aidashboard/api/config"
	"github.com/aidashboard/api/internal/contentreader"
	"github.com/aidashboard/api/internal/reportsourcecatalog"
	"github.com/aidashboard/api/internal/sessionsync"
	appstorage "github.com/aidashboard/api/storage"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type mutableMemoryUsageStore struct {
	objects map[string][]byte
}

func (store *mutableMemoryUsageStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	content, ok := store.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func (store *mutableMemoryUsageStore) Delete(_ context.Context, key string) error {
	delete(store.objects, key)
	return nil
}

func TestMeteringEnvelopeClearAndReplayIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990031, "usage-metering-clear", "claude-code")
	defer fixture.cleanup(t)
	content := readUsageFixture(t, "claude_monotonic.jsonl")
	fixture.appendChunk(t, content)
	if _, err := database.Exec(`
		UPDATE session_source_generations SET prefix_checkpoint_hash = $2 WHERE id = $1`,
		fixture.generationID, sessionsync.HashBytes(content)); err != nil {
		t.Fatal(err)
	}
	store := &mutableMemoryUsageStore{objects: map[string][]byte{}}
	for key, value := range fixture.store {
		store.objects[key] = append([]byte(nil), value...)
	}

	usageProcessor, _ := NewProcessor(database, store, "5m")
	if err := usageProcessor.Process(context.Background(), fixture.jobs[0]); err != nil {
		t.Fatal(err)
	}
	oldRevisionID := activeMetricsRevision(t, database, fixture.sourceID)
	projectionRevisionID, digestRevisionID, projectedEvents := insertReadableProjectionFixture(t, database, store, fixture)
	reader, err := contentreader.New(database, store)
	if err != nil {
		t.Fatal(err)
	}
	readResult, err := reader.Stream(context.Background(), contentreader.Request{
		RevisionID: projectionRevisionID, StartCursor: 0, EndCursor: fixture.cursor,
	}, func(contentreader.Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if readResult.EventCount != projectedEvents {
		t.Fatalf("reader events=%d projected=%d", readResult.EventCount, projectedEvents)
	}
	contributionsBefore, costsBefore, rollupsBefore := retainedUsageFactCounts(t, database, fixture.sessionID)
	if contributionsBefore == 0 || costsBefore == 0 || rollupsBefore == 0 {
		t.Fatalf("missing retained facts before clear: contributions=%d costs=%d rollups=%d",
			contributionsBefore, costsBefore, rollupsBefore)
	}

	lifecycle, err := sessionsync.NewContentLifecycleService(database)
	if err != nil {
		t.Fatal(err)
	}
	clear, err := lifecycle.RequestClear(context.Background(), fmt.Sprint(fixture.userID), fixture.sessionID, "integration clear")
	if err != nil {
		t.Fatal(err)
	}
	if clear.ContentStatus != string(sessionsync.ContentClearing) || clear.PendingJobs != 1 {
		t.Fatalf("clear=%+v", clear)
	}
	if _, err := reader.Stream(context.Background(), contentreader.Request{
		RevisionID: projectionRevisionID, StartCursor: 0, EndCursor: fixture.cursor,
	}, func(contentreader.Event) error { return nil }); !errors.Is(err, contentreader.ErrContentUnavailable) {
		t.Fatalf("reader during clear error=%v", err)
	}
	metering, _ := NewMeteringProcessor(database, store)
	buildJob := sessionsync.ProcessingJob{
		Type: sessionsync.JobBuildMeteringEnvelope, SessionID: fixture.sessionID,
		GenerationID: sql.NullString{String: fixture.generationID, Valid: true},
		ContentEpoch: sql.NullInt64{Int64: clear.ContentEpoch, Valid: true},
	}
	if err := metering.Process(context.Background(), buildJob); err != nil {
		t.Fatal(err)
	}
	var deleteJobs int64
	if err := database.QueryRow(`
		SELECT COUNT(*) FROM session_processing_jobs
		WHERE session_id = $1 AND job_type = $2 AND content_epoch = $3`,
		fixture.sessionID, sessionsync.JobDeleteObject, clear.ContentEpoch).Scan(&deleteJobs); err != nil {
		t.Fatal(err)
	}
	if deleteJobs != 1 {
		t.Fatalf("delete jobs=%d", deleteJobs)
	}
	deleteJob := sessionsync.ProcessingJob{
		Type: sessionsync.JobDeleteObject, SessionID: fixture.sessionID,
		GenerationID: fixture.jobs[0].GenerationID, ChunkID: fixture.jobs[0].ChunkID,
		ContentEpoch: sql.NullInt64{Int64: clear.ContentEpoch, Valid: true},
	}
	if err := metering.Process(context.Background(), deleteJob); err != nil {
		t.Fatal(err)
	}

	var contentStatus, objectStatus, manifestStatus, catalogStatus string
	var projectionStatus, digestStatus string
	var sourceLines, envelopeRows, contentEvents int64
	var leakedText, hasActiveProjection bool
	if err := database.QueryRow(`
		SELECT s.content_status, c.object_status, m.status,
			m.source_record_count, m.envelope_record_count,
			(SELECT COUNT(*) FROM session_content_events e
			 JOIN session_content_projection_revisions p ON p.id = e.content_projection_revision_id
			 WHERE p.generation_id = $2),
			EXISTS (
				SELECT 1 FROM session_metering_envelopes e
				WHERE e.generation_id = $2 AND e.raw_usage_json::text LIKE '%Test monotonic usage snapshots.%'
			),
			(SELECT status FROM report_source_slice_catalog WHERE session_id = s.id LIMIT 1),
			(SELECT status FROM session_content_projection_revisions WHERE id = $4),
			(SELECT status FROM session_slice_digest_revisions WHERE id = $5),
			EXISTS (SELECT 1 FROM session_sources src
				WHERE src.session_id = s.id AND src.active_content_projection_revision_id IS NOT NULL)
		FROM sessions s
		JOIN session_upload_chunks c ON c.id = $3
		JOIN session_metering_envelope_manifests m ON m.generation_id = $2
		WHERE s.id = $1`, fixture.sessionID, fixture.generationID, fixture.jobs[0].ChunkID.String,
		projectionRevisionID, digestRevisionID).Scan(
		&contentStatus, &objectStatus, &manifestStatus, &sourceLines, &envelopeRows,
		&contentEvents, &leakedText, &catalogStatus, &projectionStatus, &digestStatus,
		&hasActiveProjection,
	); err != nil {
		t.Fatal(err)
	}
	if contentStatus != string(sessionsync.ContentCleared) || objectStatus != "deleted" ||
		manifestStatus != "validated" || sourceLines != 5 || envelopeRows != 4 ||
		contentEvents != 0 || leakedText || len(store.objects) != 0 || catalogStatus != "cleared" ||
		projectionStatus != "superseded" || digestStatus != "superseded" || hasActiveProjection {
		t.Fatalf("content=%s object=%s manifest=%s lines=%d envelopes=%d events=%d leaked=%v objects=%d catalog=%s projection=%s digest=%s active_projection=%v",
			contentStatus, objectStatus, manifestStatus, sourceLines, envelopeRows,
			contentEvents, leakedText, len(store.objects), catalogStatus, projectionStatus,
			digestStatus, hasActiveProjection)
	}
	if _, err := reader.Stream(context.Background(), contentreader.Request{
		RevisionID: projectionRevisionID, StartCursor: 0, EndCursor: fixture.cursor,
	}, func(contentreader.Event) error { return nil }); !errors.Is(err, contentreader.ErrContentUnavailable) {
		t.Fatalf("reader after clear error=%v", err)
	}
	contributionsAfter, costsAfter, rollupsAfter := retainedUsageFactCounts(t, database, fixture.sessionID)
	if contributionsAfter != contributionsBefore || costsAfter != costsBefore || rollupsAfter != rollupsBefore {
		t.Fatalf("retained facts changed: before=%d/%d/%d after=%d/%d/%d",
			contributionsBefore, costsBefore, rollupsBefore,
			contributionsAfter, costsAfter, rollupsAfter)
	}
	var auditReason string
	var auditUserID int64
	var objectsDeleted bool
	if err := database.QueryRow(`
		SELECT reason, cleared_by, objects_deleted_at IS NOT NULL
		FROM session_content_tombstones WHERE id = $1`, clear.TombstoneID).Scan(
		&auditReason, &auditUserID, &objectsDeleted,
	); err != nil {
		t.Fatal(err)
	}
	if auditReason != "integration clear" || auditUserID != fixture.userID || !objectsDeleted {
		t.Fatalf("audit reason=%q user=%d objects_deleted=%v", auditReason, auditUserID, objectsDeleted)
	}
	if got := activeMetricsRevision(t, database, fixture.sourceID); got != oldRevisionID {
		t.Fatalf("content clear changed metrics revision: got=%s want=%s", got, oldRevisionID)
	}
	assertDailyUsage(t, database, fixture.sessionID, 2, 250, "estimated", 1)

	replayProcessor, _ := NewProcessor(database, store, "1h")
	var replayRevisionID string
	if err := database.QueryRow(`
		INSERT INTO session_metrics_revisions (
			source_id, generation_id, parser_version, normalizer_version,
			status, build_start_cursor, source_high_water_cursor
		) VALUES ($1, $2, $3, $4, 'building', $5, $5) RETURNING id`,
		fixture.sourceID, fixture.generationID, ParserVersion,
		replayProcessor.normalizerVersion, fixture.cursor).Scan(&replayRevisionID); err != nil {
		t.Fatal(err)
	}
	replayJob := fixture.jobs[0]
	replayJob.TargetMetricsRevisionID = sql.NullString{String: replayRevisionID, Valid: true}
	if err := replayProcessor.Process(context.Background(), replayJob); err != nil {
		t.Fatal(err)
	}
	if got := activeMetricsRevision(t, database, fixture.sourceID); got != replayRevisionID {
		t.Fatalf("envelope replay active revision=%s want=%s", got, replayRevisionID)
	}
	var total, cache1h int64
	if err := database.QueryRow(`
		SELECT COALESCE(SUM(total_tokens), 0), COALESCE(SUM(cache_write_1h_tokens), 0)
		FROM session_daily_usage WHERE revision_id = $1 AND valid_to IS NULL`, replayRevisionID).Scan(&total, &cache1h); err != nil {
		t.Fatal(err)
	}
	if total != 250 || cache1h != 10 {
		t.Fatalf("replayed total=%d cache1h=%d", total, cache1h)
	}
}

func TestMeteringEnvelopeUnsafeInputDoesNotDeleteObjectIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990032, "usage-metering-unsafe", "claude-code")
	defer fixture.cleanup(t)
	content := []byte("{not-json}\n")
	fixture.appendChunk(t, content)
	if _, err := database.Exec(`
		UPDATE session_source_generations SET prefix_checkpoint_hash = $2 WHERE id = $1`,
		fixture.generationID, sessionsync.HashBytes(content)); err != nil {
		t.Fatal(err)
	}
	store := &mutableMemoryUsageStore{objects: map[string][]byte{}}
	for key, value := range fixture.store {
		store.objects[key] = value
	}
	lifecycle, _ := sessionsync.NewContentLifecycleService(database)
	clear, err := lifecycle.RequestClear(context.Background(), fmt.Sprint(fixture.userID), fixture.sessionID, "unsafe clear")
	if err != nil {
		t.Fatal(err)
	}
	metering, _ := NewMeteringProcessor(database, store)
	err = metering.Process(context.Background(), sessionsync.ProcessingJob{
		Type: sessionsync.JobBuildMeteringEnvelope, SessionID: fixture.sessionID,
		GenerationID: sql.NullString{String: fixture.generationID, Valid: true},
		ContentEpoch: sql.NullInt64{Int64: clear.ContentEpoch, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	var contentStatus, objectStatus, manifestStatus string
	if err := database.QueryRow(`
		SELECT s.content_status, c.object_status, m.status
		FROM sessions s
		JOIN session_upload_chunks c ON c.generation_id = $2
		JOIN session_metering_envelope_manifests m ON m.generation_id = $2
		WHERE s.id = $1`, fixture.sessionID, fixture.generationID).Scan(
		&contentStatus, &objectStatus, &manifestStatus,
	); err != nil {
		t.Fatal(err)
	}
	if contentStatus != string(sessionsync.ContentClearingFailed) || objectStatus != "available" ||
		manifestStatus != "failed" || len(store.objects) != 1 {
		t.Fatalf("content=%s object=%s manifest=%s objects=%d", contentStatus, objectStatus, manifestStatus, len(store.objects))
	}
	retry, err := lifecycle.RequestClear(context.Background(), fmt.Sprint(fixture.userID), fixture.sessionID, "retry unsafe clear")
	if err != nil {
		t.Fatal(err)
	}
	if retry.ContentStatus != string(sessionsync.ContentClearing) || retry.ContentEpoch != clear.ContentEpoch+1 || retry.PendingJobs != 1 {
		t.Fatalf("retry=%+v", retry)
	}
}

func TestMeteringClearDeletesRealMinIOObjectIntegration(t *testing.T) {
	endpoint := os.Getenv("AIDA_TEST_MINIO_ENDPOINT")
	accessKey := os.Getenv("AIDA_TEST_MINIO_ACCESS_KEY")
	secretKey := os.Getenv("AIDA_TEST_MINIO_SECRET_KEY")
	if endpoint == "" || accessKey == "" || secretKey == "" {
		t.Skip("AIDA_TEST_MINIO_* is not configured")
	}
	bucket := fmt.Sprintf("aida-v2-metering-%d", time.Now().UnixNano())
	store, err := appstorage.NewMinioStorage(&appconfig.Config{
		MinioEndpoint: endpoint, MinioAccessKey: accessKey, MinioSecretKey: secretKey,
		MinioBucket: bucket,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, "")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.RemoveBucket(context.Background(), bucket) })

	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990033, "usage-metering-real-minio", "codex")
	defer fixture.cleanup(t)
	content := readUsageFixture(t, "codex_cumulative.jsonl")
	fixture.appendChunk(t, content)
	if _, err := database.Exec(`
		UPDATE session_source_generations SET prefix_checkpoint_hash = $2 WHERE id = $1`,
		fixture.generationID, sessionsync.HashBytes(content)); err != nil {
		t.Fatal(err)
	}
	var objectKey string
	for key, value := range fixture.store {
		objectKey = key
		if err := store.PutVerified(context.Background(), key, bytes.NewReader(value), int64(len(value)), sessionsync.HashBytes(value)); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = client.RemoveObject(context.Background(), bucket, objectKey, minio.RemoveObjectOptions{}) })
	lifecycle, _ := sessionsync.NewContentLifecycleService(database)
	clear, err := lifecycle.RequestClear(context.Background(), fmt.Sprint(fixture.userID), fixture.sessionID, "real MinIO clear")
	if err != nil {
		t.Fatal(err)
	}
	metering, _ := NewMeteringProcessor(database, store)
	if err := metering.Process(context.Background(), sessionsync.ProcessingJob{
		Type: sessionsync.JobBuildMeteringEnvelope, SessionID: fixture.sessionID,
		GenerationID: sql.NullString{String: fixture.generationID, Valid: true},
		ContentEpoch: sql.NullInt64{Int64: clear.ContentEpoch, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := metering.Process(context.Background(), sessionsync.ProcessingJob{
		Type: sessionsync.JobDeleteObject, SessionID: fixture.sessionID,
		GenerationID: fixture.jobs[0].GenerationID, ChunkID: fixture.jobs[0].ChunkID,
		ContentEpoch: sql.NullInt64{Int64: clear.ContentEpoch, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.StatObject(context.Background(), bucket, objectKey, minio.StatObjectOptions{}); err == nil {
		t.Fatal("real MinIO object still exists after clear")
	}
	var status string
	if err := database.QueryRow(`SELECT content_status FROM sessions WHERE id = $1`, fixture.sessionID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(sessionsync.ContentCleared) {
		t.Fatalf("content status=%s", status)
	}
}

func insertReadableProjectionFixture(
	t *testing.T,
	database *sql.DB,
	store *mutableMemoryUsageStore,
	fixture *dbUsageFixture,
) (string, string, int64) {
	t.Helper()
	var revisionID string
	if err := database.QueryRow(`
		INSERT INTO session_content_projection_revisions (
			generation_id, content_parser_version, status,
			source_high_water_cursor
		) VALUES ($1, $2, 'building', $3) RETURNING id`,
		fixture.generationID, sessionsync.ContentParserVersion, fixture.cursor).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	var sliceID string
	if err := database.QueryRow(`
		INSERT INTO session_content_slices (
			session_id, source_id, generation_id, start_cursor, end_cursor
		) VALUES ($1, $2, $3, 0, $4) RETURNING id`,
		fixture.sessionID, fixture.sourceID, fixture.generationID, fixture.cursor).Scan(&sliceID); err != nil {
		t.Fatal(err)
	}
	if err := reportsourcecatalog.EnsureSlice(context.Background(), database, sliceID); err != nil {
		t.Fatal(err)
	}
	processor, err := sessionsync.NewContentProjectionProcessor(database, store)
	if err != nil {
		t.Fatal(err)
	}
	indexJob := sessionsync.ProcessingJob{
		Type: sessionsync.JobIndexContentChunk, SessionID: fixture.sessionID,
		GenerationID:     sql.NullString{String: fixture.generationID, Valid: true},
		ChunkID:          fixture.jobs[0].ChunkID,
		TargetRevisionID: sql.NullString{String: revisionID, Valid: true},
		ContentEpoch:     sql.NullInt64{Int64: 0, Valid: true},
	}
	if err := processor.Process(context.Background(), indexJob); err != nil {
		t.Fatal(err)
	}
	if err := processor.Process(context.Background(), sessionsync.ProcessingJob{
		Type: sessionsync.JobRebuildContentRevision, SessionID: fixture.sessionID,
		GenerationID:     indexJob.GenerationID,
		TargetRevisionID: indexJob.TargetRevisionID,
		ContentEpoch:     indexJob.ContentEpoch,
	}); err != nil {
		t.Fatal(err)
	}
	var eventCount int64
	if err := database.QueryRow(`
		SELECT COUNT(*) FROM session_content_events
		WHERE content_projection_revision_id = $1`, revisionID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount == 0 {
		t.Fatal("projection produced no events")
	}
	var digestRevisionID string
	digestPayload := []byte(`{}`)
	if err := database.QueryRow(`
		INSERT INTO session_slice_digest_revisions (
			session_content_slice_id, content_projection_revision_id, generation_id,
			content_epoch, digest_version, redaction_version, status, digest_json,
			source_event_count, included_event_count, omitted_event_count,
			source_bytes, digest_bytes, source_sha256, digest_sha256, ready_at
		) VALUES ($1, $2, $3, 0, 'lifecycle-test-v1', 'redaction-test-v1', 'ready',
			$4::jsonb, $5, $5, 0, $6, $7, $8, $9, now())
		RETURNING id`, sliceID, revisionID, fixture.generationID, digestPayload,
		eventCount, fixture.cursor, len(digestPayload), sessionsync.HashBytes([]byte("source")),
		sessionsync.HashBytes(digestPayload)).Scan(&digestRevisionID); err != nil {
		t.Fatal(err)
	}
	return revisionID, digestRevisionID, eventCount
}

func retainedUsageFactCounts(t *testing.T, database *sql.DB, sessionID string) (int64, int64, int64) {
	t.Helper()
	var contributions, costs, rollups int64
	if err := database.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM session_usage_contributions WHERE member_session_id = $1),
			(SELECT COUNT(*) FROM session_usage_contribution_costs cost
				JOIN session_usage_contributions contribution ON contribution.id = cost.contribution_id
				WHERE contribution.member_session_id = $1),
			(SELECT COUNT(*) FROM session_family_rollup_versions WHERE root_session_id = $1)`,
		sessionID).Scan(&contributions, &costs, &rollups); err != nil {
		t.Fatal(err)
	}
	return contributions, costs, rollups
}
