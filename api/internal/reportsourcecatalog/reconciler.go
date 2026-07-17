package reportsourcecatalog

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"
)

const (
	defaultReconcileInterval = 10 * time.Second
	defaultReconcileBatch    = 10
)

// Reconciler is a low-priority safety net for slices that could not be
// materialized in the projection path (for example, a projection completed
// before Finalize created the immutable slice). Each run handles a small,
// bounded batch and never participates in an upload transaction.
type Reconciler struct {
	db        *sql.DB
	interval  time.Duration
	batchSize int
}

func NewReconciler(database *sql.DB) (*Reconciler, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &Reconciler{
		db:        database,
		interval:  defaultReconcileInterval,
		batchSize: defaultReconcileBatch,
	}, nil
}

func (r *Reconciler) Start(ctx context.Context) {
	go func() {
		r.runAndLog(ctx)
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.runAndLog(ctx)
			}
		}
	}()
}

func (r *Reconciler) RunOnce(ctx context.Context) (int64, error) {
	return ReconcileRevision(ctx, r.db, "", r.batchSize)
}

func (r *Reconciler) runAndLog(ctx context.Context) {
	count, err := r.RunOnce(ctx)
	if err != nil {
		log.Printf("report source catalog reconciler failed: %v", err)
		return
	}
	if count > 0 {
		log.Printf("report source catalog reconciled %d slices", count)
	}
}
