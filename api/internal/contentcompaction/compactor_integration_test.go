package contentcompaction

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/sessionsync"
)

type compactionFixture struct {
	userID       int64
	sessionID    string
	revisionID   string
	chunkID      string
	nextCursor   int64
	sourceEvents []string
}

func TestCompactorBoundedCopyMirrorCutoverRollbackAndFinalizeIntegration(t *testing.T) {
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
	fixture := createCompactionFixture(t, database)
	defer database.Exec(`DELETE FROM users WHERE id = $1`, fixture.userID)
	compactor, err := New(database)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := compactor.Run(ctx, Options{Action: ActionPlan})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Phase != "initialized" || plan.Applied || plan.RowCountsExact ||
		!plan.SourceHasPayload || plan.TargetHasPayload {
		t.Fatalf("initial plan=%+v", plan)
	}
	var initialPhase string
	var initialCopiedRows int64
	if err := database.QueryRow(`
		SELECT phase, copied_rows
		FROM session_content_events_compaction_state WHERE id = 1`).Scan(
		&initialPhase, &initialCopiedRows,
	); err != nil {
		t.Fatal(err)
	}
	if initialPhase != "initialized" || initialCopiedRows != 0 {
		t.Fatalf("plan changed state: phase=%s copied=%d", initialPhase, initialCopiedRows)
	}

	copyReport, err := compactor.Run(ctx, Options{
		Action: ActionCopy, Apply: true, BatchSize: 2, MaxBatches: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if copyReport.ProcessedRows != 2 || copyReport.ProcessedBatches != 1 ||
		copyReport.TargetRows != 2 || copyReport.RowCountsExact || copyReport.Complete {
		t.Fatalf("first copy=%+v", copyReport)
	}
	// Writes can continue while the bounded copy is between invocations. The later
	// mirror + reconcile phase must repair both sides regardless of UUID ordering.
	duringCopyInsert := fixture.insertSourceEvent(t, database, "during-copy-insert")
	var duringCopyDelete string
	if err := database.QueryRow(`
		SELECT id::text FROM session_content_events_compact ORDER BY id LIMIT 1`).Scan(
		&duringCopyDelete,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM session_content_events WHERE id = $1`, duringCopyDelete); err != nil {
		t.Fatal(err)
	}
	for attempts := 0; !copyReport.Complete && attempts < 10; attempts++ {
		copyReport, err = compactor.Run(ctx, Options{
			Action: ActionCopy, Apply: true, BatchSize: 2, MaxBatches: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !copyReport.Complete || copyReport.Phase != "copied" {
		t.Fatalf("completed copy=%+v", copyReport)
	}
	idempotentCopy, err := compactor.Run(ctx, Options{
		Action: ActionCopy, Apply: true, BatchSize: 2, MaxBatches: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if idempotentCopy.ProcessedRows != 0 || !idempotentCopy.Complete || idempotentCopy.Phase != "copied" {
		t.Fatalf("idempotent completed copy=%+v", idempotentCopy)
	}

	// Create one pre-mirror missing row and one target-only extra row. Reconcile must repair both.
	preMirrorMissing := fixture.insertSourceEvent(t, database, "pre-mirror-missing")
	targetExtra := fixture.insertLightEvent(t, database, ShadowTable, "target-extra")

	mirrorReport, err := compactor.Run(ctx, Options{Action: ActionMirror, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if mirrorReport.Phase != "mirroring" || !mirrorReport.Complete {
		t.Fatalf("mirror=%+v", mirrorReport)
	}
	mirroredInsert := fixture.insertSourceEvent(t, database, "mirrored-insert")
	var deletedID string
	if err := database.QueryRow(`
		SELECT id::text FROM session_content_events WHERE id <> $1 ORDER BY id LIMIT 1`,
		duringCopyInsert).Scan(&deletedID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM session_content_events WHERE id = $1`, deletedID); err != nil {
		t.Fatal(err)
	}
	assertEventPresence(t, database, ShadowTable, mirroredInsert, true)
	assertEventPresence(t, database, ShadowTable, deletedID, false)

	reconcileReport := Report{}
	for attempts := 0; attempts < 20; attempts++ {
		reconcileReport, err = compactor.Run(ctx, Options{
			Action: ActionReconcile, Apply: true, BatchSize: 2, MaxBatches: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if reconcileReport.Complete {
			break
		}
	}
	if !reconcileReport.Complete || reconcileReport.Phase != "reconciled" {
		t.Fatalf("reconcile=%+v", reconcileReport)
	}
	assertEventPresence(t, database, ShadowTable, preMirrorMissing, true)
	assertEventPresence(t, database, ShadowTable, targetExtra, false)
	assertEventPresence(t, database, ShadowTable, duringCopyInsert, true)
	assertEventPresence(t, database, ShadowTable, duringCopyDelete, false)
	verify, err := compactor.Run(ctx, Options{Action: ActionVerify})
	if err != nil {
		t.Fatal(err)
	}
	if !verify.Complete || verify.MissingRows != 0 || verify.ExtraRows != 0 || verify.MismatchedRows != 0 {
		t.Fatalf("verify before cutover=%+v", verify)
	}
	if !verify.RowCountsExact {
		t.Fatalf("verify row counts were not marked exact: %+v", verify)
	}

	if _, err := database.Exec(`
		UPDATE session_content_events_compact SET summary = summary || '-mismatch'
		WHERE id = $1`, preMirrorMissing); err != nil {
		t.Fatal(err)
	}
	mismatch, err := compactor.Run(ctx, Options{Action: ActionVerify})
	if err != nil {
		t.Fatal(err)
	}
	if mismatch.Complete || mismatch.MismatchedRows != 1 {
		t.Fatalf("mismatch was not detected: %+v", mismatch)
	}
	if _, err := compactor.Run(ctx, Options{
		Action: ActionCutover, Apply: true, ExpectedSourceRows: mismatch.SourceRows,
	}); err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("mismatched cutover error=%v", err)
	}
	if _, err := database.Exec(`
		UPDATE session_content_events_compact target SET summary = source.summary
		FROM session_content_events source
		WHERE target.id = source.id AND target.id = $1`, preMirrorMissing); err != nil {
		t.Fatal(err)
	}
	verify, err = compactor.Run(ctx, Options{Action: ActionVerify})
	if err != nil || !verify.Complete {
		t.Fatalf("verify after mismatch repair=%+v error=%v", verify, err)
	}

	if _, err := compactor.Run(ctx, Options{
		Action: ActionCutover, Apply: true, ExpectedSourceRows: verify.SourceRows + 1,
	}); err == nil {
		t.Fatal("cutover accepted an incorrect expected row count")
	}
	lockTx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockTx.ExecContext(ctx, `
		LOCK TABLE session_content_events_compact IN ACCESS EXCLUSIVE MODE`); err != nil {
		lockTx.Rollback()
		t.Fatal(err)
	}
	_, lockErr := compactor.Run(ctx, Options{
		Action: ActionCutover, Apply: true, ExpectedSourceRows: verify.SourceRows,
		LockTimeout: 20 * time.Millisecond,
	})
	if lockErr == nil || !strings.Contains(lockErr.Error(), "acquire cutover lock") {
		lockTx.Rollback()
		t.Fatalf("cutover lock error=%v", lockErr)
	}
	if err := lockTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var phaseAfterLockFailure string
	if err := database.QueryRow(`
		SELECT phase FROM session_content_events_compaction_state WHERE id = 1`).Scan(
		&phaseAfterLockFailure,
	); err != nil {
		t.Fatal(err)
	}
	if phaseAfterLockFailure != "reconciled" {
		t.Fatalf("phase after lock failure=%s", phaseAfterLockFailure)
	}
	var archiveAfterLockFailure bool
	if err := database.QueryRow(`
		SELECT to_regclass('public.session_content_events_payload_archive') IS NOT NULL`).Scan(
		&archiveAfterLockFailure,
	); err != nil {
		t.Fatal(err)
	}
	if archiveAfterLockFailure {
		t.Fatal("archive exists after failed cutover lock")
	}
	cutover, err := compactor.Run(ctx, Options{
		Action: ActionCutover, Apply: true, ExpectedSourceRows: verify.SourceRows,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cutover.Phase != "swapped" || cutover.TargetHasPayload || !cutover.SourceHasPayload {
		t.Fatalf("cutover=%+v", cutover)
	}

	// Simulate a query that resolved the old table before the rename. Its archive write must mirror.
	archiveInsert := fixture.insertSourceEventInto(t, database, ArchiveTable, "queued-archive-insert", true)
	assertEventPresence(t, database, SourceTable, archiveInsert, true)
	if _, err := database.Exec(`DELETE FROM session_content_events_payload_archive WHERE id = $1`, mirroredInsert); err != nil {
		t.Fatal(err)
	}
	assertEventPresence(t, database, SourceTable, mirroredInsert, false)
	postCutover, err := compactor.Run(ctx, Options{Action: ActionVerify})
	if err != nil {
		t.Fatal(err)
	}
	if !postCutover.Complete || postCutover.MissingRows != 0 || postCutover.MismatchedRows != 0 {
		t.Fatalf("verify after archive writes=%+v", postCutover)
	}

	if _, err := compactor.Run(ctx, Options{
		Action: ActionRollback, Apply: true, ExpectedSourceRows: postCutover.TargetRows + 1,
	}); err == nil {
		t.Fatal("rollback accepted an incorrect expected row count")
	}
	rollback, err := compactor.Run(ctx, Options{
		Action: ActionRollback, Apply: true, ExpectedSourceRows: postCutover.TargetRows,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rollback.Phase != "rolled_back" || !rollback.SourceHasPayload || rollback.TargetHasPayload {
		t.Fatalf("rollback=%+v", rollback)
	}
	queuedCompactInsert := fixture.insertLightEvent(t, database, ShadowTable, "queued-compact-insert")
	assertEventPresence(t, database, SourceTable, queuedCompactInsert, true)
	assertPayloadNull(t, database, SourceTable, queuedCompactInsert)

	if _, err := compactor.Run(ctx, Options{Action: ActionMirror, Apply: true}); err != nil {
		t.Fatal(err)
	}
	reconcileReport = Report{}
	for attempts := 0; attempts < 20; attempts++ {
		reconcileReport, err = compactor.Run(ctx, Options{
			Action: ActionReconcile, Apply: true, BatchSize: 3, MaxBatches: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if reconcileReport.Complete {
			break
		}
	}
	if !reconcileReport.Complete {
		t.Fatalf("second reconcile=%+v", reconcileReport)
	}
	secondVerify, err := compactor.Run(ctx, Options{Action: ActionVerify})
	if err != nil {
		t.Fatal(err)
	}
	secondCutover, err := compactor.Run(ctx, Options{
		Action: ActionCutover, Apply: true, ExpectedSourceRows: secondVerify.SourceRows,
	})
	if err != nil {
		t.Fatal(err)
	}
	if secondCutover.Phase != "swapped" {
		t.Fatalf("second cutover=%+v", secondCutover)
	}
	if _, err := compactor.Run(ctx, Options{
		Action: ActionFinalize, Apply: true, ConfirmDrop: "yes",
	}); err == nil {
		t.Fatal("finalize accepted an incorrect archive confirmation")
	}
	finalized, err := compactor.Run(ctx, Options{
		Action: ActionFinalize, Apply: true, ConfirmDrop: ArchiveTable,
	})
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Phase != "finalized" || finalized.SourceHasPayload || !finalized.Complete {
		t.Fatalf("finalized=%+v", finalized)
	}
	var archiveExists bool
	if err := database.QueryRow(`SELECT to_regclass('public.' || $1) IS NOT NULL`, ArchiveTable).Scan(&archiveExists); err != nil {
		t.Fatal(err)
	}
	if archiveExists {
		t.Fatal("archive table still exists after finalize")
	}
}

func createCompactionFixture(t *testing.T, database *sql.DB) *compactionFixture {
	t.Helper()
	fixture := &compactionFixture{userID: time.Now().UnixNano()%100000000 + 870000000}
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, $2)`, fixture.userID, fmt.Sprintf("content-compaction-%d", fixture.userID)); err != nil {
		t.Fatal(err)
	}
	var sourceID, generationID string
	if err := database.QueryRow(`
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at)
		VALUES ($1, $2, 'codex', now()) RETURNING id`, fmt.Sprintf("compaction-%d", fixture.userID), fixture.userID).Scan(&fixture.sessionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_sources (session_id, source_role, source_key)
		VALUES ($1, 'main', $2) RETURNING id`, fixture.sessionID, fmt.Sprintf("codex:compaction-%d:main", fixture.userID)).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_source_generations (source_id, status)
		VALUES ($1, 'active') RETURNING id`, sourceID).Scan(&generationID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_upload_chunks (
			generation_id, start_cursor, end_cursor, start_line, end_line,
			content_sha256, content_epoch, raw_object_key
		) VALUES ($1, 0, 100000, 1, 10000, $2, 0, $3) RETURNING id`,
		generationID, strings.Repeat("a", 64), fmt.Sprintf("compaction/%d", fixture.userID)).Scan(&fixture.chunkID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_content_projection_revisions (
			generation_id, content_parser_version, status, content_indexed_cursor,
			source_high_water_cursor, event_count, activated_at
		) VALUES ($1, $2, 'active', 100000, 100000, 10000, now()) RETURNING id`,
		generationID, sessionsync.ContentParserVersion).Scan(&fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 7; index++ {
		fixture.sourceEvents = append(fixture.sourceEvents,
			fixture.insertSourceEvent(t, database, fmt.Sprintf("seed-%d", index)))
	}
	return fixture
}

func (fixture *compactionFixture) insertSourceEvent(t *testing.T, database *sql.DB, label string) string {
	t.Helper()
	return fixture.insertSourceEventInto(t, database, SourceTable, label, true)
}

func (fixture *compactionFixture) insertSourceEventInto(
	t *testing.T,
	database *sql.DB,
	table, label string,
	withPayload bool,
) string {
	t.Helper()
	identifier, err := checkedTable(table)
	if err != nil {
		t.Fatal(err)
	}
	start := fixture.nextCursor
	fixture.nextCursor += 10
	hash := sessionsync.HashBytes([]byte(label))
	var id string
	if withPayload {
		err = database.QueryRow(`
			INSERT INTO `+identifier+` (
				content_projection_revision_id, chunk_id, source_start_cursor,
				source_end_cursor, occurred_at, event_type, summary, excerpt,
				content_payload, content_sha256
			) VALUES ($1, $2, $3, $4, now(), 'event_msg.user_message', $5, $5,
				jsonb_build_object('payload', jsonb_build_object('message', $5::text)), $6)
			RETURNING id`, fixture.revisionID, fixture.chunkID, start, start+10, label, hash).Scan(&id)
	} else {
		err = database.QueryRow(`
			INSERT INTO `+identifier+` (
				id, content_projection_revision_id, chunk_id, source_start_cursor,
				source_end_cursor, occurred_at, event_type, summary, excerpt,
				content_sha256
			) VALUES (gen_random_uuid(), $1, $2, $3, $4, now(),
				'event_msg.user_message', $5, $5, $6)
			RETURNING id`, fixture.revisionID, fixture.chunkID, start, start+10, label, hash).Scan(&id)
	}
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (fixture *compactionFixture) insertLightEvent(t *testing.T, database *sql.DB, table, label string) string {
	return fixture.insertSourceEventInto(t, database, table, label, false)
}

func assertEventPresence(t *testing.T, database *sql.DB, table, id string, want bool) {
	t.Helper()
	identifier, err := checkedTable(table)
	if err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := database.QueryRow(`SELECT EXISTS (SELECT 1 FROM `+identifier+` WHERE id = $1)`, id).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != want {
		t.Fatalf("table=%s event=%s exists=%v want=%v", table, id, exists, want)
	}
}

func assertPayloadNull(t *testing.T, database *sql.DB, table, id string) {
	t.Helper()
	identifier, err := checkedTable(table)
	if err != nil {
		t.Fatal(err)
	}
	var isNull bool
	if err := database.QueryRow(`SELECT content_payload IS NULL FROM `+identifier+` WHERE id = $1`, id).Scan(&isNull); err != nil {
		t.Fatal(err)
	}
	if !isNull {
		t.Fatalf("table=%s event=%s payload was retained", table, id)
	}
}
