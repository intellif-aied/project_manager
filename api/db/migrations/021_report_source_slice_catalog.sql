CREATE TABLE report_source_slice_catalog (
    slice_id                       UUID NOT NULL REFERENCES session_content_slices(id) ON DELETE CASCADE,
    content_projection_revision_id UUID NOT NULL REFERENCES session_content_projection_revisions(id) ON DELETE CASCADE,
    user_id                        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id                     UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    session_ref                    TEXT NOT NULL,
    agent_type                     TEXT NOT NULL,
    source_id                      UUID NOT NULL REFERENCES session_sources(id) ON DELETE CASCADE,
    generation_id                  UUID NOT NULL REFERENCES session_source_generations(id) ON DELETE CASCADE,
    content_epoch                  BIGINT NOT NULL,
    start_cursor                   BIGINT NOT NULL,
    end_cursor                     BIGINT NOT NULL,
    event_count                    BIGINT NOT NULL DEFAULT 0,
    activity_start_at              TIMESTAMPTZ,
    activity_end_at                TIMESTAMPTZ,
    last_activity_at               TIMESTAMPTZ,
    summary                        TEXT NOT NULL DEFAULT '',
    cwd                            TEXT NOT NULL DEFAULT '',
    models                         TEXT[] NOT NULL DEFAULT '{}',
    status                         TEXT NOT NULL DEFAULT 'building',
    ready_at                       TIMESTAMPTZ,
    created_at                     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (slice_id, content_projection_revision_id),
    CONSTRAINT report_source_slice_catalog_cursor_check
        CHECK (start_cursor >= 0 AND end_cursor > start_cursor),
    CONSTRAINT report_source_slice_catalog_epoch_check CHECK (content_epoch >= 0),
    CONSTRAINT report_source_slice_catalog_event_count_check CHECK (event_count >= 0),
    CONSTRAINT report_source_slice_catalog_time_check CHECK (
        activity_start_at IS NULL OR activity_end_at IS NULL OR activity_end_at >= activity_start_at
    ),
    CONSTRAINT report_source_slice_catalog_status_check CHECK (
        status IN ('building', 'ready', 'failed', 'superseded', 'cleared')
    ),
    CONSTRAINT report_source_slice_catalog_ready_check CHECK (
        status <> 'ready' OR (
            event_count > 0 AND activity_start_at IS NOT NULL AND activity_end_at IS NOT NULL AND
            last_activity_at = activity_end_at AND ready_at IS NOT NULL
        )
    )
);

CREATE INDEX idx_report_source_slice_catalog_user_ready
    ON report_source_slice_catalog(user_id, activity_end_at DESC, session_ref, slice_id)
    WHERE status = 'ready';

CREATE INDEX idx_report_source_slice_catalog_user_activity
    ON report_source_slice_catalog(user_id, activity_start_at, activity_end_at)
    WHERE status = 'ready';

CREATE INDEX idx_report_source_slice_catalog_revision_status
    ON report_source_slice_catalog(content_projection_revision_id, status, end_cursor);

CREATE INDEX idx_report_source_slice_catalog_source_status
    ON report_source_slice_catalog(source_id, status, updated_at);

CREATE INDEX idx_report_source_slice_catalog_reconcile
    ON report_source_slice_catalog(status, updated_at, slice_id)
    WHERE status IN ('building', 'failed');
