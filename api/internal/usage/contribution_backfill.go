package usage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const contributionBackfillMaxOutstandingJobs = 50

type ContributionBackfillStatus struct {
	EligibleSources          int64 `json:"eligible_sources"`
	UnsafeSources            int64 `json:"unsafe_sources"`
	MissingRevisions         int64 `json:"missing_revisions"`
	BuildingRevisions        int64 `json:"building_revisions"`
	ActiveRevisions          int64 `json:"active_revisions"`
	FailedRevisions          int64 `json:"failed_revisions"`
	PendingJobs              int64 `json:"pending_jobs"`
	DeadJobs                 int64 `json:"dead_jobs"`
	ActiveContributions      int64 `json:"active_contributions"`
	MissingFamilyMemberships int64 `json:"missing_family_memberships"`
	ReconciliationFailures   int64 `json:"reconciliation_failures"`
}

type ContributionBackfillReport struct {
	Before          ContributionBackfillStatus `json:"before"`
	After           ContributionBackfillStatus `json:"after"`
	EnqueuedSources int64                      `json:"enqueued_sources"`
	Batches         int                        `json:"batches"`
	PressurePauses  int                        `json:"pressure_pauses"`
	Complete        bool                       `json:"complete"`
	Elapsed         string                     `json:"elapsed"`
}

type ContributionRepairReport struct {
	SourceID             string `json:"source_id"`
	RevisionID           string `json:"revision_id"`
	DeletedJobs          int64  `json:"deleted_jobs"`
	DeletedClaims        int64  `json:"deleted_claims"`
	DeletedComponents    int64  `json:"deleted_components"`
	DeletedContributions int64  `json:"deleted_contributions"`
	DeletedLogicalEvents int64  `json:"deleted_logical_events"`
	DeletedObservations  int64  `json:"deleted_observations"`
	DeletedCheckpoints   int64  `json:"deleted_checkpoints"`
	DeletedDailyRows     int64  `json:"deleted_daily_rows"`
	EnqueuedJobs         int64  `json:"enqueued_jobs"`
}

