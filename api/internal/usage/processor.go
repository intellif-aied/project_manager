package usage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/pricing"
	"github.com/aidashboard/api/internal/sessionsync"
	"github.com/aidashboard/api/internal/tokenrollup"
)

const (
	ParserVersion     = "usage-parser-v5"
	NormalizerVersion = "token-normalizer-v1"
)

var (
	ErrUsageOutOfOrder  = errors.New("usage parser chunk is out of order")
	ErrUsageUnavailable = errors.New("usage parser input is unavailable")
	ErrUsageQualityGate = errors.New("usage revision failed its quality gate")
)

type ObjectStore interface {
	Download(context.Context, string) (io.ReadCloser, error)
}

type Processor struct {
	db                      *sql.DB
	store                   ObjectStore
	claudeCacheWriteVariant string
	normalizerVersion       string
	rollups                 *tokenrollup.Builder
}

func NewProcessor(database *sql.DB, store ObjectStore, claudeCacheWriteVariant string) (*Processor, error) {
	if database == nil || store == nil {
		return nil, errors.New("database and object store are required")
	}
	normalizerVersion, err := NormalizerRevision(claudeCacheWriteVariant)
	if err != nil {
		return nil, err
	}
	return &Processor{
		db: database, store: store,
		claudeCacheWriteVariant: claudeCacheWriteVariant,
		normalizerVersion:       normalizerVersion,
		rollups:                 tokenrollup.NewBuilder(),
	}, nil
}

func NormalizerRevision(claudeCacheWriteVariant string) (string, error) {
	if claudeCacheWriteVariant != "" && claudeCacheWriteVariant != "5m" && claudeCacheWriteVariant != "1h" {
		return "", errors.New("Claude cache write variant must be empty, 5m, or 1h")
	}
	variantVersion := claudeCacheWriteVariant
	if variantVersion == "" {
		variantVersion = "unspecified"
	}
	return NormalizerVersion + ":claude-cache-write-" + variantVersion, nil
}

func (p *Processor) Process(ctx context.Context, job sessionsync.ProcessingJob) error {
	switch job.Type {
	case sessionsync.JobParseUsageChunk:
		return p.processChunk(ctx, job)
	case sessionsync.JobRebuildMetricsRevision:
		return p.processActivation(ctx, job)
	default:
		return fmt.Errorf("unsupported usage job type %q", job.Type)
	}
}

type usageChunk struct {
	ID                  string
	GenerationID        string
	StartCursor         int64
	EndCursor           int64
	ObjectKey           string
	ContentHash         string
	ObjectStatus        string
	SourceID            string
	SessionID           string
	UserID              int64
	Provider            string
	SessionRef          string
	SourceFormat        string
	UsageCapability     string
	GenerationStatus    string
	GenerationHighWater int64
}

func (p *Processor) processChunk(ctx context.Context, job sessionsync.ProcessingJob) error {
	if !job.ChunkID.Valid || !job.GenerationID.Valid {
		return ErrUsageUnavailable
	}
	chunk, err := p.loadChunk(ctx, job)
	if err != nil {
		return err
	}
	if chunk.ObjectStatus != "available" && chunk.ObjectStatus != "delete_pending" && chunk.ObjectStatus != "deleted" {
		return ErrUsageUnavailable
	}
	var content []byte
	if chunk.ObjectStatus == "available" {
		object, err := p.store.Download(ctx, chunk.ObjectKey)
		if err != nil {
			return err
		}
		want := chunk.EndCursor - chunk.StartCursor
		content, err = io.ReadAll(io.LimitReader(object, want+1))
		closeErr := object.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if int64(len(content)) != want || hashBytes(content) != chunk.ContentHash {
			return fmt.Errorf("%w: object size/hash does not match accepted chunk", ErrUsageUnavailable)
		}
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sessionsync.LockSessionForUpdate(ctx, tx, chunk.SessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "usage-revision:"+chunk.GenerationID); err != nil {
		return err
	}
	revision, err := p.metricsRevisionForJob(ctx, tx, chunk, job)
	if err != nil {
		return err
	}
	checkpoint, err := p.lockParserCheckpoint(ctx, tx, revision.ID, chunk)
	if err != nil {
		return err
	}
	if checkpoint.ParsedCursor >= chunk.EndCursor {
		if _, err := tx.ExecContext(ctx, `UPDATE session_upload_chunks SET usage_parse_status = 'parsed' WHERE id = $1`, chunk.ID); err != nil {
			return err
		}
		return tx.Commit()
	}
	if checkpoint.ParsedCursor != chunk.StartCursor {
		if checkpoint.ParsedCursor < chunk.StartCursor {
			if err := enqueueUsagePrefixRebuildJobs(ctx, tx, revision.ID, chunk, checkpoint.ParsedCursor); err != nil {
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
		}
		return fmt.Errorf("%w: parsed cursor=%d chunk start=%d", ErrUsageOutOfOrder, checkpoint.ParsedCursor, chunk.StartCursor)
	}
	var parsed ParseResult
	if chunk.ObjectStatus == "available" {
		if chunk.SourceFormat == "aida_event_v1" {
			parsed, err = ParseCanonicalChunk(chunk.Provider, bytes.NewReader(content), chunk.StartCursor, checkpoint.State)
		} else {
			parsed, err = ParseProviderChunk(chunk.Provider, bytes.NewReader(content), chunk.StartCursor, checkpoint.State)
		}
	} else {
		parsed, err = p.parseEnvelopeChunk(ctx, tx, chunk, checkpoint.State)
	}
	if err != nil {
		return err
	}
	if chunk.SourceFormat == "aida_event_v1" {
		if err := p.resolveCanonicalOwners(ctx, tx, chunk, &parsed); err != nil {
			return err
		}
		applyCanonicalCapabilityGate(chunk.UsageCapability, &parsed)
	}
	if chunk.Provider == "codex" {
		if err := p.applyCodexForkMetadata(ctx, tx, chunk.SessionID, &parsed); err != nil {
			return err
		}
	}
	if parsed.EndCursor != chunk.EndCursor {
		return fmt.Errorf("%w: parsed cursor=%d chunk end=%d", ErrUsageUnavailable, parsed.EndCursor, chunk.EndCursor)
	}
	for _, record := range parsed.Records {
		if err := p.applyRecord(ctx, tx, revision.ID, chunk, record); err != nil {
			return err
		}
	}
	if err := updateCheckpoint(ctx, tx, checkpoint.ID, chunk, parsed); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE session_upload_chunks SET usage_parse_status = 'parsed' WHERE id = $1`, chunk.ID); err != nil {
		return err
	}
	quality, counts, err := revisionQualityAndCounts(ctx, tx, revision.ID, parsed.MalformedCount, parsed.UnknownUsageCount)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_metrics_revisions
		SET validated_through_cursor = $2,
			source_high_water_cursor = GREATEST(source_high_water_cursor, $3),
			scanned_event_count = scanned_event_count + $4,
			usage_observation_count = $5, usage_event_count = $6,
			advanced_observation_count = $7, duplicate_usage_event_count = $8,
			malformed_event_count = malformed_event_count + $9,
			unknown_usage_event_count = unknown_usage_event_count + $10,
			conflict_usage_event_count = $11, quality_status = $12,
			reconciliation_json = jsonb_build_object(
				'parsed_cursor', $2::bigint, 'source_high_water_cursor', $3::bigint,
				'observations', $5::bigint, 'logical_events', $6::bigint,
				'active_components', $13::bigint
			)
		WHERE id = $1`, revision.ID, chunk.EndCursor, chunk.GenerationHighWater,
		parsed.ScannedLineCount, counts.Observations, counts.Events, counts.Advances,
		counts.Duplicates, parsed.MalformedCount, parsed.UnknownUsageCount, counts.Conflicts,
		quality, counts.Components); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_source_metrics_states (
			source_id, target_generation_id, status, active_usage_parsed_cursor, source_high_water_cursor
		) VALUES ($1, $2, 'pending', 0, $3)
		ON CONFLICT (source_id) DO UPDATE
		SET source_high_water_cursor = CASE
				WHEN session_source_metrics_states.target_generation_id = EXCLUDED.target_generation_id
					THEN GREATEST(session_source_metrics_states.source_high_water_cursor, EXCLUDED.source_high_water_cursor)
				ELSE EXCLUDED.source_high_water_cursor END,
			target_generation_id = EXCLUDED.target_generation_id,
			status = CASE
				WHEN session_source_metrics_states.active_revision_id = $4 THEN session_source_metrics_states.status
				WHEN session_source_metrics_states.active_revision_id IS NOT NULL
					AND session_source_metrics_states.target_generation_id = EXCLUDED.target_generation_id
					AND session_source_metrics_states.active_usage_parsed_cursor >= EXCLUDED.source_high_water_cursor
					THEN session_source_metrics_states.status
				ELSE 'rebuilding'
			END,
			updated_at = now()`, chunk.SourceID, chunk.GenerationID, chunk.GenerationHighWater, revision.ID); err != nil {
		return err
	}
	if err := p.activateIfReady(ctx, tx, revision.ID, chunk.SourceID, chunk.GenerationID); err != nil {
		return err
	}
	return tx.Commit()
}

func enqueueUsagePrefixRebuildJobs(
	ctx context.Context,
	tx *sql.Tx,
	revisionID string,
	chunk usageChunk,
	parsedCursor int64,
) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO session_processing_jobs (
			job_type, session_id, generation_id, chunk_id,
			target_metrics_revision_id, payload, created_at
		)
		SELECT $1, $2, ch.generation_id, ch.id, $3,
			jsonb_build_object('reason', 'parser_revision_prefix_rebuild'), ch.accepted_at
		FROM session_upload_chunks ch
		WHERE ch.generation_id = $4
			AND ch.start_cursor >= $5
			AND ch.end_cursor <= $6
		ORDER BY ch.start_cursor, ch.id
		ON CONFLICT DO NOTHING`,
		sessionsync.JobParseUsageChunk, chunk.SessionID, revisionID,
		chunk.GenerationID, parsedCursor, chunk.StartCursor)
	return err
}

