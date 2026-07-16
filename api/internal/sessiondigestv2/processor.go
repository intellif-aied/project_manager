package sessiondigestv2

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"strings"

	"github.com/aidashboard/api/internal/sessionsync"
)

type Processor struct {
	db     *sql.DB
	config Config
}

func NewProcessor(database *sql.DB, config Config) (*Processor, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	normalized, err := config.Normalized()
	if err != nil {
		return nil, err
	}
	return &Processor{db: database, config: normalized}, nil
}

func (p *Processor) Process(ctx context.Context, job sessionsync.ProcessingJob) error {
	if job.Type != JobType || !job.TargetDigestRevisionID.Valid ||
		!job.ContentEpoch.Valid || !job.GenerationID.Valid {
		return ErrDigestUnavailable
	}
	target, err := p.loadTarget(ctx, job)
	if err != nil {
		if errors.Is(err, ErrStaleDigestSource) {
			if markErr := p.markSuperseded(ctx, job.TargetDigestRevisionID.String); markErr != nil {
				return ErrDigestStatePersistence
			}
			return err
		}
		return p.recordFailure(ctx, job, err)
	}
	if target.Ready {
		return nil
	}
	if err := p.ensureCurrent(ctx, target.Revision); err != nil {
		if errors.Is(err, ErrStaleDigestSource) {
			if markErr := p.markSuperseded(ctx, target.Revision.ID); markErr != nil {
				return ErrDigestStatePersistence
			}
			return err
		}
		return p.recordFailure(ctx, job, err)
	}

	extractor := NewExtractor()
	sourceHasher := sha256.New()
	rows, err := p.db.QueryContext(ctx, safeEventProjectionSQL,
		target.Revision.ProjectionRevisionID,
		target.Revision.StartCursor,
		target.Revision.EndCursor,
	)
	if err != nil {
		return p.recordFailure(ctx, job, err)
	}
	for rows.Next() {
		var event Event
		var payload []byte
		if err := rows.Scan(
			&event.StartCursor, &event.EndCursor, &event.OccurredAt, &event.EventType,
			&event.Summary, &event.Excerpt, &payload, &event.ContentSHA, &event.PayloadBytes,
		); err != nil {
			rows.Close()
			return p.recordFailure(ctx, job, err)
		}
		event.Payload = payload
		writeSourceIdentity(sourceHasher, event)
		extractor.Consume(event)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return p.recordFailure(ctx, job, err)
	}
	if err := rows.Close(); err != nil {
		return p.recordFailure(ctx, job, err)
	}

	digest, sourceCount, includedCount, omittedCount, truncated, encoded :=
		extractor.Result(p.config.ItemMaxBytes)
	result := BuildResult{
		Digest:             digest,
		SourceEventCount:   sourceCount,
		IncludedEventCount: includedCount,
		OmittedEventCount:  omittedCount,
		SourceBytes:        extractor.SourceBytes(),
		DigestBytes:        len(encoded),
		Truncated:          truncated,
		SourceSHA256:       hex.EncodeToString(sourceHasher.Sum(nil)),
		DigestSHA256:       HashBytes(encoded),
		DigestJSON:         encoded,
	}
	if err := p.commitReady(ctx, target.Revision, result); err != nil {
		if errors.Is(err, ErrStaleDigestSource) {
			if markErr := p.markSuperseded(ctx, target.Revision.ID); markErr != nil {
				return ErrDigestStatePersistence
			}
			return err
		}
		return p.recordFailure(ctx, job, err)
	}
	return nil
}

