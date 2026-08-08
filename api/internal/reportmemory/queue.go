package reportmemory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/biztime"
)

func QueueReportChange(ctx context.Context, tx *sql.Tx, reportID, userID, reportDate string, now time.Time) error {
	if tx == nil || strings.TrimSpace(reportID) == "" || strings.TrimSpace(userID) == "" || strings.TrimSpace(reportDate) == "" {
		return errors.New("report memory change is incomplete")
	}
	var content, runID, briefSignature string
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(NULLIF(r.submitted_content, ''), r.content),
		       COALESCE(r.managed_agent_run_id::text, ''),
		       COALESCE(review.final_brief_json::text, b.brief_hash, '')
		FROM daily_reports r
		LEFT JOIN report_run_briefs b ON b.run_id = r.managed_agent_run_id
		LEFT JOIN report_review_jobs review ON review.run_id = r.managed_agent_run_id
			AND review.status = 'written'
		WHERE r.id = $1 AND r.user_id = $2 AND r.report_date = $3::date
		  AND r.status IN ('saved', 'submitted')`, reportID, userID, reportDate).
		Scan(&content, &runID, &briefSignature)
	if err != nil {
		return err
	}
	eventFingerprint := memorySourceFingerprint(reportID, reportDate, content, runID, briefSignature)
	dueAt := nextNightlyWindow(now)
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "project-memory-queue:"+userID); err != nil {
		return err
	}
	var existingFingerprint string
	var lastEventFingerprint string
	var existingWatermark int64
	err = tx.QueryRowContext(ctx, `
		SELECT desired_source_fingerprint, COALESCE(last_event_fingerprint, ''), desired_evidence_watermark
		FROM report_project_memory_jobs WHERE user_id = $1 FOR UPDATE`, userID).
		Scan(&existingFingerprint, &lastEventFingerprint, &existingWatermark)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `
		INSERT INTO report_project_memory_jobs (
			user_id, report_date, report_id, dirty_from_date,
			desired_source_fingerprint, last_event_fingerprint,
			desired_evidence_watermark, status, due_at, rebuild_required
		) VALUES ($1, $2::date, $3, COALESCE((
			SELECT min(report_date) FROM (
				SELECT report_date FROM daily_reports
				WHERE user_id = $1 AND report_date <= $2::date
				  AND status IN ('saved', 'submitted')
				  AND NULLIF(BTRIM(COALESCE(NULLIF(submitted_content, ''), content, '')), '') IS NOT NULL
				ORDER BY report_date DESC, updated_at DESC LIMIT $7
			) recent
		), $2::date), $4, $4, 1, 'pending', $5,
			EXISTS (
				SELECT 1 FROM report_project_memory_snapshots snapshot
				WHERE snapshot.user_id = $1 AND snapshot.resolver_version = $6
				  AND snapshot.evidence_cutoff_date >= $2::date
			))`, userID, reportDate, reportID, eventFingerprint, dueAt, ResolverVersion, maxMemorySnapshotDepth)
		return err
	}
	if err != nil {
		return err
	}
	if lastEventFingerprint == eventFingerprint {
		return nil
	}
	nextWatermark := existingWatermark + 1
	aggregateFingerprint := memorySourceFingerprint(existingFingerprint, eventFingerprint, strconv.FormatInt(nextWatermark, 10))
	_, err = tx.ExecContext(ctx, `
		UPDATE report_project_memory_jobs SET
			report_id = CASE WHEN $2::date >= report_date THEN $3 ELSE report_id END,
			report_date = GREATEST(report_date, $2::date),
			dirty_from_date = CASE WHEN status = 'succeeded' THEN $2::date ELSE LEAST(dirty_from_date, $2::date) END,
			desired_source_fingerprint = $4, last_event_fingerprint = $8,
			desired_evidence_watermark = $5,
			rebuild_required = rebuild_required OR EXISTS (
				SELECT 1 FROM report_project_memory_snapshots snapshot
				WHERE snapshot.user_id = $1 AND snapshot.resolver_version = $7
				  AND snapshot.evidence_cutoff_date >= $2::date
			),
			status = CASE WHEN status IN ('submitting', 'running') THEN status ELSE 'pending' END,
			due_at = $6, attempts = 0, last_error = NULL, updated_at = now()
		WHERE user_id = $1`, userID, reportDate, reportID, aggregateFingerprint, nextWatermark, dueAt, ResolverVersion, eventFingerprint)
	return err
}

func QueueReportChangeDB(ctx context.Context, database *sql.DB, reportID, userID, reportDate string, now time.Time) error {
	if database == nil {
		return sql.ErrConnDone
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := QueueReportChange(ctx, tx, reportID, userID, reportDate, now); err != nil {
		return err
	}
	return tx.Commit()
}

func memorySourceFingerprint(values ...string) string {
	hash := sha256.New()
	hash.Write([]byte(ResolverVersion))
	for _, value := range values {
		hash.Write([]byte{0})
		hash.Write([]byte(strings.TrimSpace(value)))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func nextNightlyWindow(now time.Time) time.Time {
	local := biztime.InLocation(now)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), 2, 0, 0, 0, biztime.Location())
	if !local.Before(candidate) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate.UTC()
}
