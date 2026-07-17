package reportsource

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/sessiondigestv2"
	"github.com/lib/pq"
)

type DigestV2ContentItem struct {
	SourceItemRef string                 `json:"source_item_ref"`
	SessionRef    string                 `json:"session_ref"`
	AgentType     string                 `json:"agent_type"`
	ActivityStart string                 `json:"activity_start_at"`
	ActivityEnd   string                 `json:"activity_end_at"`
	StartDate     string                 `json:"activity_start_date"`
	EndDate       string                 `json:"activity_end_date"`
	DigestSHA256  string                 `json:"digest_sha256"`
	Digest        sessiondigestv2.Digest `json:"digest"`
	Coverage      DigestItemCoverage     `json:"coverage"`
}

type digestV2ContentPage struct {
	SourceMode       string                               `json:"source_mode"`
	ContentMode      string                               `json:"content_mode"`
	Timezone         string                               `json:"timezone,omitempty"`
	DigestVersion    string                               `json:"digest_version"`
	RedactionVersion string                               `json:"redaction_version"`
	ContentSnapshot  string                               `json:"content_snapshot_at"`
	Completeness     string                               `json:"completeness"`
	ReturnedCount    int                                  `json:"returned_item_count"`
	HasMore          bool                                 `json:"has_more"`
	NextCursor       *string                              `json:"next_cursor"`
	Coverage         DigestCoverage                       `json:"coverage"`
	Budget           DigestBudget                         `json:"budget"`
	ReportPeriod     *sessiondigestv2.ReportPeriodSummary `json:"report_period_summary"`
	Items            []DigestV2ContentItem                `json:"items"`
}

