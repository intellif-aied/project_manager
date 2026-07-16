package reportsource

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/sessiondigest"
	"github.com/aidashboard/api/internal/sessiondigestv2"
	"github.com/lib/pq"
)

const (
	ReadModeFull     = "full"
	ReadModeShadow   = "shadow"
	ReadModeDigestV1 = "digest_v1"
	ReadModeDigestV2 = "digest_v2"

	defaultDigestTargetBytes    = 64 << 10
	defaultDigestHardLimitBytes = 128 << 10
)

var (
	ErrDigestNotReady        = errors.New("report source digest is not ready")
	ErrDigestFailed          = errors.New("report source digest build failed")
	ErrDigestLimitExceeded   = errors.New("report source digest exceeds the hard limit")
	ErrDigestVersionMismatch = errors.New("report source digest version does not match")
	ErrDigestCorrupt         = errors.New("report source digest payload is corrupt")
	ErrReadModeMismatch      = errors.New("report source read mode does not match")
)

type Config struct {
	SessionReadMode     string
	DigestVersion       string
	RedactionVersion    string
	DigestTargetBytes   int
	DigestHardLimit     int
	DigestRolloutPct    int
	DigestCanaryUserIDs []string
}

func DefaultConfig() Config {
	return Config{
		SessionReadMode:   ReadModeFull,
		DigestVersion:     sessiondigest.Version,
		RedactionVersion:  sessiondigest.RedactionVersion,
		DigestTargetBytes: defaultDigestTargetBytes,
		DigestHardLimit:   defaultDigestHardLimitBytes,
		DigestRolloutPct:  100,
	}
}

func (c Config) Normalized() (Config, error) {
	defaults := DefaultConfig()
	c.SessionReadMode = strings.ToLower(strings.TrimSpace(c.SessionReadMode))
	if c.SessionReadMode == "" {
		c.SessionReadMode = defaults.SessionReadMode
	}
	if c.DigestVersion == "" {
		c.DigestVersion = defaults.DigestVersion
	}
	if c.RedactionVersion == "" {
		c.RedactionVersion = defaults.RedactionVersion
	}
	if c.DigestTargetBytes == 0 {
		c.DigestTargetBytes = defaults.DigestTargetBytes
	}
	if c.DigestHardLimit == 0 {
		c.DigestHardLimit = defaults.DigestHardLimit
	}
	if c.SessionReadMode != ReadModeFull && c.SessionReadMode != ReadModeShadow &&
		c.SessionReadMode != ReadModeDigestV1 && c.SessionReadMode != ReadModeDigestV2 {
		return Config{}, errors.New("invalid report session read mode")
	}
	if c.RedactionVersion != sessiondigest.RedactionVersion ||
		(c.DigestVersion != sessiondigest.Version && c.DigestVersion != sessiondigestv2.Version) {
		return Config{}, errors.New("unsupported report digest version")
	}
	if (c.SessionReadMode == ReadModeDigestV1 && c.DigestVersion != sessiondigest.Version) ||
		(c.SessionReadMode == ReadModeDigestV2 && c.DigestVersion != sessiondigestv2.Version) {
		return Config{}, errors.New("report read mode and digest version do not match")
	}
	if c.DigestTargetBytes < 1024 || c.DigestHardLimit < c.DigestTargetBytes || c.DigestHardLimit > 4<<20 {
		return Config{}, errors.New("invalid report digest byte budget")
	}
	if c.DigestRolloutPct < 0 || c.DigestRolloutPct > 100 {
		return Config{}, errors.New("invalid report digest rollout percent")
	}
	seen := map[string]struct{}{}
	canary := make([]string, 0, len(c.DigestCanaryUserIDs))
	for _, value := range c.DigestCanaryUserIDs {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return Config{}, errors.New("invalid report digest canary user ID")
		}
		value = strconv.FormatInt(parsed, 10)
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		canary = append(canary, value)
	}
	c.DigestCanaryUserIDs = canary
	return c, nil
}

func (c Config) RequiredReadMode(userID string) string {
	if c.SessionReadMode == ReadModeDigestV2 {
		return ReadModeDigestV2
	}
	if c.SessionReadMode != ReadModeDigestV1 {
		return ReadModeFull
	}
	for _, candidate := range c.DigestCanaryUserIDs {
		if candidate == userID {
			return ReadModeDigestV1
		}
	}
	sum := sha256.Sum256([]byte("aida-report-digest-rollout:" + userID))
	bucket := int(binary.BigEndian.Uint64(sum[:8]) % 100)
	if bucket < c.DigestRolloutPct {
		return ReadModeDigestV1
	}
	return ReadModeFull
}

