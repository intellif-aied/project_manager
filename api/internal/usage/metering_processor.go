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
	"hash"
	"io"
	"time"

	"github.com/aidashboard/api/internal/reportsourcecatalog"
	"github.com/aidashboard/api/internal/sessionsync"
)

const MeteringEnvelopeVersion = "metering-envelope-v1"

var ErrMeteringEnvelopeUnsafe = errors.New("metering envelope cannot prove complete usage coverage")

type MutableObjectStore interface {
	ObjectStore
	Delete(context.Context, string) error
}

type MeteringProcessor struct {
	db    *sql.DB
	store MutableObjectStore
}

func NewMeteringProcessor(database *sql.DB, store MutableObjectStore) (*MeteringProcessor, error) {
	if database == nil || store == nil {
		return nil, errors.New("database and mutable object store are required")
	}
	return &MeteringProcessor{db: database, store: store}, nil
}

func (p *MeteringProcessor) Process(ctx context.Context, job sessionsync.ProcessingJob) error {
	switch job.Type {
	case sessionsync.JobBuildMeteringEnvelope:
		return p.buildEnvelope(ctx, job)
	case sessionsync.JobDeleteObject:
		return p.deleteObject(ctx, job)
	default:
		return fmt.Errorf("unsupported metering job type %q", job.Type)
	}
}

type meteringGeneration struct {
	ID             string
	SessionID      string
	Provider       string
	SourceFormat   string
	ExpectedCursor int64
	PrefixHash     string
	ContentStatus  string
	ContentEpoch   int64
}

type meteringChunk struct {
	ID           string
	StartCursor  int64
	EndCursor    int64
	ContentHash  string
	ObjectKey    string
	ObjectStatus string
}

type meteringRecord struct {
	ChunkID  string
	Record   UsageRecord
	LineHash string
}

type meteringScan struct {
	Generation       meteringGeneration
	Chunks           []meteringChunk
	Records          []meteringRecord
	ChunkLineCounts  map[string]int64
	ChunkUsageCounts map[string]int64
	SourceLineCount  int64
	SourceChecksum   string
	EnvelopeChecksum string
}

func (p *MeteringProcessor) buildEnvelope(ctx context.Context, job sessionsync.ProcessingJob) error {
	if !job.GenerationID.Valid || !job.ContentEpoch.Valid {
		return ErrUsageUnavailable
	}
	validated, err := p.manifestAlreadyValidated(ctx, job.GenerationID.String, job.ContentEpoch.Int64)
	if err != nil {
		return err
	}
	if validated {
		return p.enqueueDeletesIfReady(ctx, job.SessionID, job.ContentEpoch.Int64)
	}
	scan, err := p.scanGeneration(ctx, job.GenerationID.String, job.ContentEpoch.Int64)
	if err != nil {
		if errors.Is(err, ErrMeteringEnvelopeUnsafe) {
			return p.recordEnvelopeFailure(ctx, job, err.Error())
		}
		return err
	}
	if err := p.persistEnvelope(ctx, job, scan); err != nil {
		return err
	}
	return p.enqueueDeletesIfReady(ctx, job.SessionID, job.ContentEpoch.Int64)
}

