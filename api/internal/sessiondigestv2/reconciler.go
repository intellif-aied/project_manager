package sessiondigestv2

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"sort"
	"time"

	"github.com/aidashboard/api/internal/sessionsync"
)

// Reconciler discovers fully indexed immutable slices after upload and content
// projection have completed. It creates only v2 revisions and v2 jobs.
type Reconciler struct {
	db       *sql.DB
	config   Config
	interval time.Duration
}

func NewReconciler(database *sql.DB, config Config) (*Reconciler, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	normalized, err := config.Normalized()
	if err != nil {
		return nil, err
	}
	return &Reconciler{db: database, config: normalized, interval: 5 * time.Second}, nil
}

func (r *Reconciler) Start(ctx context.Context) {
	go func() {
		if _, err := r.RunOnce(ctx); err != nil {
			log.Printf("session digest v2 reconciler failed: %v", err)
		}
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := r.RunOnce(ctx); err != nil {
					log.Printf("session digest v2 reconciler failed: %v", err)
				}
			}
		}
	}()
}

func (r *Reconciler) RunOnce(ctx context.Context) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT sl.id::text, s.id::text, rev.id::text, sl.generation_id::text,
			s.content_epoch
		FROM session_content_slices sl
		JOIN sessions s ON s.id = sl.session_id
		JOIN session_sources src ON src.id = sl.source_id AND src.session_id = s.id
		JOIN session_source_generations g
			ON g.id = sl.generation_id AND g.id = src.active_generation_id AND g.status = 'active'
		JOIN session_content_projection_revisions rev
			ON rev.id = src.active_content_projection_revision_id
			AND rev.generation_id = g.id AND rev.status = 'active'
		WHERE s.content_status = 'available'
			AND rev.content_indexed_cursor >= sl.end_cursor
			AND NOT EXISTS (
				SELECT 1 FROM session_slice_digest_revisions d
				WHERE d.session_content_slice_id = sl.id
					AND d.content_projection_revision_id = rev.id
					AND d.content_epoch = s.content_epoch
					AND d.digest_version = $1
					AND d.redaction_version = $2
			)
		ORDER BY sl.created_at DESC, sl.id DESC
		LIMIT $3
		FOR UPDATE OF sl SKIP LOCKED`,
		r.config.DigestVersion, r.config.RedactionVersion, r.config.ReconcileBatch,
	)
	if err != nil {
		return 0, err
	}
	type candidate struct {
		sliceID, sessionID, revisionID, generationID string
		contentEpoch                                 int64
	}
	candidates := make([]candidate, 0, r.config.ReconcileBatch)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(
			&item.sliceID, &item.sessionID, &item.revisionID,
			&item.generationID, &item.contentEpoch,
		); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	sessionIDs := make([]string, 0, len(candidates))
	seenSessions := make(map[string]struct{}, len(candidates))
	for _, item := range candidates {
		if _, exists := seenSessions[item.sessionID]; exists {
			continue
		}
		seenSessions[item.sessionID] = struct{}{}
		sessionIDs = append(sessionIDs, item.sessionID)
	}
	sort.Strings(sessionIDs)
	for _, sessionID := range sessionIDs {
		if err := sessionsync.LockSessionForUpdate(ctx, tx, sessionID); err != nil {
			return 0, err
		}
	}

	created := 0
	for _, item := range candidates {
		var digestRevisionID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO session_slice_digest_revisions (
				session_content_slice_id, content_projection_revision_id,
				generation_id, content_epoch, digest_version, redaction_version
			) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (
				session_content_slice_id, content_projection_revision_id, content_epoch,
				digest_version, redaction_version
			) DO NOTHING
			RETURNING id::text`,
			item.sliceID, item.revisionID, item.generationID, item.contentEpoch,
			r.config.DigestVersion, r.config.RedactionVersion,
		).Scan(&digestRevisionID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO session_processing_jobs (
				job_type, session_id, generation_id, target_digest_revision_id,
				content_epoch, max_attempts, urgency
			) VALUES ($1, $2, $3, $4, $5, 5, 'background')
			ON CONFLICT DO NOTHING`,
			JobType, item.sessionID, item.generationID, digestRevisionID, item.contentEpoch,
		); err != nil {
			return 0, err
		}
		created++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return created, nil
}