const safeEventProjectionSQL = `
	SELECT source_start_cursor, source_end_cursor, occurred_at, event_type,
		''::text, ''::text,
		CASE event_type
		WHEN 'event_msg.user_message' THEN jsonb_build_object('payload', jsonb_build_object(
			'message', left(COALESCE(content_payload #>> '{payload,message}', ''), 8192)))
		WHEN 'event_msg.agent_message' THEN jsonb_build_object('payload', jsonb_build_object(
			'phase', COALESCE(content_payload #>> '{payload,phase}', ''),
			'message', left(COALESCE(content_payload #>> '{payload,message}', ''), 8192)))
		WHEN 'event_msg.task_complete' THEN jsonb_build_object('payload', jsonb_build_object(
			'last_agent_message', left(COALESCE(content_payload #>> '{payload,last_agent_message}', ''), 8192)))
		WHEN 'event_msg.patch_apply_end' THEN jsonb_build_object('payload', jsonb_build_object(
			'changes', COALESCE((
				SELECT jsonb_object_agg(file_name, jsonb_build_object())
				FROM (
					SELECT file_name
					FROM jsonb_object_keys(CASE
						WHEN jsonb_typeof(content_payload #> '{payload,changes}') = 'object'
						THEN content_payload #> '{payload,changes}' ELSE '{}'::jsonb END) AS file_name
					ORDER BY file_name LIMIT 200
				) files
			), '{}'::jsonb)))
		WHEN 'response_item.message' THEN jsonb_build_object('payload', jsonb_build_object(
			'role', COALESCE(content_payload #>> '{payload,role}', ''),
			'phase', COALESCE(content_payload #>> '{payload,phase}', ''),
			'content', left(COALESCE(content_payload #>> '{payload,content}', ''), 8192)))
		WHEN 'response_item.custom_tool_call' THEN jsonb_build_object('payload', jsonb_build_object(
			'name', COALESCE(content_payload #>> '{payload,name}', ''),
			'call_id', COALESCE(content_payload #>> '{payload,call_id}', ''),
			'input', COALESCE((
				SELECT string_agg(
					'*** ' || match[1] || ' File: ' || match[2],
					E'\n' ORDER BY ordinal
				)
				FROM (
					SELECT match, ordinal
					FROM regexp_matches(
						COALESCE(content_payload #>> '{payload,input}', ''),
						'(?m)^\*\*\* (Add|Update|Delete) File: (.+)$', 'g'
					) WITH ORDINALITY AS found(match, ordinal)
					ORDER BY ordinal LIMIT 200
				) matches
			), '')))
		WHEN 'response_item.function_call' THEN jsonb_build_object('payload', jsonb_build_object(
			'call_id', COALESCE(content_payload #>> '{payload,call_id}', ''),
			'arguments', left(COALESCE(content_payload #>> '{payload,arguments}', ''), 8192)))
		WHEN 'response_item.function_call_output' THEN
			jsonb_build_object('payload', jsonb_build_object(
				'call_id', COALESCE(content_payload #>> '{payload,call_id}', ''),
				'output', left(COALESCE(content_payload #>> '{payload,output}', ''), 1024) || E'\n' ||
					right(COALESCE(content_payload #>> '{payload,output}', ''), 4096)))
		WHEN 'response_item.custom_tool_call_output' THEN
			jsonb_build_object('payload', jsonb_build_object(
				'call_id', COALESCE(content_payload #>> '{payload,call_id}', ''),
				'output', left(COALESCE(content_payload #>> '{payload,output}', ''), 1024) || E'\n' ||
					right(COALESCE(content_payload #>> '{payload,output}', ''), 4096)))
		WHEN 'user' THEN jsonb_build_object('message', jsonb_build_object('content', COALESCE((
			SELECT jsonb_agg(safe_block ORDER BY ordinal)
			FROM (
				SELECT ordinal, CASE block->>'type'
					WHEN 'text' THEN jsonb_build_object(
						'type', 'text', 'text', left(COALESCE(block->>'text', ''), 8192))
					WHEN 'tool_result' THEN jsonb_build_object(
						'type', 'tool_result', 'tool_use_id', COALESCE(block->>'tool_use_id', ''),
						'content', left(COALESCE(block->>'content', ''), 1024) || E'\n' ||
							right(COALESCE(block->>'content', ''), 4096))
					ELSE jsonb_build_object('type', COALESCE(block->>'type', 'unknown'))
				END AS safe_block
				FROM jsonb_array_elements(CASE
					WHEN jsonb_typeof(content_payload #> '{message,content}') = 'array'
					THEN content_payload #> '{message,content}' ELSE '[]'::jsonb END)
					WITH ORDINALITY AS blocks(block, ordinal)
				ORDER BY ordinal LIMIT 100
			) safe_blocks
		), '[]'::jsonb)))
		WHEN 'assistant' THEN jsonb_build_object('message', jsonb_build_object('content', COALESCE((
			SELECT jsonb_agg(safe_block ORDER BY ordinal)
			FROM (
				SELECT ordinal, CASE block->>'type'
					WHEN 'text' THEN jsonb_build_object(
						'type', 'text', 'text', left(COALESCE(block->>'text', ''), 8192))
					WHEN 'tool_use' THEN jsonb_build_object(
						'type', 'tool_use', 'id', COALESCE(block->>'id', ''),
						'name', COALESCE(block->>'name', ''),
						'input', jsonb_build_object(
							'file_path', left(COALESCE(block #>> '{input,file_path}', ''), 1024),
							'command', left(COALESCE(block #>> '{input,command}', ''), 8192)))
					ELSE jsonb_build_object('type', COALESCE(block->>'type', 'unknown'))
				END AS safe_block
				FROM jsonb_array_elements(CASE
					WHEN jsonb_typeof(content_payload #> '{message,content}') = 'array'
					THEN content_payload #> '{message,content}' ELSE '[]'::jsonb END)
					WITH ORDINALITY AS blocks(block, ordinal)
				ORDER BY ordinal LIMIT 100
			) safe_blocks
		), '[]'::jsonb)))
		ELSE NULL END,
		content_sha256, source_end_cursor - source_start_cursor
	FROM session_content_events
	WHERE content_projection_revision_id = $1
		AND source_start_cursor >= $2 AND source_end_cursor <= $3
	ORDER BY source_start_cursor, source_end_cursor, id`

