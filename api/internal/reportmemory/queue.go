package reportmemory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
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
	fingerprint := memorySourceFingerprint(reportID, reportDate, content, runID, briefSignature)
	dueAt := nextNightlyWindow(now)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO report_project_memory_jobs (
			user_id, report_date, report_id, desired_source_fingerprint, status, due_at
		) VALUES ($1, $2::date, $3, $4, 'pending', $5)
		ON CONFLICT (user_id, report_date) DO UPDATE SET
			report_id = EXCLUDED.report_id,
			desired_source_fingerprint = EXCLUDED.desired_source_fingerprint,
			status = CASE
				WHEN report_project_memory_jobs.status IN ('submitting', 'running') THEN report_project_memory_jobs.status
				ELSE 'pending'
			END,
			due_at = EXCLUDED.due_at,
			attempts = CASE
				WHEN report_project_memory_jobs.desired_source_fingerprint IS DISTINCT FROM EXCLUDED.desired_source_fingerprint THEN 0
				ELSE report_project_memory_jobs.attempts
			END,
			last_error = NULL,
			updated_at = now()`, userID, reportDate, reportID, fingerprint, dueAt)
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
