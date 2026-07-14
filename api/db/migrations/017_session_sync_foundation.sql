ALTER TABLE sessions
    ADD COLUMN parent_session_ref TEXT,
    ADD COLUMN last_activity_at TIMESTAMPTZ DEFAULT now(),
    ADD COLUMN cwd TEXT,
    ADD COLUMN project_name TEXT,
    ADD COLUMN content_status TEXT NOT NULL DEFAULT 'available',
    ADD COLUMN content_epoch BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN active_source_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

UPDATE sessions
SET last_activity_at = COALESCE(ended_at, started_at, uploaded_at)
WHERE last_activity_at IS NULL;

ALTER TABLE sessions
    ALTER COLUMN last_activity_at SET NOT NULL,
    DROP CONSTRAINT IF EXISTS sessions_session_ref_user_id_key,
    ADD CONSTRAINT sessions_user_agent_ref_key UNIQUE (user_id, agent_type, session_ref),
    ADD CONSTRAINT sessions_content_status_check
        CHECK (content_status IN ('available', 'clearing', 'clearing_failed', 'cleared', 'deleted')),
    ADD CONSTRAINT sessions_content_epoch_check CHECK (content_epoch >= 0),
    ADD CONSTRAINT sessions_active_source_count_check CHECK (active_source_count >= 0);

CREATE INDEX idx_sessions_user_last_activity
    ON sessions(user_id, last_activity_at DESC);

CREATE TABLE session_sources (
    id                                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id                            UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source_role                           TEXT NOT NULL,
    source_key                            TEXT NOT NULL,
    active_generation_id                  UUID,
    staging_generation_id                 UUID,
    active_content_projection_revision_id UUID,
    created_at                            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT session_sources_role_not_empty CHECK (btrim(source_role) <> ''),
    CONSTRAINT session_sources_key_not_empty CHECK (btrim(source_key) <> ''),
    CONSTRAINT session_sources_active_staging_distinct CHECK (
        active_generation_id IS NULL OR staging_generation_id IS NULL OR active_generation_id <> staging_generation_id
    ),
    UNIQUE (session_id, source_role)
);

CREATE INDEX idx_session_sources_session ON session_sources(session_id);

CREATE TABLE session_source_generations (
    id                                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id                           UUID NOT NULL REFERENCES session_sources(id) ON DELETE CASCADE,
    status                              TEXT NOT NULL,
    expected_cursor                     BIGINT NOT NULL DEFAULT 0,
    prefix_checkpoint_hash              TEXT NOT NULL DEFAULT '',
    prefix_checkpoint_algorithm_version TEXT NOT NULL DEFAULT 'sha256-prefix-v1',
    prefix_checkpoint_state             BYTEA,
    prefix_checkpoint_state_format      TEXT,
    source_size                         BIGINT,
    started_at                          TIMESTAMPTZ NOT NULL DEFAULT now(),
    finalized_at                        TIMESTAMPTZ,
    superseded_at                       TIMESTAMPTZ,
    CONSTRAINT session_source_generations_status_check
        CHECK (status IN ('staging', 'active', 'superseded', 'abandoned')),
    CONSTRAINT session_source_generations_cursor_check CHECK (expected_cursor >= 0),
    CONSTRAINT session_source_generations_size_check CHECK (source_size IS NULL OR source_size >= 0),
    CONSTRAINT session_source_generations_prefix_state_check CHECK (
        (expected_cursor = 0) OR
        (prefix_checkpoint_hash <> '' AND prefix_checkpoint_state IS NOT NULL AND prefix_checkpoint_state_format IS NOT NULL)
    )
);

CREATE UNIQUE INDEX idx_session_source_one_active_generation
    ON session_source_generations(source_id)
    WHERE status = 'active';

CREATE UNIQUE INDEX idx_session_source_one_staging_generation
    ON session_source_generations(source_id)
    WHERE status = 'staging';

CREATE INDEX idx_session_source_generations_source
    ON session_source_generations(source_id, started_at DESC);

