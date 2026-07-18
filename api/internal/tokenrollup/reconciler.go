package tokenrollup

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"
)

const (
	defaultCleanupInterval      = 5 * time.Minute
	defaultSnapshotCleanupBatch = 2
	defaultRollupCleanupBatch   = 5
)

type CleanupResult struct {
	Snapshots int64
	Rollups   int64
}

// Reconciler removes expired query snapshots and superseded derived Rollups in
// small background batches. Cleanup never runs in a Token HTTP request and it
// never removes an active Rollup or a Rollup still referenced by a snapshot.
type Reconciler struct {
	db            *sql.DB
	interval      time.Duration
	snapshotBatch int
	rollupBatch   int
}

func NewReconciler(database *sql.DB) (*Reconciler, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &Reconciler{
		db: database, interval: defaultCleanupInterval,
		snapshotBatch: defaultSnapshotCleanupBatch, rollupBatch: defaultRollupCleanupBatch,
	}, nil
}

func (r *Reconciler) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				result, err := r.RunOnce(ctx)
				if err != nil {
					log.Printf("token rollup cleanup deferred: %v", err)
					continue
				}
				if result.Snapshots > 0 || result.Rollups > 0 {
					log.Printf("token rollup cleanup removed snapshots=%d rollups=%d",
						result.Snapshots, result.Rollups)
				}
			}
		}
	}()
}

func (r *Reconciler) RunOnce(ctx context.Context) (CleanupResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return CleanupResult{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout = '100ms'`); err != nil {
		return CleanupResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout = '5s'`); err != nil {
		return CleanupResult{}, err
	}

	var result CleanupResult
	snapshotDelete, err := tx.ExecContext(ctx, `
		WITH candidates AS (
			SELECT id FROM token_query_snapshots
			WHERE expires_at <= statement_timestamp()
			ORDER BY expires_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		DELETE FROM token_query_snapshots snapshot
		USING candidates WHERE snapshot.id = candidates.id`, r.snapshotBatch)
	if err != nil {
		return CleanupResult{}, err
	}
	result.Snapshots, _ = snapshotDelete.RowsAffected()

	rollupDelete, err := tx.ExecContext(ctx, `
		WITH candidates AS (
			SELECT rollup.id
			FROM session_family_rollup_versions rollup
			WHERE rollup.status = 'superseded'
				AND rollup.superseded_at < statement_timestamp() - interval '30 minutes'
				AND NOT EXISTS (
					SELECT 1 FROM token_query_snapshot_rollups reference
					WHERE reference.rollup_version_id = rollup.id
				)
			ORDER BY rollup.superseded_at, rollup.id
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		DELETE FROM session_family_rollup_versions rollup
		USING candidates WHERE rollup.id = candidates.id`, r.rollupBatch)
	if err != nil {
		return CleanupResult{}, err
	}
	result.Rollups, _ = rollupDelete.RowsAffected()
	if err := tx.Commit(); err != nil {
		return CleanupResult{}, err
	}
	return result, nil
}
