package usage

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
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
	assertContributionLedger(t, database, fixture.sessionID, 3, 250, map[string]int64{
		contributionInitial: 2, contributionAdvance: 1,
	}, []int64{190, 60})
	assertActiveFamilyRollup(t, database, fixture.sessionID, 250, 250, 0, 2)
}

func TestProcessorCodexCumulativeAcrossChunksIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990022, "usage-codex-cumulative", "codex")
	defer fixture.cleanup(t)
	content := readUsageFixture(t, "codex_cumulative.jsonl")
	lines := completeLines(content)
	firstChunk := bytes.Join(lines[:3], nil)
	fixture.appendChunk(t, firstChunk)
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
	assertContributionLedger(t, database, fixture.sessionID, 2, 220, map[string]int64{
		contributionCheckpointDelta: 2,
	}, []int64{120, 100})
	assertActiveFamilyRollup(t, database, fixture.sessionID, 220, 220, 0, 2)
	if _, err := database.Exec(`
		INSERT INTO session_processing_jobs (job_type, session_id, content_epoch, payload)
		SELECT 'purge_session', $1, 0, jsonb_build_object('test_job', sequence)
		FROM generate_series(1, $2) sequence`, fixture.sessionID, contributionBackfillMaxOutstandingJobs); err != nil {
		t.Fatal(err)
	}
	if busy, err := ContributionBackfillForegroundBusy(context.Background(), database); err != nil || !busy {
		t.Fatalf("backfill queue pressure busy=%t err=%v", busy, err)
	}
}