func (p *Processor) loadChunk(ctx context.Context, job sessionsync.ProcessingJob) (usageChunk, error) {
	var chunk usageChunk
	err := p.db.QueryRowContext(ctx, `
		SELECT c.id, c.generation_id, c.start_cursor, c.end_cursor, c.raw_object_key,
			c.content_sha256, c.object_status,
			src.id, s.id, s.user_id, s.agent_type, s.session_ref, src.source_format,
			COALESCE(src.ingestion_metadata->>'usage_capability','unavailable'),
			g.status, g.expected_cursor
		FROM session_upload_chunks c
		JOIN session_source_generations g ON g.id = c.generation_id
		JOIN session_sources src ON src.id = g.source_id
		JOIN sessions s ON s.id = src.session_id
		WHERE c.id = $1 AND c.generation_id = $2`, job.ChunkID.String, job.GenerationID.String).Scan(
		&chunk.ID, &chunk.GenerationID, &chunk.StartCursor, &chunk.EndCursor,
		&chunk.ObjectKey, &chunk.ContentHash, &chunk.ObjectStatus, &chunk.SourceID, &chunk.SessionID,
		&chunk.UserID, &chunk.Provider, &chunk.SessionRef, &chunk.SourceFormat, &chunk.UsageCapability,
		&chunk.GenerationStatus, &chunk.GenerationHighWater,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return usageChunk{}, ErrUsageUnavailable
	}
	return chunk, err
}

func applyCanonicalCapabilityGate(capability string, parsed *ParseResult) {
	for index := range parsed.Records {
		record := &parsed.Records[index]
		switch capability {
		case "exact":
		case "estimated":
			if record.Quality == QualityExact {
				record.Quality = QualityEstimated
				record.QualityReason = "adapter release is limited to estimated usage"
			}
		default:
			record.Quality = QualityConflict
			record.QualityReason = "adapter release does not permit usage accounting"
		}
	}
}

func (p *Processor) resolveCanonicalOwners(ctx context.Context, tx *sql.Tx, chunk usageChunk, parsed *ParseResult) error {
	for index := range parsed.Records {
		record := &parsed.Records[index]
		var ownerID string
		err := tx.QueryRowContext(ctx, `
			WITH RECURSIVE ancestors AS (
				SELECT id, session_ref, parent_session_ref, 0 AS depth
				FROM sessions WHERE id = $1
				UNION ALL
				SELECT parent.id, parent.session_ref, parent.parent_session_ref, ancestors.depth + 1
				FROM ancestors
				JOIN sessions parent ON parent.user_id = $2 AND parent.agent_type = $3
					AND parent.session_ref = ancestors.parent_session_ref
				WHERE ancestors.depth < 100
			)
			SELECT id FROM ancestors WHERE session_ref = $4 LIMIT 1`,
			chunk.SessionID, chunk.UserID, chunk.Provider, record.OwnerSessionRef).Scan(&ownerID)
		if errors.Is(err, sql.ErrNoRows) {
			record.Quality = QualityConflict
			record.QualityReason = "canonical owner_session_ref is not the current session or an ancestor"
			continue
		}
		if err != nil {
			return err
		}
		record.OwnerSessionID = ownerID
	}
	return nil
}

