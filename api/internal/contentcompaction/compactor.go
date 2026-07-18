package contentcompaction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Compactor struct {
	db *sql.DB
}

func New(database *sql.DB) (*Compactor, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &Compactor{db: database}, nil
}

func (c *Compactor) Run(ctx context.Context, options Options) (Report, error) {
	if err := options.validate(); err != nil {
		return Report{}, err
	}
	if options.LockTimeout == 0 {
		options.LockTimeout = 5 * time.Second
	}
	processedRows := int64(0)
	processedBatches := 0
	switch options.Action {
	case ActionPlan:
	case ActionCopy:
		rows, batches, err := c.copy(ctx, options)
		if err != nil {
			return Report{}, err
		}
		processedRows, processedBatches = rows, batches
	case ActionMirror:
		if err := c.startMirror(ctx, options.LockTimeout); err != nil {
			return Report{}, err
		}
	case ActionReconcile:
		rows, batches, err := c.reconcile(ctx, options)
		if err != nil {
			return Report{}, err
		}
		processedRows, processedBatches = rows, batches
	case ActionVerify:
	case ActionCutover:
		if err := c.cutover(ctx, options); err != nil {
			return Report{}, err
		}
	case ActionRollback:
		if err := c.rollback(ctx, options); err != nil {
			return Report{}, err
		}
	case ActionFinalize:
		if err := c.finalize(ctx, options); err != nil {
			return Report{}, err
		}
	}

	state, err := c.loadState(ctx, c.db)
	if err != nil {
		return Report{}, err
	}
	source, target := tablesForPhase(state.Phase)
	report, err := c.basicReport(ctx, options.Action, options.Apply, state, source, target)
	if err != nil {
		return Report{}, err
	}
	report.ProcessedRows = processedRows
	report.ProcessedBatches = processedBatches
	report.ExpectedSourceRows = options.ExpectedSourceRows
	if options.Action == ActionVerify {
		if err := c.populateDeepVerification(ctx, c.db, &report, source, target); err != nil {
			return Report{}, err
		}
		report.Complete = verificationComplete(state.Phase, report)
	} else {
		report.Complete = actionComplete(options.Action, state.Phase)
	}
	return report, nil
}

type stateQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (c *Compactor) loadState(ctx context.Context, queryer stateQueryer) (migrationState, error) {
	var state migrationState
	err := queryer.QueryRowContext(ctx, `
		SELECT phase, COALESCE(copy_cursor::text, ''),
			COALESCE(reconcile_missing_cursor::text, ''),
			COALESCE(reconcile_extra_cursor::text, ''),
			reconcile_missing_complete, reconcile_extra_complete,
			source_rows_at_start, copied_rows,
			reconciled_missing_rows, reconciled_extra_rows,
			source_table, shadow_table, archive_table
		FROM session_content_events_compaction_state WHERE id = 1`).Scan(
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

func tablesForPhase(phase string) (string, string) {
	switch phase {
	case "swapped":
		return ArchiveTable, SourceTable
	case "finalized":
		return SourceTable, SourceTable
	default:
		return SourceTable, ShadowTable
	}
}

func (c *Compactor) basicReport(
	ctx context.Context,
	action Action,
	applied bool,
	state migrationState,
	source, target string,
) (Report, error) {
	report := Report{
		Action: action, Applied: applied, Phase: state.Phase,
		SourceTable: source, TargetTable: target,
		CopyCursor: state.CopyCursor, MissingCursor: state.MissingCursor,
		ExtraCursor: state.ExtraCursor,
	}
	var err error
	report.RowCountsExact = action == ActionVerify
	if report.RowCountsExact {
		report.SourceRows, err = c.tableRows(ctx, c.db, source)
		if err != nil {
			return Report{}, err
		}
		report.TargetRows, err = c.tableRows(ctx, c.db, target)
		if err != nil {
			return Report{}, err
		}
	} else {
		report.SourceRows, err = c.estimatedTableRows(ctx, c.db, source)
		if err != nil {
			return Report{}, err
		}
		report.TargetRows, err = c.estimatedTableRows(ctx, c.db, target)
		if err != nil {
			return Report{}, err
		}
		if action == ActionCopy {
			report.SourceRows = state.SourceRowsAtStart
			report.TargetRows = state.CopiedRows
		}
	}
	report.SourceBytes, err = c.relationBytes(ctx, source)
	if err != nil {
		return Report{}, err
	}
	report.TargetBytes, err = c.relationBytes(ctx, target)
	if err != nil {
		return Report{}, err
	}
	report.SourceHasPayload, err = c.tableHasColumn(ctx, source, "content_payload")
	if err != nil {
		return Report{}, err
	}
	report.TargetHasPayload, err = c.tableHasColumn(ctx, target, "content_payload")
	if err != nil {
		return Report{}, err
	}
	return report, nil
}

func (c *Compactor) estimatedTableRows(
	ctx context.Context,
	queryer stateQueryer,
	table string,
) (int64, error) {
	if _, err := checkedTable(table); err != nil {
		return 0, err
	}
	var count int64
	err := queryer.QueryRowContext(ctx, `
		SELECT GREATEST(COALESCE(relation.reltuples, 0)::bigint, 0)
		FROM pg_class relation
		JOIN pg_namespace namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = 'public' AND relation.relname = $1`, table).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return count, err
}

func (c *Compactor) tableRows(ctx context.Context, queryer stateQueryer, table string) (int64, error) {
	identifier, err := checkedTable(table)
	if err != nil {
		return 0, err
	}
	exists, err := c.tableExists(ctx, queryer, table)
	if err != nil || !exists {
		return 0, err
	}
	var count int64
	err = queryer.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+identifier).Scan(&count)
	return count, err
}

func (c *Compactor) tableExists(ctx context.Context, queryer stateQueryer, table string) (bool, error) {
	if _, err := checkedTable(table); err != nil {
		return false, err
	}
	var exists bool
	err := queryer.QueryRowContext(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists)
	return exists, err
}

func (c *Compactor) relationBytes(ctx context.Context, table string) (int64, error) {
	if _, err := checkedTable(table); err != nil {
		return 0, err
	}
	var size int64
	err := c.db.QueryRowContext(ctx, `
		SELECT COALESCE(pg_total_relation_size(to_regclass('public.' || $1)), 0)`, table).Scan(&size)
	return size, err
}

func (c *Compactor) tableHasColumn(ctx context.Context, table, column string) (bool, error) {
	if _, err := checkedTable(table); err != nil {
		return false, err
	}
	var exists bool
	err := c.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		)`, table, column).Scan(&exists)
	return exists, err
}

func (c *Compactor) populateDeepVerification(
	ctx context.Context,
	queryer stateQueryer,
	report *Report,
	source, target string,
) error {
	if source == target {
		return nil
	}
	sourceSQL, err := checkedTable(source)
	if err != nil {
		return err
	}
	targetSQL, err := checkedTable(target)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`
		SELECT
			COUNT(*) FILTER (WHERE target.id IS NULL),
			COUNT(*) FILTER (WHERE source.id IS NULL),
			COUNT(*) FILTER (
				WHERE source.id IS NOT NULL AND target.id IS NOT NULL AND ROW(
					source.content_projection_revision_id, source.chunk_id,
					source.source_start_cursor, source.source_end_cursor,
					source.occurred_at, source.event_type, source.summary,
					source.excerpt, source.content_sha256, source.created_at
				) IS DISTINCT FROM ROW(
					target.content_projection_revision_id, target.chunk_id,
					target.source_start_cursor, target.source_end_cursor,
					target.occurred_at, target.event_type, target.summary,
					target.excerpt, target.content_sha256, target.created_at
				)
			)
		FROM %[1]s source
		FULL OUTER JOIN %[2]s target ON target.id = source.id`, sourceSQL, targetSQL)
	return queryer.QueryRowContext(ctx, query).Scan(
		&report.MissingRows, &report.ExtraRows, &report.MismatchedRows,
	)
}

func verificationComplete(phase string, report Report) bool {
	if phase == "finalized" {
		return !report.SourceHasPayload
	}
	if phase == "swapped" {
		return report.MissingRows == 0 && report.MismatchedRows == 0 && !report.TargetHasPayload
	}
	return report.MissingRows == 0 && report.ExtraRows == 0 && report.MismatchedRows == 0 &&
		!report.TargetHasPayload
}

func actionComplete(action Action, phase string) bool {
	switch action {
	case ActionPlan:
		return false
	case ActionCopy:
		return phase == "copied" || phase == "mirroring" || phase == "reconciling" ||
			phase == "reconciled" || phase == "swapped" || phase == "finalized"
	case ActionMirror:
		return phase == "mirroring" || phase == "reconciling" || phase == "reconciled"
	case ActionReconcile:
		return phase == "reconciled"
	case ActionCutover:
		return phase == "swapped"
	case ActionRollback:
		return phase == "rolled_back"
	case ActionFinalize:
		return phase == "finalized"
	default:
		return false
	}
}
