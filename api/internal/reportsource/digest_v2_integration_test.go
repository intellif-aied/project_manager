package reportsource

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/sessiondigestv2"
)

func TestDigestV2SelectionFreezeReadAndWriteGuardIntegration(t *testing.T) {
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
	userID := time.Now().UnixNano()%100000000 + 780000000
	userIDText := stringInt64(userID)
	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO users (id, username) VALUES ($1, 'digest-v2-report-source')`,
		userID,
	); err != nil {
		t.Fatal(err)
	}
	defer database.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	fixture := insertReportSourceFixture(t, database, userID)

	config := DefaultConfig()
	config.SessionReadMode = ReadModeDigestV2
	config.DigestVersion = sessiondigestv2.Version
	service, err := NewServiceWithConfig(database, config)
	if err != nil {
		t.Fatal(err)
	}
	period := Period{Start: "2026-07-06", End: "2026-07-12"}
	selection, err := service.CreateExplicit(
		ctx, userIDText, "personal_weekly", period,
		[]SourceInput{{SliceKey: fixture.sliceKeys[0]}, {SliceKey: fixture.sliceKeys[1]}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.CreateAttachedRun(
		ctx, userIDText, "personal_weekly", period, selection.ID,
		"report_agent_run", "agent-v2-test", "",
		false,
		map[string]any{"report_type": "personal_weekly"},
	); !errors.Is(err, ErrDigestNotReady) {
		t.Fatalf("attach without ready v2 digests must fail: %v", err)
	}

	for index, sliceID := range fixture.sliceKeys {
		digest := sessiondigestv2.EmptyDigest()
		digest.WorkUnits = append(digest.WorkUnits, sessiondigestv2.WorkUnit{
			WorkUnitRef:     "wu-v2-" + string(rune('a'+index)),
			Sequence:        1,
			ActivityStartAt: "2026-07-07T01:00:00Z",
			ActivityEndAt:   "2026-07-07T02:00:00Z",
			PeriodRelation:  "unknown",
			Goal: sessiondigestv2.Goal{
				Text: "实现结果优先的 Digest", Source: "user_message",
			},
			Category:      "implementation",
			Status:        "completed",
			EvidenceGrade: "A",
			ResultStatements: []sessiondigestv2.ResultStatement{{
				Text:   "完成结果优先的结构化摘要",
				Source: "derived_evidence",
				EvidenceRefs: []string{
					"ev-file-1",
				},
			}},
			AgentClaims: []sessiondigestv2.AgentClaim{},
			Evidence: []sessiondigestv2.Evidence{{
				Ref: "ev-file-1", Kind: "file_change", Status: "changed",
				Summary: "修改 api/internal/sessiondigestv2/extractor.go",
			}},
			Changes: []sessiondigestv2.Change{{
				Path:      "api/internal/sessiondigestv2/extractor.go",
				Operation: "update", EvidenceRef: "ev-file-1",
			}},
			Validations: []sessiondigestv2.Validation{},
			Unresolved:  []sessiondigestv2.Unresolved{},
		})
		digest.DailySummaries = sessiondigestv2.BuildDailySummaries(
			digest.WorkUnits, time.FixedZone("Asia/Shanghai", 8*60*60),
			0,
		)
		digest.Coverage = sessiondigestv2.Coverage{
			SourceEventCount: 2, IncludedEventCount: 1, OmittedEventCount: 1,
			SourceWorkUnitCount: 1, DetailedWorkUnitCount: 1,
			Representation: "result_focused",
		}
		digest.SessionSummary = sessiondigestv2.SessionSummary{
			PrimaryResultCount: 1, VerifiedResultCount: 1,
			StatusCounts: sessiondigestv2.StatusCounts{Completed: 1},
		}
		encoded, _ := json.Marshal(digest)
		if _, err := database.ExecContext(ctx, `
			INSERT INTO session_slice_digest_revisions (
				session_content_slice_id, content_projection_revision_id, generation_id,
				content_epoch, digest_version, redaction_version, status, digest_json,
				source_event_count, included_event_count, omitted_event_count,
				source_bytes, digest_bytes, truncated, source_sha256, digest_sha256, ready_at
			)
			SELECT sl.id, $2, sl.generation_id, s.content_epoch,
				$3, $4, 'ready', $5::jsonb, 2, 1, 1,
				1000000, $6, false, $7, $8, now()
			FROM session_content_slices sl JOIN sessions s ON s.id = sl.session_id
			WHERE sl.id = $1`,
			sliceID, fixture.revisionID, sessiondigestv2.Version,
			sessiondigestv2.RedactionVersion, string(encoded), len(encoded),
			hash64("digest-v2-source-"+sliceID), sessiondigestv2.HashBytes(encoded),
		); err != nil {
			t.Fatal(err)
		}
	}

	runID, attached, err := service.CreateAttachedRun(
		ctx, userIDText, "personal_weekly", period, selection.ID,
		"report_agent_run", "agent-v2-test", "",
		false,
		map[string]any{"report_type": "personal_weekly"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if runID == "" || attached.RequiredReadMode != ReadModeDigestV2 {
		t.Fatalf("unexpected attached v2 selection: run=%s selection=%+v", runID, attached)
	}
	assertDigestSelectionValidation(
		t, ctx, database, service, userIDText, selection.ID, runID, period,
		ErrSourceIncomplete,
	)
	if _, err := service.ReadAttachedSelection(
		ctx, userIDText, selection.ID, runID, "personal_weekly", period, "cursor",
	); !errors.Is(err, ErrReadModeMismatch) {
		t.Fatalf("digest v2 accepted a page cursor: %v", err)
	}
	page, err := service.ReadAttachedSelection(
		ctx, userIDText, selection.ID, runID, "personal_weekly", period, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	var payload digestV2ContentPage
	if err := json.Unmarshal(page.FrozenPayload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ContentMode != ReadModeDigestV2 || !payload.Coverage.Complete ||
		payload.HasMore || payload.ReturnedCount != 2 ||
		payload.Budget.ActualBytes != len(page.FrozenPayload) {
		t.Fatalf("invalid v2 payload: %+v bytes=%d", payload, len(page.FrozenPayload))
	}
	for _, item := range payload.Items {
		if item.Digest.SchemaVersion != sessiondigestv2.Version ||
			item.Digest.ReportPeriodSummary != nil ||
			len(item.Digest.WorkUnits) != 0 {
			t.Fatalf("invalid v2 item: %+v", item)
		}
	}
	if payload.ReportPeriod == nil ||
		len(payload.ReportPeriod.Days) != 1 ||
		len(payload.ReportPeriod.Days[0].Highlights) != 2 ||
		!payload.ReportPeriod.Days[0].OutcomeCoverage.Complete ||
		payload.ReportPeriod.Days[0].OutcomeCoverage.SourceCount != 2 ||
		payload.ReportPeriod.Days[0].OutcomeCoverage.RepresentedCount != 2 {
		t.Fatalf("selection-level report summary was not preserved: %+v", payload.ReportPeriod)
	}
	visible := string(page.FrozenPayload)
	if !strings.Contains(visible, `"work_units"`) ||
		strings.Count(visible, `"report_period_summary"`) != 1 ||
		strings.Contains(visible, `"events"`) ||
		strings.Contains(visible, `"payload"`) {
		t.Fatalf("unexpected v2 source shape: %s", visible)
	}
	assertDigestSelectionValidation(
		t, ctx, database, service, userIDText, selection.ID, runID, period, nil,
	)
}