func TestProcessorLocksSessionBeforeUsageRevisionIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990040, "usage-session-lock-order", "claude-code")
	defer fixture.cleanup(t)
	fixture.appendChunk(t, readUsageFixture(t, "claude_monotonic.jsonl"))

	processorURL, err := url.Parse(os.Getenv("AIDA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	query := processorURL.Query()
	query.Set("application_name", "usage-session-lock-order-test")
	processorURL.RawQuery = query.Encode()
	processorDatabase, err := projectdb.Connect(processorURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer processorDatabase.Close()
	processorDatabase.SetMaxOpenConns(1)

	blocker, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	var lockedSessionID string
	if err := blocker.QueryRow(`SELECT id FROM sessions WHERE id = $1 FOR UPDATE`, fixture.sessionID).Scan(&lockedSessionID); err != nil {
		t.Fatal(err)
	}

	processor, err := NewProcessor(processorDatabase, fixture.store, "5m")
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- processor.Process(context.Background(), fixture.jobs[0])
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		select {
		case processErr := <-result:
			t.Fatalf("processor returned before waiting for the session lock: %v", processErr)
		default:
		}
		var waiting bool
		if err := database.QueryRow(`
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE application_name = 'usage-session-lock-order-test'
					AND wait_event_type = 'Lock'
			)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("processor did not wait for the locked session")
		}
		time.Sleep(10 * time.Millisecond)
	}

	probe, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var revisionLockAvailable bool
	if err := probe.QueryRow(
		`SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))`,
		"usage-revision:"+fixture.generationID,
	).Scan(&revisionLockAvailable); err != nil {
		_ = probe.Rollback()
		t.Fatal(err)
	}
	_ = probe.Rollback()
	if !revisionLockAvailable {
		t.Fatal("usage revision lock was acquired before the session lock")
	}

	if err := blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	select {
	case processErr := <-result:
		if processErr != nil {
			t.Fatal(processErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("processor did not finish after the session lock was released")
	}
}

func TestProcessorSuppressesExactCrossSourceUsageDuplicatesIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	const userID = int64(990039)
	cleanupUsageUser(t, database, userID)
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, $2)`, userID, "usage-cross-source-dedup"); err != nil {
		t.Fatal(err)
	}
	defer cleanupUsageUser(t, database, userID)

	shared := `{"timestamp":"2026-07-10T00:01:00Z","type":"assistant","message":{"id":"shared-message","model":"claude-sonnet","usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"
	owner := newUsageFixtureForExistingUser(t, database, userID, "usage-dedup-owner", "claude-code")
	owner.appendChunk(t, []byte(shared+
		`{"timestamp":"2026-07-10T00:02:00Z","type":"assistant","message":{"id":"owner-message","model":"claude-sonnet","usage":{"input_tokens":20,"output_tokens":5}}}`+"\n"))
	ownerProcessor, _ := NewProcessor(database, owner.store, "5m")
	if err := ownerProcessor.Process(context.Background(), owner.jobs[0]); err != nil {
		t.Fatal(err)
	}

	candidate := newUsageFixtureForExistingUser(t, database, userID, "usage-dedup-candidate", "claude-code")
	candidate.appendChunk(t, []byte(shared+
		`{"timestamp":"2026-07-10T00:03:00Z","type":"assistant","message":{"id":"candidate-message","model":"claude-sonnet","usage":{"input_tokens":30,"output_tokens":5}}}`+"\n"))
	candidateProcessor, _ := NewProcessor(database, candidate.store, "5m")
	if err := candidateProcessor.Process(context.Background(), candidate.jobs[0]); err != nil {
		t.Fatal(err)
	}

	var status string
	var duplicates, activeComponents, contributions, claims, totalTokens int64
	if err := database.QueryRow(`
		SELECT revision.status, revision.duplicate_usage_event_count,
			(SELECT COUNT(*) FROM session_usage_components component
			 WHERE component.revision_id = revision.id AND component.valid_to IS NULL),
			(SELECT COUNT(*) FROM session_usage_contributions contribution
			 WHERE contribution.revision_id = revision.id),
			(SELECT COUNT(*) FROM session_usage_event_claims claim
			 WHERE claim.active_source_id = revision.source_id),
			(SELECT COALESCE(SUM(contribution.total_tokens), 0)
			 FROM session_usage_contributions contribution WHERE contribution.revision_id = revision.id)
		FROM session_metrics_revisions revision
		WHERE revision.generation_id = $1`, candidate.generationID).Scan(
		&status, &duplicates, &activeComponents, &contributions, &claims, &totalTokens); err != nil {
		t.Fatal(err)
	}
	if status != "active" || duplicates != 1 || activeComponents != 1 || contributions != 1 ||
		claims != 1 || totalTokens != 35 {
		t.Fatalf("status=%s duplicates=%d components=%d contributions=%d claims=%d tokens=%d",
			status, duplicates, activeComponents, contributions, claims, totalTokens)
	}
	assertContributionLedger(t, database, owner.sessionID, 2, 40, map[string]int64{
		contributionInitial: 2,
	}, []int64{40})
	assertContributionLedger(t, database, candidate.sessionID, 1, 35, map[string]int64{
		contributionInitial: 1,
	}, []int64{35})

	mismatch := newUsageFixtureForExistingUser(t, database, userID, "usage-dedup-mismatch", "claude-code")
	mismatch.appendChunk(t, []byte(
		`{"timestamp":"2026-07-10T00:04:00Z","type":"assistant","message":{"id":"shared-message","model":"claude-sonnet","usage":{"input_tokens":11,"output_tokens":5}}}`+"\n"))
	mismatchProcessor, _ := NewProcessor(database, mismatch.store, "5m")
	if err := mismatchProcessor.Process(context.Background(), mismatch.jobs[0]); err != nil {
		t.Fatal(err)
	}
	var mismatchStatus, mismatchQuality, mismatchReason string
	if err := database.QueryRow(`
		SELECT status, quality_status, calculation_reason
		FROM session_metrics_revisions
		WHERE generation_id = $1`, mismatch.generationID).Scan(
		&mismatchStatus, &mismatchQuality, &mismatchReason); err != nil {
		t.Fatal(err)
	}
	if mismatchStatus != "failed" || mismatchQuality != "conflict" ||
		mismatchReason != "provider event differs from the event claimed by another source" {
		t.Fatalf("mismatch status=%s quality=%s reason=%s",
			mismatchStatus, mismatchQuality, mismatchReason)
	}
}

func TestProcessorLateParentMergesSessionFamilyRollupIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	const userID = int64(990035)
	cleanupUsageUser(t, database, userID)
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, $2)`, userID, "usage-family-late-parent"); err != nil {
		t.Fatal(err)
	}
	defer cleanupUsageUser(t, database, userID)

	child := newUsageFixtureForExistingUser(t, database, userID, "usage-family-child", "claude-code")
	if _, err := database.Exec(`UPDATE sessions SET parent_session_ref = 'usage-family-root' WHERE id = $1`, child.sessionID); err != nil {
		t.Fatal(err)
	}
	content := readUsageFixture(t, "claude_monotonic.jsonl")
	child.appendChunk(t, content)
	childProcessor, _ := NewProcessor(database, child.store, "5m")
	if err := childProcessor.Process(context.Background(), child.jobs[0]); err != nil {
		t.Fatal(err)
	}
	var childFamilyQuality string
	if err := database.QueryRow(`
		SELECT family.quality_status
		FROM session_family_versions family
		JOIN session_family_memberships membership ON membership.family_version_id = family.id
		WHERE membership.member_session_id = $1 AND membership.valid_to IS NULL
			AND family.status = 'active'`, child.sessionID).Scan(&childFamilyQuality); err != nil {
		t.Fatal(err)
	}
	if childFamilyQuality != "pending" {
		t.Fatalf("child family quality before parent=%s want=pending", childFamilyQuality)
	}

	root := newUsageFixtureForExistingUser(t, database, userID, "usage-family-root", "claude-code")
	root.appendChunk(t, bytes.ReplaceAll(content, []byte("msg-"), []byte("root-msg-")))
	rootProcessor, _ := NewProcessor(database, root.store, "5m")
	if err := rootProcessor.Process(context.Background(), root.jobs[0]); err != nil {
		t.Fatal(err)
	}

	var memberCount, childDepth int64
	var activeFamilyQuality string
	if err := database.QueryRow(`
		SELECT family.member_count, family.quality_status,
			MAX(membership.depth) FILTER (WHERE membership.member_session_id = $2)
		FROM session_family_versions family
		JOIN session_family_memberships membership ON membership.family_version_id = family.id
		WHERE family.root_session_id = $1 AND family.status = 'active'
		GROUP BY family.member_count, family.quality_status`, root.sessionID, child.sessionID).Scan(
		&memberCount, &activeFamilyQuality, &childDepth); err != nil {
		t.Fatal(err)
	}
	if memberCount != 2 || childDepth != 1 || activeFamilyQuality != "exact" {
		t.Fatalf("family members=%d childDepth=%d quality=%s", memberCount, childDepth, activeFamilyQuality)
	}
	assertActiveFamilyRollup(t, database, root.sessionID, 500, 250, 250, 2)

	var activeChildRoot string
	if err := database.QueryRow(`
		SELECT membership.root_session_id::text
		FROM session_family_memberships membership
		JOIN session_family_versions family ON family.id = membership.family_version_id
		WHERE membership.member_session_id = $1 AND membership.valid_to IS NULL
			AND family.status = 'active'`, child.sessionID).Scan(&activeChildRoot); err != nil {
		t.Fatal(err)
	}
	if activeChildRoot != root.sessionID {
		t.Fatalf("child root=%s want=%s", activeChildRoot, root.sessionID)
	}
}

