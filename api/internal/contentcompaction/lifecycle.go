package contentcompaction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (c *Compactor) startMirror(ctx context.Context, lockTimeout time.Duration) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setLocalLockTimeout(ctx, tx, lockTimeout); err != nil {
		return err
	}
	state, err := c.lockState(ctx, tx)
	if err != nil {
		return err
	}
	switch state.Phase {
	case "mirroring", "reconciling", "reconciled":
		return tx.Commit()
	case "copied", "rolled_back":
	default:
		return fmt.Errorf("mirror is not allowed in phase %s", state.Phase)
	}
	if err := c.installMirrorTx(ctx, tx, SourceTable, ShadowTable); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_content_events_compaction_state
		SET phase = 'mirroring', mirror_started_at = now(),
			reconcile_missing_cursor = NULL, reconcile_extra_cursor = NULL,
			reconcile_missing_complete = false, reconcile_extra_complete = false,
			reconciled_at = NULL, updated_at = now()
		WHERE id = 1`); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Compactor) cutover(ctx context.Context, options Options) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setLocalLockTimeout(ctx, tx, options.LockTimeout); err != nil {
		return err
	}
	state, err := c.lockState(ctx, tx)
	if err != nil {
		return err
	}
	if state.Phase == "swapped" {
		return tx.Commit()
	}
	if state.Phase != "reconciled" {
		return fmt.Errorf("cutover requires reconciled phase, current phase is %s", state.Phase)
	}
	archiveExists, err := c.tableExists(ctx, tx, ArchiveTable)
	if err != nil {
		return err
	}
	if archiveExists {
		return errors.New("archive table already exists; refuse to overwrite rollback data")
	}
	if _, err := tx.ExecContext(ctx, `
		LOCK TABLE "session_content_events", "session_content_events_compact"
		IN ACCESS EXCLUSIVE MODE`); err != nil {
		return fmt.Errorf("acquire cutover lock: %w", err)
	}
	triggerExists, err := c.mirrorTriggerExists(ctx, tx, SourceTable)
	if err != nil {
		return err
	}
	if !triggerExists {
		return errors.New("cutover requires the source mirror trigger")
	}
	sourceRows, err := c.tableRows(ctx, tx, SourceTable)
	if err != nil {
		return err
	}
	if sourceRows != options.ExpectedSourceRows {
		return fmt.Errorf("source rows changed: got %d, expected %d", sourceRows, options.ExpectedSourceRows)
	}
	report := Report{}
	if err := c.populateDeepVerification(ctx, tx, &report, SourceTable, ShadowTable); err != nil {
		return err
	}
	if report.MissingRows != 0 || report.ExtraRows != 0 || report.MismatchedRows != 0 {
		return fmt.Errorf("cutover verification failed: missing=%d extra=%d mismatch=%d",
			report.MissingRows, report.ExtraRows, report.MismatchedRows)
	}
	if _, err := tx.ExecContext(ctx, `
		ALTER TABLE "session_content_events" RENAME TO "session_content_events_payload_archive";
		ALTER TABLE "session_content_events_compact" RENAME TO "session_content_events";
	`); err != nil {
		return err
	}
	if err := c.replaceMirrorFunctionTx(ctx, tx, SourceTable); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_content_events_compaction_state
		SET phase = 'swapped', swapped_at = now(), rolled_back_at = NULL,
			updated_at = now() WHERE id = 1`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = c.db.ExecContext(ctx, `ANALYZE "session_content_events"`)
	return err
}

