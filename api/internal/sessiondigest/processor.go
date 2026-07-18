package sessiondigest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"strings"

	"github.com/aidashboard/api/internal/contentreader"
	"github.com/aidashboard/api/internal/sessionsync"
)

type ContentReader interface {
	Stream(
		context.Context,
		contentreader.Request,
		func(contentreader.Event) error,
	) (contentreader.Result, error)
}

type Processor struct {
	db     *sql.DB
	reader ContentReader
	config Config
}

func NewProcessor(database *sql.DB, reader ContentReader, config Config) (*Processor, error) {
	if database == nil || reader == nil {
		return nil, errors.New("database and content reader are required")
	}
	normalized, err := config.Normalized()
	if err != nil {
		return nil, err
	}
	return &Processor{db: database, reader: reader, config: normalized}, nil
}

func (p *Processor) Process(ctx context.Context, job sessionsync.ProcessingJob) error {
	if job.Type != JobType || !job.TargetDigestRevisionID.Valid || !job.ContentEpoch.Valid || !job.GenerationID.Valid {
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
	_, err = p.reader.Stream(ctx, contentreader.Request{
		RevisionID:  target.Revision.ProjectionRevisionID,
		StartCursor: target.Revision.StartCursor,
		EndCursor:   target.Revision.EndCursor,
	}, func(source contentreader.Event) error {
		event, err := projectSafeEvent(source)
		if err != nil {
			return err
		}
		writeSourceIdentity(sourceHasher, event)
		extractor.Consume(event)
		return nil
	})
	if err != nil {
		return p.recordFailure(ctx, job, err)
	}
	digest, sourceCount, includedCount, omittedCount, truncated, encoded := extractor.Result(p.config.ItemMaxBytes)
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

func (p *Processor) loadTarget(ctx context.Context, job sessionsync.ProcessingJob) (ProcessingTarget, error) {
	var target ProcessingTarget
	err := p.db.QueryRowContext(ctx, `
		SELECT d.id::text, d.session_content_slice_id::text, sl.session_id::text,
			d.content_projection_revision_id::text, d.generation_id::text,
			d.content_epoch, sl.start_cursor, sl.end_cursor, d.status,
			d.digest_version, d.redaction_version
		FROM session_slice_digest_revisions d
		JOIN session_content_slices sl ON sl.id = d.session_content_slice_id
		WHERE d.id = $1`, job.TargetDigestRevisionID.String).Scan(
		&target.Revision.ID, &target.Revision.SliceID, &target.Revision.SessionID,
		&target.Revision.ProjectionRevisionID, &target.Revision.GenerationID,
		&target.Revision.ContentEpoch, &target.Revision.StartCursor, &target.Revision.EndCursor,
		&target.Revision.Status, &target.Revision.DigestVersion, &target.Revision.RedactionVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ProcessingTarget{}, ErrDigestUnavailable
	}
	if err != nil {
		return ProcessingTarget{}, err
	}
	if target.Revision.GenerationID != job.GenerationID.String || target.Revision.ContentEpoch != job.ContentEpoch.Int64 ||
		target.Revision.DigestVersion != p.config.DigestVersion || target.Revision.RedactionVersion != p.config.RedactionVersion {
		return ProcessingTarget{}, ErrStaleDigestSource
	}
	switch target.Revision.Status {
	case "ready":
		target.Ready = true
		return target, nil
	case "pending", "building":
		_, err = p.db.ExecContext(ctx, `
			UPDATE session_slice_digest_revisions
			SET status = 'building', build_started_at = COALESCE(build_started_at, now()), error_code = NULL
			WHERE id = $1 AND status IN ('pending', 'building')`, target.Revision.ID)
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
			AND sl.start_cursor = $5 AND sl.end_cursor = $6`, revision.SliceID,
		revision.ProjectionRevisionID, revision.SessionID, revision.GenerationID,
		revision.StartCursor, revision.EndCursor).Scan(&contentStatus, &contentEpoch,
		&activeGenerationID, &activeProjectionID, &generationStatus, &projectionStatus, &indexedCursor)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrStaleDigestSource
	}
	if err != nil {
		return err
	}
	if contentStatus != "available" || contentEpoch != revision.ContentEpoch ||
		!activeGenerationID.Valid || activeGenerationID.String != revision.GenerationID ||
		!activeProjectionID.Valid || activeProjectionID.String != revision.ProjectionRevisionID ||
		generationStatus != "active" || projectionStatus != "active" || indexedCursor < revision.EndCursor {
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
		FOR SHARE OF s, src, sl, rev`, revision.SliceID, revision.ProjectionRevisionID,
		revision.SessionID, revision.GenerationID).Scan(&contentStatus, &contentEpoch,
		&activeGenerationID, &activeProjectionID, &indexedCursor)
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
		revision.DigestVersion, revision.RedactionVersion)
	if err != nil {
		return err
	}
	changed, err := resultSQL.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM session_slice_digest_revisions WHERE id = $1`, revision.ID).Scan(&status); err == nil && status == "ready" {
			return tx.Commit()
		}
		return ErrDigestUnavailable
	}
	return tx.Commit()
}

func (p *Processor) markSuperseded(ctx context.Context, revisionID string) error {
	_, err := p.db.ExecContext(ctx, `
		UPDATE session_slice_digest_revisions
		SET status = 'superseded', superseded_at = COALESCE(superseded_at, now()), error_code = NULL
		WHERE id = $1 AND status IN ('pending', 'building')`, revisionID)
	return err
}

func (p *Processor) recordFailure(ctx context.Context, job sessionsync.ProcessingJob, cause error) error {
	if !job.TargetDigestRevisionID.Valid || job.Attempts < job.MaxAttempts {
		return cause
	}
	result, err := p.db.ExecContext(ctx, `
		UPDATE session_slice_digest_revisions
		SET status = 'failed', error_code = $2
		WHERE id = $1 AND status IN ('pending', 'building')`,
		job.TargetDigestRevisionID.String, FailureCode(cause))
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
		return "digest_source_stale"
	case errors.Is(err, ErrDigestUnavailable):
		return "digest_source_unavailable"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "digest_build_cancelled"
	default:
		return "digest_build_failed"
	}
}

func writeSourceIdentity(target hash.Hash, event Event) {
	_, _ = fmt.Fprintf(target, "%d:%d:%s:%s\n", event.StartCursor, event.EndCursor,
		strings.TrimSpace(event.EventType), strings.TrimSpace(event.ContentSHA))
}