func TestProcessorFamilyBackfillExcludesLegacyParserRevisionIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	const userID = int64(990037)
	cleanupUsageUser(t, database, userID)
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, $2)`, userID, "usage-family-mixed-parser"); err != nil {
		t.Fatal(err)
	}
	defer cleanupUsageUser(t, database, userID)

	content := readUsageFixture(t, "claude_monotonic.jsonl")
	root := newUsageFixtureForExistingUser(t, database, userID, "usage-family-mixed-root", "claude-code")
	root.appendChunk(t, content)
	rootProcessor, _ := NewProcessor(database, root.store, "5m")
	if err := rootProcessor.Process(context.Background(), root.jobs[0]); err != nil {
		t.Fatal(err)
	}
	rootRevisionID := activeMetricsRevision(t, database, root.sourceID)
	if _, err := database.Exec(`DELETE FROM session_usage_contributions WHERE revision_id = $1`, rootRevisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE session_metrics_revisions SET parser_version = 'usage-parser-v4' WHERE id = $1`, rootRevisionID); err != nil {
		t.Fatal(err)
	}

	child := newUsageFixtureForExistingUser(t, database, userID, "usage-family-mixed-child", "claude-code")
	if _, err := database.Exec(`UPDATE sessions SET parent_session_ref = 'usage-family-mixed-root' WHERE id = $1`, child.sessionID); err != nil {
		t.Fatal(err)
	}
	child.appendChunk(t, bytes.ReplaceAll(content, []byte("msg-"), []byte("child-msg-")))
	childProcessor, _ := NewProcessor(database, child.store, "5m")
	if err := childProcessor.Process(context.Background(), child.jobs[0]); err != nil {
		t.Fatal(err)
	}
	assertActiveFamilyRollup(t, database, root.sessionID, 250, 0, 250, 2)
}

