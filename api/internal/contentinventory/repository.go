package contentinventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var errRevisionNotEligible = errors.New("content revision is not eligible for inventory")

const eligibleRevisionPredicate = `
	session.content_status = 'available'
	AND revision.status IN ('building', 'validated', 'active', 'superseded')`

type repository interface {
	plan(context.Context) (string, int64, error)
	list(context.Context, string, string, int) ([]candidate, bool, error)
	find(context.Context, string) (candidate, error)
}

type postgresRepository struct {
	db *sql.DB
}

func (r *postgresRepository) plan(ctx context.Context) (string, int64, error) {
	query := fmt.Sprintf(`
		SELECT
			COALESCE((
				SELECT revision.id::text
				FROM session_content_projection_revisions revision
				JOIN session_source_generations generation ON generation.id = revision.generation_id
				JOIN session_sources source ON source.id = generation.source_id
				JOIN sessions session ON session.id = source.session_id
				WHERE %s
				ORDER BY revision.id DESC LIMIT 1
			), ''),
			(
				SELECT COUNT(*)
				FROM session_content_projection_revisions revision
				JOIN session_source_generations generation ON generation.id = revision.generation_id
				JOIN session_sources source ON source.id = generation.source_id
				JOIN sessions session ON session.id = source.session_id
				WHERE %s
			)`, eligibleRevisionPredicate, eligibleRevisionPredicate)
	var through string
	var count int64
	err := r.db.QueryRowContext(ctx, query).Scan(&through, &count)
	return through, count, err
}

func (r *postgresRepository) list(
	ctx context.Context,
	after, through string,
	limit int,
) ([]candidate, bool, error) {
	var afterArg any
	if after != "" {
		afterArg = after
	}
	query := fmt.Sprintf(`
		SELECT revision.id::text, revision.build_start_cursor, revision.content_indexed_cursor
		FROM session_content_projection_revisions revision
		JOIN session_source_generations generation ON generation.id = revision.generation_id
		JOIN session_sources source ON source.id = generation.source_id
		JOIN sessions session ON session.id = source.session_id
		WHERE %s
			AND ($1::uuid IS NULL OR revision.id > $1::uuid)
			AND revision.id <= $2::uuid
		ORDER BY revision.id
		LIMIT $3`, eligibleRevisionPredicate)
	rows, err := r.db.QueryContext(ctx, query, afterArg, through, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]candidate, 0, limit+1)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.RevisionID, &item.StartCursor, &item.EndCursor); err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

func (r *postgresRepository) find(ctx context.Context, revisionID string) (candidate, error) {
	query := fmt.Sprintf(`
		SELECT revision.id::text, revision.build_start_cursor, revision.content_indexed_cursor
		FROM session_content_projection_revisions revision
		JOIN session_source_generations generation ON generation.id = revision.generation_id
		JOIN session_sources source ON source.id = generation.source_id
		JOIN sessions session ON session.id = source.session_id
		WHERE revision.id = $1 AND %s`, eligibleRevisionPredicate)
	var item candidate
	err := r.db.QueryRowContext(ctx, query, revisionID).Scan(
		&item.RevisionID, &item.StartCursor, &item.EndCursor,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return candidate{}, fmt.Errorf("%w: %s", errRevisionNotEligible, revisionID)
	}
	return item, err
}
