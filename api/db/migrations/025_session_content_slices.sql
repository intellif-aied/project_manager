CREATE TABLE session_content_slices (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id    UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source_id     UUID NOT NULL REFERENCES session_sources(id) ON DELETE CASCADE,
    generation_id UUID NOT NULL REFERENCES session_source_generations(id) ON DELETE CASCADE,
    start_cursor  BIGINT NOT NULL,
    end_cursor    BIGINT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT session_content_slices_cursor_check
        CHECK (start_cursor >= 0 AND end_cursor > start_cursor),
    UNIQUE (generation_id, start_cursor, end_cursor)
);

CREATE INDEX idx_session_content_slices_source_created
    ON session_content_slices(source_id, created_at DESC, id DESC);

CREATE INDEX idx_session_content_slices_generation_cursor
    ON session_content_slices(generation_id, start_cursor, end_cursor);