func TestContributionBackfillEnqueuesVersionedJobsInCursorOrderIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990036, "usage-contribution-backfill", "codex")
	defer fixture.cleanup(t)
	content := readUsageFixture(t, "codex_cumulative.jsonl")
	lines := completeLines(content)
	firstChunk := bytes.Join(lines[:3], nil)
	fixture.appendChunk(t, firstChunk)
	fixture.appendChunk(t, bytes.Join(lines[3:], nil))

	var oldRevisionID string
	if err := database.QueryRow(`
		INSERT INTO session_metrics_revisions (
			source_id, generation_id, parser_version, normalizer_version,
			status, validated_through_cursor, source_high_water_cursor,
			validated_at, activated_at
		) VALUES ($1, $2, 'usage-parser-v4', 'token-normalizer-v1:claude-cache-write-5m',
			'active', $3, $3, now(), now())
		RETURNING id`, fixture.sourceID, fixture.generationID, fixture.cursor).Scan(&oldRevisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO session_source_metrics_states (
			source_id, active_revision_id, target_generation_id, status,
			active_usage_parsed_cursor, source_high_water_cursor
		) VALUES ($1, $2, $3, 'ready', $4, $4)`, fixture.sourceID,
		oldRevisionID, fixture.generationID, fixture.cursor); err != nil {
		t.Fatal(err)
	}
	normalizerVersion, _ := NormalizerRevision("5m")
	before, err := InspectContributionBackfill(context.Background(), database, normalizerVersion)
	if err != nil {
		t.Fatal(err)
	}
	if before.EligibleSources < 1 || before.MissingRevisions < 1 {
		t.Fatalf("before=%+v", before)
	}
	added, err := enqueueOneContributionBackfill(context.Background(), database, normalizerVersion, fixture.sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if !added {
		t.Fatal("target contribution backfill was not enqueued")
	}
	if again, err := enqueueOneContributionBackfill(context.Background(), database, normalizerVersion, fixture.sourceID); err != nil || again {
		t.Fatalf("idempotent enqueue=%t err=%v", again, err)
	}
	var failedJobID string
	if err := database.QueryRow(`
		SELECT job.id::text
		FROM session_processing_jobs job
		JOIN session_upload_chunks chunk ON chunk.id = job.chunk_id
		JOIN session_metrics_revisions revision ON revision.id = job.target_metrics_revision_id
		WHERE revision.generation_id = $1 AND revision.parser_version = $2
			AND job.job_type = 'parse_usage_chunk'
		ORDER BY chunk.start_cursor DESC LIMIT 1`, fixture.generationID, ParserVersion).Scan(&failedJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE session_processing_jobs
		SET status = 'dead', attempts = max_attempts, last_error = 'simulated old-code failure'
		WHERE id = $1`, failedJobID); err != nil {
		t.Fatal(err)
	}
	if recovered, err := enqueueOneContributionBackfill(context.Background(), database, normalizerVersion, fixture.sourceID); err != nil || !recovered {
		t.Fatalf("recover dead backfill job=%t err=%v", recovered, err)
	}
	var recoveredStatus string
	var recoveredAttempts int
	var recoveredError sql.NullString
	if err := database.QueryRow(`
		SELECT status, attempts, last_error FROM session_processing_jobs WHERE id = $1`, failedJobID).Scan(
		&recoveredStatus, &recoveredAttempts, &recoveredError); err != nil {
		t.Fatal(err)
	}
	if recoveredStatus != "pending" || recoveredAttempts != 0 || recoveredError.Valid {
		t.Fatalf("recovered job status=%s attempts=%d error=%v", recoveredStatus, recoveredAttempts, recoveredError)
	}
	if again, err := enqueueOneContributionBackfill(context.Background(), database, normalizerVersion, fixture.sourceID); err != nil || again {
		t.Fatalf("post-recovery idempotent enqueue=%t err=%v", again, err)
	}

	rows, err := database.Query(`
		SELECT job.job_type, COALESCE(chunk.start_cursor, -1)
		FROM session_processing_jobs job
		LEFT JOIN session_upload_chunks chunk ON chunk.id = job.chunk_id
		JOIN session_metrics_revisions revision ON revision.id = job.target_metrics_revision_id
		WHERE revision.generation_id = $1 AND revision.parser_version = $2
		ORDER BY job.created_at, job.id`, fixture.generationID, ParserVersion)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	types := []string{}
	cursors := []int64{}
	for rows.Next() {
		var jobType string
		var cursor int64
		if err := rows.Scan(&jobType, &cursor); err != nil {
			t.Fatal(err)
		}
		types = append(types, jobType)
		cursors = append(cursors, cursor)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(types) != "[parse_usage_chunk parse_usage_chunk rebuild_metrics_revision]" ||
		fmt.Sprint(cursors) != fmt.Sprintf("[0 %d -1]", len(firstChunk)) {
		t.Fatalf("jobs types=%v cursors=%v", types, cursors)
	}
	var targetRevisionID string
	if err := database.QueryRow(`
		SELECT id::text FROM session_metrics_revisions
		WHERE generation_id = $1 AND parser_version = $2 AND normalizer_version = $3`,
		fixture.generationID, ParserVersion, normalizerVersion).Scan(&targetRevisionID); err != nil {
		t.Fatal(err)
	}
	processor, _ := NewProcessor(database, fixture.store, "5m")
	firstJob := fixture.jobs[0]
	firstJob.TargetMetricsRevisionID = sql.NullString{String: targetRevisionID, Valid: true}
	if err := processor.Process(context.Background(), firstJob); err != nil {
		t.Fatal(err)
	}
	var visibleRevisionID, visibleStatus string
	if err := database.QueryRow(`
		SELECT active_revision_id::text, status
		FROM session_source_metrics_states WHERE source_id = $1`, fixture.sourceID).Scan(
		&visibleRevisionID, &visibleStatus); err != nil {
		t.Fatal(err)
	}
	if visibleRevisionID != oldRevisionID || visibleStatus != "ready" {
		t.Fatalf("partial rebuild exposed revision=%s status=%s want old=%s/ready",
			visibleRevisionID, visibleStatus, oldRevisionID)
	}
	secondJob := fixture.jobs[1]
	secondJob.TargetMetricsRevisionID = sql.NullString{String: targetRevisionID, Valid: true}
	if err := processor.Process(context.Background(), secondJob); err != nil {
		t.Fatal(err)
	}
	if got := activeMetricsRevision(t, database, fixture.sourceID); got != targetRevisionID {
		t.Fatalf("active revision=%s want=%s", got, targetRevisionID)
	}
	assertContributionLedger(t, database, fixture.sessionID, 2, 220, map[string]int64{
		contributionCheckpointDelta: 2,
	}, []int64{120, 100})
	assertActiveFamilyRollup(t, database, fixture.sessionID, 220, 220, 0, 2)
}

