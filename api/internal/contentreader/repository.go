package contentreader

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type readTarget struct {
	GenerationID         string
	ParserVersion        string
	RevisionStatus       string
	BuildStartCursor     int64
	ContentIndexedCursor int64
	ContentStatus        string
	LegacyObjectKey      string
	LegacyFallbackTime   sql.NullTime
}

type contentChunk struct {
	ID            string
	StartCursor   int64
	EndCursor     int64
	ContentSHA256 string
	ObjectKey     string
	ObjectStatus  string
	EventStartAt  sql.NullTime
}

type expectedEvent struct {
	StartCursor   int64
	EndCursor     int64
	ContentSHA256 string
}

type expectedEventIterator interface {
	Next() bool
	Event() expectedEvent
	Err() error
	Close() error
}

type metadataRepository interface {
	resolveTarget(context.Context, string) (readTarget, error)
	listChunks(context.Context, string, int64, int64) ([]contentChunk, error)
	expectedEvents(context.Context, string, int64, int64) (expectedEventIterator, error)
}

type postgresRepository struct {
	db *sql.DB
}

func (r *postgresRepository) resolveTarget(ctx context.Context, revisionID string) (readTarget, error) {
	var target readTarget
	err := r.db.QueryRowContext(ctx, `
		SELECT revision.generation_id, revision.content_parser_version, revision.status,
			revision.build_start_cursor, revision.content_indexed_cursor,
			session.content_status, COALESCE(session.raw_log_url, ''), session.started_at
		FROM session_content_projection_revisions revision
		JOIN session_source_generations generation ON generation.id = revision.generation_id
		JOIN session_sources source ON source.id = generation.source_id
		JOIN sessions session ON session.id = source.session_id
		WHERE revision.id = $1`, revisionID).Scan(
		&target.GenerationID,
		&target.ParserVersion,
		&target.RevisionStatus,
		&target.BuildStartCursor,
		&target.ContentIndexedCursor,
		&target.ContentStatus,
		&target.LegacyObjectKey,
		&target.LegacyFallbackTime,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return readTarget{}, fmt.Errorf("%w: revision %s", ErrRevisionUnavailable, revisionID)
	}
	if err != nil {
		return readTarget{}, err
	}
	return target, nil
}

func (r *postgresRepository) listChunks(
	ctx context.Context,
	generationID string,
	startCursor, endCursor int64,
) ([]contentChunk, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, start_cursor, end_cursor, content_sha256, raw_object_key,
			object_status, event_start_at
		FROM session_upload_chunks
		WHERE generation_id = $1
			AND end_cursor > $2
			AND start_cursor < $3
		ORDER BY start_cursor, end_cursor, id`, generationID, startCursor, endCursor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chunks []contentChunk
	for rows.Next() {
		var chunk contentChunk
		if err := rows.Scan(
			&chunk.ID,
			&chunk.StartCursor,
			&chunk.EndCursor,
			&chunk.ContentSHA256,
			&chunk.ObjectKey,
			&chunk.ObjectStatus,
			&chunk.EventStartAt,
		); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return chunks, nil
}

func (r *postgresRepository) expectedEvents(
	ctx context.Context,
	revisionID string,
	startCursor, endCursor int64,
) (expectedEventIterator, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT source_start_cursor, source_end_cursor, content_sha256
		FROM session_content_events
		WHERE content_projection_revision_id = $1
			AND source_end_cursor > $2
			AND source_start_cursor < $3
		ORDER BY source_start_cursor, source_end_cursor, id`, revisionID, startCursor, endCursor)
	if err != nil {
		return nil, err
	}
	return &postgresExpectedEventIterator{rows: rows}, nil
}

type postgresExpectedEventIterator struct {
	rows    *sql.Rows
	current expectedEvent
	err     error
}

func (i *postgresExpectedEventIterator) Next() bool {
	if i.err != nil || !i.rows.Next() {
		return false
	}
	i.err = i.rows.Scan(&i.current.StartCursor, &i.current.EndCursor, &i.current.ContentSHA256)
	return i.err == nil
}

func (i *postgresExpectedEventIterator) Event() expectedEvent {
	return i.current
}

func (i *postgresExpectedEventIterator) Err() error {
	if i.err != nil {
		return i.err
	}
	return i.rows.Err()
}

func (i *postgresExpectedEventIterator) Close() error {
	return i.rows.Close()
}
