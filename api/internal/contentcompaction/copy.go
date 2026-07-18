package contentcompaction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const eventColumns = `
	id, content_projection_revision_id, chunk_id,
	source_start_cursor, source_end_cursor, occurred_at,
	event_type, summary, excerpt, content_sha256, created_at`

const eventUpdateAssignments = `
	content_projection_revision_id = EXCLUDED.content_projection_revision_id,
	chunk_id = EXCLUDED.chunk_id,
	source_start_cursor = EXCLUDED.source_start_cursor,
	source_end_cursor = EXCLUDED.source_end_cursor,
	occurred_at = EXCLUDED.occurred_at,
	event_type = EXCLUDED.event_type,
	summary = EXCLUDED.summary,
	excerpt = EXCLUDED.excerpt,
	content_sha256 = EXCLUDED.content_sha256,
	created_at = EXCLUDED.created_at`

func qualifiedEventColumns(alias string) string {
	return fmt.Sprintf(`
		%s.id, %s.content_projection_revision_id, %s.chunk_id,
		%s.source_start_cursor, %s.source_end_cursor, %s.occurred_at,
		%s.event_type, %s.summary, %s.excerpt, %s.content_sha256, %s.created_at`,
		alias, alias, alias, alias, alias, alias, alias, alias, alias, alias, alias)
}

func eventFingerprint(alias string) string {
	return fmt.Sprintf(`md5(COALESCE(string_agg(ROW(
		%[1]s.id, %[1]s.content_projection_revision_id, %[1]s.chunk_id,
		%[1]s.source_start_cursor, %[1]s.source_end_cursor, %[1]s.occurred_at,
		%[1]s.event_type, %[1]s.summary, %[1]s.excerpt,
		%[1]s.content_sha256, %[1]s.created_at
	)::text, '' ORDER BY %[1]s.id), ''))`, alias)
}

func (c *Compactor) copy(ctx context.Context, options Options) (int64, int, error) {
	var processed int64
	batches := 0
	for batches < options.MaxBatches {
		rows, complete, err := c.copyBatch(ctx, options.BatchSize)
		if err != nil {
			return processed, batches, err
		}
		processed += rows
		batches++
		if complete {
			break
		}
	}
	return processed, batches, nil
}

func (c *Compactor) copyBatch(ctx context.Context, batchSize int) (int64, bool, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()

	state, err := c.lockState(ctx, tx)
	if err != nil {
		return 0, false, err
	}
	switch state.Phase {
	case "copied", "mirroring", "reconciling", "reconciled", "swapped", "finalized":
		return 0, true, tx.Commit()
	case "initialized", "copying":
	default:
		return 0, false, fmt.Errorf("copy is not allowed in phase %s", state.Phase)
	}
	if state.Phase == "initialized" {
		rows, err := c.tableRows(ctx, tx, SourceTable)
		if err != nil {
			return 0, false, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_content_events_compaction_state
			SET phase = 'copying', source_rows_at_start = $1, updated_at = now()
			WHERE id = 1`, rows); err != nil {
			return 0, false, err
		}
	}

	var cursor any
	if state.CopyCursor != "" {
		cursor = state.CopyCursor
	}
	query := fmt.Sprintf(`
		WITH batch AS MATERIALIZED (
			SELECT %s FROM %s source
			WHERE ($1::uuid IS NULL OR source.id > $1::uuid)
			ORDER BY source.id
			LIMIT $2
		), upserted AS (
			INSERT INTO %s (%s)
			SELECT %s FROM batch
			ON CONFLICT (id) DO UPDATE SET %s
			RETURNING %s
		)
		SELECT
			COALESCE((SELECT id::text FROM batch ORDER BY id DESC LIMIT 1), ''),
			(SELECT COUNT(*) FROM batch),
			(SELECT %s FROM batch source),
			(SELECT %s FROM upserted target)`,
		eventColumns, `"`+SourceTable+`"`, `"`+ShadowTable+`"`, eventColumns,
		eventColumns, eventUpdateAssignments, eventColumns,
		eventFingerprint("source"), eventFingerprint("target"))
	var endCursor, sourceFingerprint, targetFingerprint string
	var rowCount int64
	if err := tx.QueryRowContext(ctx, query, cursor, batchSize).Scan(
		&endCursor, &rowCount, &sourceFingerprint, &targetFingerprint,
	); err != nil {
		return 0, false, err
	}
	if sourceFingerprint != targetFingerprint {
		return 0, false, errors.New("copy batch source and target fingerprints differ")
	}
	if rowCount == 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_content_events_compaction_state
			SET phase = 'copied', copy_completed_at = now(), updated_at = now()
			WHERE id = 1`); err != nil {
			return 0, false, err
		}
		return 0, true, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_content_events_compaction_batches (
			operation, start_after, end_at, row_count, source_fingerprint, target_fingerprint
		) VALUES ('copy', $1::uuid, $2::uuid, $3, $4, $5)`,
		cursor, endCursor, rowCount, sourceFingerprint, targetFingerprint); err != nil {
		return 0, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_content_events_compaction_state
		SET phase = 'copying', copy_cursor = $1::uuid,
			copied_rows = copied_rows + $2, updated_at = now()
		WHERE id = 1`, endCursor, rowCount); err != nil {
		return 0, false, err
	}
	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	return rowCount, false, nil
}