func TestContributionBackfillBuildsSourceWithoutLegacyMetricsStateIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990038, "usage-contribution-no-legacy-state", "claude-code")
	defer fixture.cleanup(t)
	content := readUsageFixture(t, "claude_monotonic.jsonl")
	fixture.appendChunk(t, content)

	normalizerVersion, _ := NormalizerRevision("5m")
	added, err := enqueueOneContributionBackfill(context.Background(), database, normalizerVersion, fixture.sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if !added {
		t.Fatal("source without a legacy metrics state was not enqueued")
	}
	var targetRevisionID string
	if err := database.QueryRow(`
		SELECT id::text FROM session_metrics_revisions
		WHERE generation_id = $1 AND parser_version = $2 AND normalizer_version = $3`,
		fixture.generationID, ParserVersion, normalizerVersion).Scan(&targetRevisionID); err != nil {
		t.Fatal(err)
	}
	job := fixture.jobs[0]
	job.TargetMetricsRevisionID = sql.NullString{String: targetRevisionID, Valid: true}
	processor, _ := NewProcessor(database, fixture.store, "5m")
	if err := processor.Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if got := activeMetricsRevision(t, database, fixture.sourceID); got != targetRevisionID {
		t.Fatalf("active revision=%s want=%s", got, targetRevisionID)
	}
}