func (p *Processor) loadTarget(
	ctx context.Context,
	job sessionsync.ProcessingJob,
) (ProcessingTarget, error) {
	var target ProcessingTarget
	err := p.db.QueryRowContext(ctx, `
		SELECT d.id::text, d.session_content_slice_id::text, sl.session_id::text,
			d.content_projection_revision_id::text, d.generation_id::text,
			d.content_epoch, sl.start_cursor, sl.end_cursor, d.status,
			d.digest_version, d.redaction_version
		FROM session_slice_digest_revisions d
		JOIN session_content_slices sl ON sl.id = d.session_content_slice_id
		WHERE d.id = $1`,
		job.TargetDigestRevisionID.String,
	).Scan(
		&target.Revision.ID, &target.Revision.SliceID, &target.Revision.SessionID,
		&target.Revision.ProjectionRevisionID, &target.Revision.GenerationID,
		&target.Revision.ContentEpoch, &target.Revision.StartCursor, &target.Revision.EndCursor,
		&target.Revision.Status, &target.Revision.DigestVersion, &target.Revision.RedactionVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ProcessingTarget{}, ErrDigestUnavailable
	}
	if err != nil {
		return ProcessingTarget{}, err
	}
	if target.Revision.GenerationID != job.GenerationID.String ||
		target.Revision.ContentEpoch != job.ContentEpoch.Int64 ||
		target.Revision.DigestVersion != p.config.DigestVersion ||
		target.Revision.RedactionVersion != p.config.RedactionVersion {
		return ProcessingTarget{}, ErrStaleDigestSource
	}
	switch target.Revision.Status {
	case "ready":
		target.Ready = true
		return target, nil
	case "pending", "building":
		_, err = p.db.ExecContext(ctx, `
			UPDATE session_slice_digest_revisions
			SET status = 'building', build_started_at = COALESCE(build_started_at, now()),
				error_code = NULL
			WHERE id = $1 AND status IN ('pending', 'building')`,
			target.Revision.ID,
		)
		return target, err
	case "superseded":
		return ProcessingTarget{}, ErrStaleDigestSource
	default:
		return ProcessingTarget{}, ErrDigestUnavailable
	}
}

func (p *Processor) ensureCurrent(ctx context.Context, revision Revision) error {
	var contentStatus, generationStatus, projectionStatus string
	var contentEpoch, indexedCursor int64
	var activeGenerationID, activeProjectionID sql.NullString
	err := p.db.QueryRowContext(ctx, `
		SELECT s.content_status, s.content_epoch, src.active_generation_id,
			src.active_content_projection_revision_id, g.status, rev.status,
			rev.content_indexed_cursor
		FROM session_content_slices sl
		JOIN sessions s ON s.id = sl.session_id
		JOIN session_sources src ON src.id = sl.source_id
		JOIN session_source_generations g ON g.id = sl.generation_id
		JOIN session_content_projection_revisions rev ON rev.id = $2
		WHERE sl.id = $1 AND sl.session_id = $3 AND sl.generation_id = $4
			AND sl.start_cursor = $5 AND sl.end_cursor = $6`,
		revision.SliceID, revision.ProjectionRevisionID, revision.SessionID,
		revision.GenerationID, revision.StartCursor, revision.EndCursor,
	).Scan(
		&contentStatus, &contentEpoch, &activeGenerationID, &activeProjectionID,
		&generationStatus, &projectionStatus, &indexedCursor,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrStaleDigestSource
	}
	if err != nil {
		return err
	}
	if contentStatus != "available" || contentEpoch != revision.ContentEpoch ||
		!activeGenerationID.Valid || activeGenerationID.String != revision.GenerationID ||
		!activeProjectionID.Valid || activeProjectionID.String != revision.ProjectionRevisionID ||
		generationStatus != "active" || projectionStatus != "active" ||
		indexedCursor < revision.EndCursor {
		return ErrStaleDigestSource
	}
	return nil
}

