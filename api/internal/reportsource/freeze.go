package reportsource

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/aidashboard/api/internal/observability"
	"github.com/aidashboard/api/internal/sessiondigest"
	"github.com/aidashboard/api/internal/sessiondigestv2"
)

func (s *Service) FreezeRunDigests(ctx context.Context, runID string) (string, error) {
	if s == nil || s.db == nil || runID == "" {
		return "", ErrInvalidRequest
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var selection Selection
	var periodStart, periodEnd time.Time
	var frozenAt sql.NullTime
	var stage, status string
	err = tx.QueryRowContext(ctx, `
		SELECT sel.id::text, sel.report_type, sel.period_start, sel.period_end,
			sel.selection_mode, sel.status, sel.content_snapshot_at,
			sel.required_read_mode, sel.digest_frozen_at,
			r.execution_stage, r.status
		FROM ai_runs r
		JOIN report_source_selections sel ON sel.attached_run_id = r.id
		WHERE r.id = $1 AND r.business_type = 'report_agent_run'
		FOR UPDATE OF r, sel`, runID,
	).Scan(
		&selection.ID, &selection.ReportType, &periodStart, &periodEnd,
		&selection.Mode, &selection.Status, &selection.ContentSnapshotAt,
		&selection.RequiredReadMode, &frozenAt, &stage, &status,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrSelectionMismatch
	}
	if err != nil {
		return "", err
	}
	if selection.Status != "attached" || status != "pending" || stage != "building_context" ||
		selection.RequiredReadMode != ReadModeDigestV2 {
		return "", ErrSelectionMismatch
	}
	if frozenAt.Valid {
		if err := s.requireDigestFrozenTx(ctx, tx, selection.ID, runID); err != nil {
			return "", err
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return selection.ID, nil
	}
	selection.Period = Period{
		Start: periodStart.Format("2006-01-02"), End: periodEnd.Format("2006-01-02"),
	}
	if err := s.freezeSelectionV2ForRun(ctx, tx, selection); err != nil {
		return "", err
	}
	if err := s.requireDigestFrozenTx(ctx, tx, selection.ID, runID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	var payloadBytes int
	if err := s.db.QueryRowContext(ctx, `
		SELECT selection_digest_bytes FROM report_source_selections WHERE id = $1`,
		selection.ID,
	).Scan(&payloadBytes); err != nil {
		log.Printf("observe frozen selection payload size failed: selection_id=%s error=%v", selection.ID, err)
	} else {
		observability.ObservePayload("selection", payloadBytes)
	}
	return selection.ID, nil
}

func (s *Service) RequireDigestFrozen(
	ctx context.Context,
	selectionID, runID string,
) error {
	if s == nil || s.db == nil || selectionID == "" || runID == "" {
		return ErrSelectionMismatch
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.requireDigestFrozenTx(ctx, tx, selectionID, runID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) requireDigestFrozenTx(
	ctx context.Context,
	tx *sql.Tx,
	selectionID, runID string,
) error {
	var mode, digestVersion, redactionVersion, hash, compaction string
	var payload []byte
	var payloadBytes int
	var targetBytes, hardLimitBytes sql.NullInt64
	var frozenAt time.Time
	err := tx.QueryRowContext(ctx, `
		SELECT required_read_mode, digest_version_snapshot, redaction_version_snapshot,
			selection_digest_payload, selection_digest_sha256, selection_digest_bytes,
			selection_digest_compaction, digest_target_bytes_snapshot,
			digest_hard_limit_bytes_snapshot, digest_frozen_at
		FROM report_source_selections
		WHERE id = $1 AND attached_run_id = $2 AND status = 'attached'
			AND digest_frozen_at IS NOT NULL`, selectionID, runID,
	).Scan(
		&mode, &digestVersion, &redactionVersion, &payload, &hash, &payloadBytes,
		&compaction, &targetBytes, &hardLimitBytes, &frozenAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDigestNotReady
	}
	if err != nil {
		return err
	}
	if payloadBytes != len(payload) || !json.Valid(payload) {
		return ErrDigestCorrupt
	}
	if sessiondigestv2.HashBytes(payload) != hash {
		return ErrDigestCorrupt
	}
	switch mode {
	case ReadModeDigestV1:
		if digestVersion != sessiondigest.Version || redactionVersion != sessiondigest.RedactionVersion ||
			!targetBytes.Valid || !hardLimitBytes.Valid ||
			(compaction != "detailed" && compaction != "compact") {
			return ErrDigestVersionMismatch
		}
	case ReadModeDigestV2:
		if digestVersion == sessiondigestv2.Version && compaction != "none" {
			return ErrDigestCorrupt
		}
		if err := validateDigestV2Payload(
			payload, digestVersion, redactionVersion, targetBytes, hardLimitBytes,
		); err != nil {
			return err
		}
	default:
		return ErrDigestCorrupt
	}
	var total, valid int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE
			d.id IS NOT NULL AND d.status = 'ready'
			AND d.session_content_slice_id = i.session_content_slice_id
			AND d.generation_id = i.source_generation_id
			AND d.content_projection_revision_id = i.content_projection_revision_id
			AND d.content_epoch = i.content_epoch_snapshot
			AND d.digest_sha256 = i.digest_sha256_snapshot
			AND d.digest_version = i.digest_version_snapshot
			AND d.digest_version = $2
			AND d.redaction_version = $3
		)
		FROM report_source_selection_items i
		LEFT JOIN session_slice_digest_revisions d ON d.id = i.digest_revision_id
		WHERE i.selection_id = $1`, selectionID, digestVersion, redactionVersion,
	).Scan(&total, &valid); err != nil {
		return err
	}
	if total == 0 || total != valid {
		return ErrDigestCorrupt
	}
	return nil
}