func TestProcessorNewNormalizerQueuesPrefixRebuildIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990034, "usage-prefix-rebuild", "claude-code")
	defer fixture.cleanup(t)
	content := readUsageFixture(t, "claude_monotonic.jsonl")
	lines := completeLines(content)
	fixture.appendChunk(t, bytes.Join(lines[:3], nil))

	fiveMinuteProcessor, _ := NewProcessor(database, fixture.store, "5m")
	if err := fiveMinuteProcessor.Process(context.Background(), fixture.jobs[0]); err != nil {
		t.Fatal(err)
	}
	oldRevisionID := activeMetricsRevision(t, database, fixture.sourceID)

	fixture.appendChunk(t, bytes.Join(lines[3:], nil))
	oneHourProcessor, _ := NewProcessor(database, fixture.store, "1h")
	if err := oneHourProcessor.Process(context.Background(), fixture.jobs[1]); !errors.Is(err, ErrUsageOutOfOrder) {
		t.Fatalf("first append under new normalizer error=%v", err)
	}

	var newRevisionID, prefixChunkID string
	if err := database.QueryRow(`
		SELECT r.id, j.chunk_id
		FROM session_metrics_revisions r
		JOIN session_processing_jobs j ON j.target_metrics_revision_id = r.id
		WHERE r.generation_id = $1
			AND r.parser_version = $2
			AND r.normalizer_version = $3
			AND j.job_type = 'parse_usage_chunk'`,
		fixture.generationID, ParserVersion, oneHourProcessor.normalizerVersion,
	).Scan(&newRevisionID, &prefixChunkID); err != nil {
		t.Fatal(err)
	}
	if newRevisionID == oldRevisionID || prefixChunkID != fixture.jobs[0].ChunkID.String {
		t.Fatalf("old revision=%s new revision=%s prefix chunk=%s", oldRevisionID, newRevisionID, prefixChunkID)
	}

	prefixJob := fixture.jobs[0]
	prefixJob.TargetMetricsRevisionID = sql.NullString{String: newRevisionID, Valid: true}
	if err := oneHourProcessor.Process(context.Background(), prefixJob); err != nil {
		t.Fatalf("process prefix rebuild: %v", err)
	}
	if err := oneHourProcessor.Process(context.Background(), fixture.jobs[1]); err != nil {
		t.Fatalf("process appended chunk: %v", err)
	}
	if got := activeMetricsRevision(t, database, fixture.sourceID); got != newRevisionID {
		t.Fatalf("active revision=%s want=%s", got, newRevisionID)
	}
}

func TestProcessorCodexLongContextBillingBoundaryIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990033, "usage-codex-long-context", "codex")
	defer fixture.cleanup(t)
	content := []byte(
		`{"timestamp":"2026-07-10T00:00:00Z","type":"turn_context","payload":{"model":"gpt-5.6-sol"}}` + "\n" +
			`{"timestamp":"2026-07-10T00:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":272000,"cached_input_tokens":270000,"output_tokens":100,"total_tokens":272100},"last_token_usage":{"input_tokens":272000,"cached_input_tokens":270000,"output_tokens":100,"total_tokens":272100}}}}` + "\n" +
			`{"timestamp":"2026-07-10T00:02:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":544001,"cached_input_tokens":540000,"output_tokens":200,"total_tokens":544201},"last_token_usage":{"input_tokens":272001,"cached_input_tokens":270000,"output_tokens":100,"total_tokens":272101}}}}` + "\n",
	)
	fixture.appendChunk(t, content)
	processor, _ := NewProcessor(database, fixture.store, "")
	if err := processor.Process(context.Background(), fixture.jobs[0]); err != nil {
		t.Fatal(err)
	}
	rows, err := database.Query(`
		SELECT billing_variant, normalized_total_tokens
		FROM session_usage_components
		WHERE session_id = $1 AND valid_to IS NULL
		ORDER BY occurred_at`, fixture.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	wantVariants := []string{"unknown", "long_context"}
	wantTotals := []int64{272100, 272101}
	index := 0
	for rows.Next() {
		var variant string
		var total int64
		if err := rows.Scan(&variant, &total); err != nil {
			t.Fatal(err)
		}
		if index >= len(wantVariants) || variant != wantVariants[index] || total != wantTotals[index] {
			t.Fatalf("row[%d] variant=%s total=%d", index, variant, total)
		}
		index++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if index != len(wantVariants) {
		t.Fatalf("rows=%d want=%d", index, len(wantVariants))
	}
}

func TestProcessorDailyUsagePreservesOrganizationHistoryIntegration(t *testing.T) {
	database := openUsageIntegrationDatabase(t)
	fixture := newUsageFixture(t, database, 990031, "usage-codex-org-history", "codex")
	defer fixture.cleanup(t)

	teamBefore := "10000000-0000-4000-8000-000000000001"
	teamAfter := "10000000-0000-4000-8000-000000000002"
	departmentBefore := "20000000-0000-4000-8000-000000000001"
	departmentAfter := "20000000-0000-4000-8000-000000000002"
	if _, err := database.Exec(`DELETE FROM team_department_memberships WHERE team_id IN ($1, $2)`, teamBefore, teamAfter); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := database.Exec(`DELETE FROM team_department_memberships WHERE team_id IN ($1, $2)`, teamBefore, teamAfter); err != nil {
			t.Errorf("cleanup team department memberships: %v", err)
		}
	}()
	if _, err := database.Exec(`
		INSERT INTO user_team_memberships(user_id, team_id, effective_from, effective_to, source)
		VALUES ($1, $2, '2026-07-10T00:00:00Z', '2026-07-10T01:30:00Z', 'test'),
		       ($1, $3, '2026-07-10T01:30:00Z', NULL, 'test')`,
		fixture.userID, teamBefore, teamAfter); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO team_department_memberships(team_id, department_id, effective_from, source)
		VALUES ($1, $2, '2026-07-10T00:00:00Z', 'test'),
		       ($3, $4, '2026-07-10T00:00:00Z', 'test')`,
		teamBefore, departmentBefore, teamAfter, departmentAfter); err != nil {
		t.Fatal(err)
	}
	content := []byte(
		`{"timestamp":"2026-07-10T00:59:00Z","type":"turn_context","payload":{"model":"gpt-test"}}` + "\n" +
			`{"timestamp":"2026-07-10T01:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":120}}}}` + "\n" +
			`{"timestamp":"2026-07-10T02:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":180,"cached_input_tokens":50,"output_tokens":40,"reasoning_output_tokens":10,"total_tokens":220}}}}` + "\n",
	)
	fixture.appendChunk(t, content)
	processor, _ := NewProcessor(database, fixture.store, "")
	if err := processor.Process(context.Background(), fixture.jobs[0]); err != nil {
		t.Fatal(err)
	}
	var rows, total, teams, departments int64
	if err := database.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(total_tokens), 0),
			COUNT(DISTINCT team_id_snapshot), COUNT(DISTINCT department_id_snapshot)
		FROM session_daily_usage
		WHERE session_id = $1 AND valid_to IS NULL`, fixture.sessionID).Scan(
		&rows, &total, &teams, &departments,
	); err != nil {
		t.Fatal(err)
	}
	if rows != 2 || total != 220 || teams != 2 || departments != 2 {
		t.Fatalf("rows=%d total=%d teams=%d departments=%d", rows, total, teams, departments)
	}
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