func (c *Compactor) reconcile(ctx context.Context, options Options) (int64, int, error) {
	var processed int64
	batches := 0
	for batches < options.MaxBatches {
		rows, complete, err := c.reconcileBatch(ctx, options.BatchSize)
		if err != nil {
			return processed, batches, err
		}
		processed += rows
		batches++
		if complete {
			break
		}
	}
	return processed, batches, nil
}

func (c *Compactor) reconcileBatch(ctx context.Context, batchSize int) (int64, bool, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	state, err := c.lockState(ctx, tx)
	if err != nil {
		return 0, false, err
	}
	if state.Phase == "reconciled" {
		return 0, true, tx.Commit()
	}
	if state.Phase != "mirroring" && state.Phase != "reconciling" {
		return 0, false, fmt.Errorf("reconcile is not allowed in phase %s", state.Phase)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_content_events_compaction_state
		SET phase = 'reconciling', updated_at = now() WHERE id = 1`); err != nil {
		return 0, false, err
	}
	if !state.MissingComplete {
		rows, exhausted, err := c.reconcileMissingBatch(ctx, tx, state.MissingCursor, batchSize)
		if err != nil {
			return 0, false, err
		}
		if exhausted {
			if _, err := tx.ExecContext(ctx, `
				UPDATE session_content_events_compaction_state
				SET reconcile_missing_complete = true, updated_at = now() WHERE id = 1`); err != nil {
				return 0, false, err
			}
		}
		if err := tx.Commit(); err != nil {
			return 0, false, err
		}
		return rows, false, nil
	}
	if !state.ExtraComplete {
		rows, exhausted, err := c.reconcileExtraBatch(ctx, tx, state.ExtraCursor, batchSize)
		if err != nil {
			return 0, false, err
		}
		if exhausted {
			if _, err := tx.ExecContext(ctx, `
				UPDATE session_content_events_compaction_state
				SET reconcile_extra_complete = true, updated_at = now() WHERE id = 1`); err != nil {
				return 0, false, err
			}
		}
		if err := tx.Commit(); err != nil {
			return 0, false, err
		}
		return rows, false, nil
	}

	report := Report{}
	if err := c.populateDeepVerification(ctx, tx, &report, SourceTable, ShadowTable); err != nil {
		return 0, false, err
	}
	if report.MissingRows != 0 || report.ExtraRows != 0 || report.MismatchedRows != 0 {
		return 0, false, fmt.Errorf(
			"reconciliation is not exact: missing=%d extra=%d mismatch=%d",
			report.MissingRows, report.ExtraRows, report.MismatchedRows,
		)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_content_events_compaction_state
		SET phase = 'reconciled', reconciled_at = now(), updated_at = now()
		WHERE id = 1`); err != nil {
		return 0, false, err
	}
	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	return 0, true, nil
}

func (c *Compactor) reconcileMissingBatch(
	ctx context.Context,
	tx *sql.Tx,
	cursor string,
	batchSize int,
) (int64, bool, error) {
	var cursorArg any
	if cursor != "" {
		cursorArg = cursor
	}
	query := fmt.Sprintf(`
		WITH batch AS MATERIALIZED (
		SELECT %s FROM %s source
			LEFT JOIN %s target ON target.id = source.id
			WHERE target.id IS NULL AND ($1::uuid IS NULL OR source.id > $1::uuid)
			ORDER BY source.id LIMIT $2
		), upserted AS (
			INSERT INTO %s (%s)
			SELECT %s FROM batch
			ON CONFLICT (id) DO UPDATE SET %s
			RETURNING %s
		)
		SELECT
			COALESCE((SELECT id::text FROM batch ORDER BY id DESC LIMIT 1), ''),
			(SELECT COUNT(*) FROM batch),
			(SELECT %s FROM batch source),
			(SELECT %s FROM upserted target)`,
		qualifiedEventColumns("source"), `"`+SourceTable+`"`, `"`+ShadowTable+`"`,
		`"`+ShadowTable+`"`, eventColumns, eventColumns, eventUpdateAssignments, eventColumns,
		eventFingerprint("source"), eventFingerprint("target"))
	var endCursor, sourceFingerprint, targetFingerprint string
	var rows int64
	if err := tx.QueryRowContext(ctx, query, cursorArg, batchSize).Scan(
		&endCursor, &rows, &sourceFingerprint, &targetFingerprint,
	); err != nil {
		return 0, false, err
	}
	if sourceFingerprint != targetFingerprint {
		return 0, false, errors.New("missing-row reconciliation fingerprints differ")
	}
	if rows == 0 {
		return 0, true, nil
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_content_events_compaction_batches (
			operation, start_after, end_at, row_count, source_fingerprint, target_fingerprint
		) VALUES ('reconcile_missing', $1::uuid, $2::uuid, $3, $4, $5)`,
		cursorArg, endCursor, rows, sourceFingerprint, targetFingerprint); err != nil {
		return 0, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_content_events_compaction_state
		SET reconcile_missing_cursor = $1::uuid,
			reconciled_missing_rows = reconciled_missing_rows + $2,
			updated_at = now() WHERE id = 1`, endCursor, rows); err != nil {
		return 0, false, err
	}
	return rows, false, nil
}