func (p *Processor) parseEnvelopeChunk(
	ctx context.Context,
	tx *sql.Tx,
	chunk usageChunk,
	state ParseState,
) (ParseResult, error) {
	var manifestID string
	var scannedLines, expectedRecords int64
	err := tx.QueryRowContext(ctx, `
		SELECT ec.manifest_id, ec.source_record_count, ec.potential_usage_record_count
		FROM session_metering_envelope_chunks ec
		JOIN session_metering_envelope_manifests m ON m.id = ec.manifest_id
		WHERE ec.chunk_id = $1 AND ec.generation_id = $2
			AND ec.source_start_cursor = $3 AND ec.source_end_cursor = $4
			AND m.status = 'validated' AND m.envelope_version = $5
		ORDER BY m.content_epoch DESC LIMIT 1`, chunk.ID, chunk.GenerationID,
		chunk.StartCursor, chunk.EndCursor, MeteringEnvelopeVersion).Scan(
		&manifestID, &scannedLines, &expectedRecords,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ParseResult{}, ErrUsageUnavailable
	}
	if err != nil {
		return ParseResult{}, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT provider, usage_event_key, identity_strategy, provider_event_fingerprint,
			source_start_cursor, source_end_cursor, occurred_at, COALESCE(raw_model, ''),
			raw_usage_json, parsed_counters_json, raw_usage_hash,
			quality_status, COALESCE(quality_reason, ''), COALESCE(owner_session_ref, '')
		FROM session_metering_envelopes
		WHERE manifest_id = $1 AND chunk_id = $2
		ORDER BY source_start_cursor, source_end_cursor, id`, manifestID, chunk.ID)
	if err != nil {
		return ParseResult{}, err
	}
	defer rows.Close()
	result := ParseResult{
		EndCursor: chunk.EndCursor, ScannedLineCount: scannedLines, State: state,
	}
	for rows.Next() {
		var record UsageRecord
		var rawUsage, countersJSON []byte
		var quality string
		if err := rows.Scan(
			&record.Provider, &record.EventKey, &record.IdentityStrategy, &record.ProviderFingerprint,
			&record.SourceStartCursor, &record.SourceEndCursor, &record.OccurredAt, &record.RawModel,
			&rawUsage, &countersJSON, &record.RawUsageHash, &quality, &record.QualityReason,
			&record.OwnerSessionRef,
		); err != nil {
			return ParseResult{}, err
		}
		record.RawUsage = append(json.RawMessage(nil), rawUsage...)
		record.Quality = QualityStatus(quality)
		if err := json.Unmarshal(countersJSON, &record.Counters); err != nil {
			return ParseResult{}, err
		}
		record.Delta = record.Counters
		if record.Provider == "codex" {
			if result.State.PreviousCodexCounters != nil {
				record.Delta = subtractCounters(record.Counters, *result.State.PreviousCodexCounters)
				if countersHaveNegative(record.Delta) {
					record.Quality = QualityConflict
					record.QualityReason = "cumulative token counters decreased without a verified reset boundary"
				}
			}
			counters := record.Counters
			result.State.PreviousCodexCounters = &counters
		}
		if record.RawModel != "" {
			result.State.ActiveModel = record.RawModel
		}
		result.Records = append(result.Records, record)
	}
	if err := rows.Err(); err != nil {
		return ParseResult{}, err
	}
	if int64(len(result.Records)) != expectedRecords {
		return ParseResult{}, fmt.Errorf("%w: envelope record count=%d expected=%d", ErrUsageUnavailable, len(result.Records), expectedRecords)
	}
	return result, nil
}

type metricsRevision struct {
	ID     string
	Status string
}

func (p *Processor) metricsRevisionForJob(
	ctx context.Context,
	tx *sql.Tx,
	chunk usageChunk,
	job sessionsync.ProcessingJob,
) (metricsRevision, error) {
	if job.TargetMetricsRevisionID.Valid {
		var revision metricsRevision
		err := tx.QueryRowContext(ctx, `
			SELECT id, status
			FROM session_metrics_revisions
			WHERE id = $1 AND source_id = $2 AND generation_id = $3
				AND parser_version = $4 AND normalizer_version = $5`,
			job.TargetMetricsRevisionID.String, chunk.SourceID, chunk.GenerationID,
			ParserVersion, p.normalizerVersion,
		).Scan(&revision.ID, &revision.Status)
		if errors.Is(err, sql.ErrNoRows) {
			return metricsRevision{}, ErrUsageUnavailable
		}
		return revision, err
	}
	var revision metricsRevision
	err := tx.QueryRowContext(ctx, `
		INSERT INTO session_metrics_revisions (
			source_id, generation_id, parser_version, normalizer_version,
			status, build_start_cursor, source_high_water_cursor
		) VALUES ($1, $2, $3, $4, 'building', $5, $5)
		ON CONFLICT (generation_id, parser_version, normalizer_version) DO UPDATE
		SET source_high_water_cursor = GREATEST(session_metrics_revisions.source_high_water_cursor, EXCLUDED.source_high_water_cursor)
		RETURNING id, status`, chunk.SourceID, chunk.GenerationID, ParserVersion, p.normalizerVersion, chunk.GenerationHighWater,
	).Scan(&revision.ID, &revision.Status)
	return revision, err
}

type parserCheckpoint struct {
	ID           string
	ParsedCursor int64
	State        ParseState
}

func (p *Processor) lockParserCheckpoint(ctx context.Context, tx *sql.Tx, revisionID string, chunk usageChunk) (parserCheckpoint, error) {
	provider := normalizeProvider(chunk.Provider)
	if chunk.SourceFormat == "aida_event_v1" {
		provider = "canonical"
	}
	if provider == "" {
		return parserCheckpoint{}, ErrUsageUnavailable
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_parser_checkpoints (
			revision_id, generation_id, provider, parser_version, normalizer_version
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (revision_id, provider) DO NOTHING`,
		revisionID, chunk.GenerationID, provider, ParserVersion, p.normalizerVersion); err != nil {
		return parserCheckpoint{}, err
	}
	var checkpoint parserCheckpoint
	var previousJSON []byte
	var activeModel sql.NullString
	var forkParent, forkSource sql.NullString
	var forkedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT id, parsed_cursor, previous_token_counters_json, counter_segment, active_model,
			root_metadata_seen, fork_parent_session_ref, fork_source, forked_at,
			fork_baseline_ready, fork_baseline_missing, fork_metadata_conflict
		FROM session_parser_checkpoints
		WHERE revision_id = $1 AND provider = $2 FOR UPDATE`, revisionID, provider).Scan(
		&checkpoint.ID, &checkpoint.ParsedCursor, &previousJSON,
		&checkpoint.State.CounterSegment, &activeModel, &checkpoint.State.RootMetadataSeen,
		&forkParent, &forkSource, &forkedAt, &checkpoint.State.ForkBaselineReady,
		&checkpoint.State.ForkBaselineMissing, &checkpoint.State.ForkMetadataConflict,
	); err != nil {
		return parserCheckpoint{}, err
	}
	if activeModel.Valid {
		checkpoint.State.ActiveModel = activeModel.String
	}
	checkpoint.State.ForkParentSessionRef = forkParent.String
	checkpoint.State.ForkSource = forkSource.String
	if forkedAt.Valid {
		value := forkedAt.Time.UTC()
		checkpoint.State.ForkedAt = &value
	}
	var counters TokenCounters
	if len(previousJSON) > 0 && string(previousJSON) != "{}" {
		if err := json.Unmarshal(previousJSON, &counters); err != nil {
			return parserCheckpoint{}, err
		}
		checkpoint.State.PreviousCodexCounters = &counters
	}
	return checkpoint, nil
}

func updateCheckpoint(ctx context.Context, tx *sql.Tx, checkpointID string, chunk usageChunk, parsed ParseResult) error {
	previous := []byte(`{}`)
	if parsed.State.PreviousCodexCounters != nil {
		previous, _ = json.Marshal(parsed.State.PreviousCodexCounters)
	}
	checkpointContent, _ := json.Marshal(map[string]any{
		"cursor": parsed.EndCursor, "previous": json.RawMessage(previous),
		"segment": parsed.State.CounterSegment, "model": parsed.State.ActiveModel,
		"root_metadata_seen":      parsed.State.RootMetadataSeen,
		"fork_parent_session_ref": parsed.State.ForkParentSessionRef,
		"fork_source":             parsed.State.ForkSource, "forked_at": parsed.State.ForkedAt,
		"fork_baseline_ready":    parsed.State.ForkBaselineReady,
		"fork_baseline_missing":  parsed.State.ForkBaselineMissing,
		"fork_metadata_conflict": parsed.State.ForkMetadataConflict,
	})
	hash := sha256.Sum256(checkpointContent)
	_, err := tx.ExecContext(ctx, `
		UPDATE session_parser_checkpoints
		SET parsed_cursor = $2, previous_token_counters_json = $3,
			counter_segment = $4, active_model = NULLIF($5, ''), checkpoint_hash = $6,
			root_metadata_seen = $7, fork_parent_session_ref = NULLIF($8, ''),
			fork_source = NULLIF($9, ''), forked_at = $10,
			fork_baseline_ready = $11, fork_baseline_missing = $12,
			fork_metadata_conflict = $13, updated_at = now()
		WHERE id = $1`, checkpointID, parsed.EndCursor, previous,
		parsed.State.CounterSegment, parsed.State.ActiveModel, hex.EncodeToString(hash[:]),
		parsed.State.RootMetadataSeen, parsed.State.ForkParentSessionRef, parsed.State.ForkSource,
		parsed.State.ForkedAt, parsed.State.ForkBaselineReady, parsed.State.ForkBaselineMissing,
		parsed.State.ForkMetadataConflict)
	return err
}

func (p *Processor) applyCodexForkMetadata(
	ctx context.Context,
	tx *sql.Tx,
	sessionID string,
	parsed *ParseResult,
) error {
	if parsed == nil || parsed.State.ForkParentSessionRef == "" {
		return nil
	}
	var storedParent sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT parent_session_ref FROM sessions WHERE id = $1 FOR UPDATE`, sessionID,
	).Scan(&storedParent); err != nil {
		return err
	}
	if storedParent.Valid && strings.TrimSpace(storedParent.String) != "" &&
		strings.TrimSpace(storedParent.String) != parsed.State.ForkParentSessionRef {
		parsed.State.ForkMetadataConflict = true
		for index := range parsed.Records {
			parsed.Records[index].Quality = QualityConflict
			parsed.Records[index].QualityReason = "Codex fork parent metadata conflicts with the stored session"
		}
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE sessions
		SET parent_session_ref = COALESCE(NULLIF(parent_session_ref, ''), $2),
			forked_at = COALESCE($3, forked_at),
			fork_source = COALESCE(NULLIF($4, ''), fork_source),
			started_at = CASE WHEN $3::timestamptz IS NULL THEN started_at ELSE $3 END,
			updated_at = now()
		WHERE id = $1`, sessionID, parsed.State.ForkParentSessionRef,
		parsed.State.ForkedAt, parsed.State.ForkSource)
	return err
}

func (p *Processor) applyRecord(ctx context.Context, tx *sql.Tx, revisionID string, chunk usageChunk, record UsageRecord) error {
	if record.Provider == "codex" {
		record.EventKey = "codex:generation:" + chunk.GenerationID + ":" + record.EventKey
		record.ProviderFingerprint = "codex:generation:" + chunk.GenerationID + ":" + record.ProviderFingerprint
	}
	var observationID string
	parsedCounters, _ := json.Marshal(record.Counters)
	err := tx.QueryRowContext(ctx, `
		INSERT INTO session_usage_observations (
			revision_id, generation_id, chunk_id, provider,
			source_start_cursor, source_end_cursor, occurred_at, raw_model,
			raw_usage_json, parsed_counters_json, raw_usage_hash,
			parser_version, quality_status, quality_reason
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, $10, $11, $12, $13, NULLIF($14, ''))
		ON CONFLICT (
			revision_id, provider, generation_id, source_start_cursor, source_end_cursor, raw_usage_hash
		) DO UPDATE SET raw_usage_hash = EXCLUDED.raw_usage_hash
		RETURNING id`, revisionID, chunk.GenerationID, chunk.ID, record.Provider,
		record.SourceStartCursor, record.SourceEndCursor, record.OccurredAt, record.RawModel,
		[]byte(record.RawUsage), parsedCounters, record.RawUsageHash,
		ParserVersion, record.Quality, record.QualityReason,
	).Scan(&observationID)
	if err != nil {
		return err
	}

	var logicalID, currentObservationID, foldStatus string
	var logicalOccurred time.Time
	var logicalModel sql.NullString
	var currentUsage []byte
	var currentHash string
	err = tx.QueryRowContext(ctx, `
		SELECT e.id, e.current_observation_id, e.fold_status, e.logical_occurred_at,
			e.logical_raw_model, o.parsed_counters_json, o.raw_usage_hash
		FROM session_logical_usage_events e
		JOIN session_usage_observations o ON o.id = e.current_observation_id
		WHERE e.revision_id = $1 AND e.provider = $2 AND e.usage_event_key = $3
		FOR UPDATE`, revisionID, record.Provider, record.EventKey).Scan(
		&logicalID, &currentObservationID, &foldStatus, &logicalOccurred,
		&logicalModel, &currentUsage, &currentHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		status := "current"
		if record.Quality == QualityConflict {
			status = "conflict"
		} else if record.Quality == QualityIncomplete {
			status = "incomplete"
		}
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO session_logical_usage_events (
				revision_id, generation_id, provider, usage_event_key, identity_strategy,
				current_observation_id, logical_occurred_at, logical_raw_model,
				fold_status, fold_reason, provider_event_fingerprint
			) VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, NULLIF($10, ''), $11)
			RETURNING id`, revisionID, chunk.GenerationID, record.Provider, record.EventKey,
			record.IdentityStrategy, observationID, record.OccurredAt, record.RawModel,
			status, record.QualityReason, record.ProviderFingerprint).Scan(&logicalID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_usage_observations
			SET logical_usage_event_id = $2
			WHERE id = $1 AND logical_usage_event_id IS NULL`, observationID, logicalID); err != nil {
			return err
		}
		if status == "current" {
			quality, err := p.replaceComponent(ctx, tx, revisionID, logicalID, observationID, chunk, record)
			if err != nil || quality == QualityConflict || quality == QualityIncomplete {
				return err
			}
			kind := contributionInitial
			if record.Provider == "codex" {
				kind = contributionCheckpointDelta
			}
			return p.insertContribution(ctx, tx, revisionID, logicalID, "", observationID, chunk, record, kind)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_usage_observations
		SET logical_usage_event_id = $2
		WHERE id = $1 AND logical_usage_event_id IS NULL`, observationID, logicalID); err != nil {
		return err
	}
	if foldStatus == "conflict" || foldStatus == "incomplete" {
		_, err := tx.ExecContext(ctx, `
			UPDATE session_logical_usage_events
			SET observation_count = observation_count + 1, updated_at = now() WHERE id = $1`, logicalID)
		return err
	}
	var currentCounters TokenCounters
	if err := json.Unmarshal(currentUsage, &currentCounters); err != nil {
		return err
	}
	current := UsageRecord{
		Provider: record.Provider, EventKey: record.EventKey, RawUsageHash: currentHash,
		OccurredAt: logicalOccurred, RawModel: logicalModel.String, Counters: currentCounters, Delta: currentCounters,
	}
	fold := FoldClaudeObservation(current, record)
	if record.Provider == "codex" {
		fold = FoldResult{Action: FoldConflict, Reason: "Codex generation_cursor event key unexpectedly repeated"}
	} else if record.Provider == "canonical" {
		if current.RawUsageHash == record.RawUsageHash {
			fold = FoldResult{Action: FoldDuplicate, Reason: "same canonical usage fact"}
		} else {
			fold = FoldResult{Action: FoldConflict, Reason: "canonical usage_fact_id changed payload"}
		}
	}
	switch fold.Action {
	case FoldDuplicate:
		_, err = tx.ExecContext(ctx, `
			UPDATE session_logical_usage_events
			SET observation_count = observation_count + 1,
				duplicate_observation_count = duplicate_observation_count + 1,
				fold_reason = $2, updated_at = now() WHERE id = $1`, logicalID, fold.Reason)
		return err
	case FoldAdvance:
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_logical_usage_events
			SET current_observation_id = $2, observation_count = observation_count + 1,
				advance_count = advance_count + 1, fold_reason = $3, updated_at = now()
			WHERE id = $1`, logicalID, observationID, fold.Reason); err != nil {
			return err
		}
		quality, err := p.replaceComponent(ctx, tx, revisionID, logicalID, observationID, chunk, record)
		if err != nil || quality == QualityConflict || quality == QualityIncomplete {
			return err
		}
		deltaRecord := record
		deltaRecord.Delta = subtractCounters(record.Counters, currentCounters)
		return p.insertContribution(ctx, tx, revisionID, logicalID, currentObservationID,
			observationID, chunk, deltaRecord, contributionAdvance)
	default:
		_, err = tx.ExecContext(ctx, `
			UPDATE session_logical_usage_events
			SET fold_status = 'conflict', observation_count = observation_count + 1,
				fold_reason = $2, updated_at = now() WHERE id = $1`, logicalID, fold.Reason)
		return err
	}
}

func (p *Processor) replaceComponent(
	ctx context.Context,
	tx *sql.Tx,
	revisionID, logicalID, observationID string,
	chunk usageChunk,
	record UsageRecord,
) (QualityStatus, error) {
	normalized, err := NormalizeWithOptions(record, NormalizerOptions{ClaudeCacheWriteVariant: p.claudeCacheWriteVariant})
	if err != nil {
		status := QualityConflict
		if errors.Is(err, ErrClaudeCacheWriteVariantRequired) {
			status = QualityIncomplete
		}
		if _, updateErr := tx.ExecContext(ctx, `
			UPDATE session_logical_usage_events
			SET fold_status = $2, fold_reason = $3, updated_at = now() WHERE id = $1`,
			logicalID, status, err.Error()); updateErr != nil {
			return status, updateErr
		}
		return status, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_usage_components SET valid_to = now()
		WHERE revision_id = $1 AND logical_usage_event_id = $2 AND valid_to IS NULL`, revisionID, logicalID); err != nil {
		return record.Quality, err
	}
	billingVariant := billingVariantForRecord(record, p.claudeCacheWriteVariant)
	componentQuality := record.Quality
	if normalized.IsEstimated && componentQuality == QualityExact {
		componentQuality = QualityEstimated
	}
	memberSessionID := chunk.SessionID
	if record.OwnerSessionID != "" {
		memberSessionID = record.OwnerSessionID
	}
	assumptions, _ := json.Marshal(map[string]any{
		"quality_reason":       record.QualityReason,
		"request_input_tokens": record.Delta.RequestInputTokens,
	})
	_, err = tx.ExecContext(ctx, `
		INSERT INTO session_usage_components (
			revision_id, logical_usage_event_id, observation_id, chunk_id,
			session_id, user_id, activity_date, occurred_at, provider,
			raw_model, canonical_model, billing_variant,
			uncached_input_tokens, cache_read_tokens, cache_write_5m_tokens,
			cache_write_1h_tokens, output_tokens, normalized_total_tokens,
			normalization_strategy, quality_status, is_estimated, assumptions_json
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7::date, $8, $9,
			NULLIF($10, ''), NULLIF($10, ''), $11,
			$12, $13, $14, $15, $16, $17, $18, $19, $20, $21
		)`, revisionID, logicalID, observationID, chunk.ID, memberSessionID, chunk.UserID,
		biztime.Date(record.OccurredAt), record.OccurredAt, record.Provider, record.RawModel, billingVariant,
		normalized.UncachedInputTokens, normalized.CacheReadTokens, normalized.CacheWrite5mTokens,
		normalized.CacheWrite1hTokens, normalized.OutputTokens, normalized.TotalTokens,
		normalized.Strategy, componentQuality, normalized.IsEstimated, assumptions)
	return componentQuality, err
}

