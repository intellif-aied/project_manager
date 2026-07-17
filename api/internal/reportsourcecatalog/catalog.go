package reportsourcecatalog

import (
	"context"
	"database/sql"
	"errors"
)

const (
	StatusBuilding   = "building"
	StatusReady      = "ready"
	StatusFailed     = "failed"
	StatusSuperseded = "superseded"
	StatusCleared    = "cleared"
)

type queryExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// EnsureSlice creates lightweight building rows for every usable projection
// revision of a newly finalized slice. It never reads event payloads.
func EnsureSlice(ctx context.Context, database queryExecer, sliceID string) error {
	if database == nil || sliceID == "" {
		return errors.New("database and slice ID are required")
	}
	_, err := database.ExecContext(ctx, `
		INSERT INTO report_source_slice_catalog (
			slice_id, content_projection_revision_id, user_id, session_id,
			session_ref, agent_type, source_id, generation_id, content_epoch,
			start_cursor, end_cursor, cwd, models, status
		)
		SELECT sl.id, rev.id, s.user_id, s.id, s.session_ref, s.agent_type,
			src.id, sl.generation_id, s.content_epoch, sl.start_cursor, sl.end_cursor,
			COALESCE(s.cwd, ''), COALESCE(s.models, '{}'::text[]), 'building'
		FROM session_content_slices sl
		JOIN sessions s ON s.id = sl.session_id
		JOIN session_sources src ON src.id = sl.source_id AND src.session_id = s.id
		JOIN session_content_projection_revisions rev
			ON rev.generation_id = sl.generation_id
			AND rev.status IN ('building', 'validated', 'active')
		WHERE sl.id = $1
		ON CONFLICT (slice_id, content_projection_revision_id) DO UPDATE
		SET user_id = EXCLUDED.user_id,
			session_id = EXCLUDED.session_id,
			session_ref = EXCLUDED.session_ref,
			agent_type = EXCLUDED.agent_type,
			source_id = EXCLUDED.source_id,
			generation_id = EXCLUDED.generation_id,
			content_epoch = EXCLUDED.content_epoch,
			start_cursor = EXCLUDED.start_cursor,
			end_cursor = EXCLUDED.end_cursor,
			cwd = EXCLUDED.cwd,
			models = EXCLUDED.models,
			status = CASE
				WHEN report_source_slice_catalog.status = 'ready' THEN 'ready'
				ELSE 'building'
			END,
			updated_at = now()`, sliceID)
	return err
}

// ReconcileRevision materializes at most batchSize complete slices for one
// projection revision. Passing an empty revision ID reconciles the next global
// batch and is used by the low-priority safety reconciler.
func ReconcileRevision(
	ctx context.Context,
	database queryExecer,
	revisionID string,
	batchSize int,
) (int64, error) {
	return reconcile(ctx, database, revisionID, batchSize, revisionID != "")
}

// BackfillBatch materializes a bounded batch that may not have a catalog row
// yet. It is intentionally separate from the always-on reconciler so deploying
// the additive schema never starts an unbounded historical scan.
func BackfillBatch(ctx context.Context, database queryExecer, batchSize int) (int64, error) {
	return reconcile(ctx, database, "", batchSize, true)
}