func (s *Service) freezeSelectionV2ForRun(
	ctx context.Context,
	tx *sql.Tx,
	selection Selection,
) error {
	if s.config.DigestVersion != sessiondigestv2.Version ||
		s.config.RedactionVersion != sessiondigestv2.RedactionVersion {
		return ErrDigestVersionMismatch
	}
	periodStart, err := biztime.ParseDate(selection.Period.Start)
	if err != nil {
		return ErrSelectionMismatch
	}
	periodEnd, err := biztime.ParseDate(selection.Period.End)
	if err != nil {
		return ErrSelectionMismatch
	}
	rows, err := s.loadDigestRevisionRows(ctx, tx, selection.ID)
	if err != nil {
		return err
	}
	page := digestV2ContentPage{
		SourceMode:       selection.Mode,
		ContentMode:      ReadModeDigestV2,
		Timezone:         biztime.Zone,
		DigestVersion:    s.config.DigestVersion,
		RedactionVersion: s.config.RedactionVersion,
		ContentSnapshot:  biztime.FormatRFC3339(selection.ContentSnapshotAt),
		Completeness:     "complete",
		HasMore:          false,
		NextCursor:       nil,
		Items:            []DigestV2ContentItem{},
	}
	page.Coverage.SourceItemCount = len(rows)
	selectionItemIDs := make([]string, 0, len(rows))
	digestRevisionIDs := make([]string, 0, len(rows))
	digestHashes := make([]string, 0, len(rows))
	digestVersions := make([]string, 0, len(rows))
	periodSummaries := make(
		[]sessiondigestv2.ReportPeriodSummarySource, 0, len(rows),
	)
	for _, row := range rows {
		if !row.RevisionID.Valid || !row.Status.Valid ||
			row.Status.String == "pending" || row.Status.String == "building" {
			return ErrDigestNotReady
		}
		if row.Status.String == "failed" {
			return ErrDigestFailed
		}
		if row.Status.String != "ready" || !row.DigestSHA256.Valid ||
			!row.DigestVersion.Valid || !row.RedactionVersion.Valid ||
			!row.SourceEventCount.Valid || !row.IncludedEventCount.Valid ||
			!row.OmittedEventCount.Valid || !row.Truncated.Valid {
			return ErrDigestCorrupt
		}
		if row.DigestVersion.String != s.config.DigestVersion ||
			row.RedactionVersion.String != s.config.RedactionVersion {
			return ErrDigestVersionMismatch
		}
		var digest sessiondigestv2.Digest
		if err := json.Unmarshal(row.DigestJSON, &digest); err != nil ||
			digest.SchemaVersion != sessiondigestv2.Version ||
			digest.DailySummaries == nil ||
			digest.WorkUnits == nil || digest.DiscussionAggregates == nil {
			return ErrDigestCorrupt
		}
		canonicalDigest, err := json.Marshal(digest)
		if err != nil || sessiondigestv2.HashBytes(canonicalDigest) != row.DigestSHA256.String {
			return ErrDigestCorrupt
		}
		digest, _, periodTruncated := sessiondigestv2.PrepareForPeriod(
			digest,
			periodStart,
			periodEnd,
			biztime.Location(),
			sessiondigestv2.DefaultPeriodItemBytes,
		)
		periodSummaries = append(
			periodSummaries,
			sessiondigestv2.ReportPeriodSummarySource{
				SourceRef: row.SessionRef,
				Summary:   digest.ReportPeriodSummary,
			},
		)
		// The selection-level report_period_summary is the only report-facing
		// outcome list. Keep item digests as provenance/coverage metadata so an
		// Agent consumes one complete chronological list instead of duplicate
		// per-session highlights.
		digest.ReportPeriodSummary = nil
		item := DigestV2ContentItem{
			SourceItemRef: row.SelectionItemID,
			SessionRef:    row.SessionRef,
			AgentType:     row.AgentType,
			ActivityStart: biztime.FormatRFC3339(row.ActivityStart),
			ActivityEnd:   biztime.FormatRFC3339(row.ActivityEnd),
			StartDate:     biztime.Date(row.ActivityStart),
			EndDate:       biztime.Date(row.ActivityEnd),
			DigestSHA256:  row.DigestSHA256.String,
			Digest:        digest,
			Coverage: DigestItemCoverage{
				SourceEventCount:   row.SourceEventCount.Int64,
				IncludedEventCount: row.IncludedEventCount.Int64,
				OmittedEventCount:  row.OmittedEventCount.Int64,
				Truncated:          row.Truncated.Bool || periodTruncated,
				Representation:     "period_result_focused",
			},
		}
		page.Items = append(page.Items, item)
		page.Coverage.SourceEventCount += item.Coverage.SourceEventCount
		page.Coverage.IncludedEventCount += item.Coverage.IncludedEventCount
		page.Coverage.OmittedEventCount += item.Coverage.OmittedEventCount
		if item.Coverage.Truncated {
			page.Coverage.TruncatedItemCount++
		}
		selectionItemIDs = append(selectionItemIDs, row.SelectionItemID)
		digestRevisionIDs = append(digestRevisionIDs, row.RevisionID.String)
		digestHashes = append(digestHashes, row.DigestSHA256.String)
		digestVersions = append(digestVersions, row.DigestVersion.String)
	}
	if len(selectionItemIDs) > 0 {
		result, err := tx.ExecContext(ctx, `
			UPDATE report_source_selection_items i
			SET digest_revision_id = binding.digest_revision_id,
				digest_sha256_snapshot = binding.digest_sha256,
				digest_version_snapshot = binding.digest_version
			FROM unnest($2::uuid[], $3::uuid[], $4::text[], $5::text[])
				AS binding(selection_item_id, digest_revision_id, digest_sha256, digest_version)
			WHERE i.id = binding.selection_item_id AND i.selection_id = $1`,
			selection.ID, pq.Array(selectionItemIDs), pq.Array(digestRevisionIDs),
			pq.Array(digestHashes), pq.Array(digestVersions),
		)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != int64(len(selectionItemIDs)) {
			return ErrSelectionConflict
		}
	}
	page.ReturnedCount = len(page.Items)
	page.Coverage.RepresentedItemCount = len(page.Items)
	page.Coverage.Complete =
		page.Coverage.SourceItemCount == page.Coverage.RepresentedItemCount
	page.ReportPeriod = sessiondigestv2.MergeReportPeriodSummarySources(
		periodSummaries,
		selection.Period.Start,
		selection.Period.End,
		0,
	)
	if !sessiondigestv2.ReportPeriodOutcomeCoverageComplete(page.ReportPeriod) {
		return ErrDigestCorrupt
	}
	payload, compaction, err := assembleDigestV2Payload(
		page, s.config.DigestTargetBytes, s.config.DigestHardLimit,
	)
	if err != nil {
		return err
	}
	hash := sessiondigestv2.HashBytes(payload)
	result, err := tx.ExecContext(ctx, `
		UPDATE report_source_selections
		SET required_read_mode = 'digest_v2', read_completed_mode = NULL,
			selection_digest_payload = $2, selection_digest_sha256 = $3,
			selection_digest_bytes = $4, selection_digest_compaction = $5,
			digest_version_snapshot = $6, redaction_version_snapshot = $7,
			digest_target_bytes_snapshot = $8, digest_hard_limit_bytes_snapshot = $9
		WHERE id = $1 AND status = 'prepared'`,
		selection.ID, payload, hash, len(payload), compaction,
		s.config.DigestVersion, s.config.RedactionVersion,
		s.config.DigestTargetBytes, s.config.DigestHardLimit,
	)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrSelectionConflict
	}
	return nil
}