func (p *Processor) commitReady(ctx context.Context, revision Revision, result BuildResult) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var contentStatus string
	var contentEpoch, indexedCursor int64
	var activeGenerationID, activeProjectionID sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT s.content_status, s.content_epoch, src.active_generation_id,
			src.active_content_projection_revision_id, rev.content_indexed_cursor
		FROM session_content_slices sl
		JOIN sessions s ON s.id = sl.session_id
		JOIN session_sources src ON src.id = sl.source_id
		JOIN session_content_projection_revisions rev ON rev.id = $2 AND rev.status = 'active'
		WHERE sl.id = $1 AND sl.session_id = $3 AND sl.generation_id = $4
		FOR SHARE OF s, src, sl, rev`,
		revision.SliceID, revision.ProjectionRevisionID,
		revision.SessionID, revision.GenerationID,
	).Scan(
		&contentStatus, &contentEpoch, &activeGenerationID,
		&activeProjectionID, &indexedCursor,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrStaleDigestSource
	}
	if err != nil {
		return err
	}
	if contentStatus != "available" || contentEpoch != revision.ContentEpoch ||
		!activeGenerationID.Valid || activeGenerationID.String != revision.GenerationID ||
		!activeProjectionID.Valid || activeProjectionID.String != revision.ProjectionRevisionID ||
		indexedCursor < revision.EndCursor {
		return ErrStaleDigestSource
	}
	resultSQL, err := tx.ExecContext(ctx, `
		UPDATE session_slice_digest_revisions
		SET status = 'ready', digest_json = $2::jsonb,
			source_event_count = $3, included_event_count = $4, omitted_event_count = $5,
			source_bytes = $6, digest_bytes = $7, truncated = $8,
			source_sha256 = $9, digest_sha256 = $10, error_code = NULL, ready_at = now()
		WHERE id = $1 AND status IN ('pending', 'building')
			AND content_projection_revision_id = $11 AND generation_id = $12
			AND content_epoch = $13 AND digest_version = $14 AND redaction_version = $15`,
		revision.ID, string(result.DigestJSON), result.SourceEventCount,
		result.IncludedEventCount, result.OmittedEventCount, result.SourceBytes,
		result.DigestBytes, result.Truncated, result.SourceSHA256, result.DigestSHA256,
		revision.ProjectionRevisionID, revision.GenerationID, revision.ContentEpoch,
		revision.DigestVersion, revision.RedactionVersion,
	)
	if err != nil {
		return err
	}
	changed, err := resultSQL.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		var status string
		if err := tx.QueryRowContext(
			ctx, `SELECT status FROM session_slice_digest_revisions WHERE id = $1`,
			revision.ID,
		).Scan(&status); err == nil && status == "ready" {
			return tx.Commit()
		}
		return ErrDigestUnavailable
	}
	return tx.Commit()
}

func (p *Processor) markSuperseded(ctx context.Context, revisionID string) error {
	_, err := p.db.ExecContext(ctx, `
		UPDATE session_slice_digest_revisions
		SET status = 'superseded', superseded_at = COALESCE(superseded_at, now()),
			error_code = NULL
		WHERE id = $1 AND status IN ('pending', 'building')`,
		revisionID,
	)
	return err
}

func (p *Processor) recordFailure(
	ctx context.Context,
	job sessionsync.ProcessingJob,
	cause error,
) error {
	if !job.TargetDigestRevisionID.Valid || job.Attempts < job.MaxAttempts {
		return cause
	}
	result, err := p.db.ExecContext(ctx, `
		UPDATE session_slice_digest_revisions
		SET status = 'failed', error_code = $2
		WHERE id = $1 AND status IN ('pending', 'building')`,
		job.TargetDigestRevisionID.String, FailureCode(cause),
	)
	if err != nil {
		return ErrDigestStatePersistence
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return ErrDigestStatePersistence
	}
	return cause
}

func FailureCode(err error) string {
	switch {
	case errors.Is(err, ErrStaleDigestSource):
		return "digest_v2_source_stale"
	case errors.Is(err, ErrDigestUnavailable):
		return "digest_v2_source_unavailable"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "digest_v2_build_cancelled"
	default:
		return "digest_v2_build_failed"
	}
}

func writeSourceIdentity(target hash.Hash, event Event) {
	_, _ = fmt.Fprintf(
		target, "%d:%d:%s:%s\n", event.StartCursor, event.EndCursor,
		strings.TrimSpace(event.EventType), strings.TrimSpace(event.ContentSHA),
	)
}