func (c *Compactor) rollback(ctx context.Context, options Options) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setLocalLockTimeout(ctx, tx, options.LockTimeout); err != nil {
		return err
	}
	state, err := c.lockState(ctx, tx)
	if err != nil {
		return err
	}
	if state.Phase == "rolled_back" {
		return tx.Commit()
	}
	if state.Phase != "swapped" {
		return fmt.Errorf("rollback requires swapped phase, current phase is %s", state.Phase)
	}
	if _, err := tx.ExecContext(ctx, `
		LOCK TABLE "session_content_events", "session_content_events_payload_archive"
		IN ACCESS EXCLUSIVE MODE`); err != nil {
		return fmt.Errorf("acquire rollback lock: %w", err)
	}
	currentRows, err := c.tableRows(ctx, tx, SourceTable)
	if err != nil {
		return err
	}
	if currentRows != options.ExpectedSourceRows {
		return fmt.Errorf("current rows changed: got %d, expected %d", currentRows, options.ExpectedSourceRows)
	}
	upsertQuery := fmt.Sprintf(`
		INSERT INTO %s AS existing (%s)
		SELECT %s FROM %s current
		ON CONFLICT (id) DO UPDATE SET %s
		WHERE ROW(
			existing.content_projection_revision_id, existing.chunk_id,
			existing.source_start_cursor, existing.source_end_cursor,
			existing.occurred_at, existing.event_type, existing.summary,
			existing.excerpt, existing.content_sha256, existing.created_at
		) IS DISTINCT FROM ROW(
			EXCLUDED.content_projection_revision_id, EXCLUDED.chunk_id,
			EXCLUDED.source_start_cursor, EXCLUDED.source_end_cursor,
			EXCLUDED.occurred_at, EXCLUDED.event_type, EXCLUDED.summary,
			EXCLUDED.excerpt, EXCLUDED.content_sha256, EXCLUDED.created_at
		)`, `"`+ArchiveTable+`"`, eventColumns, eventColumns,
		`"`+SourceTable+`"`, eventUpdateAssignments)
	upsertResult, err := tx.ExecContext(ctx, upsertQuery)
	if err != nil {
		return err
	}
	upserted, err := upsertResult.RowsAffected()
	if err != nil {
		return err
	}
	deleteResult, err := tx.ExecContext(ctx, `
		DELETE FROM "session_content_events_payload_archive" archive
		WHERE NOT EXISTS (
			SELECT 1 FROM "session_content_events" current WHERE current.id = archive.id
		)`)
	if err != nil {
		return err
	}
	deleted, err := deleteResult.RowsAffected()
	if err != nil {
		return err
	}
	report := Report{}
	if err := c.populateDeepVerification(ctx, tx, &report, SourceTable, ArchiveTable); err != nil {
		return err
	}
	if report.MissingRows != 0 || report.ExtraRows != 0 || report.MismatchedRows != 0 {
		return fmt.Errorf("rollback synchronization failed: missing=%d extra=%d mismatch=%d",
			report.MissingRows, report.ExtraRows, report.MismatchedRows)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_content_events_compaction_batches (operation, row_count)
		VALUES ('rollback_sync', $1)`, upserted+deleted); err != nil {
		return err
	}
	if err := c.dropMirrorTriggerTx(ctx, tx, ArchiveTable); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		ALTER TABLE "session_content_events" RENAME TO "session_content_events_compact";
		ALTER TABLE "session_content_events_payload_archive" RENAME TO "session_content_events";
	`); err != nil {
		return err
	}
	if err := c.installMirrorTx(ctx, tx, ShadowTable, SourceTable); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_content_events_compaction_state
		SET phase = 'rolled_back', rolled_back_at = now(),
			reconcile_missing_cursor = NULL, reconcile_extra_cursor = NULL,
			reconcile_missing_complete = false, reconcile_extra_complete = false,
			updated_at = now() WHERE id = 1`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = c.db.ExecContext(ctx, `ANALYZE "session_content_events"`)
	return err
}

func (c *Compactor) finalize(ctx context.Context, options Options) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setLocalLockTimeout(ctx, tx, options.LockTimeout); err != nil {
		return err
	}
	state, err := c.lockState(ctx, tx)
	if err != nil {
		return err
	}
	if state.Phase == "finalized" {
		return tx.Commit()
	}
	if state.Phase != "swapped" {
		return fmt.Errorf("finalize requires swapped phase, current phase is %s", state.Phase)
	}
	if _, err := tx.ExecContext(ctx, `
		LOCK TABLE "session_content_events", "session_content_events_payload_archive"
		IN ACCESS EXCLUSIVE MODE`); err != nil {
		return fmt.Errorf("acquire finalize lock: %w", err)
	}
	report := Report{}
	if err := c.populateDeepVerification(ctx, tx, &report, ArchiveTable, SourceTable); err != nil {
		return err
	}
	if report.MissingRows != 0 || report.MismatchedRows != 0 {
		return fmt.Errorf("archive is not fully represented in current table: missing=%d mismatch=%d",
			report.MissingRows, report.MismatchedRows)
	}
	if err := c.dropMirrorTriggerTx(ctx, tx, ArchiveTable); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DROP TABLE "session_content_events_payload_archive";
		DROP FUNCTION IF EXISTS "mirror_session_content_events_compaction"();
		ALTER TABLE "session_content_events"
			RENAME CONSTRAINT "session_content_events_compact_pkey"
			TO "session_content_events_pkey";
		ALTER TABLE "session_content_events"
			RENAME CONSTRAINT "session_content_events_compact_projection_fkey"
			TO "session_content_events_content_projection_revision_id_fkey";
		ALTER TABLE "session_content_events"
			RENAME CONSTRAINT "session_content_events_compact_chunk_fkey"
			TO "session_content_events_chunk_id_fkey";
		ALTER TABLE "session_content_events"
			RENAME CONSTRAINT "session_content_events_compact_cursor_check"
			TO "session_content_events_cursor_check";
		ALTER TABLE "session_content_events"
			RENAME CONSTRAINT "session_content_events_compact_type_not_empty"
			TO "session_content_events_type_not_empty";
		ALTER TABLE "session_content_events"
			RENAME CONSTRAINT "session_content_events_compact_hash_check"
			TO "session_content_events_hash_check";
		ALTER INDEX "idx_session_content_events_compact_source_range"
			RENAME TO "idx_session_content_events_source_range";
		ALTER INDEX "idx_session_content_events_compact_revision_cursor"
			RENAME TO "idx_session_content_events_revision_cursor";
		ALTER INDEX "idx_session_content_events_compact_occurred"
			RENAME TO "idx_session_content_events_occurred";
		UPDATE session_content_events_compaction_state
		SET phase = 'finalized', finalized_at = now(), updated_at = now()
		WHERE id = 1;
	`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = c.db.ExecContext(ctx, `ANALYZE "session_content_events"`)
	return err
}

