package reportsource

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
)

func TestReportSourceSelectionLifecycleIntegration(t *testing.T) {
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
	userID := int64(990040)
	otherUserID := int64(990041)
	_, _ = database.Exec(`DELETE FROM sessions WHERE user_id IN ($1, $2)`, userID, otherUserID)
	_, _ = database.Exec(`DELETE FROM users WHERE id IN ($1, $2)`, userID, otherUserID)
	if _, err := database.Exec(`
		INSERT INTO users (id, username) VALUES ($1, 'v2-report-source'), ($2, 'v2-report-source-other')`,
		userID, otherUserID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = database.Exec(`DELETE FROM sessions WHERE user_id IN ($1, $2)`, userID, otherUserID)
		_, _ = database.Exec(`DELETE FROM users WHERE id IN ($1, $2)`, userID, otherUserID)
	}()

	fixture := insertReportSourceFixture(t, database, userID)
	service, err := NewService(database)
	if err != nil {
		t.Fatal(err)
	}

	page, err := service.ListCandidates(ctx, "990040", CandidateQuery{Query: "source-e2e", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].SessionRef != "report-source-e2e" ||
		page.Items[0].ContentIndexStatus != "ready" {
		t.Fatalf("candidate page=%+v", page)
	}

	selection, err := service.CreateExplicit(ctx, "990040", "personal_weekly", Period{
		Start: "2026-07-06", End: "2026-07-12",
	}, []SourceInput{
		{SessionRef: "report-source-e2e", AgentType: "codex", ActivityStart: fixture.times[0], ActivityEnd: fixture.times[1]},
		{SessionRef: "report-source-e2e", AgentType: "codex", ActivityStart: fixture.times[1], ActivityEnd: fixture.times[2]},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Mode != "explicit" || selection.Status != "prepared" || len(selection.Items) != 1 ||
		selection.Items[0].StartCursor != 0 || selection.Items[0].EndCursor != 300 ||
		selection.Items[0].ContentEventCount != 3 {
		t.Fatalf("selection=%+v", selection)
	}

	if _, err := database.Exec(`UPDATE sessions SET content_status = 'clearing' WHERE id = $1`, fixture.sessionID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.CreateAttachedRun(ctx, "990040", "personal_weekly", selection.Period,
		selection.ID, "report_agent_run", "agent-test", "MiniMax-M2.5", map[string]any{}); !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("attach clearing source err=%v", err)
	}
	if _, err := database.Exec(`UPDATE sessions SET content_status = 'available' WHERE id = $1`, fixture.sessionID); err != nil {
		t.Fatal(err)
	}
	runID, attached, err := service.CreateAttachedRun(ctx, "990040", "personal_weekly", selection.Period,
		selection.ID, "report_agent_run", "agent-test", "MiniMax-M2.5", map[string]any{"report_type": "personal_weekly"})
	if err != nil {
		t.Fatal(err)
	}
	if runID == "" || attached.Status != "attached" || attached.ID != selection.ID {
		t.Fatalf("run=%s attached=%+v", runID, attached)
	}
	var inputRaw []byte
	if err := database.QueryRow(`SELECT input_ref_json FROM ai_runs WHERE id = $1`, runID).Scan(&inputRaw); err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if err := json.Unmarshal(inputRaw, &input); err != nil || input["report_source_selection_id"] != selection.ID {
		t.Fatalf("input=%s err=%v", inputRaw, err)
	}
	pageOne, err := service.ReadAttachedSelection(ctx, "990040", selection.ID, runID, "personal_weekly", selection.Period, "")
	if err != nil {
		t.Fatal(err)
	}
	if pageOne.SourceMode != "explicit" || pageOne.HasMore || pageOne.ReturnedEvents != 3 || len(pageOne.Items) != 1 {
		t.Fatalf("selection content page=%+v", pageOne)
	}
	if _, err := json.Marshal(pageOne); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadAttachedSelection(ctx, "990040", selection.ID, defaultUUID(), "personal_weekly", selection.Period, ""); !errors.Is(err, ErrSelectionMismatch) {
		t.Fatalf("wrong run selection read err=%v", err)
	}

	if _, err := database.Exec(`
		INSERT INTO session_content_events (
			content_projection_revision_id, chunk_id, source_start_cursor, source_end_cursor,
			occurred_at, event_type, summary, excerpt, content_payload, content_sha256
		) VALUES ($1, $2, 300, 350, $3, 'message', 'later', 'later', '{}'::jsonb, $4)`,
		fixture.revisionID, fixture.chunkID, fixture.times[2].Add(time.Hour), hash64("d")); err != nil {
		t.Fatal(err)
	}
	var snapEnd, snapCount int64
	if err := database.QueryRow(`
		SELECT end_cursor, content_event_count FROM report_source_selection_items WHERE selection_id = $1`,
		selection.ID).Scan(&snapEnd, &snapCount); err != nil {
		t.Fatal(err)
	}
	if snapEnd != 300 || snapCount != 3 {
		t.Fatalf("snapshot drifted end=%d count=%d", snapEnd, snapCount)
	}
	pageAfterAppend, err := service.ReadAttachedSelection(ctx, "990040", selection.ID, runID, "personal_weekly", selection.Period, "")
	if err != nil || pageAfterAppend.ReturnedEvents != 3 {
		t.Fatalf("snapshot read drifted page=%+v err=%v", pageAfterAppend, err)
	}

	defaultRunID, defaultSelection, err := service.CreateAttachedRun(ctx, "990040", "personal_daily",
		Period{Start: "2026-07-10", End: "2026-07-10"}, "", "report_agent_run", "agent-test", "", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if defaultRunID == "" || defaultSelection.Mode != "default" || defaultSelection.Status != "attached" ||
		len(defaultSelection.Items) != 1 || defaultSelection.Items[0].ContentEventCount != 3 {
		t.Fatalf("default selection=%+v", defaultSelection)
	}

	lastPagedTime := fixture.times[2].Add(102 * time.Minute)
	if _, err := database.Exec(`
		UPDATE session_upload_chunks SET end_cursor = 1400, event_end_at = $2 WHERE id = $1`, fixture.chunkID, lastPagedTime); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE session_source_generations SET expected_cursor = 1400 WHERE id = (
			SELECT generation_id FROM session_upload_chunks WHERE id = $1
		)`, fixture.chunkID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE session_content_projection_revisions
		SET content_indexed_cursor = 1400, source_high_water_cursor = 1400 WHERE id = $1`, fixture.revisionID); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 101; index++ {
		start := int64(350 + index*10)
		if _, err := database.Exec(`
			INSERT INTO session_content_events (
				content_projection_revision_id, chunk_id, source_start_cursor, source_end_cursor,
				occurred_at, event_type, summary, excerpt, content_payload, content_sha256
			) VALUES ($1, $2, $3, $4, $5, 'message', 'paged', 'paged', '{}'::jsonb, $6)`,
			fixture.revisionID, fixture.chunkID, start, start+10, fixture.times[2].Add(time.Duration(index+2)*time.Minute), hash64("paged-"+strconv.Itoa(index))); err != nil {
			t.Fatal(err)
		}
	}
	pagedSelection, err := service.CreateExplicit(ctx, "990040", "personal_weekly", selection.Period,
		[]SourceInput{{SessionRef: "report-source-e2e", AgentType: "codex", ActivityStart: fixture.times[0], ActivityEnd: lastPagedTime}})
	if err != nil {
		t.Fatal(err)
	}
	pagedRunID, pagedAttached, err := service.CreateAttachedRun(ctx, "990040", "personal_weekly", selection.Period,
		pagedSelection.ID, "report_agent_run", "agent-test", "", map[string]any{"report_type": "personal_weekly"})
	if err != nil {
		t.Fatal(err)
	}
	firstPage, err := service.ReadAttachedSelection(ctx, "990040", pagedAttached.ID, pagedRunID, "personal_weekly", selection.Period, "")
	if err != nil {
		t.Fatal(err)
	}
	if !firstPage.HasMore || firstPage.NextCursor == nil || firstPage.ReturnedEvents != 100 || firstPage.Completeness != "partial" {
		t.Fatalf("first paged read=%+v", firstPage)
	}
	retriedFirstPage, err := service.ReadAttachedSelection(ctx, "990040", pagedAttached.ID, pagedRunID, "personal_weekly", selection.Period, "")
	if err != nil {
		t.Fatal(err)
	}
	if retriedFirstPage.NextCursor == nil || *retriedFirstPage.NextCursor != *firstPage.NextCursor ||
		retriedFirstPage.ReturnedEvents != firstPage.ReturnedEvents {
		t.Fatalf("first page retry changed cursor or contents first=%+v retry=%+v", firstPage, retriedFirstPage)
	}
	secondPage, err := service.ReadAttachedSelection(ctx, "990040", pagedAttached.ID, pagedRunID, "personal_weekly", selection.Period, *firstPage.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if secondPage.HasMore || secondPage.ReturnedEvents != 5 || secondPage.Completeness != "complete" {
		t.Fatalf("second paged read=%+v", secondPage)
	}
	retriedSecondPage, err := service.ReadAttachedSelection(ctx, "990040", pagedAttached.ID, pagedRunID, "personal_weekly", selection.Period, *firstPage.NextCursor)
	if err != nil || retriedSecondPage.HasMore || retriedSecondPage.ReturnedEvents != secondPage.ReturnedEvents {
		t.Fatalf("second page retry changed contents page=%+v err=%v", retriedSecondPage, err)
	}
	validationTx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ValidateAttachedSelectionTx(
		ctx, validationTx, "990040", selection.ID, runID, "personal_weekly", selection.Period,
	); err != nil {
		validationTx.Rollback()
		t.Fatal(err)
	}
	clearStarted := make(chan struct{})
	clearFinished := make(chan error, 1)
	go func() {
		close(clearStarted)
		_, updateErr := database.Exec(`UPDATE sessions SET content_status = 'clearing' WHERE id = $1`, fixture.sessionID)
		clearFinished <- updateErr
	}()
	<-clearStarted
	select {
	case updateErr := <-clearFinished:
		validationTx.Rollback()
		t.Fatalf("content clear bypassed report source share lock: %v", updateErr)
	case <-time.After(100 * time.Millisecond):
	}
	if err := validationTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := <-clearFinished; err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadAttachedSelection(ctx, "990040", selection.ID, runID, "personal_weekly", selection.Period, ""); !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("cleared snapshot read err=%v", err)
	}
	if _, err := database.Exec(`UPDATE sessions SET content_status = 'available' WHERE id = $1`, fixture.sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE sessions SET content_epoch = content_epoch + 1 WHERE id = $1`, fixture.sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadAttachedSelection(ctx, "990040", selection.ID, runID, "personal_weekly", selection.Period, ""); !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("old content epoch selection was readable err=%v", err)
	}

	if _, err := service.CreateExplicit(ctx, "990041", "personal_daily", Period{Start: "2026-07-10", End: "2026-07-10"},
		[]SourceInput{{SessionRef: "report-source-e2e", AgentType: "codex", ActivityStart: fixture.times[0], ActivityEnd: fixture.times[2]}}); !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("cross-user selection err=%v", err)
	}
}

type reportSourceFixture struct {
	sessionID  string
	revisionID string
	chunkID    string
	times      []time.Time
}

func insertReportSourceFixture(t *testing.T, database *sql.DB, userID int64) reportSourceFixture {
	t.Helper()
	times := []time.Time{
		time.Date(2026, 7, 9, 23, 30, 0, 0, time.FixedZone("CST", 8*60*60)).UTC(),
		time.Date(2026, 7, 10, 9, 0, 0, 0, time.FixedZone("CST", 8*60*60)).UTC(),
		time.Date(2026, 7, 10, 18, 0, 0, 0, time.FixedZone("CST", 8*60*60)).UTC(),
	}
	var sessionID, sourceID, generationID, revisionID, chunkID string
	if err := database.QueryRow(`
		INSERT INTO sessions (
			session_ref, user_id, agent_type, started_at, last_activity_at, summary, cwd, models
		) VALUES ('report-source-e2e', $1, 'codex', $2, $3, 'Report Source E2E', '/tmp/project', ARRAY['gpt-test'])
		RETURNING id::text`, userID, times[0], times[2]).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_sources (session_id, source_role, source_key)
		VALUES ($1, 'main', 'codex:report-source-e2e:main') RETURNING id::text`, sessionID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_source_generations (
			source_id, status, expected_cursor, prefix_checkpoint_hash,
			prefix_checkpoint_state, prefix_checkpoint_state_format
		) VALUES ($1, 'active', 350, $2, '\x01'::bytea, 'sha256-state-v1') RETURNING id::text`,
		sourceID, hash64("a")).Scan(&generationID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_upload_chunks (
			generation_id, start_cursor, end_cursor, start_line, end_line, content_sha256,
			content_epoch, event_start_at, event_end_at, raw_object_key, object_status,
			content_index_status
		) VALUES ($1, 0, 350, 1, 4, $2, 0, $3, $4, 'report-source/test', 'available', 'indexed')
		RETURNING id::text`, generationID, hash64("b"), times[0], times[2].Add(time.Hour)).Scan(&chunkID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_content_projection_revisions (
			generation_id, content_parser_version, status, build_start_cursor,
			content_indexed_cursor, source_high_water_cursor, event_count, validated_at, activated_at
		) VALUES ($1, 'content-v1', 'active', 350, 350, 350, 4, now(), now())
		RETURNING id::text`, generationID).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE session_sources SET active_generation_id = $1, active_content_projection_revision_id = $2
		WHERE id = $3`, generationID, revisionID, sourceID); err != nil {
		t.Fatal(err)
	}
	for index, occurredAt := range times {
		start := int64(index * 100)
		end := start + 100
		if _, err := database.Exec(`
			INSERT INTO session_content_events (
				content_projection_revision_id, chunk_id, source_start_cursor, source_end_cursor,
				occurred_at, event_type, summary, excerpt, content_payload, content_sha256
			) VALUES ($1, $2, $3, $4, $5, 'message', $6, $6, '{}'::jsonb, $7)`,
			revisionID, chunkID, start, end, occurredAt, "event", hash64(string(rune('e'+index)))); err != nil {
			t.Fatal(err)
		}
	}
	return reportSourceFixture{sessionID: sessionID, revisionID: revisionID, chunkID: chunkID, times: times}
}

func hash64(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func defaultUUID() string {
	return "00000000-0000-0000-0000-000000000001"
}