CREATE TABLE session_upload_chunks (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    generation_id        UUID NOT NULL REFERENCES session_source_generations(id) ON DELETE CASCADE,
    start_cursor         BIGINT NOT NULL,
    end_cursor           BIGINT NOT NULL,
    start_line           BIGINT NOT NULL,
    end_line             BIGINT NOT NULL,
    content_sha256       TEXT NOT NULL,
    content_epoch        BIGINT NOT NULL,
    event_start_at       TIMESTAMPTZ,
    event_end_at         TIMESTAMPTZ,
    raw_object_key       TEXT NOT NULL,
    object_status        TEXT NOT NULL DEFAULT 'available',
    content_index_status TEXT NOT NULL DEFAULT 'pending',
    usage_parse_status   TEXT NOT NULL DEFAULT 'pending',
    accepted_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT session_upload_chunks_cursor_check
        CHECK (start_cursor >= 0 AND end_cursor > start_cursor),
    CONSTRAINT session_upload_chunks_line_check
        CHECK (start_line >= 1 AND end_line >= start_line),
    CONSTRAINT session_upload_chunks_hash_check
        CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT session_upload_chunks_epoch_check CHECK (content_epoch >= 0),
    CONSTRAINT session_upload_chunks_event_time_check
        CHECK (event_start_at IS NULL OR event_end_at IS NULL OR event_end_at >= event_start_at),
    CONSTRAINT session_upload_chunks_object_status_check
        CHECK (object_status IN ('pending', 'available', 'delete_pending', 'deleted')),
    CONSTRAINT session_upload_chunks_content_index_status_check
        CHECK (content_index_status IN ('pending', 'processing', 'indexed', 'failed')),
    CONSTRAINT session_upload_chunks_usage_parse_status_check
        CHECK (usage_parse_status IN ('pending', 'processing', 'parsed', 'failed')),
    UNIQUE (generation_id, start_cursor, end_cursor)
);

CREATE INDEX idx_session_upload_chunks_generation_cursor
    ON session_upload_chunks(generation_id, start_cursor, end_cursor);

CREATE INDEX idx_session_upload_chunks_content_pending
    ON session_upload_chunks(accepted_at)
    WHERE content_index_status IN ('pending', 'failed');

CREATE INDEX idx_session_upload_chunks_usage_pending
    ON session_upload_chunks(accepted_at)
    WHERE usage_parse_status IN ('pending', 'failed');

CREATE TABLE session_content_projection_revisions (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    generation_id            UUID NOT NULL REFERENCES session_source_generations(id) ON DELETE CASCADE,
    content_parser_version   TEXT NOT NULL,
    status                   TEXT NOT NULL,
    build_start_cursor       BIGINT NOT NULL DEFAULT 0,
    content_indexed_cursor   BIGINT NOT NULL DEFAULT 0,
    source_high_water_cursor BIGINT NOT NULL DEFAULT 0,
    event_count              BIGINT NOT NULL DEFAULT 0,
    malformed_event_count    BIGINT NOT NULL DEFAULT 0,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    validated_at             TIMESTAMPTZ,
    activated_at             TIMESTAMPTZ,
    superseded_at            TIMESTAMPTZ,
    CONSTRAINT session_content_projection_status_check
        CHECK (status IN ('building', 'validated', 'active', 'failed', 'superseded')),
    CONSTRAINT session_content_projection_cursor_check CHECK (
        build_start_cursor >= 0 AND
        content_indexed_cursor >= 0 AND
        source_high_water_cursor >= content_indexed_cursor
    ),
    CONSTRAINT session_content_projection_count_check
        CHECK (event_count >= 0 AND malformed_event_count >= 0)
);

CREATE UNIQUE INDEX idx_session_generation_one_active_content_projection
    ON session_content_projection_revisions(generation_id)
    WHERE status = 'active';

CREATE INDEX idx_session_content_projection_generation
    ON session_content_projection_revisions(generation_id, created_at DESC);

CREATE TABLE session_content_events (
    id                             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    content_projection_revision_id UUID NOT NULL REFERENCES session_content_projection_revisions(id) ON DELETE CASCADE,
    chunk_id                       UUID NOT NULL REFERENCES session_upload_chunks(id) ON DELETE CASCADE,
    source_start_cursor            BIGINT NOT NULL,
    source_end_cursor              BIGINT NOT NULL,
    occurred_at                    TIMESTAMPTZ NOT NULL,
    event_type                     TEXT NOT NULL,
    summary                        TEXT,
    excerpt                        TEXT,
    content_payload                JSONB,
    content_sha256                 TEXT NOT NULL,
    created_at                     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT session_content_events_cursor_check
        CHECK (source_start_cursor >= 0 AND source_end_cursor > source_start_cursor),
    CONSTRAINT session_content_events_type_not_empty CHECK (btrim(event_type) <> ''),
    CONSTRAINT session_content_events_hash_check
        CHECK (content_sha256 ~ '^[0-9a-f]{64}$')
);

CREATE INDEX idx_session_content_events_revision_cursor
    ON session_content_events(content_projection_revision_id, source_start_cursor, source_end_cursor);