type DigestCoverage struct {
	Complete             bool  `json:"complete"`
	SourceItemCount      int   `json:"source_item_count"`
	RepresentedItemCount int   `json:"represented_item_count"`
	SourceEventCount     int64 `json:"source_event_count"`
	IncludedEventCount   int64 `json:"included_event_count"`
	OmittedEventCount    int64 `json:"omitted_event_count"`
	TruncatedItemCount   int   `json:"truncated_item_count"`
}

type DigestBudget struct {
	TargetBytes    int    `json:"target_bytes"`
	HardLimitBytes int    `json:"hard_limit_bytes"`
	ActualBytes    int    `json:"actual_bytes"`
	Compaction     string `json:"compaction"`
}

type DigestItemCoverage struct {
	SourceEventCount   int64  `json:"source_event_count"`
	IncludedEventCount int64  `json:"included_event_count"`
	OmittedEventCount  int64  `json:"omitted_event_count"`
	Truncated          bool   `json:"truncated"`
	Representation     string `json:"representation"`
}

type DigestContentItem struct {
	SourceItemRef string               `json:"source_item_ref"`
	SessionRef    string               `json:"session_ref"`
	AgentType     string               `json:"agent_type"`
	ActivityStart string               `json:"activity_start_at"`
	ActivityEnd   string               `json:"activity_end_at"`
	StartDate     string               `json:"activity_start_date"`
	EndDate       string               `json:"activity_end_date"`
	DigestSHA256  string               `json:"digest_sha256"`
	Digest        sessiondigest.Digest `json:"digest"`
	Coverage      DigestItemCoverage   `json:"coverage"`
}

type digestContentPage struct {
	SourceMode       string              `json:"source_mode"`
	ContentMode      string              `json:"content_mode"`
	Timezone         string              `json:"timezone,omitempty"`
	DigestVersion    string              `json:"digest_version"`
	RedactionVersion string              `json:"redaction_version"`
	ContentSnapshot  string              `json:"content_snapshot_at"`
	Completeness     string              `json:"completeness"`
	ReturnedCount    int                 `json:"returned_item_count"`
	HasMore          bool                `json:"has_more"`
	NextCursor       *string             `json:"next_cursor"`
	Coverage         DigestCoverage      `json:"coverage"`
	Budget           DigestBudget        `json:"budget"`
	Items            []DigestContentItem `json:"items"`
}

type digestRevisionRow struct {
	SelectionItemID    string
	SessionRef         string
	AgentType          string
	ActivityStart      time.Time
	ActivityEnd        time.Time
	RevisionID         sql.NullString
	Status             sql.NullString
	DigestJSON         []byte
	DigestSHA256       sql.NullString
	DigestVersion      sql.NullString
	RedactionVersion   sql.NullString
	SourceEventCount   sql.NullInt64
	IncludedEventCount sql.NullInt64
	OmittedEventCount  sql.NullInt64
	Truncated          sql.NullBool
}