type revisionCounts struct {
	Observations int64
	Events       int64
	Advances     int64
	Duplicates   int64
	Conflicts    int64
	Components   int64
}

func revisionQualityAndCounts(ctx context.Context, tx *sql.Tx, revisionID string, newMalformed, newUnknown int64) (QualityStatus, revisionCounts, error) {
	var counts revisionCounts
	var incompleteEvents, estimatedObservations, incompleteObservations, conflictObservations int64
	var estimatedComponents, existingMalformed, existingUnknown int64
	err := tx.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM session_usage_observations WHERE revision_id = $1),
			COUNT(*), COALESCE(SUM(advance_count), 0), COALESCE(SUM(duplicate_observation_count), 0),
			COUNT(*) FILTER (WHERE fold_status = 'conflict'),
			COUNT(*) FILTER (WHERE fold_status = 'incomplete'),
			(SELECT COUNT(*) FROM session_usage_observations WHERE revision_id = $1 AND quality_status = 'estimated'),
			(SELECT COUNT(*) FROM session_usage_observations WHERE revision_id = $1 AND quality_status = 'incomplete'),
			(SELECT COUNT(*) FROM session_usage_observations WHERE revision_id = $1 AND quality_status = 'conflict'),
			(SELECT COUNT(*) FROM session_usage_components WHERE revision_id = $1 AND valid_to IS NULL),
			(SELECT COUNT(*) FROM session_usage_components WHERE revision_id = $1 AND valid_to IS NULL AND is_estimated),
			(SELECT malformed_event_count FROM session_metrics_revisions WHERE id = $1),
			(SELECT unknown_usage_event_count FROM session_metrics_revisions WHERE id = $1)
		FROM session_logical_usage_events WHERE revision_id = $1`, revisionID).Scan(
		&counts.Observations, &counts.Events, &counts.Advances, &counts.Duplicates,
		&counts.Conflicts, &incompleteEvents, &estimatedObservations,
		&incompleteObservations, &conflictObservations, &counts.Components,
		&estimatedComponents, &existingMalformed, &existingUnknown,
	)
	if err != nil {
		return "", counts, err
	}
	quality := QualityExact
	switch {
	case counts.Conflicts > 0 || conflictObservations > 0:
		quality = QualityConflict
	case incompleteEvents > 0 || incompleteObservations > 0 || existingMalformed+newMalformed > 0 || existingUnknown+newUnknown > 0:
		quality = QualityIncomplete
	case estimatedObservations > 0 || estimatedComponents > 0:
		quality = QualityEstimated
	}
	return quality, counts, nil
}

func (p *Processor) processActivation(ctx context.Context, job sessionsync.ProcessingJob) error {
	if !job.GenerationID.Valid {
		return ErrUsageUnavailable
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sessionsync.LockSessionForUpdate(ctx, tx, job.SessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "usage-revision:"+job.GenerationID.String); err != nil {
		return err
	}
	var revisionID, sourceID string
	var parsedCursor, highWater int64
	if job.TargetMetricsRevisionID.Valid {
		err = tx.QueryRowContext(ctx, `
			SELECT r.id, r.source_id, r.validated_through_cursor, g.expected_cursor
			FROM session_metrics_revisions r
			JOIN session_source_generations g ON g.id = r.generation_id
			WHERE r.id = $1 AND r.generation_id = $2
				AND r.parser_version = $3 AND r.normalizer_version = $4
			FOR UPDATE OF r, g`, job.TargetMetricsRevisionID.String, job.GenerationID.String,
			ParserVersion, p.normalizerVersion).Scan(&revisionID, &sourceID, &parsedCursor, &highWater)
	} else {
		err = tx.QueryRowContext(ctx, `
			SELECT r.id, r.source_id, r.validated_through_cursor, g.expected_cursor
			FROM session_metrics_revisions r
			JOIN session_source_generations g ON g.id = r.generation_id
			WHERE r.generation_id = $1 AND r.parser_version = $2 AND r.normalizer_version = $3
			FOR UPDATE OF r, g`, job.GenerationID.String, ParserVersion, p.normalizerVersion).Scan(
			&revisionID, &sourceID, &parsedCursor, &highWater)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUsageOutOfOrder
	}
	if err != nil {
		return err
	}
	if parsedCursor != highWater {
		return ErrUsageOutOfOrder
	}
	if err := p.activateIfReady(ctx, tx, revisionID, sourceID, job.GenerationID.String); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Processor) activateIfReady(ctx context.Context, tx *sql.Tx, revisionID, sourceID, generationID string) error {
	var revisionStatus string
	var quality QualityStatus
	var parsedCursor, highWater int64
	var generationStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT r.status, r.quality_status, r.validated_through_cursor, g.expected_cursor, g.status
		FROM session_metrics_revisions r
		JOIN session_source_generations g ON g.id = r.generation_id
		WHERE r.id = $1 AND r.generation_id = $2 FOR UPDATE OF r, g`, revisionID, generationID).Scan(
		&revisionStatus, &quality, &parsedCursor, &highWater, &generationStatus); err != nil {
		return err
	}
	if parsedCursor != highWater || generationStatus != "active" {
		return nil
	}
	if revisionStatus == "failed" || revisionStatus == "superseded" {
		return nil
	}
	if quality == QualityConflict || quality == QualityIncomplete {
		if revisionStatus == "active" {
			return fmt.Errorf("%w: active revision append is %s", ErrUsageQualityGate, quality)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_metrics_revisions
			SET status = 'failed', calculation_reason = $2 WHERE id = $1`, revisionID, "quality gate: "+string(quality)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE session_source_metrics_states
			SET status = 'error', source_high_water_cursor = $2,
				last_error = $3, updated_at = now() WHERE source_id = $1`,
			sourceID, highWater, "metrics revision quality gate: "+string(quality))
		return err
	}
	var oldRevisionID sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT active_revision_id FROM session_source_metrics_states WHERE source_id = $1 FOR UPDATE`, sourceID).Scan(&oldRevisionID); err != nil {
		return err
	}
	if oldRevisionID.Valid && oldRevisionID.String != revisionID {
		covered, reason, err := replacementCoversActiveRevision(ctx, tx, oldRevisionID.String, revisionID)
		if err != nil {
			return err
		}
		if !covered {
			if _, err := tx.ExecContext(ctx, `
				UPDATE session_metrics_revisions
				SET status = 'building', calculation_reason = $2 WHERE id = $1`, revisionID, reason); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `
				UPDATE session_source_metrics_states
				SET status = 'rebuilding', last_error = $2, updated_at = now()
				WHERE source_id = $1`, sourceID, reason)
			return err
		}
	}
	var claimUserID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT s.user_id
		FROM session_sources src
		JOIN sessions s ON s.id = src.session_id
		WHERE src.id = $1`, sourceID).Scan(&claimUserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		fmt.Sprintf("usage-claims:%d", claimUserID)); err != nil {
		return err
	}
	var crossSourceClaims, mismatchedCrossSourceClaims int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (
			WHERE candidate_observation.raw_usage_hash IS DISTINCT FROM claimed_observation.raw_usage_hash
		)
		FROM session_logical_usage_events e
		JOIN session_metrics_revisions r ON r.id = e.revision_id
		JOIN session_sources src ON src.id = r.source_id
		JOIN sessions s ON s.id = src.session_id
		JOIN session_usage_observations candidate_observation
			ON candidate_observation.id = e.current_observation_id
		JOIN session_usage_event_claims c
			ON c.user_id = s.user_id AND c.provider = e.provider
			AND c.provider_event_fingerprint = e.provider_event_fingerprint
		JOIN session_logical_usage_events claimed_event
			ON claimed_event.id = c.active_logical_usage_event_id
		JOIN session_usage_observations claimed_observation
			ON claimed_observation.id = claimed_event.current_observation_id
		WHERE e.revision_id = $1 AND c.active_source_id <> $2`, revisionID, sourceID).Scan(
		&crossSourceClaims, &mismatchedCrossSourceClaims); err != nil {
		return err
	}
	if mismatchedCrossSourceClaims > 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_metrics_revisions
			SET status = 'failed', quality_status = 'conflict', conflict_usage_event_count = conflict_usage_event_count + $2,
				calculation_reason = 'provider event differs from the event claimed by another source'
			WHERE id = $1`, revisionID, mismatchedCrossSourceClaims); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE session_source_metrics_states
			SET status = 'error', last_error = 'provider event claim content conflict', updated_at = now()
			WHERE source_id = $1`, sourceID)
		return err
	}
	if crossSourceClaims > 0 {
		if err := suppressClaimedUsageEvents(ctx, tx, revisionID, sourceID, claimUserID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_metrics_revisions
			SET duplicate_usage_event_count = duplicate_usage_event_count + $2,
				calculation_reason = format('suppressed %s usage events already claimed by another source', $2),
				reconciliation_json = reconciliation_json || jsonb_build_object(
					'cross_source_suppressed_events', $2::bigint,
					'active_components', (
						SELECT COUNT(*) FROM session_usage_components
						WHERE revision_id = $1 AND valid_to IS NULL
					)
				)
			WHERE id = $1`, revisionID, crossSourceClaims); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_usage_event_claims (
			user_id, provider, provider_event_fingerprint,
			active_source_id, active_generation_id, active_revision_id, active_logical_usage_event_id
		)
		SELECT s.user_id, e.provider, e.provider_event_fingerprint,
			r.source_id, r.generation_id, r.id, e.id
		FROM session_logical_usage_events e
		JOIN session_metrics_revisions r ON r.id = e.revision_id
		JOIN session_sources src ON src.id = r.source_id
		JOIN sessions s ON s.id = src.session_id
		WHERE e.revision_id = $1 AND e.fold_status = 'current'
		ON CONFLICT (user_id, provider, provider_event_fingerprint) DO UPDATE
		SET active_generation_id = EXCLUDED.active_generation_id,
			active_revision_id = EXCLUDED.active_revision_id,
			active_logical_usage_event_id = EXCLUDED.active_logical_usage_event_id,
			transferred_at = CASE
				WHEN session_usage_event_claims.active_revision_id <> EXCLUDED.active_revision_id THEN now()
				ELSE session_usage_event_claims.transferred_at END
		WHERE session_usage_event_claims.active_source_id = EXCLUDED.active_source_id`, revisionID); err != nil {
		return err
	}
	if oldRevisionID.Valid && oldRevisionID.String != revisionID {
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_metrics_revisions SET status = 'superseded', superseded_at = now()
			WHERE id = $1 AND status = 'active'`, oldRevisionID.String); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_usage_components SET valid_to = now() WHERE revision_id = $1 AND valid_to IS NULL`, oldRevisionID.String); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_daily_usage SET valid_to = now() WHERE revision_id = $1 AND valid_to IS NULL`, oldRevisionID.String); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM session_usage_event_claims WHERE active_revision_id = $1`, oldRevisionID.String); err != nil {
			return err
		}
	}
	if revisionStatus != "active" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE session_metrics_revisions
			SET status = 'active', validated_at = COALESCE(validated_at, now()), activated_at = now(),
				source_high_water_cursor = $2
			WHERE id = $1`, revisionID, highWater); err != nil {
			return err
		}
	}
	if err := rebuildDailyUsage(ctx, tx, revisionID); err != nil {
		return err
	}
	if _, err := pricing.RecalculateRevisionTx(ctx, tx, revisionID); err != nil {
		return err
	}
	if _, err := pricing.EnsureContributionRevisionCostsTx(ctx, tx, revisionID); err != nil {
		return err
	}
	var rollupSessionID string
	if err := tx.QueryRowContext(ctx, `SELECT session_id FROM session_sources WHERE id = $1`, sourceID).Scan(&rollupSessionID); err != nil {
		return err
	}
	if err := p.rollups.BuildForActivation(ctx, tx, rollupSessionID, sourceID,
		revisionID, pricing.CalculatorVersion); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE session_source_metrics_states
		SET active_revision_id = $2, target_generation_id = $4,
			status = 'ready', active_usage_parsed_cursor = $3,
			source_high_water_cursor = $3, last_error = NULL, updated_at = now()
		WHERE source_id = $1`, sourceID, revisionID, highWater, generationID)
	return err
}

func suppressClaimedUsageEvents(
	ctx context.Context,
	tx *sql.Tx,
	revisionID, sourceID string,
	userID int64,
) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM session_usage_contributions contribution
		WHERE contribution.revision_id = $1
			AND EXISTS (
				SELECT 1
				FROM session_logical_usage_events event
				JOIN session_usage_event_claims claim
					ON claim.user_id = $2 AND claim.provider = event.provider
					AND claim.provider_event_fingerprint = event.provider_event_fingerprint
				WHERE event.id = contribution.logical_usage_event_id
					AND event.revision_id = $1 AND claim.active_source_id <> $3
			)`, revisionID, userID, sourceID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE session_usage_components component
		SET valid_to = now()
		WHERE component.revision_id = $1 AND component.valid_to IS NULL
			AND EXISTS (
				SELECT 1
				FROM session_logical_usage_events event
				JOIN session_usage_event_claims claim
					ON claim.user_id = $2 AND claim.provider = event.provider
					AND claim.provider_event_fingerprint = event.provider_event_fingerprint
				WHERE event.id = component.logical_usage_event_id
					AND event.revision_id = $1 AND claim.active_source_id <> $3
			)`, revisionID, userID, sourceID)
	return err
}

func replacementCoversActiveRevision(ctx context.Context, tx *sql.Tx, oldRevisionID, newRevisionID string) (bool, string, error) {
	var oldGenerationID, newGenerationID string
	if err := tx.QueryRowContext(ctx, `
		SELECT old.generation_id, candidate.generation_id
		FROM session_metrics_revisions old
		JOIN session_metrics_revisions candidate ON candidate.id = $2
		WHERE old.id = $1`, oldRevisionID, newRevisionID).Scan(&oldGenerationID, &newGenerationID); err != nil {
		return false, "", err
	}
	if oldGenerationID == newGenerationID {
		return true, "", nil
	}

	var missingStableFacts int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM session_logical_usage_events old
		WHERE old.revision_id = $1 AND old.fold_status = 'current'
			AND old.identity_strategy = 'message.id'
			AND NOT EXISTS (
				SELECT 1 FROM session_logical_usage_events candidate
				WHERE candidate.revision_id = $2 AND candidate.fold_status = 'current'
					AND candidate.provider = old.provider
					AND candidate.provider_event_fingerprint = old.provider_event_fingerprint
			)`, oldRevisionID, newRevisionID).Scan(&missingStableFacts); err != nil {
		return false, "", err
	}
	if missingStableFacts > 0 {
		return false, fmt.Sprintf("replacement revision is missing %d stable provider facts", missingStableFacts), nil
	}

	var regressedGroups int64
	if err := tx.QueryRowContext(ctx, `
		WITH old_unstable AS (
			SELECT c.provider, c.activity_date, COALESCE(c.canonical_model, '') AS model,
				SUM(c.uncached_input_tokens) AS uncached_input,
				SUM(c.cache_read_tokens) AS cache_read,
				SUM(c.cache_write_5m_tokens) AS cache_write_5m,
				SUM(c.cache_write_1h_tokens) AS cache_write_1h,
				SUM(c.output_tokens) AS output
			FROM session_usage_components c
			JOIN session_logical_usage_events e ON e.id = c.logical_usage_event_id
			WHERE c.revision_id = $1 AND c.valid_to IS NULL AND e.identity_strategy <> 'message.id'
			GROUP BY c.provider, c.activity_date, COALESCE(c.canonical_model, '')
		), new_unstable AS (
			SELECT c.provider, c.activity_date, COALESCE(c.canonical_model, '') AS model,
				SUM(c.uncached_input_tokens) AS uncached_input,
				SUM(c.cache_read_tokens) AS cache_read,
				SUM(c.cache_write_5m_tokens) AS cache_write_5m,
				SUM(c.cache_write_1h_tokens) AS cache_write_1h,
				SUM(c.output_tokens) AS output
			FROM session_usage_components c
			JOIN session_logical_usage_events e ON e.id = c.logical_usage_event_id
			WHERE c.revision_id = $2 AND c.valid_to IS NULL AND e.identity_strategy <> 'message.id'
			GROUP BY c.provider, c.activity_date, COALESCE(c.canonical_model, '')
		)
		SELECT COUNT(*)
		FROM old_unstable old
		LEFT JOIN new_unstable candidate USING (provider, activity_date, model)
		WHERE candidate.provider IS NULL
			OR candidate.uncached_input < old.uncached_input
			OR candidate.cache_read < old.cache_read
			OR candidate.cache_write_5m < old.cache_write_5m
			OR candidate.cache_write_1h < old.cache_write_1h
			OR candidate.output < old.output`, oldRevisionID, newRevisionID).Scan(&regressedGroups); err != nil {
		return false, "", err
	}
	if regressedGroups > 0 {
		return false, fmt.Sprintf("replacement revision regresses %d non-stable usage groups", regressedGroups), nil
	}
	return true, "", nil
}