func reconcile(
	ctx context.Context,
	database queryExecer,
	revisionID string,
	batchSize int,
	includeMissing bool,
) (int64, error) {
	if database == nil {
		return 0, errors.New("database is required")
	}
	if batchSize <= 0 {
		return 0, errors.New("batch size must be positive")
	}
	var revision any
	if revisionID != "" {
		revision = revisionID
	}
	result, err := database.ExecContext(ctx, `
		WITH candidates AS MATERIALIZED (
			SELECT sl.id AS slice_id, rev.id AS revision_id, s.user_id, s.id AS session_id,
				s.session_ref, s.agent_type, src.id AS source_id, sl.generation_id,
				s.content_epoch, sl.start_cursor, sl.end_cursor,
				COALESCE(s.cwd, '') AS cwd, COALESCE(s.models, '{}'::text[]) AS models,
				(
					rev.status = 'active' AND
					src.active_content_projection_revision_id = rev.id AND
					s.content_status = 'available'
				) AS should_be_ready
			FROM session_content_slices sl
			JOIN sessions s ON s.id = sl.session_id
			JOIN session_sources src ON src.id = sl.source_id AND src.session_id = s.id
			JOIN session_content_projection_revisions rev
				ON rev.generation_id = sl.generation_id
				AND rev.status IN ('building', 'validated', 'active')
			LEFT JOIN report_source_slice_catalog existing
				ON existing.slice_id = sl.id
				AND existing.content_projection_revision_id = rev.id
			WHERE rev.content_indexed_cursor >= sl.end_cursor
				AND ($1::uuid IS NULL OR rev.id = $1::uuid)
				AND ($3::boolean OR existing.slice_id IS NOT NULL)
				AND (
					existing.slice_id IS NULL OR
					(existing.status = 'building' AND (
						existing.event_count = 0 OR
						(
							rev.status = 'active' AND
							src.active_content_projection_revision_id = rev.id AND
							s.content_status = 'available'
						)
					)) OR
					(existing.content_epoch <> s.content_epoch AND existing.status <> 'cleared')
				)
			ORDER BY existing.updated_at NULLS FIRST, sl.created_at DESC, sl.id, rev.created_at, rev.id
			LIMIT $2
			FOR UPDATE OF sl SKIP LOCKED
		), materialized AS (
			SELECT candidate.*,
				stats.activity_start_at, stats.activity_end_at, stats.event_count,
				COALESCE(summary.summary,
					'Session 增量内容（' || stats.event_count::text || ' 条记录）') AS summary
			FROM candidates candidate
			CROSS JOIN LATERAL (
				SELECT MIN(event.occurred_at) AS activity_start_at,
					MAX(event.occurred_at) AS activity_end_at,
					COUNT(*) AS event_count
				FROM session_content_events event
				WHERE event.content_projection_revision_id = candidate.revision_id
					AND event.source_start_cursor >= candidate.start_cursor
					AND event.source_end_cursor <= candidate.end_cursor
			) stats
			LEFT JOIN LATERAL (
				SELECT NULLIF(btrim(event.summary), '') AS summary
				FROM session_content_events event
				WHERE event.content_projection_revision_id = candidate.revision_id
					AND event.source_start_cursor >= candidate.start_cursor
					AND event.source_end_cursor <= candidate.end_cursor
					AND NULLIF(btrim(event.summary), '') IS NOT NULL
					AND event.event_type NOT IN (
						'response_item.custom_tool_call', 'response_item.function_call'
					)
				ORDER BY CASE event.event_type
					WHEN 'event_msg.user_message' THEN 0
					WHEN 'event_msg.agent_message' THEN 1
					WHEN 'response_item.message' THEN 2
					ELSE 3
				END, event.source_start_cursor, event.id
				LIMIT 1
			) summary ON true
		)
		INSERT INTO report_source_slice_catalog (
			slice_id, content_projection_revision_id, user_id, session_id,
			session_ref, agent_type, source_id, generation_id, content_epoch,
			start_cursor, end_cursor, event_count, activity_start_at,
			activity_end_at, last_activity_at, summary, cwd, models, status, ready_at
		)
		SELECT slice_id, revision_id, user_id, session_id, session_ref, agent_type,
			source_id, generation_id, content_epoch, start_cursor, end_cursor,
			event_count, activity_start_at, activity_end_at, activity_end_at,
			summary, cwd, models,
			CASE
				WHEN event_count = 0 THEN 'failed'
				WHEN should_be_ready THEN 'ready'
				ELSE 'building'
			END,
			CASE WHEN event_count > 0 AND should_be_ready THEN now() ELSE NULL END
		FROM materialized
		ON CONFLICT (slice_id, content_projection_revision_id) DO UPDATE
		SET user_id = EXCLUDED.user_id,
			session_id = EXCLUDED.session_id,
			session_ref = EXCLUDED.session_ref,
			agent_type = EXCLUDED.agent_type,
			source_id = EXCLUDED.source_id,
			generation_id = EXCLUDED.generation_id,
			content_epoch = EXCLUDED.content_epoch,
			start_cursor = EXCLUDED.start_cursor,
			end_cursor = EXCLUDED.end_cursor,
			event_count = EXCLUDED.event_count,
			activity_start_at = EXCLUDED.activity_start_at,
			activity_end_at = EXCLUDED.activity_end_at,
			last_activity_at = EXCLUDED.last_activity_at,
			summary = EXCLUDED.summary,
			cwd = EXCLUDED.cwd,
			models = EXCLUDED.models,
			status = EXCLUDED.status,
			ready_at = CASE
				WHEN EXCLUDED.status = 'ready' THEN
					COALESCE(report_source_slice_catalog.ready_at, EXCLUDED.ready_at)
				ELSE NULL
			END,
			updated_at = now()`, revision, batchSize, includeMissing)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// ActivateRevision switches only catalog metadata. It does not read event
// payloads, so projection activation remains a short transaction.
func ActivateRevision(
	ctx context.Context,
	database queryExecer,
	sourceID, revisionID string,
) error {
	if database == nil || sourceID == "" || revisionID == "" {
		return errors.New("database, source ID, and revision ID are required")
	}
	_, err := database.ExecContext(ctx, `
		WITH superseded AS (
			UPDATE report_source_slice_catalog
			SET status = 'superseded', ready_at = NULL, updated_at = now()
			WHERE source_id = $1
				AND content_projection_revision_id <> $2
				AND status IN ('building', 'ready')
			RETURNING slice_id
		)
		UPDATE report_source_slice_catalog catalog
		SET status = 'ready',
			ready_at = COALESCE(catalog.ready_at, now()),
			updated_at = now()
		FROM sessions session, session_sources source,
			session_content_projection_revisions revision
		WHERE catalog.source_id = $1
			AND catalog.content_projection_revision_id = $2
			AND catalog.event_count > 0
			AND session.id = catalog.session_id
			AND session.content_status = 'available'
			AND session.content_epoch = catalog.content_epoch
			AND source.id = catalog.source_id
			AND source.active_content_projection_revision_id = catalog.content_projection_revision_id
			AND revision.id = catalog.content_projection_revision_id
			AND revision.status = 'active'`, sourceID, revisionID)
	return err
}

func MarkSessionCleared(ctx context.Context, database queryExecer, sessionID string) error {
	if database == nil || sessionID == "" {
		return errors.New("database and session ID are required")
	}
	_, err := database.ExecContext(ctx, `
		UPDATE report_source_slice_catalog
		SET status = 'cleared', ready_at = NULL, updated_at = now()
		WHERE session_id = $1 AND status <> 'cleared'`, sessionID)
	return err
}
