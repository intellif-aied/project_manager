CREATE TABLE report_source_selections (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             BIGINT NOT NULL REFERENCES users(id),
    report_type         TEXT NOT NULL,
    period_start        DATE NOT NULL,
    period_end          DATE NOT NULL,
    selection_mode      TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'prepared',
    content_snapshot_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    attached_run_id     UUID REFERENCES ai_runs(id) ON DELETE SET NULL,
    attached_at         TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_source_selection_type_check
        CHECK (report_type IN ('personal_daily', 'personal_weekly')),
    CONSTRAINT report_source_selection_period_check CHECK (period_end >= period_start),
    CONSTRAINT report_source_selection_mode_check CHECK (selection_mode IN ('default', 'explicit')),
    CONSTRAINT report_source_selection_status_check CHECK (status IN ('prepared', 'attached', 'expired')),
    CONSTRAINT report_source_selection_attachment_check CHECK (
        (status = 'attached' AND attached_run_id IS NOT NULL AND attached_at IS NOT NULL) OR
        (status <> 'attached')
    )
);

CREATE UNIQUE INDEX idx_report_source_selection_one_run
    ON report_source_selections(attached_run_id)
    WHERE attached_run_id IS NOT NULL;

CREATE INDEX idx_report_source_selections_user_created
    ON report_source_selections(user_id, created_at DESC);

CREATE TABLE report_source_selection_items (
    id                             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    selection_id                   UUID NOT NULL REFERENCES report_source_selections(id) ON DELETE CASCADE,
    session_id                     UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    session_ref_snapshot           TEXT NOT NULL,
    agent_type                     TEXT NOT NULL,
    source_id                      UUID NOT NULL REFERENCES session_sources(id) ON DELETE CASCADE,
    source_generation_id           UUID NOT NULL REFERENCES session_source_generations(id) ON DELETE CASCADE,
    content_projection_revision_id UUID NOT NULL REFERENCES session_content_projection_revisions(id) ON DELETE CASCADE,
    start_cursor                   BIGINT NOT NULL,
    end_cursor                     BIGINT NOT NULL,
    activity_start_at              TIMESTAMPTZ NOT NULL,
    activity_end_at                TIMESTAMPTZ NOT NULL,
    summary_snapshot               TEXT,
    content_status_snapshot        TEXT NOT NULL,
    content_epoch_snapshot         BIGINT NOT NULL,
    content_event_count            BIGINT NOT NULL DEFAULT 0,
    created_at                     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_source_selection_item_cursor_check CHECK (start_cursor >= 0 AND end_cursor > start_cursor),
    CONSTRAINT report_source_selection_item_time_check CHECK (activity_end_at >= activity_start_at),
    CONSTRAINT report_source_selection_item_epoch_check CHECK (content_epoch_snapshot >= 0),
    CONSTRAINT report_source_selection_item_event_count_check CHECK (content_event_count > 0),
    UNIQUE (selection_id, source_id, start_cursor, end_cursor)
);

CREATE INDEX idx_report_source_selection_items_selection
    ON report_source_selection_items(selection_id, created_at, id);

CREATE INDEX idx_report_source_selection_items_revision_range
    ON report_source_selection_items(content_projection_revision_id, start_cursor, end_cursor);

CREATE TABLE report_source_page_cursors (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    selection_id      UUID NOT NULL REFERENCES report_source_selections(id) ON DELETE CASCADE,
    user_id           BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_offset       INTEGER NOT NULL,
    next_event_cursor BIGINT NOT NULL,
    expires_at        TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_source_page_cursor_position_check
        CHECK (item_offset >= 0 AND next_event_cursor >= 0)
);

CREATE INDEX idx_report_source_page_cursors_expiry
    ON report_source_page_cursors(expires_at);

CREATE UNIQUE INDEX uq_report_source_page_cursors_position
    ON report_source_page_cursors(selection_id, user_id, item_offset, next_event_cursor);