CREATE INDEX idx_session_content_events_occurred
    ON session_content_events(content_projection_revision_id, occurred_at);

CREATE TABLE session_content_tombstones (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id                 UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    cleared_by                 BIGINT NOT NULL REFERENCES users(id),
    cleared_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    reason                     TEXT,
    last_active_generation_ids UUID[] NOT NULL DEFAULT '{}',
    restored_at                TIMESTAMPTZ,
    restored_by                BIGINT REFERENCES users(id),
    restore_status             TEXT NOT NULL DEFAULT 'none',
    restore_generation_id      UUID REFERENCES session_source_generations(id) ON DELETE SET NULL,
    restore_requested_at       TIMESTAMPTZ,
    restore_expires_at         TIMESTAMPTZ,
    CONSTRAINT session_content_tombstones_restore_status_check
        CHECK (restore_status IN ('none', 'waiting_upload', 'building', 'failed', 'restored')),
    CONSTRAINT session_content_tombstones_restore_time_check
        CHECK (restore_expires_at IS NULL OR restore_requested_at IS NULL OR restore_expires_at >= restore_requested_at)
);

CREATE UNIQUE INDEX idx_session_content_one_unrestored_tombstone
    ON session_content_tombstones(session_id)
    WHERE restored_at IS NULL;

CREATE TABLE session_processing_jobs (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_type           TEXT NOT NULL,
    session_id         UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    generation_id      UUID REFERENCES session_source_generations(id) ON DELETE CASCADE,
    chunk_id           UUID REFERENCES session_upload_chunks(id) ON DELETE CASCADE,
    target_revision_id UUID REFERENCES session_content_projection_revisions(id) ON DELETE CASCADE,
    content_epoch      BIGINT,
    payload            JSONB NOT NULL DEFAULT '{}',
    status             TEXT NOT NULL DEFAULT 'pending',
    attempts           INTEGER NOT NULL DEFAULT 0,
    max_attempts       INTEGER NOT NULL DEFAULT 5,
    lease_owner        TEXT,
    lease_until        TIMESTAMPTZ,
    heartbeat_at       TIMESTAMPTZ,
    next_retry_at      TIMESTAMPTZ,
    last_error         TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at         TIMESTAMPTZ,
    completed_at       TIMESTAMPTZ,
    CONSTRAINT session_processing_jobs_type_check CHECK (job_type IN (
        'index_content_chunk', 'parse_usage_chunk', 'rebuild_content_revision',
        'rebuild_metrics_revision', 'build_metering_envelope', 'delete_object', 'purge_session'
    )),
    CONSTRAINT session_processing_jobs_status_check
        CHECK (status IN ('pending', 'leased', 'retry_wait', 'completed', 'dead')),
    CONSTRAINT session_processing_jobs_attempts_check
        CHECK (attempts >= 0 AND max_attempts > 0 AND attempts <= max_attempts),
    CONSTRAINT session_processing_jobs_epoch_check CHECK (content_epoch IS NULL OR content_epoch >= 0),
    CONSTRAINT session_processing_jobs_content_epoch_required CHECK (
        job_type NOT IN ('index_content_chunk', 'rebuild_content_revision', 'build_metering_envelope', 'delete_object', 'purge_session')
        OR content_epoch IS NOT NULL
    )
);

CREATE INDEX idx_session_processing_jobs_claim
    ON session_processing_jobs(status, next_retry_at, lease_until, created_at);

CREATE INDEX idx_session_processing_jobs_session
    ON session_processing_jobs(session_id, created_at DESC);

CREATE UNIQUE INDEX idx_session_processing_one_content_job_per_chunk
    ON session_processing_jobs(job_type, chunk_id)
    WHERE job_type = 'index_content_chunk' AND chunk_id IS NOT NULL;

CREATE UNIQUE INDEX idx_session_processing_one_usage_job_per_chunk
    ON session_processing_jobs(job_type, chunk_id)
    WHERE job_type = 'parse_usage_chunk' AND chunk_id IS NOT NULL;

ALTER TABLE session_sources
    ADD CONSTRAINT session_sources_active_generation_fk
        FOREIGN KEY (active_generation_id) REFERENCES session_source_generations(id) ON DELETE SET NULL,
    ADD CONSTRAINT session_sources_staging_generation_fk
        FOREIGN KEY (staging_generation_id) REFERENCES session_source_generations(id) ON DELETE SET NULL,
    ADD CONSTRAINT session_sources_active_content_projection_fk
        FOREIGN KEY (active_content_projection_revision_id) REFERENCES session_content_projection_revisions(id) ON DELETE SET NULL;
