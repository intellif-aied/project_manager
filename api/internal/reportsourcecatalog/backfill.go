package reportsourcecatalog

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type BackfillStatus struct {
	Eligible int64 `json:"eligible"`
	Missing  int64 `json:"missing"`
	Building int64 `json:"building"`
	Ready    int64 `json:"ready"`
	Failed   int64 `json:"failed"`
}

func InspectBackfill(ctx context.Context, database *sql.DB) (BackfillStatus, error) {
	if database == nil {
		return BackfillStatus{}, errors.New("database is required")
	}
	var status BackfillStatus
	err := database.QueryRowContext(ctx, `
		WITH eligible AS (
			SELECT sl.id AS slice_id, revision.id AS revision_id
			FROM session_content_slices sl
			JOIN sessions session ON session.id = sl.session_id
			JOIN session_sources source
				ON source.id = sl.source_id
				AND source.session_id = session.id
			JOIN session_content_projection_revisions revision
				ON revision.id = source.active_content_projection_revision_id
				AND revision.generation_id = sl.generation_id
				AND revision.status = 'active'
			WHERE session.content_status = 'available'
				AND revision.content_indexed_cursor >= sl.end_cursor
		)
		SELECT COUNT(*) AS eligible,
			COUNT(*) FILTER (WHERE catalog.slice_id IS NULL) AS missing,
			COUNT(*) FILTER (WHERE catalog.status = 'building') AS building,
			COUNT(*) FILTER (WHERE catalog.status = 'ready') AS ready,
			COUNT(*) FILTER (WHERE catalog.status = 'failed') AS failed
		FROM eligible
		LEFT JOIN report_source_slice_catalog catalog
			ON catalog.slice_id = eligible.slice_id
			AND catalog.content_projection_revision_id = eligible.revision_id`,
	).Scan(&status.Eligible, &status.Missing, &status.Building, &status.Ready, &status.Failed)
	return status, err
}

// ForegroundBusy is deliberately conservative: catalog history work waits if
// another backend already has a long-running foreground query or a lock wait.
// This guard complements the short transaction-local timeouts in RunBackfillBatch.
func ForegroundBusy(ctx context.Context, database *sql.DB) (bool, error) {
	if database == nil {
		return false, errors.New("database is required")
	}
	var busy bool
	err := database.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_stat_activity activity
			WHERE activity.datname = current_database()
				AND activity.pid <> pg_backend_pid()
				AND (
					activity.wait_event_type = 'Lock' OR
					(
						activity.state = 'active' AND
						activity.query_start < clock_timestamp() - interval '1 second'
					)
				)
		)`,
	).Scan(&busy)
	return busy, err
}

func RunBackfillBatch(ctx context.Context, database *sql.DB, batchSize int) (int64, error) {
	if database == nil {
		return 0, errors.New("database is required")
	}
	if batchSize <= 0 {
		return 0, errors.New("batch size must be positive")
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout = '250ms'`); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout = '5s'`); err != nil {
		return 0, err
	}
	count, err := BackfillBatch(ctx, tx, batchSize)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

type BackfillReport struct {
	Before         BackfillStatus `json:"before"`
	After          BackfillStatus `json:"after"`
	Processed      int64          `json:"processed"`
	Batches        int            `json:"batches"`
	PressurePauses int            `json:"pressure_pauses"`
	Complete       bool           `json:"complete"`
	Elapsed        time.Duration  `json:"elapsed"`
}