func (s *Service) freezeSelectionForRun(ctx context.Context, tx *sql.Tx, selection Selection, requiredMode string) error {
	if requiredMode == ReadModeFull {
		result, err := tx.ExecContext(ctx, `
			UPDATE report_source_selections
			SET required_read_mode = 'full', read_completed_mode = NULL,
				selection_digest_payload = NULL, selection_digest_sha256 = NULL,
				selection_digest_bytes = NULL, selection_digest_compaction = NULL,
				digest_version_snapshot = NULL, redaction_version_snapshot = NULL,
				digest_target_bytes_snapshot = NULL, digest_hard_limit_bytes_snapshot = NULL
			WHERE id = $1 AND status = 'prepared'`, selection.ID)
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
	if requiredMode == ReadModeDigestV2 {
		return s.freezeSelectionV2ForRun(ctx, tx, selection)
	}
	if requiredMode != ReadModeDigestV1 {
		return ErrReadModeMismatch
	}
	rows, err := s.loadDigestRevisionRows(ctx, tx, selection.ID)
	if err != nil {
		return err
	}
	page := digestContentPage{
		SourceMode: selection.Mode, ContentMode: ReadModeDigestV1,
		Timezone:      biztime.Zone,
		DigestVersion: s.config.DigestVersion, RedactionVersion: s.config.RedactionVersion,
		ContentSnapshot: biztime.FormatRFC3339(selection.ContentSnapshotAt), Completeness: "complete",
		HasMore: false, NextCursor: nil, Items: []DigestContentItem{},
	}
	page.Coverage.SourceItemCount = len(rows)
	selectionItemIDs := make([]string, 0, len(rows))
	digestRevisionIDs := make([]string, 0, len(rows))
	digestHashes := make([]string, 0, len(rows))
	digestVersions := make([]string, 0, len(rows))
	for _, row := range rows {
		if !row.RevisionID.Valid || !row.Status.Valid || row.Status.String == "pending" || row.Status.String == "building" {
			return ErrDigestNotReady
		}
		if row.Status.String == "failed" {
			return ErrDigestFailed
		}
		if row.Status.String != "ready" || !row.DigestSHA256.Valid || !row.DigestVersion.Valid || !row.RedactionVersion.Valid ||
			!row.SourceEventCount.Valid || !row.IncludedEventCount.Valid || !row.OmittedEventCount.Valid || !row.Truncated.Valid {
			return ErrDigestCorrupt
		}
		if row.DigestVersion.String != s.config.DigestVersion || row.RedactionVersion.String != s.config.RedactionVersion {
			return ErrDigestVersionMismatch
		}
		var digest sessiondigest.Digest
		if err := json.Unmarshal(row.DigestJSON, &digest); err != nil {
			return ErrDigestCorrupt
		}
		if digest.Goals == nil || digest.Outcomes == nil || digest.FilesChanged == nil || digest.Validations == nil || digest.Blockers == nil {
			return ErrDigestCorrupt
		}
		canonicalDigest, err := json.Marshal(digest)
		if err != nil || sessiondigest.HashBytes(canonicalDigest) != row.DigestSHA256.String {
			return ErrDigestCorrupt
		}
		item := DigestContentItem{
			SourceItemRef: row.SelectionItemID, SessionRef: row.SessionRef, AgentType: row.AgentType,
			ActivityStart: biztime.FormatRFC3339(row.ActivityStart),
			ActivityEnd:   biztime.FormatRFC3339(row.ActivityEnd),
			StartDate:     biztime.Date(row.ActivityStart),
			EndDate:       biztime.Date(row.ActivityEnd),
			DigestSHA256:  row.DigestSHA256.String, Digest: digest,
			Coverage: DigestItemCoverage{
				SourceEventCount:   row.SourceEventCount.Int64,
				IncludedEventCount: row.IncludedEventCount.Int64,
				OmittedEventCount:  row.OmittedEventCount.Int64,
				Truncated:          row.Truncated.Bool, Representation: "detailed",
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
			WHERE i.id = binding.selection_item_id AND i.selection_id = $1`, selection.ID,
			pq.Array(selectionItemIDs), pq.Array(digestRevisionIDs), pq.Array(digestHashes), pq.Array(digestVersions))
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
	page.Coverage.Complete = page.Coverage.SourceItemCount == page.Coverage.RepresentedItemCount
	payload, compaction, err := assembleDigestPayload(page, s.config.DigestTargetBytes, s.config.DigestHardLimit)
	if err != nil {
		return err
	}
	hash := sessiondigest.HashBytes(payload)
	result, err := tx.ExecContext(ctx, `
		UPDATE report_source_selections
		SET required_read_mode = 'digest_v1', read_completed_mode = NULL,
			selection_digest_payload = $2, selection_digest_sha256 = $3,
			selection_digest_bytes = $4, selection_digest_compaction = $5,
			digest_version_snapshot = $6, redaction_version_snapshot = $7,
			digest_target_bytes_snapshot = $8, digest_hard_limit_bytes_snapshot = $9
		WHERE id = $1 AND status = 'prepared'`, selection.ID, payload, hash, len(payload), compaction,
		s.config.DigestVersion, s.config.RedactionVersion, s.config.DigestTargetBytes, s.config.DigestHardLimit)
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

func (s *Service) readFrozenDigestSelection(
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
			AND sel.status = 'attached' AND sel.required_read_mode = 'digest_v1'`,
		selectionID, userID, runID).Scan(&sourceItems, &validDigestItems); err != nil {
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
			AND status = 'attached' AND required_read_mode = 'digest_v1'`,
		selectionID, userID, runID).Scan(&payload, &hash, &payloadBytes, &digestVersion,
		&redactionVersion, &targetBytes, &hardLimitBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return ContentPage{}, ErrReadModeMismatch
	}
	if err != nil {
		return ContentPage{}, err
	}
	if digestVersion != s.config.DigestVersion || redactionVersion != s.config.RedactionVersion {
		return ContentPage{}, ErrDigestVersionMismatch
	}
	if payloadBytes != len(payload) || len(payload) > hardLimitBytes || targetBytes <= 0 ||
		hardLimitBytes < targetBytes || sessiondigest.HashBytes(payload) != hash || !json.Valid(payload) {
		return ContentPage{}, ErrDigestCorrupt
	}
	var page digestContentPage
	if err := json.Unmarshal(payload, &page); err != nil || page.ContentMode != ReadModeDigestV1 ||
		page.DigestVersion != digestVersion || page.RedactionVersion != redactionVersion ||
		!page.Coverage.Complete || page.HasMore || page.ReturnedCount != len(page.Items) ||
		page.Coverage.SourceItemCount != page.Coverage.RepresentedItemCount ||
		page.Coverage.RepresentedItemCount != len(page.Items) || page.Budget.ActualBytes != len(payload) ||
		page.Budget.TargetBytes != targetBytes || page.Budget.HardLimitBytes != hardLimitBytes {
		return ContentPage{}, ErrDigestCorrupt
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE report_source_selections
		SET read_completed_at = COALESCE(read_completed_at, now()), read_completed_mode = 'digest_v1'
		WHERE id = $1 AND user_id = $2 AND attached_run_id = $3
			AND status = 'attached' AND required_read_mode = 'digest_v1'`,
		selectionID, userID, runID)
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

func (s *Service) loadDigestRevisionRows(ctx context.Context, tx *sql.Tx, selectionID string) ([]digestRevisionRow, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT i.id::text, i.session_ref_snapshot, i.agent_type,
			i.activity_start_at, i.activity_end_at,
			d.id::text, d.status, d.digest_json::text, d.digest_sha256,
			d.digest_version, d.redaction_version, d.source_event_count,
			d.included_event_count, d.omitted_event_count, d.truncated
		FROM report_source_selection_items i
		LEFT JOIN session_slice_digest_revisions d
			ON d.session_content_slice_id = i.session_content_slice_id
			AND d.content_projection_revision_id = i.content_projection_revision_id
			AND d.generation_id = i.source_generation_id
			AND d.content_epoch = i.content_epoch_snapshot
			AND d.digest_version = $2 AND d.redaction_version = $3
		WHERE i.selection_id = $1
		ORDER BY i.created_at, i.id`, selectionID, s.config.DigestVersion, s.config.RedactionVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []digestRevisionRow{}
	for rows.Next() {
		var row digestRevisionRow
		var digestText sql.NullString
		if err := rows.Scan(&row.SelectionItemID, &row.SessionRef, &row.AgentType,
			&row.ActivityStart, &row.ActivityEnd, &row.RevisionID, &row.Status,
			&digestText, &row.DigestSHA256, &row.DigestVersion, &row.RedactionVersion,
			&row.SourceEventCount, &row.IncludedEventCount, &row.OmittedEventCount,
			&row.Truncated); err != nil {
			return nil, err
		}
		if digestText.Valid {
			row.DigestJSON = []byte(digestText.String)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func assembleDigestPayload(page digestContentPage, targetBytes, hardLimit int) ([]byte, string, error) {
	page.Budget = DigestBudget{TargetBytes: targetBytes, HardLimitBytes: hardLimit, Compaction: "detailed"}
	payload, err := marshalStableActualBytes(&page)
	if err != nil {
		return nil, "", err
	}
	if len(payload) > targetBytes {
		page.Budget.Compaction = "compact"
		for index := range page.Items {
			page.Items[index].Digest = sessiondigest.CompactDigest(page.Items[index].Digest)
			page.Items[index].Coverage.Representation = "compact"
			if !page.Items[index].Coverage.Truncated {
				page.Items[index].Coverage.Truncated = true
				page.Coverage.TruncatedItemCount++
			}
		}
		payload, err = marshalStableActualBytes(&page)
		if err != nil {
			return nil, "", err
		}
	}
	if len(payload) > hardLimit {
		return nil, "", ErrDigestLimitExceeded
	}
	return payload, page.Budget.Compaction, nil
}

func marshalStableActualBytes(page *digestContentPage) ([]byte, error) {
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

func (s *Service) validateFrozenDigestSelectionTx(
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
			AND status = 'attached' AND required_read_mode = 'digest_v1'
			AND read_completed_mode = 'digest_v1'`, selectionID, userID, runID).Scan(
		&payload, &hash, &payloadBytes, &digestVersion, &redactionVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSourceIncomplete
	}
	if err != nil {
		return err
	}
	if digestVersion != s.config.DigestVersion || redactionVersion != s.config.RedactionVersion {
		return ErrDigestVersionMismatch
	}
	if payloadBytes != len(payload) || !json.Valid(payload) || sessiondigest.HashBytes(payload) != hash {
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
		WHERE i.selection_id = $1`, selectionID).Scan(&sourceItems, &validDigestItems); err != nil {
		return err
	}
	if sourceItems != validDigestItems {
		return ErrDigestCorrupt
	}
	return nil
}