func (p *MeteringProcessor) manifestAlreadyValidated(ctx context.Context, generationID string, epoch int64) (bool, error) {
	var validated bool
	err := p.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM session_metering_envelope_manifests m
			JOIN session_source_generations g ON g.id = m.generation_id
			WHERE m.generation_id = $1 AND m.content_epoch = $2
				AND m.envelope_version = $3 AND m.status = 'validated'
				AND m.metering_exported_cursor = g.expected_cursor
				AND m.source_high_water_cursor = g.expected_cursor
				AND m.potential_usage_record_count = m.envelope_record_count
				AND m.source_checksum IS NOT NULL AND m.envelope_checksum IS NOT NULL
		)`, generationID, epoch, MeteringEnvelopeVersion).Scan(&validated)
	return validated, err
}

func (p *MeteringProcessor) scanGeneration(ctx context.Context, generationID string, epoch int64) (meteringScan, error) {
	var scan meteringScan
	err := p.db.QueryRowContext(ctx, `
		SELECT g.id, s.id, s.agent_type, src.source_format, g.expected_cursor, g.prefix_checkpoint_hash,
			s.content_status, s.content_epoch
		FROM session_source_generations g
		JOIN session_sources src ON src.id = g.source_id
		JOIN sessions s ON s.id = src.session_id
		WHERE g.id = $1`, generationID).Scan(
		&scan.Generation.ID, &scan.Generation.SessionID, &scan.Generation.Provider, &scan.Generation.SourceFormat,
		&scan.Generation.ExpectedCursor, &scan.Generation.PrefixHash,
		&scan.Generation.ContentStatus, &scan.Generation.ContentEpoch,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return meteringScan{}, ErrUsageUnavailable
	}
	if err != nil {
		return meteringScan{}, err
	}
	if scan.Generation.ContentStatus != string(sessionsync.ContentClearing) || scan.Generation.ContentEpoch != epoch {
		return meteringScan{}, ErrStaleMeteringEpoch
	}
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, start_cursor, end_cursor, content_sha256, raw_object_key, object_status
		FROM session_upload_chunks
		WHERE generation_id = $1 ORDER BY start_cursor`, generationID)
	if err != nil {
		return meteringScan{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var chunk meteringChunk
		if err := rows.Scan(&chunk.ID, &chunk.StartCursor, &chunk.EndCursor, &chunk.ContentHash, &chunk.ObjectKey, &chunk.ObjectStatus); err != nil {
			return meteringScan{}, err
		}
		scan.Chunks = append(scan.Chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return meteringScan{}, err
	}
	if len(scan.Chunks) == 0 {
		return meteringScan{}, fmt.Errorf("%w: generation has no chunks", ErrMeteringEnvelopeUnsafe)
	}

	scan.ChunkLineCounts = map[string]int64{}
	scan.ChunkUsageCounts = map[string]int64{}
	sourceHasher := sha256.New()
	envelopeHasher := sha256.New()
	state := ParseState{}
	expectedCursor := int64(0)
	for _, chunk := range scan.Chunks {
		if chunk.StartCursor != expectedCursor || chunk.ObjectStatus != "available" {
			return meteringScan{}, fmt.Errorf("%w: chunk coverage/status is incomplete", ErrMeteringEnvelopeUnsafe)
		}
		content, err := p.readVerifiedChunk(ctx, chunk)
		if err != nil {
			return meteringScan{}, err
		}
		_, _ = sourceHasher.Write(content)
		var parsed ParseResult
		if scan.Generation.SourceFormat == "aida_event_v1" {
			parsed, err = ParseCanonicalChunk(scan.Generation.Provider, bytes.NewReader(content), chunk.StartCursor, state)
		} else {
			parsed, err = ParseProviderChunk(scan.Generation.Provider, bytes.NewReader(content), chunk.StartCursor, state)
		}
		if err != nil {
			return meteringScan{}, fmt.Errorf("%w: %v", ErrMeteringEnvelopeUnsafe, err)
		}
		if parsed.EndCursor != chunk.EndCursor || parsed.MalformedCount > 0 || parsed.UnknownUsageCount > 0 {
			return meteringScan{}, fmt.Errorf(
				"%w: chunk %s has malformed=%d unknown=%d",
				ErrMeteringEnvelopeUnsafe, chunk.ID, parsed.MalformedCount, parsed.UnknownUsageCount,
			)
		}
		state = parsed.State
		scan.SourceLineCount += parsed.ScannedLineCount
		scan.ChunkLineCounts[chunk.ID] = parsed.ScannedLineCount
		scan.ChunkUsageCounts[chunk.ID] = int64(len(parsed.Records))
		for _, record := range parsed.Records {
			localStart := record.SourceStartCursor - chunk.StartCursor
			localEnd := record.SourceEndCursor - chunk.StartCursor
			if localStart < 0 || localEnd > int64(len(content)) || localEnd <= localStart {
				return meteringScan{}, fmt.Errorf("%w: usage record is outside chunk", ErrMeteringEnvelopeUnsafe)
			}
			entry := meteringRecord{
				ChunkID: chunk.ID, Record: record,
				LineHash: hashBytes(content[localStart:localEnd]),
			}
			scan.Records = append(scan.Records, entry)
			writeEnvelopeChecksum(envelopeHasher, entry)
		}
		expectedCursor = chunk.EndCursor
	}
	if expectedCursor != scan.Generation.ExpectedCursor {
		return meteringScan{}, fmt.Errorf("%w: exported cursor=%d highwater=%d", ErrMeteringEnvelopeUnsafe, expectedCursor, scan.Generation.ExpectedCursor)
	}
	scan.SourceChecksum = hex.EncodeToString(sourceHasher.Sum(nil))
	if scan.SourceChecksum != scan.Generation.PrefixHash {
		return meteringScan{}, fmt.Errorf("%w: source checksum does not match generation checkpoint", ErrMeteringEnvelopeUnsafe)
	}
	scan.EnvelopeChecksum = hex.EncodeToString(envelopeHasher.Sum(nil))
	return scan, nil
}

func (p *MeteringProcessor) readVerifiedChunk(ctx context.Context, chunk meteringChunk) ([]byte, error) {
	object, err := p.store.Download(ctx, chunk.ObjectKey)
	if err != nil {
		return nil, err
	}
	defer object.Close()
	want := chunk.EndCursor - chunk.StartCursor
	content, err := io.ReadAll(io.LimitReader(object, want+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) != want || hashBytes(content) != chunk.ContentHash {
		return nil, fmt.Errorf("%w: chunk size/hash mismatch", ErrMeteringEnvelopeUnsafe)
	}
	return content, nil
}

func writeEnvelopeChecksum(hasher hash.Hash, entry meteringRecord) {
	counters, _ := json.Marshal(entry.Record.Counters)
	payload, _ := json.Marshal(struct {
		ChunkID         string          `json:"chunk_id"`
		Start           int64           `json:"start"`
		End             int64           `json:"end"`
		Provider        string          `json:"provider"`
		EventKey        string          `json:"event_key"`
		Fingerprint     string          `json:"fingerprint"`
		OccurredAt      time.Time       `json:"occurred_at"`
		Model           string          `json:"model"`
		RawUsage        json.RawMessage `json:"raw_usage"`
		Counters        json.RawMessage `json:"counters"`
		RawUsageHash    string          `json:"raw_usage_hash"`
		OwnerSessionRef string          `json:"owner_session_ref,omitempty"`
		SourceLineHash  string          `json:"source_line_hash"`
	}{
		ChunkID: entry.ChunkID, Start: entry.Record.SourceStartCursor, End: entry.Record.SourceEndCursor,
		Provider: entry.Record.Provider, EventKey: entry.Record.EventKey,
		Fingerprint: entry.Record.ProviderFingerprint, OccurredAt: entry.Record.OccurredAt,
		Model: entry.Record.RawModel, RawUsage: entry.Record.RawUsage, Counters: counters,
		RawUsageHash: entry.Record.RawUsageHash, OwnerSessionRef: entry.Record.OwnerSessionRef,
		SourceLineHash: entry.LineHash,
	})
	_, _ = hasher.Write(payload)
	_, _ = hasher.Write([]byte{'\n'})
}

func (p *MeteringProcessor) persistEnvelope(ctx context.Context, job sessionsync.ProcessingJob, scan meteringScan) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	var epoch, highWater int64
	if err := tx.QueryRowContext(ctx, `
		SELECT s.content_status, s.content_epoch, g.expected_cursor
		FROM session_source_generations g
		JOIN session_sources src ON src.id = g.source_id
		JOIN sessions s ON s.id = src.session_id
		WHERE g.id = $1 AND s.id = $2
		FOR UPDATE OF s, g`, scan.Generation.ID, scan.Generation.SessionID).Scan(&status, &epoch, &highWater); err != nil {
		return err
	}
	if status != string(sessionsync.ContentClearing) || epoch != job.ContentEpoch.Int64 || highWater != scan.Generation.ExpectedCursor {
		return ErrStaleMeteringEpoch
	}
	var manifestID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO session_metering_envelope_manifests (
			generation_id, content_epoch, envelope_version, status,
			metering_exported_cursor, source_high_water_cursor,
			source_record_count, potential_usage_record_count, envelope_record_count,
			source_checksum, envelope_checksum, failure_reason, validated_at
		) VALUES ($1, $2, $3, 'validated', $4, $4, $5, $6, $6, $7, $8, NULL, now())
		ON CONFLICT (generation_id, content_epoch, envelope_version) DO UPDATE
		SET status = 'validated', metering_exported_cursor = EXCLUDED.metering_exported_cursor,
			source_high_water_cursor = EXCLUDED.source_high_water_cursor,
			source_record_count = EXCLUDED.source_record_count,
			potential_usage_record_count = EXCLUDED.potential_usage_record_count,
			envelope_record_count = EXCLUDED.envelope_record_count,
			source_checksum = EXCLUDED.source_checksum,
			envelope_checksum = EXCLUDED.envelope_checksum,
			failure_reason = NULL, validated_at = now()
		RETURNING id`, scan.Generation.ID, job.ContentEpoch.Int64, MeteringEnvelopeVersion,
		scan.Generation.ExpectedCursor, scan.SourceLineCount, len(scan.Records),
		scan.SourceChecksum, scan.EnvelopeChecksum).Scan(&manifestID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM session_metering_envelope_chunks WHERE manifest_id = $1`, manifestID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM session_metering_envelopes WHERE manifest_id = $1`, manifestID); err != nil {
		return err
	}
	for _, chunk := range scan.Chunks {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO session_metering_envelope_chunks (
				manifest_id, generation_id, chunk_id, source_start_cursor, source_end_cursor,
				source_record_count, potential_usage_record_count, source_checksum
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			manifestID, scan.Generation.ID, chunk.ID, chunk.StartCursor, chunk.EndCursor,
			scan.ChunkLineCounts[chunk.ID], scan.ChunkUsageCounts[chunk.ID], chunk.ContentHash); err != nil {
			return err
		}
	}
	for _, entry := range scan.Records {
		counters, _ := json.Marshal(entry.Record.Counters)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO session_metering_envelopes (
				manifest_id, generation_id, chunk_id, source_start_cursor, source_end_cursor,
				provider, usage_event_key, identity_strategy, provider_event_fingerprint,
				occurred_at, raw_model, raw_usage_json, parsed_counters_json,
				raw_usage_hash, source_record_hash, quality_status, quality_reason, envelope_version,
				owner_session_ref
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, ''),
				$12, $13, $14, $15, $16, NULLIF($17, ''), $18, NULLIF($19, '')
			)`, manifestID, scan.Generation.ID, entry.ChunkID,
			entry.Record.SourceStartCursor, entry.Record.SourceEndCursor,
			entry.Record.Provider, entry.Record.EventKey, entry.Record.IdentityStrategy,
			entry.Record.ProviderFingerprint, entry.Record.OccurredAt, entry.Record.RawModel,
			[]byte(entry.Record.RawUsage), counters, entry.Record.RawUsageHash, entry.LineHash,
			entry.Record.Quality, entry.Record.QualityReason, MeteringEnvelopeVersion,
			entry.Record.OwnerSessionRef); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (p *MeteringProcessor) recordEnvelopeFailure(ctx context.Context, job sessionsync.ProcessingJob, reason string) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_metering_envelope_manifests (
			generation_id, content_epoch, envelope_version, status, failure_reason
		) VALUES ($1, $2, $3, 'failed', $4)
		ON CONFLICT (generation_id, content_epoch, envelope_version) DO UPDATE
		SET status = 'failed', failure_reason = EXCLUDED.failure_reason`,
		job.GenerationID.String, job.ContentEpoch.Int64, MeteringEnvelopeVersion, reason); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE sessions SET content_status = 'clearing_failed', updated_at = now()
		WHERE id = $1 AND content_status = 'clearing' AND content_epoch = $2`,
		job.SessionID, job.ContentEpoch.Int64); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *MeteringProcessor) enqueueDeletesIfReady(ctx context.Context, sessionID string, epoch int64) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	var currentEpoch int64
	if err := tx.QueryRowContext(ctx, `SELECT content_status, content_epoch FROM sessions WHERE id = $1 FOR UPDATE`, sessionID).Scan(&status, &currentEpoch); err != nil {
		return err
	}
	if status != string(sessionsync.ContentClearing) || currentEpoch != epoch {
		return nil
	}
	var invalidManifests int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM (
			SELECT DISTINCT g.id, g.expected_cursor
			FROM session_source_generations g
			JOIN session_sources src ON src.id = g.source_id
			JOIN session_upload_chunks c ON c.generation_id = g.id
			WHERE src.session_id = $1
		) generations
		LEFT JOIN session_metering_envelope_manifests m
			ON m.generation_id = generations.id AND m.content_epoch = $2
			AND m.envelope_version = $3 AND m.status = 'validated'
		WHERE m.id IS NULL OR m.metering_exported_cursor <> generations.expected_cursor
			OR m.source_high_water_cursor <> generations.expected_cursor
			OR m.potential_usage_record_count <> m.envelope_record_count
			OR m.source_checksum IS NULL OR m.envelope_checksum IS NULL`,
		sessionID, epoch, MeteringEnvelopeVersion).Scan(&invalidManifests); err != nil {
		return err
	}
	if invalidManifests > 0 {
		return ErrUsageOutOfOrder
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_processing_jobs (
			job_type, session_id, generation_id, chunk_id, content_epoch
		)
		SELECT $3, $1, c.generation_id, c.id, $2
		FROM session_upload_chunks c
		JOIN session_source_generations g ON g.id = c.generation_id
		JOIN session_sources src ON src.id = g.source_id
		WHERE src.session_id = $1 AND c.object_status IN ('available', 'delete_pending')
		ON CONFLICT DO NOTHING`, sessionID, epoch, sessionsync.JobDeleteObject); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *MeteringProcessor) deleteObject(ctx context.Context, job sessionsync.ProcessingJob) error {
	if !job.ChunkID.Valid || !job.GenerationID.Valid || !job.ContentEpoch.Valid {
		return ErrUsageUnavailable
	}
	var objectKey, objectStatus, contentStatus string
	var epoch int64
	err := p.db.QueryRowContext(ctx, `
		SELECT c.raw_object_key, c.object_status, s.content_status, s.content_epoch
		FROM session_upload_chunks c
		JOIN session_source_generations g ON g.id = c.generation_id
		JOIN session_sources src ON src.id = g.source_id
		JOIN sessions s ON s.id = src.session_id
		WHERE c.id = $1 AND c.generation_id = $2 AND s.id = $3`,
		job.ChunkID.String, job.GenerationID.String, job.SessionID).Scan(
		&objectKey, &objectStatus, &contentStatus, &epoch,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if objectStatus == "deleted" {
		return p.finalizeClearIfReady(ctx, job.SessionID, job.ContentEpoch.Int64)
	}
	if contentStatus != string(sessionsync.ContentClearing) || epoch != job.ContentEpoch.Int64 {
		return nil
	}
	ready, err := p.allManifestsReady(ctx, job.SessionID, epoch)
	if err != nil {
		return err
	}
	if !ready {
		return ErrUsageOutOfOrder
	}
	if _, err := p.db.ExecContext(ctx, `
		UPDATE session_upload_chunks SET object_status = 'delete_pending'
		WHERE id = $1 AND object_status = 'available'`, job.ChunkID.String); err != nil {
		return err
	}
	if err := p.store.Delete(ctx, objectKey); err != nil {
		return err
	}
	if _, err := p.db.ExecContext(ctx, `
		UPDATE session_upload_chunks SET object_status = 'deleted'
		WHERE id = $1 AND object_status IN ('available', 'delete_pending')`, job.ChunkID.String); err != nil {
		return err
	}
	return p.finalizeClearIfReady(ctx, job.SessionID, epoch)
}

func (p *MeteringProcessor) allManifestsReady(ctx context.Context, sessionID string, epoch int64) (bool, error) {
	var ready bool
	err := p.db.QueryRowContext(ctx, `
		SELECT NOT EXISTS (
			SELECT 1
			FROM (
				SELECT DISTINCT g.id, g.expected_cursor
				FROM session_source_generations g
				JOIN session_sources src ON src.id = g.source_id
				JOIN session_upload_chunks c ON c.generation_id = g.id
				WHERE src.session_id = $1
			) generations
			LEFT JOIN session_metering_envelope_manifests m
				ON m.generation_id = generations.id AND m.content_epoch = $2
				AND m.envelope_version = $3 AND m.status = 'validated'
			WHERE m.id IS NULL OR m.metering_exported_cursor <> generations.expected_cursor
				OR m.source_high_water_cursor <> generations.expected_cursor
				OR m.potential_usage_record_count <> m.envelope_record_count
				OR m.source_checksum IS NULL OR m.envelope_checksum IS NULL
		)`, sessionID, epoch, MeteringEnvelopeVersion).Scan(&ready)
	return ready, err
}

func (p *MeteringProcessor) finalizeClearIfReady(ctx context.Context, sessionID string, epoch int64) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	var currentEpoch, remaining int64
	if err := tx.QueryRowContext(ctx, `
		SELECT content_status, content_epoch FROM sessions WHERE id = $1 FOR UPDATE`, sessionID).Scan(&status, &currentEpoch); err != nil {
		return err
	}
	if status != string(sessionsync.ContentClearing) || currentEpoch != epoch {
		return nil
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM session_upload_chunks c
		JOIN session_source_generations g ON g.id = c.generation_id
		JOIN session_sources src ON src.id = g.source_id
		WHERE src.session_id = $1 AND c.object_status <> 'deleted'`, sessionID).Scan(&remaining); err != nil {
		return err
	}
	if remaining > 0 {
		return nil
	}
	ready, err := p.allManifestsReadyTx(ctx, tx, sessionID, epoch)
	if err != nil {
		return err
	}
	if !ready {
		return ErrUsageOutOfOrder
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_sources SET active_content_projection_revision_id = NULL, updated_at = now()
		WHERE session_id = $1`, sessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_content_projection_revisions
		SET status = 'superseded', superseded_at = COALESCE(superseded_at, now())
		WHERE generation_id IN (
			SELECT g.id FROM session_source_generations g
			JOIN session_sources src ON src.id = g.source_id WHERE src.session_id = $1
		) AND status = 'active'`, sessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_slice_digest_revisions digest
		SET status = 'superseded', superseded_at = COALESCE(superseded_at, now())
		WHERE digest.session_content_slice_id IN (
			SELECT slice.id FROM session_content_slices slice WHERE slice.session_id = $1
		) AND digest.status IN ('pending', 'building', 'ready', 'failed')`, sessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM session_content_events
		WHERE content_projection_revision_id IN (
			SELECT p.id FROM session_content_projection_revisions p
			JOIN session_source_generations g ON g.id = p.generation_id
			JOIN session_sources src ON src.id = g.source_id WHERE src.session_id = $1
		)`, sessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_activity_slices
		SET summary = NULL, excerpt = NULL, tool_calls_json = '{}'::jsonb,
			git_commits = '{}', source_has_raw_log = false, updated_at = now()
		WHERE session_id = $1`, sessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE sessions
		SET content_status = 'cleared', content_cleared_at = now(),
			summary = NULL, tool_calls_json = NULL, git_commits = NULL,
			raw_log_url = NULL, updated_at = now()
		WHERE id = $1 AND content_epoch = $2`, sessionID, epoch); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_content_tombstones SET objects_deleted_at = now()
		WHERE session_id = $1 AND restored_at IS NULL`, sessionID); err != nil {
		return err
	}
	if err := reportsourcecatalog.MarkSessionCleared(ctx, tx, sessionID); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *MeteringProcessor) allManifestsReadyTx(ctx context.Context, tx *sql.Tx, sessionID string, epoch int64) (bool, error) {
	var ready bool
	err := tx.QueryRowContext(ctx, `
		SELECT NOT EXISTS (
			SELECT 1
			FROM (
				SELECT DISTINCT g.id, g.expected_cursor
				FROM session_source_generations g
				JOIN session_sources src ON src.id = g.source_id
				JOIN session_upload_chunks c ON c.generation_id = g.id
				WHERE src.session_id = $1
			) generations
			LEFT JOIN session_metering_envelope_manifests m
				ON m.generation_id = generations.id AND m.content_epoch = $2
				AND m.envelope_version = $3 AND m.status = 'validated'
			WHERE m.id IS NULL OR m.metering_exported_cursor <> generations.expected_cursor
				OR m.source_high_water_cursor <> generations.expected_cursor
				OR m.potential_usage_record_count <> m.envelope_record_count
				OR m.source_checksum IS NULL OR m.envelope_checksum IS NULL
		)`, sessionID, epoch, MeteringEnvelopeVersion).Scan(&ready)
	return ready, err
}

var ErrStaleMeteringEpoch = errors.New("metering job content epoch is stale")