func assertContributionLedger(
	t *testing.T,
	database *sql.DB,
	sessionID string,
	wantCount, wantTotal int64,
	wantKinds map[string]int64,
	wantChunkTotals []int64,
) {
	t.Helper()
	var count, total, linkedObservations int64
	if err := database.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(total_tokens), 0),
			(SELECT COUNT(*) FROM session_usage_observations observation
			 JOIN session_metrics_revisions revision ON revision.id = observation.revision_id
			 JOIN session_sources source ON source.id = revision.source_id
			 WHERE source.session_id = $1 AND observation.logical_usage_event_id IS NOT NULL)
		FROM session_usage_contributions
		WHERE member_session_id = $1`, sessionID).Scan(&count, &total, &linkedObservations); err != nil {
		t.Fatal(err)
	}
	if count != wantCount || total != wantTotal {
		t.Fatalf("contributions count=%d total=%d want count=%d total=%d", count, total, wantCount, wantTotal)
	}
	if linkedObservations < wantCount {
		t.Fatalf("linked observations=%d want at least %d", linkedObservations, wantCount)
	}
	rows, err := database.Query(`
		SELECT contribution_kind, COUNT(*)
		FROM session_usage_contributions
		WHERE member_session_id = $1
		GROUP BY contribution_kind`, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	gotKinds := map[string]int64{}
	for rows.Next() {
		var kind string
		var kindCount int64
		if err := rows.Scan(&kind, &kindCount); err != nil {
			t.Fatal(err)
		}
		gotKinds[kind] = kindCount
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(gotKinds) != len(wantKinds) {
		t.Fatalf("contribution kinds=%v want=%v", gotKinds, wantKinds)
	}
	for kind, want := range wantKinds {
		if gotKinds[kind] != want {
			t.Fatalf("contribution kind %s count=%d want=%d", kind, gotKinds[kind], want)
		}
	}
	chunkRows, err := database.Query(`
		SELECT COALESCE(SUM(contribution.total_tokens), 0)
		FROM session_usage_contributions contribution
		JOIN session_upload_chunks chunk ON chunk.id = contribution.chunk_id
		WHERE contribution.member_session_id = $1
		GROUP BY chunk.id, chunk.start_cursor
		ORDER BY chunk.start_cursor`, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer chunkRows.Close()
	index := 0
	for chunkRows.Next() {
		var got int64
		if err := chunkRows.Scan(&got); err != nil {
			t.Fatal(err)
		}
		if index >= len(wantChunkTotals) || got != wantChunkTotals[index] {
			t.Fatalf("chunk[%d] total=%d want=%v", index, got, wantChunkTotals)
		}
		index++
	}
	if err := chunkRows.Err(); err != nil {
		t.Fatal(err)
	}
	if index != len(wantChunkTotals) {
		t.Fatalf("chunk totals rows=%d want=%d", index, len(wantChunkTotals))
	}
}

func assertActiveFamilyRollup(
	t *testing.T,
	database *sql.DB,
	rootSessionID string,
	wantFamily, wantSelf, wantSubagent, wantDailyRows int64,
) {
	t.Helper()
	var rollupID, quality string
	if err := database.QueryRow(`
		SELECT id::text, quality_status
		FROM session_family_rollup_versions
		WHERE root_session_id = $1 AND status = 'active'`, rootSessionID).Scan(&rollupID, &quality); err != nil {
		t.Fatal(err)
	}
	if quality != "estimated" && quality != "exact" {
		t.Fatalf("active family rollup quality=%s", quality)
	}
	var family, self, subagent, daily, chunk, dailyRows int64
	var versionContributions, totalContributions, dailyContributions, chunkContributions int64
	var sourceCount, revisionRefCount, invalidActivityRanges int64
	if err := database.QueryRow(`
		SELECT
			COALESCE((SELECT SUM(total_tokens) FROM session_family_token_totals WHERE rollup_version_id = $1), 0),
			COALESCE((SELECT SUM(self_total_tokens) FROM session_family_token_totals WHERE rollup_version_id = $1), 0),
			COALESCE((SELECT SUM(subagent_total_tokens) FROM session_family_token_totals WHERE rollup_version_id = $1), 0),
			COALESCE((SELECT SUM(total_tokens) FROM session_family_daily_usage WHERE rollup_version_id = $1), 0),
			COALESCE((SELECT SUM(total_tokens) FROM session_chunk_usage WHERE rollup_version_id = $1), 0),
			(SELECT COUNT(DISTINCT activity_date) FROM session_family_daily_usage WHERE rollup_version_id = $1),
			(SELECT contribution_count FROM session_family_rollup_versions WHERE id = $1),
			COALESCE((SELECT SUM(contribution_count) FROM session_family_token_totals WHERE rollup_version_id = $1), 0),
			COALESCE((SELECT SUM(contribution_count) FROM session_family_daily_usage WHERE rollup_version_id = $1), 0),
			COALESCE((SELECT SUM(contribution_count) FROM session_chunk_usage WHERE rollup_version_id = $1), 0),
			(SELECT source_count FROM session_family_rollup_versions WHERE id = $1),
			(SELECT COUNT(*) FROM session_family_rollup_revision_refs WHERE rollup_version_id = $1),
			(SELECT COUNT(*) FROM session_family_daily_usage
			 WHERE rollup_version_id = $1 AND activity_end_at < activity_start_at)`,
		rollupID).Scan(&family, &self, &subagent, &daily, &chunk, &dailyRows,
		&versionContributions, &totalContributions, &dailyContributions, &chunkContributions,
		&sourceCount, &revisionRefCount, &invalidActivityRanges); err != nil {
		t.Fatal(err)
	}
	if family != wantFamily || self != wantSelf || subagent != wantSubagent ||
		daily != wantFamily || chunk != wantFamily || dailyRows != wantDailyRows ||
		versionContributions != totalContributions || versionContributions != dailyContributions ||
		versionContributions != chunkContributions || sourceCount != revisionRefCount || invalidActivityRanges != 0 {
		t.Fatalf("rollup family=%d self=%d subagent=%d daily=%d chunk=%d dailyRows=%d contributions=%d/%d/%d/%d sources=%d refs=%d invalidRanges=%d",
			family, self, subagent, daily, chunk, dailyRows, versionContributions,
			totalContributions, dailyContributions, chunkContributions, sourceCount,
			revisionRefCount, invalidActivityRanges)
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