func assembleDigestV2Payload(
	page digestV2ContentPage,
	targetBytes, hardLimit int,
) ([]byte, string, error) {
	page.Budget = DigestBudget{
		TargetBytes: targetBytes, HardLimitBytes: hardLimit, Compaction: "detailed",
	}
	payload, err := marshalStableV2ActualBytes(&page)
	if err != nil {
		return nil, "", err
	}
	if len(payload) > targetBytes {
		page.Budget.Compaction = "compact"
		sessiondigestv2.CompactReportPeriodSummary(page.ReportPeriod, false)
		for index := range page.Items {
			page.Items[index].Digest = sessiondigestv2.CompactDigest(page.Items[index].Digest)
			page.Items[index].Coverage.Representation = "compact"
			if !page.Items[index].Coverage.Truncated {
				page.Items[index].Coverage.Truncated = true
				page.Coverage.TruncatedItemCount++
			}
		}
		payload, err = marshalStableV2ActualBytes(&page)
		if err != nil {
			return nil, "", err
		}
	}
	if len(payload) > hardLimit {
		page.Budget.Compaction = "compact_minimal"
		sessiondigestv2.CompactReportPeriodSummary(page.ReportPeriod, true)
		payload, err = marshalStableV2ActualBytes(&page)
		if err != nil {
			return nil, "", err
		}
	}
	if len(payload) > hardLimit {
		return nil, "", ErrDigestLimitExceeded
	}
	return payload, page.Budget.Compaction, nil
}

func marshalStableV2ActualBytes(page *digestV2ContentPage) ([]byte, error) {
	last := -1
	for iteration := 0; iteration < 8; iteration++ {
		encoded, err := json.Marshal(page)
		if err != nil {
			return nil, err
		}
		actual := len(encoded)
		if actual == last && page.Budget.ActualBytes == actual {
			return encoded, nil
		}
		last = actual
		page.Budget.ActualBytes = actual
	}
	return nil, ErrDigestCorrupt
}