func rebuildDailyUsage(ctx context.Context, tx *sql.Tx, revisionID string) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_daily_usage SET valid_to = now()
		WHERE revision_id = $1 AND valid_to IS NULL`, revisionID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO session_daily_usage (
			revision_id, session_id, user_id, team_id_snapshot, department_id_snapshot,
			activity_date, provider, canonical_model, billing_variant,
			uncached_input_tokens, cache_read_tokens, cache_write_5m_tokens,
			cache_write_1h_tokens, output_tokens, total_tokens, quality_status
		)
		SELECT revision_id, session_id, user_id, team_id_snapshot, department_id_snapshot,
			activity_date, provider, canonical_model, billing_variant,
			SUM(uncached_input_tokens), SUM(cache_read_tokens), SUM(cache_write_5m_tokens),
			SUM(cache_write_1h_tokens), SUM(output_tokens), SUM(normalized_total_tokens),
			CASE
				WHEN BOOL_OR(quality_status = 'conflict') THEN 'conflict'
				WHEN BOOL_OR(quality_status = 'incomplete') THEN 'incomplete'
				WHEN BOOL_OR(quality_status = 'estimated') THEN 'estimated'
				ELSE 'exact'
			END
		FROM session_usage_components
		WHERE revision_id = $1 AND valid_to IS NULL
		GROUP BY revision_id, session_id, user_id, team_id_snapshot, department_id_snapshot,
			activity_date, provider, canonical_model, billing_variant`, revisionID)
	return err
}