func (c *Compactor) installMirrorTx(
	ctx context.Context,
	tx *sql.Tx,
	triggerTable, targetTable string,
) error {
	for _, table := range []string{SourceTable, ShadowTable, ArchiveTable} {
		exists, err := c.tableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if exists {
			if err := c.dropMirrorTriggerTx(ctx, tx, table); err != nil {
				return err
			}
		}
	}
	if err := c.replaceMirrorFunctionTx(ctx, tx, targetTable); err != nil {
		return err
	}
	triggerSQL, err := checkedTable(triggerTable)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`
		CREATE TRIGGER "%s"
		AFTER INSERT OR DELETE ON %s
		FOR EACH ROW EXECUTE FUNCTION "%s"()`,
		MirrorTrigger, triggerSQL, MirrorFunction))
	return err
}

func (c *Compactor) replaceMirrorFunctionTx(
	ctx context.Context,
	tx *sql.Tx,
	targetTable string,
) error {
	targetSQL, err := checkedTable(targetTable)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`
		CREATE OR REPLACE FUNCTION "%s"() RETURNS TRIGGER AS $mirror$
		BEGIN
			IF TG_OP = 'INSERT' THEN
				INSERT INTO %s (%s)
				VALUES (
					NEW.id, NEW.content_projection_revision_id, NEW.chunk_id,
					NEW.source_start_cursor, NEW.source_end_cursor, NEW.occurred_at,
					NEW.event_type, NEW.summary, NEW.excerpt, NEW.content_sha256, NEW.created_at
				)
				ON CONFLICT (id) DO UPDATE SET %s;
				RETURN NULL;
			ELSIF TG_OP = 'DELETE' THEN
				DELETE FROM %s WHERE id = OLD.id;
				RETURN NULL;
			END IF;
			RETURN NULL;
		END;
		$mirror$ LANGUAGE plpgsql`, MirrorFunction, targetSQL, eventColumns,
		eventUpdateAssignments, targetSQL)
	_, err = tx.ExecContext(ctx, query)
	return err
}

func (c *Compactor) dropMirrorTriggerTx(ctx context.Context, tx *sql.Tx, table string) error {
	tableSQL, err := checkedTable(table)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`DROP TRIGGER IF EXISTS "%s" ON %s`, MirrorTrigger, tableSQL))
	return err
}

func (c *Compactor) mirrorTriggerExists(
	ctx context.Context,
	queryer stateQueryer,
	table string,
) (bool, error) {
	if _, err := checkedTable(table); err != nil {
		return false, err
	}
	var exists bool
	err := queryer.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_trigger trigger
			JOIN pg_class relation ON relation.oid = trigger.tgrelid
			JOIN pg_namespace namespace ON namespace.oid = relation.relnamespace
			WHERE namespace.nspname = 'public' AND relation.relname = $1
				AND trigger.tgname = $2 AND NOT trigger.tgisinternal
		)`, table, MirrorTrigger).Scan(&exists)
	return exists, err
}

func setLocalLockTimeout(ctx context.Context, tx *sql.Tx, timeout time.Duration) error {
	_, err := tx.ExecContext(ctx, `SELECT set_config('lock_timeout', $1, true)`, timeout.String())
	return err
}