func (c *Compactor) reconcileExtraBatch(
	ctx context.Context,
	tx *sql.Tx,
	cursor string,
	batchSize int,
) (int64, bool, error) {
	var cursorArg any
	if cursor != "" {
		cursorArg = cursor
	}
	query := fmt.Sprintf(`
		WITH batch AS MATERIALIZED (
		SELECT %s FROM %s target
			LEFT JOIN %s source ON source.id = target.id
			WHERE source.id IS NULL AND ($1::uuid IS NULL OR target.id > $1::uuid)
			ORDER BY target.id LIMIT $2
		), deleted AS (
			DELETE FROM %s target USING batch
			WHERE target.id = batch.id
			RETURNING %s
		)
		SELECT
			COALESCE((SELECT id::text FROM batch ORDER BY id DESC LIMIT 1), ''),
			(SELECT COUNT(*) FROM batch),
			(SELECT %s FROM batch source),
			(SELECT %s FROM deleted target)`,
		qualifiedEventColumns("target"), `"`+ShadowTable+`"`, `"`+SourceTable+`"`,
		`"`+ShadowTable+`"`, eventColumns,
		eventFingerprint("source"), eventFingerprint("target"))
	var endCursor, sourceFingerprint, targetFingerprint string
	var rows int64
	if err := tx.QueryRowContext(ctx, query, cursorArg, batchSize).Scan(
		&endCursor, &rows, &sourceFingerprint, &targetFingerprint,
	); err != nil {
		return 0, false, err
	}
	if sourceFingerprint != targetFingerprint {
		return 0, false, errors.New("extra-row reconciliation fingerprints differ")
	}
	if rows == 0 {
		return 0, true, nil
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_content_events_compaction_batches (
			operation, start_after, end_at, row_count, source_fingerprint, target_fingerprint
		) VALUES ('reconcile_extra', $1::uuid, $2::uuid, $3, $4, $5)`,
		cursorArg, endCursor, rows, sourceFingerprint, targetFingerprint); err != nil {
		return 0, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_content_events_compaction_state
		SET reconcile_extra_cursor = $1::uuid,
			reconciled_extra_rows = reconciled_extra_rows + $2,
			updated_at = now() WHERE id = 1`, endCursor, rows); err != nil {
		return 0, false, err
	}
	return rows, false, nil
}

func (c *Compactor) lockState(ctx context.Context, tx *sql.Tx) (migrationState, error) {
	var state migrationState
	err := tx.QueryRowContext(ctx, `
		SELECT phase, COALESCE(copy_cursor::text, ''),
			COALESCE(reconcile_missing_cursor::text, ''),
			COALESCE(reconcile_extra_cursor::text, ''),
			reconcile_missing_complete, reconcile_extra_complete,
			source_rows_at_start, copied_rows,
			reconciled_missing_rows, reconciled_extra_rows,
			source_table, shadow_table, archive_table
		FROM session_content_events_compaction_state WHERE id = 1
		FOR UPDATE`).Scan(
		&state.Phase, &state.CopyCursor, &state.MissingCursor, &state.ExtraCursor,
		&state.MissingComplete, &state.ExtraComplete,
		&state.SourceRowsAtStart, &state.CopiedRows,
		&state.ReconciledMissingRows, &state.ReconciledExtraRows,
		&state.SourceTable, &state.ShadowTable, &state.ArchiveTable,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return migrationState{}, errors.New("content event compaction state is missing; run migration 025")
	}
	if err != nil {
		return migrationState{}, err
	}
	if state.SourceTable != SourceTable || state.ShadowTable != ShadowTable || state.ArchiveTable != ArchiveTable {
		return migrationState{}, errors.New("content event compaction table identity was modified")
	}
	return state, nil
}
