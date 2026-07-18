CREATE TABLE session_content_events_compact (
    id                             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    content_projection_revision_id UUID NOT NULL,
    chunk_id                       UUID NOT NULL,
    source_start_cursor            BIGINT NOT NULL,
    source_end_cursor              BIGINT NOT NULL,
    occurred_at                    TIMESTAMPTZ NOT NULL,
    event_type                     TEXT NOT NULL,
    summary                        TEXT,
    excerpt                        TEXT,
    content_sha256                 TEXT NOT NULL,
    created_at                     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT session_content_events_compact_projection_fkey
        FOREIGN KEY (content_projection_revision_id)
        REFERENCES session_content_projection_revisions(id) ON DELETE CASCADE,
    CONSTRAINT session_content_events_compact_chunk_fkey
        FOREIGN KEY (chunk_id)
        REFERENCES session_upload_chunks(id) ON DELETE CASCADE,
    CONSTRAINT session_content_events_compact_cursor_check
        CHECK (source_start_cursor >= 0 AND source_end_cursor > source_start_cursor),
    CONSTRAINT session_content_events_compact_type_not_empty
        CHECK (btrim(event_type) <> ''),
    CONSTRAINT session_content_events_compact_hash_check
        CHECK (content_sha256 ~ '^[0-9a-f]{64}$')
);

CREATE UNIQUE INDEX idx_session_content_events_compact_source_range
    ON session_content_events_compact (
        content_projection_revision_id, chunk_id, source_start_cursor, source_end_cursor
    );

CREATE INDEX idx_session_content_events_compact_revision_cursor
    ON session_content_events_compact (
        content_projection_revision_id, source_start_cursor, source_end_cursor
    );

CREATE INDEX idx_session_content_events_compact_occurred
    ON session_content_events_compact (content_projection_revision_id, occurred_at);

CREATE TABLE session_content_events_compaction_state (
    id                         SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    phase                      TEXT NOT NULL DEFAULT 'initialized',
    source_table               TEXT NOT NULL DEFAULT 'session_content_events',
    shadow_table               TEXT NOT NULL DEFAULT 'session_content_events_compact',
    archive_table              TEXT NOT NULL DEFAULT 'session_content_events_payload_archive',
    copy_cursor                UUID,
    reconcile_missing_cursor   UUID,
    reconcile_extra_cursor     UUID,
    reconcile_missing_complete BOOLEAN NOT NULL DEFAULT false,
    reconcile_extra_complete   BOOLEAN NOT NULL DEFAULT false,
    source_rows_at_start       BIGINT NOT NULL DEFAULT 0,
    copied_rows                BIGINT NOT NULL DEFAULT 0,
    reconciled_missing_rows    BIGINT NOT NULL DEFAULT 0,
    reconciled_extra_rows      BIGINT NOT NULL DEFAULT 0,
    copy_completed_at          TIMESTAMPTZ,
    mirror_started_at          TIMESTAMPTZ,
    reconciled_at              TIMESTAMPTZ,
    swapped_at                 TIMESTAMPTZ,
    rolled_back_at             TIMESTAMPTZ,
    finalized_at               TIMESTAMPTZ,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT session_content_events_compaction_phase_check CHECK (
        phase IN (
            'initialized', 'copying', 'copied', 'mirroring', 'reconciling',
            'reconciled', 'swapped', 'rolled_back', 'finalized'
        )
    ),
    CONSTRAINT session_content_events_compaction_counts_check CHECK (
        source_rows_at_start >= 0 AND copied_rows >= 0 AND
        reconciled_missing_rows >= 0 AND reconciled_extra_rows >= 0
    )
);

INSERT INTO session_content_events_compaction_state (id)
VALUES (1)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE session_content_events_compaction_batches (
    id                   BIGSERIAL PRIMARY KEY,
    operation            TEXT NOT NULL,
    start_after          UUID,
    end_at               UUID,
    row_count            INTEGER NOT NULL,
    source_fingerprint   CHAR(32),
    target_fingerprint   CHAR(32),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT session_content_events_compaction_batch_operation_check CHECK (
        operation IN ('copy', 'reconcile_missing', 'reconcile_extra', 'rollback_verify')
    ),
    CONSTRAINT session_content_events_compaction_batch_count_check CHECK (row_count >= 0),
    CONSTRAINT session_content_events_compaction_batch_fingerprint_check CHECK (
        (source_fingerprint IS NULL OR source_fingerprint ~ '^[0-9a-f]{32}$') AND
        (target_fingerprint IS NULL OR target_fingerprint ~ '^[0-9a-f]{32}$')
    )
);

CREATE INDEX idx_session_content_events_compaction_batches_operation
    ON session_content_events_compaction_batches (operation, id);
