package reportsource

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/sessiondigest"
)

func TestDigestSelectionFreezeReadAndWriteGuardIntegration(t *testing.T) {
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
	userID := time.Now().UnixNano()%100000000 + 770000000
	userIDText := stringInt64(userID)
	if _, err := database.ExecContext(ctx, `INSERT INTO users (id, username) VALUES ($1, 'digest-report-source')`, userID); err != nil {
		t.Fatal(err)
	}
	defer database.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	fixture := insertReportSourceFixture(t, database, userID)

	config := DefaultConfig()
	config.SessionReadMode = ReadModeDigestV1
	config.DigestRolloutPct = 100
	service, err := NewServiceWithConfig(database, config)
	if err != nil {
		t.Fatal(err)
	}
	period := Period{Start: "2026-07-06", End: "2026-07-12"}
	selection, err := service.CreateExplicit(ctx, userIDText, "personal_weekly", period, []SourceInput{
		{SliceKey: fixture.sliceKeys[0]}, {SliceKey: fixture.sliceKeys[1]},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.CreateAttachedRun(ctx, userIDText, "personal_weekly", period,
		selection.ID, "report_agent_run", "agent-test", "", map[string]any{"report_type": "personal_weekly"}); !errors.Is(err, ErrDigestNotReady) {
		t.Fatalf("attach without ready digests must fail before run creation: %v", err)
	}
	var runCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_runs WHERE user_id = $1`, userID).Scan(&runCount); err != nil || runCount != 0 {
		t.Fatalf("digest-not-ready created an ai_run: count=%d err=%v", runCount, err)
	}

	for index, sliceID := range fixture.sliceKeys {
		digest := sessiondigest.EmptyDigest()
		digest.Goals = append(digest.Goals, "实现服务端 Digest")
		digest.Outcomes = append(digest.Outcomes, "完成来源切片 "+string(rune('A'+index)))
		digest.FilesChanged = append(digest.FilesChanged, "api/internal/reportsource/digest.go")
		encoded, _ := json.Marshal(digest)
		if _, err := database.ExecContext(ctx, `
			INSERT INTO session_slice_digest_revisions (
				session_content_slice_id, content_projection_revision_id, generation_id,
				content_epoch, digest_version, redaction_version, status, digest_json,
				source_event_count, included_event_count, omitted_event_count,
				source_bytes, digest_bytes, truncated, source_sha256, digest_sha256, ready_at
			)
			SELECT sl.id, $2, sl.generation_id, s.content_epoch,
				$3, $4, 'ready', $5::jsonb, $6, 1, $7, 1000000, $8, false, $9, $10, now()
			FROM session_content_slices sl JOIN sessions s ON s.id = sl.session_id
			WHERE sl.id = $1`, sliceID, fixture.revisionID, sessiondigest.Version,
			sessiondigest.RedactionVersion, string(encoded), 2-index, 1-index, len(encoded),
			hash64("digest-source-"+sliceID), sessiondigest.HashBytes(encoded)); err != nil {
			t.Fatal(err)
		}
	}

	runID, attached, err := service.CreateAttachedRun(ctx, userIDText, "personal_weekly", period,
		selection.ID, "report_agent_run", "agent-test", "", map[string]any{"report_type": "personal_weekly"})
	if err != nil {
		t.Fatal(err)
	}
	if runID == "" || attached.RequiredReadMode != ReadModeDigestV1 {
		t.Fatalf("unexpected attached digest selection: run=%s selection=%+v", runID, attached)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE report_source_selections SET required_read_mode = 'full' WHERE id = $1`, selection.ID); err == nil ||
		!strings.Contains(err.Error(), "attached report source digest payload is immutable") {
		t.Fatalf("attached digest payload was mutable: %v", err)
	}
	var requiredMode, completedMode string
	var completedAt any
	if err := database.QueryRowContext(ctx, `
		SELECT required_read_mode, COALESCE(read_completed_mode, ''), read_completed_at
		FROM report_source_selections WHERE id = $1`, selection.ID).Scan(
		&requiredMode, &completedMode, &completedAt); err != nil {
		t.Fatal(err)
	}
	if requiredMode != ReadModeDigestV1 || completedMode != "" || completedAt != nil {
		t.Fatalf("selection was marked read before MCP delivery: required=%s completed=%s at=%v", requiredMode, completedMode, completedAt)
	}
	assertDigestSelectionValidation(t, ctx, database, service, userIDText, selection.ID, runID, period, ErrSourceIncomplete)
	if _, err := service.ReadAttachedSelection(ctx, userIDText, selection.ID, runID,
		"personal_weekly", period, "legacy-page-cursor"); !errors.Is(err, ErrReadModeMismatch) {
		t.Fatalf("digest read accepted a page cursor: %v", err)
	}

	page, err := service.ReadAttachedSelection(ctx, userIDText, selection.ID, runID, "personal_weekly", period, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.FrozenPayload) == 0 || !json.Valid(page.FrozenPayload) {
		t.Fatalf("missing frozen digest payload: %q", page.FrozenPayload)
	}
	var payload digestContentPage
	if err := json.Unmarshal(page.FrozenPayload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ContentMode != ReadModeDigestV1 || !payload.Coverage.Complete || payload.HasMore ||
		payload.ReturnedCount != 2 || payload.Coverage.SourceItemCount != 2 ||
		payload.Coverage.RepresentedItemCount != 2 || payload.Budget.ActualBytes != len(page.FrozenPayload) {
		t.Fatalf("invalid digest payload contract: %+v bytes=%d", payload, len(page.FrozenPayload))
	}
	if payload.Timezone != biztime.Zone || !strings.Contains(payload.ContentSnapshot, "+08:00") {
		t.Fatalf("digest payload is missing the Shanghai time contract: %+v", payload)
	}
	for _, item := range payload.Items {
		if !strings.Contains(item.ActivityStart, "+08:00") || !strings.Contains(item.ActivityEnd, "+08:00") ||
			item.StartDate == "" || item.EndDate == "" {
			t.Fatalf("digest item is missing local time fields: %+v", item)
		}
	}
	visibleJSON := string(page.FrozenPayload)
	for _, forbidden := range []string{`"events"`, `"payload"`, "reasoning", "token_usage"} {
		if containsString(visibleJSON, forbidden) {
			t.Fatalf("forbidden full-source field %q leaked: %s", forbidden, visibleJSON)
		}
	}
	second, err := service.ReadAttachedSelection(ctx, userIDText, selection.ID, runID, "personal_weekly", period, "")
	if err != nil || string(second.FrozenPayload) != string(page.FrozenPayload) {
		t.Fatalf("repeated digest read changed frozen bytes: err=%v", err)
	}
	assertDigestSelectionValidation(t, ctx, database, service, userIDText, selection.ID, runID, period, nil)
}

func assertDigestSelectionValidation(
	t *testing.T,
	ctx context.Context,
	database interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	},
	service *Service,
	userID, selectionID, runID string,
	period Period,
	want error,
) {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	err = service.ValidateAttachedSelectionTx(ctx, tx, userID, selectionID, runID, "personal_weekly", period)
	if !errors.Is(err, want) {
		t.Fatalf("validation error=%v want=%v", err, want)
	}
}

func stringInt64(value int64) string { return strconv.FormatInt(value, 10) }

func containsString(value, fragment string) bool { return strings.Contains(value, fragment) }