func (s *Service) readFrozenDigestV2Selection(
	ctx context.Context,
	tx *sql.Tx,
	userID, selectionID, runID string,
) (ContentPage, error) {
	var sourceItems, validDigestItems int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE
			d.id IS NOT NULL AND d.status = 'ready'
			AND d.digest_sha256 = i.digest_sha256_snapshot
			AND d.digest_version = i.digest_version_snapshot
			AND d.digest_version = sel.digest_version_snapshot
			AND d.redaction_version = sel.redaction_version_snapshot
		)
		FROM report_source_selections sel
		JOIN report_source_selection_items i ON i.selection_id = sel.id
		LEFT JOIN session_slice_digest_revisions d ON d.id = i.digest_revision_id
		WHERE sel.id = $1 AND sel.user_id = $2 AND sel.attached_run_id = $3
			AND sel.status = 'attached' AND sel.required_read_mode = 'digest_v2'`,
		selectionID, userID, runID,
	).Scan(&sourceItems, &validDigestItems); err != nil {
		return ContentPage{}, err
	}
	if sourceItems != validDigestItems {
		return ContentPage{}, ErrDigestCorrupt
	}
	var payload []byte
	var hash, digestVersion, redactionVersion string
	var payloadBytes, targetBytes, hardLimitBytes int
	err := tx.QueryRowContext(ctx, `
		SELECT selection_digest_payload, selection_digest_sha256,
			selection_digest_bytes, digest_version_snapshot, redaction_version_snapshot,
			digest_target_bytes_snapshot, digest_hard_limit_bytes_snapshot
		FROM report_source_selections
		WHERE id = $1 AND user_id = $2 AND attached_run_id = $3
			AND status = 'attached' AND required_read_mode = 'digest_v2'`,
		selectionID, userID, runID,
	).Scan(
		&payload, &hash, &payloadBytes, &digestVersion,
		&redactionVersion, &targetBytes, &hardLimitBytes,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ContentPage{}, ErrReadModeMismatch
	}
	if err != nil {
		return ContentPage{}, err
	}
	if digestVersion != s.config.DigestVersion ||
		redactionVersion != s.config.RedactionVersion {
		return ContentPage{}, ErrDigestVersionMismatch
	}
	if payloadBytes != len(payload) || len(payload) > hardLimitBytes ||
		targetBytes <= 0 || hardLimitBytes < targetBytes ||
		sessiondigestv2.HashBytes(payload) != hash || !json.Valid(payload) {
		return ContentPage{}, ErrDigestCorrupt
	}
	var page digestV2ContentPage
	if err := json.Unmarshal(payload, &page); err != nil ||
		page.ContentMode != ReadModeDigestV2 ||
		page.DigestVersion != digestVersion ||
		page.RedactionVersion != redactionVersion ||
		!page.Coverage.Complete || page.HasMore ||
		page.ReturnedCount != len(page.Items) ||
		page.Coverage.SourceItemCount != page.Coverage.RepresentedItemCount ||
		page.Coverage.RepresentedItemCount != len(page.Items) ||
		page.ReportPeriod == nil ||
		!sessiondigestv2.ReportPeriodOutcomeCoverageComplete(page.ReportPeriod) ||
		page.Budget.ActualBytes != len(payload) ||
		page.Budget.TargetBytes != targetBytes ||
		page.Budget.HardLimitBytes != hardLimitBytes {
		return ContentPage{}, ErrDigestCorrupt
	}
	for _, item := range page.Items {
		if item.Digest.SchemaVersion != sessiondigestv2.Version ||
			item.Digest.DailySummaries == nil ||
			item.Digest.ReportPeriodSummary != nil ||
			item.Digest.WorkUnits == nil || item.Digest.DiscussionAggregates == nil {
			return ContentPage{}, ErrDigestCorrupt
		}
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE report_source_selections
		SET read_completed_at = COALESCE(read_completed_at, now()),
			read_completed_mode = 'digest_v2'
		WHERE id = $1 AND user_id = $2 AND attached_run_id = $3
			AND status = 'attached' AND required_read_mode = 'digest_v2'`,
		selectionID, userID, runID,
	)
	if err != nil {
		return ContentPage{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return ContentPage{}, err
	}
	if changed != 1 {
		return ContentPage{}, ErrSelectionMismatch
	}
	if err := tx.Commit(); err != nil {
		return ContentPage{}, err
	}
	return ContentPage{FrozenPayload: append(json.RawMessage(nil), payload...)}, nil
}

func (s *Service) validateFrozenDigestV2SelectionTx(
	ctx context.Context,
	tx *sql.Tx,
	userID, selectionID, runID string,
) error {
	var payload []byte
	var hash, digestVersion, redactionVersion string
	var payloadBytes int
	err := tx.QueryRowContext(ctx, `
		SELECT selection_digest_payload, selection_digest_sha256, selection_digest_bytes,
			digest_version_snapshot, redaction_version_snapshot
		FROM report_source_selections
		WHERE id = $1 AND user_id = $2 AND attached_run_id = $3
			AND status = 'attached' AND required_read_mode = 'digest_v2'
			AND read_completed_mode = 'digest_v2'`,
		selectionID, userID, runID,
	).Scan(&payload, &hash, &payloadBytes, &digestVersion, &redactionVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSourceIncomplete
	}
	if err != nil {
		return err
	}
	if digestVersion != s.config.DigestVersion ||
		redactionVersion != s.config.RedactionVersion {
		return ErrDigestVersionMismatch
	}
	if payloadBytes != len(payload) || !json.Valid(payload) ||
		sessiondigestv2.HashBytes(payload) != hash {
		return ErrDigestCorrupt
	}
	var page digestV2ContentPage
	if err := json.Unmarshal(payload, &page); err != nil ||
		page.ContentMode != ReadModeDigestV2 ||
		page.DigestVersion != sessiondigestv2.Version ||
		page.ReportPeriod == nil ||
		!sessiondigestv2.ReportPeriodOutcomeCoverageComplete(page.ReportPeriod) {
		return ErrDigestCorrupt
	}
	var sourceItems, validDigestItems int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE
			d.id IS NOT NULL AND d.status = 'ready'
			AND d.digest_sha256 = i.digest_sha256_snapshot
			AND d.digest_version = i.digest_version_snapshot
		)
		FROM report_source_selection_items i
		LEFT JOIN session_slice_digest_revisions d ON d.id = i.digest_revision_id
		WHERE i.selection_id = $1`,
		selectionID,
	).Scan(&sourceItems, &validDigestItems); err != nil {
		return err
	}
	if sourceItems != validDigestItems {
		return ErrDigestCorrupt
	}
	return nil
}