// RepairFailedContributionRevision clears only the derived rows of one
// quality-gated revision and queues the same source for a fresh parse. Raw
// upload chunks remain untouched. It refuses to reset a revision referenced by
// an active Rollup or Snapshot so an online aggregate can never be rewritten
// underneath a reader.
func RepairFailedContributionRevision(
	ctx context.Context,
	database *sql.DB,
	normalizerVersion, sourceID string,
) (ContributionRepairReport, error) {
	if database == nil || normalizerVersion == "" || sourceID == "" {
		return ContributionRepairReport{}, errors.New("database, normalizer version, and source id are required")
	}
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ContributionRepairReport{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout = '250ms'`); err != nil {
		return ContributionRepairReport{}, err
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout = '60s'`); err != nil {
		return ContributionRepairReport{}, err
	}

	var report ContributionRepairReport
	var activeRevision sql.NullString
	var stateStatus, revisionStatus string
	var generationID, sessionID string
	var expectedCursor int64
	err = tx.QueryRowContext(ctx, `
		SELECT revision.id::text, revision.status, session.id::text,
			generation.id::text, generation.expected_cursor,
			state.active_revision_id::text, state.status
		FROM session_metrics_revisions revision
		JOIN session_sources source ON source.id = revision.source_id
		JOIN sessions session ON session.id = source.session_id
		JOIN session_source_generations generation ON generation.id = source.active_generation_id
		JOIN session_source_metrics_states state ON state.source_id = source.id
		WHERE source.id = $1 AND revision.parser_version = $2
			AND revision.normalizer_version = $3 AND revision.status = 'failed'
		FOR UPDATE OF revision, source, generation, state`,
		sourceID, ParserVersion, normalizerVersion).Scan(
		&report.RevisionID, &revisionStatus, &sessionID, &generationID,
		&expectedCursor, &activeRevision, &stateStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return ContributionRepairReport{}, fmt.Errorf("failed revision not found for source %s", sourceID)
	}
	if err != nil {
		return ContributionRepairReport{}, err
	}
	if revisionStatus != "failed" || stateStatus == "ready" || activeRevision.Valid {
		return ContributionRepairReport{}, fmt.Errorf("source %s is not a reset-safe failed revision", sourceID)
	}
	var activeRollupRefs, snapshotRefs int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_family_rollup_revision_refs WHERE revision_id = $1`,
		report.RevisionID).Scan(&activeRollupRefs); err != nil {
		return ContributionRepairReport{}, err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM token_query_snapshot_rollups snapshot
		JOIN session_family_rollup_revision_refs reference
			ON reference.rollup_version_id = snapshot.rollup_version_id
		WHERE reference.revision_id = $1`, report.RevisionID).Scan(&snapshotRefs); err != nil {
		return ContributionRepairReport{}, err
	}
	if activeRollupRefs != 0 || snapshotRefs != 0 {
		return ContributionRepairReport{}, fmt.Errorf(
			"source %s revision is referenced by rollup=%d snapshot=%d",
			sourceID, activeRollupRefs, snapshotRefs)
	}
	report.SourceID = sourceID

	deleteRows := func(query string, args ...any) (int64, error) {
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return 0, err
		}
		return result.RowsAffected()
	}
	if report.DeletedJobs, err = deleteRows(
		`DELETE FROM session_processing_jobs WHERE target_metrics_revision_id = $1`, report.RevisionID); err != nil {
		return ContributionRepairReport{}, err
	}
	if report.DeletedClaims, err = deleteRows(
		`DELETE FROM session_usage_event_claims WHERE active_revision_id = $1`, report.RevisionID); err != nil {
		return ContributionRepairReport{}, err
	}
	if report.DeletedComponents, err = deleteRows(
		`DELETE FROM session_usage_components WHERE revision_id = $1`, report.RevisionID); err != nil {
		return ContributionRepairReport{}, err
	}
	if report.DeletedContributions, err = deleteRows(
		`DELETE FROM session_usage_contributions WHERE revision_id = $1`, report.RevisionID); err != nil {
		return ContributionRepairReport{}, err
	}
	if report.DeletedLogicalEvents, err = deleteRows(
		`DELETE FROM session_logical_usage_events WHERE revision_id = $1`, report.RevisionID); err != nil {
		return ContributionRepairReport{}, err
	}
	if report.DeletedObservations, err = deleteRows(
		`DELETE FROM session_usage_observations WHERE revision_id = $1`, report.RevisionID); err != nil {
		return ContributionRepairReport{}, err
	}
	if report.DeletedCheckpoints, err = deleteRows(
		`DELETE FROM session_parser_checkpoints WHERE revision_id = $1`, report.RevisionID); err != nil {
		return ContributionRepairReport{}, err
	}
	if report.DeletedDailyRows, err = deleteRows(
		`DELETE FROM session_daily_usage WHERE revision_id = $1`, report.RevisionID); err != nil {
		return ContributionRepairReport{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_metrics_revisions
		SET status = 'building', quality_status = 'exact',
			build_start_cursor = 0, validated_through_cursor = 0,
			source_high_water_cursor = $2, scanned_event_count = 0,
			usage_observation_count = 0, usage_event_count = 0,
			advanced_observation_count = 0, duplicate_usage_event_count = 0,
			malformed_event_count = 0, unknown_usage_event_count = 0,
			conflict_usage_event_count = 0, reconciliation_json = '{}'::jsonb,
			calculation_reason = 'targeted repair after quality-gate review',
			validated_at = NULL, activated_at = NULL, superseded_at = NULL
		WHERE id = $1`, report.RevisionID, expectedCursor); err != nil {
		return ContributionRepairReport{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_source_metrics_states
		SET active_revision_id = NULL, target_generation_id = $2,
			status = 'pending', active_usage_parsed_cursor = 0,
			source_high_water_cursor = $3, last_error = NULL, updated_at = now()
		WHERE source_id = $1`, sourceID, generationID, expectedCursor); err != nil {
		return ContributionRepairReport{}, err
	}
	base := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_processing_jobs (
			job_type, session_id, generation_id, chunk_id,
			target_metrics_revision_id, payload, created_at
		)
		SELECT 'parse_usage_chunk', $1, chunk.generation_id, chunk.id, $2,
			jsonb_build_object('reason', 'targeted_token_contribution_repair'),
			$3::timestamptz + ROW_NUMBER() OVER (ORDER BY chunk.start_cursor, chunk.id) * interval '1 microsecond'
		FROM session_upload_chunks chunk
		WHERE chunk.generation_id = $4`, sessionID, report.RevisionID, base, generationID); err != nil {
		return ContributionRepairReport{}, err
	}
	var chunkCount int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_upload_chunks WHERE generation_id = $1`, generationID).Scan(&chunkCount); err != nil {
		return ContributionRepairReport{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_processing_jobs (
			job_type, session_id, generation_id, target_metrics_revision_id,
			payload, created_at
		) VALUES ('rebuild_metrics_revision', $1, $2, $3,
			jsonb_build_object('reason', 'targeted_token_contribution_repair'),
			$4::timestamptz + $5 * interval '1 microsecond')`,
		sessionID, generationID, report.RevisionID, base, chunkCount+1); err != nil {
		return ContributionRepairReport{}, err
	}
	report.EnqueuedJobs = chunkCount + 1
	if err := tx.Commit(); err != nil {
		return ContributionRepairReport{}, err
	}
	return report, nil
}

func InspectContributionBackfill(
	ctx context.Context,
	database *sql.DB,
	normalizerVersion string,
) (ContributionBackfillStatus, error) {
	if database == nil || normalizerVersion == "" {
		return ContributionBackfillStatus{}, errors.New("database and normalizer version are required")
	}
	var status ContributionBackfillStatus
	err := database.QueryRowContext(ctx, `
		WITH eligible AS (
			SELECT source.id AS source_id, session.id AS session_id, generation.id AS generation_id,
				coverage.chunk_count > 0 AND coverage.min_start = 0
				AND coverage.max_end = generation.expected_cursor
				AND coverage.covered_bytes = generation.expected_cursor
				AND coverage.broken_boundaries = 0
				AND coverage.unavailable_inputs = 0 AS safe
			FROM session_sources source
			JOIN sessions session ON session.id = source.session_id
			JOIN session_source_generations generation ON generation.id = source.active_generation_id
			LEFT JOIN LATERAL (
				WITH ordered AS (
					SELECT chunk.id, chunk.start_cursor, chunk.end_cursor, chunk.object_status,
						LAG(chunk.end_cursor) OVER (ORDER BY chunk.start_cursor, chunk.id) AS previous_end
					FROM session_upload_chunks chunk
					WHERE chunk.generation_id = generation.id
				)
				SELECT COUNT(*) AS chunk_count, COALESCE(MIN(start_cursor), 0) AS min_start,
					COALESCE(MAX(end_cursor), 0) AS max_end,
					COALESCE(SUM(end_cursor - start_cursor), 0) AS covered_bytes,
					COUNT(*) FILTER (WHERE start_cursor <> COALESCE(previous_end, 0)) AS broken_boundaries,
					COUNT(*) FILTER (
						WHERE object_status <> 'available' AND NOT EXISTS (
							SELECT 1 FROM session_metering_envelope_chunks envelope_chunk
							JOIN session_metering_envelope_manifests manifest
								ON manifest.id = envelope_chunk.manifest_id
							WHERE envelope_chunk.chunk_id = ordered.id
								AND envelope_chunk.generation_id = generation.id
								AND envelope_chunk.source_start_cursor = ordered.start_cursor
								AND envelope_chunk.source_end_cursor = ordered.end_cursor
								AND manifest.status = 'validated'
								AND manifest.envelope_version = $3
						)
					) AS unavailable_inputs
				FROM ordered
			) coverage ON true
			WHERE generation.status = 'active' AND generation.expected_cursor > 0
				AND session.content_status <> 'deleted'
		), target AS (
			SELECT eligible.*, revision.id AS revision_id, revision.status AS revision_status,
				state.active_revision_id
			FROM eligible
			LEFT JOIN session_metrics_revisions revision
				ON revision.generation_id = eligible.generation_id
				AND revision.parser_version = $1 AND revision.normalizer_version = $2
			LEFT JOIN session_source_metrics_states state ON state.source_id = eligible.source_id
		)
		SELECT COUNT(*), COUNT(*) FILTER (WHERE NOT safe),
			COUNT(*) FILTER (WHERE revision_id IS NULL),
			COUNT(*) FILTER (WHERE revision_status IN ('building', 'validated')),
			COUNT(*) FILTER (WHERE revision_status = 'active' AND active_revision_id = revision_id),
			COUNT(*) FILTER (WHERE revision_status = 'failed')
		FROM target`, ParserVersion, normalizerVersion, MeteringEnvelopeVersion).Scan(
		&status.EligibleSources, &status.UnsafeSources, &status.MissingRevisions, &status.BuildingRevisions,
		&status.ActiveRevisions, &status.FailedRevisions)
	if err != nil {
		return status, err
	}
	if err := database.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE job.status IN ('pending', 'leased', 'retry_wait')),
			COUNT(*) FILTER (WHERE job.status = 'dead')
		FROM session_processing_jobs job
		JOIN session_metrics_revisions revision ON revision.id = job.target_metrics_revision_id
		WHERE revision.parser_version = $1 AND revision.normalizer_version = $2`,
		ParserVersion, normalizerVersion).Scan(&status.PendingJobs, &status.DeadJobs); err != nil {
		return status, err
	}
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM session_usage_contributions contribution
		JOIN session_metrics_revisions revision ON revision.id = contribution.revision_id
		JOIN session_source_metrics_states state
			ON state.source_id = revision.source_id AND state.active_revision_id = revision.id
		WHERE revision.parser_version = $1 AND revision.normalizer_version = $2
			AND revision.status = 'active'`, ParserVersion, normalizerVersion).Scan(
		&status.ActiveContributions); err != nil {
		return status, err
	}
	if err := database.QueryRowContext(ctx, `
		WITH active_sessions AS (
			SELECT DISTINCT source.session_id
			FROM session_metrics_revisions revision
			JOIN session_source_metrics_states state
				ON state.source_id = revision.source_id AND state.active_revision_id = revision.id
			JOIN session_sources source ON source.id = revision.source_id
			WHERE revision.parser_version = $1 AND revision.normalizer_version = $2
				AND revision.status = 'active'
		)
		SELECT COUNT(*)
		FROM active_sessions active
		WHERE NOT EXISTS (
			SELECT 1 FROM session_family_memberships membership
			JOIN session_family_versions family ON family.id = membership.family_version_id
			WHERE membership.member_session_id = active.session_id
				AND membership.valid_to IS NULL AND family.status = 'active'
		)`, ParserVersion, normalizerVersion).Scan(&status.MissingFamilyMemberships); err != nil {
		return status, err
	}
	failures, err := countContributionReconciliationFailures(ctx, database, normalizerVersion)
	if err != nil {
		return status, err
	}
	status.ReconciliationFailures = failures
	return status, nil
}

func ContributionBackfillForegroundBusy(ctx context.Context, database *sql.DB) (bool, error) {
	if database == nil {
		return false, errors.New("database is required")
	}
	var busy bool
	err := database.QueryRowContext(ctx, `
		SELECT
			EXISTS (
				SELECT 1 FROM pg_stat_activity activity
				WHERE activity.datname = current_database()
					AND activity.pid <> pg_backend_pid()
					AND (
						activity.wait_event_type = 'Lock'
						OR (activity.state = 'active'
							AND activity.query_start < clock_timestamp() - interval '1 second')
					)
			)
			OR (
				SELECT COUNT(*)
				FROM session_processing_jobs
				WHERE status IN ('pending', 'leased', 'retry_wait')
			) >= $1`, contributionBackfillMaxOutstandingJobs).Scan(&busy)
	return busy, err
}

func EnqueueContributionBackfillBatch(
	ctx context.Context,
	database *sql.DB,
	normalizerVersion string,
	batchSize int,
) (int64, error) {
	if database == nil || normalizerVersion == "" || batchSize <= 0 {
		return 0, errors.New("database, normalizer version, and positive batch size are required")
	}
	var enqueued int64
	for enqueued < int64(batchSize) {
		added, err := enqueueOneContributionBackfill(ctx, database, normalizerVersion, "")
		if err != nil {
			return enqueued, err
		}
		if !added {
			break
		}
		enqueued++
	}
	return enqueued, nil
}

func enqueueOneContributionBackfill(
	ctx context.Context,
	database *sql.DB,
	normalizerVersion string,
	sourceFilter string,
) (bool, error) {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout = '250ms'`); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout = '10s'`); err != nil {
		return false, err
	}
	type candidate struct {
		SessionID, SourceID, GenerationID, RevisionID string
		ExpectedCursor                                int64
	}
	var target candidate
	err = tx.QueryRowContext(ctx, `
		SELECT session.id::text, source.id::text, generation.id::text, generation.expected_cursor,
			COALESCE(target_revision.id::text, '')
		FROM session_sources source
		JOIN sessions session ON session.id = source.session_id
		JOIN session_source_generations generation ON generation.id = source.active_generation_id
		LEFT JOIN session_metrics_revisions target_revision
			ON target_revision.generation_id = generation.id
			AND target_revision.parser_version = $1
			AND target_revision.normalizer_version = $2
		JOIN LATERAL (
			WITH ordered AS (
				SELECT chunk.id, chunk.start_cursor, chunk.end_cursor, chunk.object_status,
					LAG(chunk.end_cursor) OVER (ORDER BY chunk.start_cursor, chunk.id) AS previous_end
				FROM session_upload_chunks chunk
				WHERE chunk.generation_id = generation.id
			)
			SELECT COUNT(*) AS chunk_count, COALESCE(MIN(start_cursor), 0) AS min_start,
				COALESCE(MAX(end_cursor), 0) AS max_end,
				COALESCE(SUM(end_cursor - start_cursor), 0) AS covered_bytes,
				COUNT(*) FILTER (WHERE start_cursor <> COALESCE(previous_end, 0)) AS broken_boundaries,
				COUNT(*) FILTER (
					WHERE object_status <> 'available' AND NOT EXISTS (
						SELECT 1 FROM session_metering_envelope_chunks envelope_chunk
						JOIN session_metering_envelope_manifests manifest
							ON manifest.id = envelope_chunk.manifest_id
						WHERE envelope_chunk.chunk_id = ordered.id
							AND envelope_chunk.generation_id = generation.id
							AND envelope_chunk.source_start_cursor = ordered.start_cursor
							AND envelope_chunk.source_end_cursor = ordered.end_cursor
							AND manifest.status = 'validated'
							AND manifest.envelope_version = $4
					)
				) AS unavailable_inputs
			FROM ordered
		) coverage ON coverage.chunk_count > 0 AND coverage.min_start = 0
			AND coverage.max_end = generation.expected_cursor
			AND coverage.covered_bytes = generation.expected_cursor
			AND coverage.broken_boundaries = 0 AND coverage.unavailable_inputs = 0
		WHERE generation.status = 'active' AND generation.expected_cursor > 0
			AND session.content_status <> 'deleted'
			AND (
				target_revision.id IS NULL OR (
					target_revision.status IN ('building', 'validated')
					AND EXISTS (
						SELECT 1 FROM session_processing_jobs failed_job
						WHERE failed_job.target_metrics_revision_id = target_revision.id
							AND failed_job.job_type IN ('parse_usage_chunk', 'rebuild_metrics_revision')
							AND failed_job.status = 'dead'
					)
				)
			)
			AND ($3::text = '' OR source.id = $3::uuid)
		ORDER BY (target_revision.id IS NULL), session.user_id, session.id, source.id
		FOR UPDATE OF source, generation SKIP LOCKED
		LIMIT 1`, ParserVersion, normalizerVersion, sourceFilter, MeteringEnvelopeVersion).Scan(
		&target.SessionID, &target.SourceID, &target.GenerationID, &target.ExpectedCursor,
		&target.RevisionID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	var chunkCount, minStart, maxEnd, coveredBytes, brokenBoundaries, unavailableInputs int64
	if err := tx.QueryRowContext(ctx, `
		WITH ordered AS (
			SELECT chunk.id, chunk.start_cursor, chunk.end_cursor, chunk.object_status,
				LAG(chunk.end_cursor) OVER (ORDER BY chunk.start_cursor, chunk.id) AS previous_end
			FROM session_upload_chunks chunk
			WHERE chunk.generation_id = $1
		)
		SELECT COUNT(*), COALESCE(MIN(start_cursor), 0), COALESCE(MAX(end_cursor), 0),
			COALESCE(SUM(end_cursor - start_cursor), 0),
			COUNT(*) FILTER (WHERE start_cursor <> COALESCE(previous_end, 0)),
			COUNT(*) FILTER (
				WHERE object_status <> 'available' AND NOT EXISTS (
					SELECT 1
					FROM session_metering_envelope_chunks envelope_chunk
					JOIN session_metering_envelope_manifests manifest
						ON manifest.id = envelope_chunk.manifest_id
					WHERE envelope_chunk.chunk_id = ordered.id
						AND envelope_chunk.generation_id = $1
						AND envelope_chunk.source_start_cursor = ordered.start_cursor
						AND envelope_chunk.source_end_cursor = ordered.end_cursor
						AND manifest.status = 'validated'
						AND manifest.envelope_version = $2
				)
			)
		FROM ordered`, target.GenerationID, MeteringEnvelopeVersion).Scan(
		&chunkCount, &minStart, &maxEnd, &coveredBytes, &brokenBoundaries, &unavailableInputs); err != nil {
		return false, err
	}
	if chunkCount == 0 || minStart != 0 || maxEnd != target.ExpectedCursor ||
		coveredBytes != target.ExpectedCursor || brokenBoundaries != 0 || unavailableInputs != 0 {
		return false, fmt.Errorf(
			"generation %s is not safe to backfill: chunks=%d range=%d-%d bytes=%d expected=%d broken=%d unavailable=%d",
			target.GenerationID, chunkCount, minStart, maxEnd, coveredBytes,
			target.ExpectedCursor, brokenBoundaries, unavailableInputs,
		)
	}
	if target.RevisionID != "" {
		result, err := tx.ExecContext(ctx, `
			UPDATE session_processing_jobs
			SET status = 'pending', attempts = 0,
				lease_owner = NULL, lease_until = NULL, heartbeat_at = NULL,
				next_retry_at = NULL, last_error = NULL, completed_at = NULL
			WHERE target_metrics_revision_id = $1
				AND job_type IN ('parse_usage_chunk', 'rebuild_metrics_revision')
				AND status = 'dead'`, target.RevisionID)
		if err != nil {
			return false, err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return false, err
		}
		if updated == 0 {
			return false, nil
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	}

	var revisionID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO session_metrics_revisions (
			source_id, generation_id, parser_version, normalizer_version,
			status, build_start_cursor, source_high_water_cursor, calculation_reason
		) VALUES ($1, $2, $3, $4, 'building', 0, $5, 'token contribution backfill')
		RETURNING id`, target.SourceID, target.GenerationID, ParserVersion,
		normalizerVersion, target.ExpectedCursor).Scan(&revisionID); err != nil {
		return false, err
	}
	base := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_processing_jobs (
			job_type, session_id, generation_id, chunk_id,
			target_metrics_revision_id, payload, created_at
		)
		SELECT $1, $2, chunk.generation_id, chunk.id, $3,
			jsonb_build_object('reason', 'token_contribution_backfill'),
			$4::timestamptz + ROW_NUMBER() OVER (ORDER BY chunk.start_cursor, chunk.id) * interval '1 microsecond'
		FROM session_upload_chunks chunk
		WHERE chunk.generation_id = $5
		ORDER BY chunk.start_cursor, chunk.id`, "parse_usage_chunk", target.SessionID,
		revisionID, base, target.GenerationID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_processing_jobs (
			job_type, session_id, generation_id, target_metrics_revision_id,
			payload, created_at
		) VALUES ($1, $2, $3, $4,
			jsonb_build_object('reason', 'token_contribution_backfill'),
			$5::timestamptz + $6 * interval '1 microsecond')`,
		"rebuild_metrics_revision", target.SessionID, target.GenerationID,
		revisionID, base, chunkCount+1); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func countContributionReconciliationFailures(
	ctx context.Context,
	database *sql.DB,
	normalizerVersion string,
) (int64, error) {
	var revisionFailures, rollupFailures, missingCosts, missingRollups int64
	if err := database.QueryRowContext(ctx, `
		WITH active_revisions AS (
			SELECT revision.id
			FROM session_metrics_revisions revision
			JOIN session_source_metrics_states state
				ON state.source_id = revision.source_id AND state.active_revision_id = revision.id
			WHERE revision.status = 'active' AND revision.parser_version = $1
				AND revision.normalizer_version = $2
		), component AS (
			SELECT revision.id,
				COALESCE(SUM(item.uncached_input_tokens), 0) AS uncached,
				COALESCE(SUM(item.cache_read_tokens), 0) AS cache_read,
				COALESCE(SUM(item.cache_write_5m_tokens), 0) AS cache_5m,
				COALESCE(SUM(item.cache_write_1h_tokens), 0) AS cache_1h,
				COALESCE(SUM(item.output_tokens), 0) AS output,
				COALESCE(SUM(item.normalized_total_tokens), 0) AS total
			FROM active_revisions revision
			LEFT JOIN session_usage_components item ON item.revision_id = revision.id AND item.valid_to IS NULL
			GROUP BY revision.id
		), contribution AS (
			SELECT revision.id,
				COALESCE(SUM(item.uncached_input_tokens), 0) AS uncached,
				COALESCE(SUM(item.cache_read_tokens), 0) AS cache_read,
				COALESCE(SUM(item.cache_write_5m_tokens), 0) AS cache_5m,
				COALESCE(SUM(item.cache_write_1h_tokens), 0) AS cache_1h,
				COALESCE(SUM(item.output_tokens), 0) AS output,
				COALESCE(SUM(item.total_tokens), 0) AS total
			FROM active_revisions revision
			LEFT JOIN session_usage_contributions item ON item.revision_id = revision.id
			GROUP BY revision.id
		)
		SELECT COUNT(*)
		FROM component
		JOIN contribution USING (id)
		WHERE ROW(component.uncached, component.cache_read, component.cache_5m,
			component.cache_1h, component.output, component.total)
			IS DISTINCT FROM
			ROW(contribution.uncached, contribution.cache_read, contribution.cache_5m,
				contribution.cache_1h, contribution.output, contribution.total)`,
		ParserVersion, normalizerVersion).Scan(&revisionFailures); err != nil {
		return 0, err
	}
	if err := database.QueryRowContext(ctx, `
		WITH active_rollups AS (
			SELECT id, contribution_count
			FROM session_family_rollup_versions WHERE status = 'active'
		), contribution AS (
			SELECT rollup.id AS rollup_version_id,
				COALESCE(SUM(item.uncached_input_tokens), 0) AS uncached,
				COALESCE(SUM(item.cache_read_tokens), 0) AS cache_read,
				COALESCE(SUM(item.cache_write_5m_tokens), 0) AS cache_5m,
				COALESCE(SUM(item.cache_write_1h_tokens), 0) AS cache_1h,
				COALESCE(SUM(item.output_tokens), 0) AS output,
				COALESCE(SUM(item.total_tokens), 0) AS tokens,
				COUNT(item.id) AS contribution_count
			FROM active_rollups rollup
			LEFT JOIN session_family_rollup_revision_refs revision_ref
				ON revision_ref.rollup_version_id = rollup.id
			LEFT JOIN session_usage_contributions item
				ON item.revision_id = revision_ref.revision_id
			GROUP BY rollup.id
		), total AS (
			SELECT rollup_version_id,
				COALESCE(SUM(uncached_input_tokens), 0) AS uncached,
				COALESCE(SUM(cache_read_tokens), 0) AS cache_read,
				COALESCE(SUM(cache_write_5m_tokens), 0) AS cache_5m,
				COALESCE(SUM(cache_write_1h_tokens), 0) AS cache_1h,
				COALESCE(SUM(output_tokens), 0) AS output,
				COALESCE(SUM(total_tokens), 0) AS tokens,
				COALESCE(SUM(self_total_tokens), 0) AS self_tokens,
				COALESCE(SUM(subagent_total_tokens), 0) AS subagent_tokens,
				COALESCE(SUM(contribution_count), 0) AS contribution_count
			FROM session_family_token_totals GROUP BY rollup_version_id
		), daily AS (
			SELECT rollup_version_id,
				COALESCE(SUM(uncached_input_tokens), 0) AS uncached,
				COALESCE(SUM(cache_read_tokens), 0) AS cache_read,
				COALESCE(SUM(cache_write_5m_tokens), 0) AS cache_5m,
				COALESCE(SUM(cache_write_1h_tokens), 0) AS cache_1h,
				COALESCE(SUM(output_tokens), 0) AS output,
				COALESCE(SUM(total_tokens), 0) AS tokens,
				COALESCE(SUM(contribution_count), 0) AS contribution_count
			FROM session_family_daily_usage GROUP BY rollup_version_id
		), chunk AS (
			SELECT rollup_version_id,
				COALESCE(SUM(uncached_input_tokens), 0) AS uncached,
				COALESCE(SUM(cache_read_tokens), 0) AS cache_read,
				COALESCE(SUM(cache_write_5m_tokens), 0) AS cache_5m,
				COALESCE(SUM(cache_write_1h_tokens), 0) AS cache_1h,
				COALESCE(SUM(output_tokens), 0) AS output,
				COALESCE(SUM(total_tokens), 0) AS tokens,
				COALESCE(SUM(contribution_count), 0) AS contribution_count
			FROM session_chunk_usage GROUP BY rollup_version_id
		)
		SELECT COUNT(*)
		FROM active_rollups version
		JOIN contribution ON contribution.rollup_version_id = version.id
		LEFT JOIN total ON total.rollup_version_id = version.id
		LEFT JOIN daily ON daily.rollup_version_id = version.id
		LEFT JOIN chunk ON chunk.rollup_version_id = version.id
		WHERE
			ROW(contribution.uncached, contribution.cache_read, contribution.cache_5m,
				contribution.cache_1h, contribution.output, contribution.tokens)
			IS DISTINCT FROM
			ROW(COALESCE(total.uncached, 0), COALESCE(total.cache_read, 0), COALESCE(total.cache_5m, 0),
				COALESCE(total.cache_1h, 0), COALESCE(total.output, 0), COALESCE(total.tokens, 0))
			OR
			ROW(COALESCE(total.uncached, 0), COALESCE(total.cache_read, 0), COALESCE(total.cache_5m, 0),
				COALESCE(total.cache_1h, 0), COALESCE(total.output, 0), COALESCE(total.tokens, 0))
			IS DISTINCT FROM
			ROW(COALESCE(daily.uncached, 0), COALESCE(daily.cache_read, 0), COALESCE(daily.cache_5m, 0),
				COALESCE(daily.cache_1h, 0), COALESCE(daily.output, 0), COALESCE(daily.tokens, 0))
			OR ROW(COALESCE(total.uncached, 0), COALESCE(total.cache_read, 0), COALESCE(total.cache_5m, 0),
				COALESCE(total.cache_1h, 0), COALESCE(total.output, 0), COALESCE(total.tokens, 0))
			IS DISTINCT FROM
			ROW(COALESCE(chunk.uncached, 0), COALESCE(chunk.cache_read, 0), COALESCE(chunk.cache_5m, 0),
				COALESCE(chunk.cache_1h, 0), COALESCE(chunk.output, 0), COALESCE(chunk.tokens, 0))
			OR COALESCE(total.self_tokens, 0) + COALESCE(total.subagent_tokens, 0) <> COALESCE(total.tokens, 0)
			OR contribution.contribution_count <> version.contribution_count
			OR contribution.contribution_count <> COALESCE(total.contribution_count, 0)
			OR contribution.contribution_count <> COALESCE(daily.contribution_count, 0)
			OR contribution.contribution_count <> COALESCE(chunk.contribution_count, 0)`).Scan(&rollupFailures); err != nil {
		return 0, err
	}
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM session_usage_contributions contribution
		JOIN session_metrics_revisions revision ON revision.id = contribution.revision_id
		JOIN session_source_metrics_states state
			ON state.source_id = revision.source_id AND state.active_revision_id = revision.id
		WHERE revision.parser_version = $1 AND revision.normalizer_version = $2
			AND NOT EXISTS (
				SELECT 1 FROM session_usage_contribution_costs cost
				WHERE cost.contribution_id = contribution.id
					AND cost.calculator_version = 'aida-cost-v1' AND cost.superseded_at IS NULL
			)`, ParserVersion, normalizerVersion).Scan(&missingCosts); err != nil {
		return 0, err
	}
	if err := database.QueryRowContext(ctx, `
		WITH active_families AS (
			SELECT DISTINCT family.id
			FROM session_family_versions family
			JOIN session_family_memberships membership ON membership.family_version_id = family.id
			JOIN session_sources source ON source.session_id = membership.member_session_id
			JOIN session_source_metrics_states state ON state.source_id = source.id
			JOIN session_metrics_revisions revision ON revision.id = state.active_revision_id
			WHERE family.status = 'active' AND membership.valid_to IS NULL
				AND revision.parser_version = $1 AND revision.normalizer_version = $2
		)
		SELECT COUNT(*) FROM active_families family
		WHERE NOT EXISTS (
			SELECT 1 FROM session_family_rollup_versions rollup
			WHERE rollup.family_version_id = family.id AND rollup.status = 'active'
		)`, ParserVersion, normalizerVersion).Scan(&missingRollups); err != nil {
		return 0, err
	}
	return revisionFailures + rollupFailures + missingCosts + missingRollups, nil
}
