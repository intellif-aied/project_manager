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


-- Consolidated from 018_report_next_day_plan.sql

ALTER TABLE daily_reports ADD COLUMN IF NOT EXISTS next_day_plan TEXT NOT NULL DEFAULT '';
ALTER TABLE team_reports ADD COLUMN IF NOT EXISTS next_day_plan TEXT NOT NULL DEFAULT '';
ALTER TABLE department_reports ADD COLUMN IF NOT EXISTS next_day_plan TEXT NOT NULL DEFAULT '';


-- Consolidated from 019_content_projection_guards.sql

CREATE UNIQUE INDEX idx_session_content_events_source_range
    ON session_content_events (
        content_projection_revision_id, chunk_id, source_start_cursor, source_end_cursor
    );

CREATE UNIQUE INDEX idx_session_processing_one_rebuild_per_revision
    ON session_processing_jobs(job_type, target_revision_id)
    WHERE job_type = 'rebuild_content_revision' AND target_revision_id IS NOT NULL;

CREATE UNIQUE INDEX idx_session_processing_one_metering_per_generation_epoch
    ON session_processing_jobs(job_type, generation_id, content_epoch)
    WHERE job_type = 'build_metering_envelope' AND generation_id IS NOT NULL AND content_epoch IS NOT NULL;


-- Consolidated from 020_usage_metrics_shadow.sql

CREATE TABLE session_metrics_revisions (
    id                              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id                       UUID NOT NULL REFERENCES session_sources(id) ON DELETE CASCADE,
    generation_id                   UUID NOT NULL REFERENCES session_source_generations(id) ON DELETE CASCADE,
    parser_version                  TEXT NOT NULL,
    normalizer_version              TEXT NOT NULL,
    status                          TEXT NOT NULL DEFAULT 'building',
    quality_status                  TEXT NOT NULL DEFAULT 'exact',
    build_start_cursor              BIGINT NOT NULL DEFAULT 0,
    validated_through_cursor        BIGINT NOT NULL DEFAULT 0,
    source_high_water_cursor        BIGINT NOT NULL DEFAULT 0,
    scanned_event_count             BIGINT NOT NULL DEFAULT 0,
    usage_observation_count         BIGINT NOT NULL DEFAULT 0,
    usage_event_count               BIGINT NOT NULL DEFAULT 0,
    advanced_observation_count      BIGINT NOT NULL DEFAULT 0,
    duplicate_usage_event_count     BIGINT NOT NULL DEFAULT 0,
    malformed_event_count           BIGINT NOT NULL DEFAULT 0,
    unknown_usage_event_count       BIGINT NOT NULL DEFAULT 0,
    conflict_usage_event_count      BIGINT NOT NULL DEFAULT 0,
    reconciliation_json             JSONB NOT NULL DEFAULT '{}',
    calculation_reason              TEXT,
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),
    validated_at                    TIMESTAMPTZ,
    activated_at                    TIMESTAMPTZ,
    superseded_at                   TIMESTAMPTZ,
    CONSTRAINT session_metrics_revision_status_check
        CHECK (status IN ('building', 'validated', 'active', 'failed', 'superseded')),
    CONSTRAINT session_metrics_revision_quality_check
        CHECK (quality_status IN ('exact', 'estimated', 'incomplete', 'conflict')),
    CONSTRAINT session_metrics_revision_cursor_check CHECK (
        build_start_cursor >= 0 AND validated_through_cursor >= 0 AND
        source_high_water_cursor >= validated_through_cursor
    ),
    CONSTRAINT session_metrics_revision_count_check CHECK (
        scanned_event_count >= 0 AND usage_observation_count >= 0 AND usage_event_count >= 0 AND
        advanced_observation_count >= 0 AND duplicate_usage_event_count >= 0 AND
        malformed_event_count >= 0 AND unknown_usage_event_count >= 0 AND conflict_usage_event_count >= 0
    ),
    UNIQUE (generation_id, parser_version, normalizer_version)
);

CREATE UNIQUE INDEX idx_session_generation_one_active_metrics_revision
    ON session_metrics_revisions(generation_id)
    WHERE status = 'active';

CREATE UNIQUE INDEX idx_session_source_one_active_metrics_revision
    ON session_metrics_revisions(source_id)
    WHERE status = 'active';

CREATE INDEX idx_session_metrics_revisions_source
    ON session_metrics_revisions(source_id, created_at DESC);

ALTER TABLE session_processing_jobs
    ADD COLUMN target_metrics_revision_id UUID REFERENCES session_metrics_revisions(id) ON DELETE CASCADE;

ALTER TABLE session_content_tombstones
    ADD COLUMN objects_deleted_at TIMESTAMPTZ;

ALTER TABLE sessions
    ADD COLUMN content_cleared_at TIMESTAMPTZ;

DROP INDEX idx_session_processing_one_usage_job_per_chunk;

CREATE UNIQUE INDEX idx_session_processing_one_usage_job_per_chunk_revision
    ON session_processing_jobs(
        job_type, chunk_id,
        COALESCE(target_metrics_revision_id, '00000000-0000-0000-0000-000000000000'::uuid)
    )
    WHERE job_type = 'parse_usage_chunk' AND chunk_id IS NOT NULL;

CREATE TABLE session_source_metrics_states (
    source_id                   UUID PRIMARY KEY REFERENCES session_sources(id) ON DELETE CASCADE,
    active_revision_id         UUID REFERENCES session_metrics_revisions(id) ON DELETE SET NULL,
    target_generation_id       UUID NOT NULL REFERENCES session_source_generations(id),
    status                     TEXT NOT NULL DEFAULT 'pending',
    active_usage_parsed_cursor BIGINT NOT NULL DEFAULT 0,
    source_high_water_cursor   BIGINT NOT NULL DEFAULT 0,
    last_error                 TEXT,
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT session_source_metrics_status_check
        CHECK (status IN ('ready', 'pending', 'rebuilding', 'error')),
    CONSTRAINT session_source_metrics_cursor_check CHECK (
        active_usage_parsed_cursor >= 0 AND source_high_water_cursor >= 0 AND
        (status <> 'ready' OR source_high_water_cursor >= active_usage_parsed_cursor)
    )
);

CREATE TABLE session_parser_checkpoints (
    id                              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    revision_id                     UUID NOT NULL REFERENCES session_metrics_revisions(id) ON DELETE CASCADE,
    generation_id                   UUID NOT NULL REFERENCES session_source_generations(id) ON DELETE CASCADE,
    provider                        TEXT NOT NULL,
    parsed_cursor                   BIGINT NOT NULL DEFAULT 0,
    previous_token_counters_json    JSONB NOT NULL DEFAULT '{}',
    counter_segment                 BIGINT NOT NULL DEFAULT 0,
    active_model                    TEXT,
    usage_event_watermark           TEXT,
    parser_version                  TEXT NOT NULL,
    normalizer_version              TEXT NOT NULL,
    checkpoint_hash                 TEXT,
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT session_parser_checkpoint_cursor_check CHECK (parsed_cursor >= 0),
    CONSTRAINT session_parser_checkpoint_segment_check CHECK (counter_segment >= 0),
    UNIQUE (revision_id, provider)
);

CREATE TABLE session_usage_observations (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    revision_id         UUID NOT NULL REFERENCES session_metrics_revisions(id) ON DELETE CASCADE,
    generation_id       UUID NOT NULL REFERENCES session_source_generations(id) ON DELETE CASCADE,
    chunk_id            UUID NOT NULL REFERENCES session_upload_chunks(id) ON DELETE CASCADE,
    provider            TEXT NOT NULL,
    source_start_cursor BIGINT NOT NULL,
    source_end_cursor   BIGINT NOT NULL,
    occurred_at         TIMESTAMPTZ NOT NULL,
    raw_model           TEXT,
    raw_usage_json      JSONB NOT NULL,
    parsed_counters_json JSONB NOT NULL,
    raw_usage_hash      TEXT NOT NULL,
    parser_version      TEXT NOT NULL,
    quality_status      TEXT NOT NULL,
    quality_reason      TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT session_usage_observation_cursor_check
        CHECK (source_start_cursor >= 0 AND source_end_cursor > source_start_cursor),
    CONSTRAINT session_usage_observation_hash_check CHECK (raw_usage_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT session_usage_observation_quality_check
        CHECK (quality_status IN ('exact', 'estimated', 'incomplete', 'conflict')),
    UNIQUE (
        revision_id, provider, generation_id,
        source_start_cursor, source_end_cursor, raw_usage_hash
    )
);

CREATE INDEX idx_session_usage_observations_revision_cursor
    ON session_usage_observations(revision_id, source_start_cursor, source_end_cursor);

CREATE TABLE session_logical_usage_events (
    id                             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    revision_id                    UUID NOT NULL REFERENCES session_metrics_revisions(id) ON DELETE CASCADE,
    generation_id                  UUID NOT NULL REFERENCES session_source_generations(id) ON DELETE CASCADE,
    provider                       TEXT NOT NULL,
    usage_event_key                TEXT NOT NULL,
    identity_strategy              TEXT NOT NULL,
    current_observation_id         UUID NOT NULL REFERENCES session_usage_observations(id) ON DELETE RESTRICT,
    logical_occurred_at            TIMESTAMPTZ NOT NULL,
    logical_raw_model              TEXT,
    fold_status                    TEXT NOT NULL DEFAULT 'current',
    observation_count              BIGINT NOT NULL DEFAULT 1,
    duplicate_observation_count    BIGINT NOT NULL DEFAULT 0,
    advance_count                  BIGINT NOT NULL DEFAULT 0,
    fold_reason                    TEXT,
    provider_event_fingerprint     TEXT NOT NULL,
    created_at                     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT session_logical_usage_event_status_check
        CHECK (fold_status IN ('current', 'incomplete', 'conflict')),
    CONSTRAINT session_logical_usage_event_count_check CHECK (
        observation_count > 0 AND duplicate_observation_count >= 0 AND advance_count >= 0
    ),
    UNIQUE (revision_id, provider, usage_event_key)
);

CREATE INDEX idx_session_logical_usage_events_revision
    ON session_logical_usage_events(revision_id, logical_occurred_at);

CREATE TABLE session_usage_components (
    id                            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    revision_id                   UUID NOT NULL REFERENCES session_metrics_revisions(id) ON DELETE CASCADE,
    logical_usage_event_id        UUID NOT NULL REFERENCES session_logical_usage_events(id) ON DELETE CASCADE,
    observation_id                UUID NOT NULL REFERENCES session_usage_observations(id) ON DELETE RESTRICT,
    chunk_id                      UUID NOT NULL REFERENCES session_upload_chunks(id) ON DELETE CASCADE,
    session_id                    UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id                       BIGINT NOT NULL REFERENCES users(id),
    team_id_snapshot              UUID,
    department_id_snapshot        UUID,
    department_attribution_source TEXT NOT NULL DEFAULT 'unknown',
    activity_date                 DATE NOT NULL,
    occurred_at                   TIMESTAMPTZ NOT NULL,
    provider                      TEXT NOT NULL,
    raw_model                     TEXT,
    canonical_model               TEXT,
    billing_variant               TEXT NOT NULL DEFAULT 'unknown',
    uncached_input_tokens         BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens             BIGINT NOT NULL DEFAULT 0,
    cache_write_5m_tokens         BIGINT NOT NULL DEFAULT 0,
    cache_write_1h_tokens         BIGINT NOT NULL DEFAULT 0,
    output_tokens                 BIGINT NOT NULL DEFAULT 0,
    normalized_total_tokens       BIGINT NOT NULL DEFAULT 0,
    normalization_strategy        TEXT NOT NULL,
    quality_status                TEXT NOT NULL,
    is_estimated                  BOOLEAN NOT NULL DEFAULT false,
    assumptions_json              JSONB NOT NULL DEFAULT '{}',
    valid_from                    TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_to                      TIMESTAMPTZ,
    CONSTRAINT session_usage_component_department_source_check
        CHECK (department_attribution_source IN ('direct', 'via_team', 'unknown')),
    CONSTRAINT session_usage_component_quality_check
        CHECK (quality_status IN ('exact', 'estimated', 'incomplete', 'conflict')),
    CONSTRAINT session_usage_component_tokens_check CHECK (
        uncached_input_tokens >= 0 AND cache_read_tokens >= 0 AND
        cache_write_5m_tokens >= 0 AND cache_write_1h_tokens >= 0 AND output_tokens >= 0 AND
        normalized_total_tokens = uncached_input_tokens + cache_read_tokens +
            cache_write_5m_tokens + cache_write_1h_tokens + output_tokens
    ),
    CONSTRAINT session_usage_component_validity_check CHECK (valid_to IS NULL OR valid_to >= valid_from)
);

CREATE UNIQUE INDEX idx_session_usage_component_one_current_per_event_model
    ON session_usage_components(revision_id, logical_usage_event_id, COALESCE(canonical_model, ''), billing_variant)
    WHERE valid_to IS NULL;

CREATE INDEX idx_session_usage_components_user_date
    ON session_usage_components(user_id, activity_date, valid_to);

CREATE TABLE session_usage_event_claims (
    id                            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                       BIGINT NOT NULL REFERENCES users(id),
    provider                      TEXT NOT NULL,
    provider_event_fingerprint    TEXT NOT NULL,
    active_source_id              UUID NOT NULL REFERENCES session_sources(id) ON DELETE CASCADE,
    active_generation_id          UUID NOT NULL REFERENCES session_source_generations(id) ON DELETE CASCADE,
    active_revision_id            UUID NOT NULL REFERENCES session_metrics_revisions(id) ON DELETE CASCADE,
    active_logical_usage_event_id UUID NOT NULL REFERENCES session_logical_usage_events(id) ON DELETE CASCADE,
    claimed_at                    TIMESTAMPTZ NOT NULL DEFAULT now(),
    transferred_at                TIMESTAMPTZ,
    UNIQUE (user_id, provider, provider_event_fingerprint)
);

CREATE TABLE session_daily_usage (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    revision_id             UUID NOT NULL REFERENCES session_metrics_revisions(id) ON DELETE CASCADE,
    session_id              UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id                 BIGINT NOT NULL REFERENCES users(id),
    team_id_snapshot        UUID,
    department_id_snapshot  UUID,
    activity_date           DATE NOT NULL,
    provider                TEXT NOT NULL,
    canonical_model         TEXT,
    billing_variant         TEXT NOT NULL DEFAULT 'unknown',
    uncached_input_tokens   BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens       BIGINT NOT NULL DEFAULT 0,
    cache_write_5m_tokens   BIGINT NOT NULL DEFAULT 0,
    cache_write_1h_tokens   BIGINT NOT NULL DEFAULT 0,
    output_tokens           BIGINT NOT NULL DEFAULT 0,
    total_tokens            BIGINT NOT NULL DEFAULT 0,
    quality_status          TEXT NOT NULL,
    valid_from              TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_to                TIMESTAMPTZ,
    CONSTRAINT session_daily_usage_quality_check
        CHECK (quality_status IN ('exact', 'estimated', 'incomplete', 'conflict')),
    CONSTRAINT session_daily_usage_tokens_check CHECK (
        uncached_input_tokens >= 0 AND cache_read_tokens >= 0 AND
        cache_write_5m_tokens >= 0 AND cache_write_1h_tokens >= 0 AND output_tokens >= 0 AND
        total_tokens = uncached_input_tokens + cache_read_tokens +
            cache_write_5m_tokens + cache_write_1h_tokens + output_tokens
    ),
    CONSTRAINT session_daily_usage_validity_check CHECK (valid_to IS NULL OR valid_to >= valid_from)
);

CREATE UNIQUE INDEX idx_session_daily_usage_one_current_group
    ON session_daily_usage(
        revision_id, session_id, user_id, activity_date, provider,
        COALESCE(canonical_model, ''), billing_variant
    ) WHERE valid_to IS NULL;

CREATE INDEX idx_session_daily_usage_user_date
    ON session_daily_usage(user_id, activity_date, valid_to);

CREATE TABLE session_metering_envelope_manifests (
    id                           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    generation_id                UUID NOT NULL REFERENCES session_source_generations(id) ON DELETE CASCADE,
    content_epoch                BIGINT NOT NULL,
    envelope_version             TEXT NOT NULL,
    status                       TEXT NOT NULL DEFAULT 'building',
    metering_exported_cursor     BIGINT NOT NULL DEFAULT 0,
    source_high_water_cursor     BIGINT NOT NULL DEFAULT 0,
    source_record_count          BIGINT NOT NULL DEFAULT 0,
    potential_usage_record_count BIGINT NOT NULL DEFAULT 0,
    envelope_record_count        BIGINT NOT NULL DEFAULT 0,
    source_checksum              TEXT,
    envelope_checksum            TEXT,
    failure_reason               TEXT,
    created_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    validated_at                 TIMESTAMPTZ,
    CONSTRAINT session_metering_manifest_status_check
        CHECK (status IN ('building', 'validated', 'failed')),
    CONSTRAINT session_metering_manifest_cursor_check CHECK (
        content_epoch >= 0 AND metering_exported_cursor >= 0 AND
        source_high_water_cursor >= metering_exported_cursor
    ),
    CONSTRAINT session_metering_manifest_count_check CHECK (
        source_record_count >= 0 AND potential_usage_record_count >= 0 AND
        envelope_record_count >= 0
    ),
    CONSTRAINT session_metering_manifest_checksum_check CHECK (
        (source_checksum IS NULL OR source_checksum ~ '^[0-9a-f]{64}$') AND
        (envelope_checksum IS NULL OR envelope_checksum ~ '^[0-9a-f]{64}$')
    ),
    UNIQUE (generation_id, content_epoch, envelope_version)
);

CREATE TABLE session_metering_envelope_chunks (
    id                           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    manifest_id                  UUID NOT NULL REFERENCES session_metering_envelope_manifests(id) ON DELETE CASCADE,
    generation_id                UUID NOT NULL REFERENCES session_source_generations(id) ON DELETE CASCADE,
    chunk_id                     UUID NOT NULL REFERENCES session_upload_chunks(id) ON DELETE CASCADE,
    source_start_cursor          BIGINT NOT NULL,
    source_end_cursor            BIGINT NOT NULL,
    source_record_count          BIGINT NOT NULL,
    potential_usage_record_count BIGINT NOT NULL,
    source_checksum              TEXT NOT NULL,
    created_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT session_metering_envelope_chunk_cursor_check
        CHECK (source_start_cursor >= 0 AND source_end_cursor > source_start_cursor),
    CONSTRAINT session_metering_envelope_chunk_count_check
        CHECK (source_record_count >= 0 AND potential_usage_record_count >= 0),
    CONSTRAINT session_metering_envelope_chunk_hash_check
        CHECK (source_checksum ~ '^[0-9a-f]{64}$'),
    UNIQUE (manifest_id, chunk_id),
    UNIQUE (manifest_id, source_start_cursor, source_end_cursor)
);

CREATE TABLE session_metering_envelopes (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    manifest_id                UUID NOT NULL REFERENCES session_metering_envelope_manifests(id) ON DELETE CASCADE,
    generation_id              UUID NOT NULL REFERENCES session_source_generations(id) ON DELETE CASCADE,
    chunk_id                   UUID NOT NULL REFERENCES session_upload_chunks(id) ON DELETE CASCADE,
    source_start_cursor        BIGINT NOT NULL,
    source_end_cursor          BIGINT NOT NULL,
    provider                   TEXT NOT NULL,
    usage_event_key            TEXT NOT NULL,
    identity_strategy          TEXT NOT NULL,
    provider_event_fingerprint TEXT NOT NULL,
    occurred_at                TIMESTAMPTZ NOT NULL,
    raw_model                  TEXT,
    raw_usage_json             JSONB NOT NULL,
    parsed_counters_json       JSONB NOT NULL,
    raw_usage_hash             TEXT NOT NULL,
    source_record_hash         TEXT NOT NULL,
    quality_status             TEXT NOT NULL,
    quality_reason             TEXT,
    envelope_version           TEXT NOT NULL,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT session_metering_envelope_cursor_check
        CHECK (source_start_cursor >= 0 AND source_end_cursor > source_start_cursor),
    CONSTRAINT session_metering_envelope_hash_check CHECK (
        raw_usage_hash ~ '^[0-9a-f]{64}$' AND source_record_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT session_metering_envelope_quality_check
        CHECK (quality_status IN ('exact', 'estimated', 'incomplete', 'conflict')),
    UNIQUE (manifest_id, provider, source_start_cursor, source_end_cursor, raw_usage_hash)
);

CREATE INDEX idx_session_metering_envelopes_generation_cursor
    ON session_metering_envelopes(generation_id, source_start_cursor, source_end_cursor);

CREATE UNIQUE INDEX idx_session_processing_one_delete_per_chunk_epoch
    ON session_processing_jobs(job_type, chunk_id, content_epoch)
    WHERE job_type = 'delete_object' AND chunk_id IS NOT NULL AND content_epoch IS NOT NULL;

CREATE UNIQUE INDEX idx_session_processing_one_rebuild_metrics_per_generation
    ON session_processing_jobs(
        job_type, generation_id,
        COALESCE(target_metrics_revision_id, '00000000-0000-0000-0000-000000000000'::uuid)
    )
    WHERE job_type = 'rebuild_metrics_revision' AND generation_id IS NOT NULL;


-- Consolidated from 021_report_source_selections.sql

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


-- Consolidated from 022_token_cost_and_org_history.sql

CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE user_team_memberships (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id        UUID NOT NULL,
    effective_from TIMESTAMPTZ NOT NULL,
    effective_to   TIMESTAMPTZ,
    source         TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT user_team_membership_range_check
        CHECK (effective_to IS NULL OR effective_to > effective_from),
    EXCLUDE USING gist (
        user_id WITH =,
        tstzrange(effective_from, effective_to, '[)') WITH &&
    )
);

CREATE INDEX idx_user_team_memberships_lookup
    ON user_team_memberships(user_id, effective_from, effective_to);

CREATE TABLE team_department_memberships (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id        UUID NOT NULL,
    department_id  UUID NOT NULL,
    effective_from TIMESTAMPTZ NOT NULL,
    effective_to   TIMESTAMPTZ,
    source         TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT team_department_membership_range_check
        CHECK (effective_to IS NULL OR effective_to > effective_from),
    EXCLUDE USING gist (
        team_id WITH =,
        tstzrange(effective_from, effective_to, '[)') WITH &&
    )
);

CREATE INDEX idx_team_department_memberships_lookup
    ON team_department_memberships(team_id, effective_from, effective_to);

CREATE TABLE user_department_memberships (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    department_id  UUID NOT NULL,
    effective_from TIMESTAMPTZ NOT NULL,
    effective_to   TIMESTAMPTZ,
    source         TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT user_department_membership_range_check
        CHECK (effective_to IS NULL OR effective_to > effective_from),
    EXCLUDE USING gist (
        user_id WITH =,
        tstzrange(effective_from, effective_to, '[)') WITH &&
    )
);

CREATE INDEX idx_user_department_memberships_lookup
    ON user_department_memberships(user_id, effective_from, effective_to);

DO $$
DECLARE
    cutover_at TIMESTAMPTZ := clock_timestamp();
BEGIN
    INSERT INTO user_team_memberships(user_id, team_id, effective_from, source)
    SELECT id, team_id, cutover_at, 'v2_cutover'
    FROM users
    WHERE team_id IS NOT NULL;

    INSERT INTO team_department_memberships(team_id, department_id, effective_from, source)
    SELECT id, department_id, cutover_at, 'v2_cutover'
    FROM teams
    WHERE department_id IS NOT NULL;

    INSERT INTO user_department_memberships(user_id, department_id, effective_from, source)
    SELECT id, department_id, cutover_at, 'v2_cutover'
    FROM users
    WHERE department_id IS NOT NULL AND app_role IN ('pm', 'director');
END $$;

CREATE OR REPLACE FUNCTION sync_user_organization_memberships()
RETURNS TRIGGER AS $$
DECLARE
    changed_at TIMESTAMPTZ := statement_timestamp();
    target_user_id BIGINT;
    old_direct_department UUID;
    new_direct_department UUID;
BEGIN
    target_user_id := COALESCE(NEW.id, OLD.id);

    IF TG_OP = 'DELETE' OR OLD.team_id IS DISTINCT FROM NEW.team_id THEN
        UPDATE user_team_memberships
        SET effective_to = changed_at
        WHERE user_id = target_user_id AND effective_to IS NULL;

        IF TG_OP <> 'DELETE' AND NEW.team_id IS NOT NULL THEN
            INSERT INTO user_team_memberships(user_id, team_id, effective_from, source)
            VALUES (NEW.id, NEW.team_id, changed_at, 'current_org_mirror');
        END IF;
    END IF;

    IF TG_OP <> 'INSERT' AND OLD.app_role IN ('pm', 'director') THEN
        old_direct_department := OLD.department_id;
    END IF;
    IF TG_OP <> 'DELETE' AND NEW.app_role IN ('pm', 'director') THEN
        new_direct_department := NEW.department_id;
    END IF;

    IF TG_OP = 'DELETE' OR old_direct_department IS DISTINCT FROM new_direct_department THEN
        UPDATE user_department_memberships
        SET effective_to = changed_at
        WHERE user_id = target_user_id AND effective_to IS NULL;

        IF new_direct_department IS NOT NULL THEN
            INSERT INTO user_department_memberships(user_id, department_id, effective_from, source)
            VALUES (NEW.id, new_direct_department, changed_at, 'current_org_mirror');
        END IF;
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_users_sync_organization_memberships
AFTER INSERT OR UPDATE OF team_id, department_id, app_role OR DELETE ON users
FOR EACH ROW EXECUTE FUNCTION sync_user_organization_memberships();

CREATE OR REPLACE FUNCTION sync_team_department_membership()
RETURNS TRIGGER AS $$
DECLARE
    changed_at TIMESTAMPTZ := statement_timestamp();
    target_team_id UUID;
BEGIN
    target_team_id := COALESCE(NEW.id, OLD.id);
    IF TG_OP = 'DELETE' OR OLD.department_id IS DISTINCT FROM NEW.department_id THEN
        UPDATE team_department_memberships
        SET effective_to = changed_at
        WHERE team_id = target_team_id AND effective_to IS NULL;

        IF TG_OP <> 'DELETE' AND NEW.department_id IS NOT NULL THEN
            INSERT INTO team_department_memberships(team_id, department_id, effective_from, source)
            VALUES (NEW.id, NEW.department_id, changed_at, 'current_org_mirror');
        END IF;
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_teams_sync_department_membership
AFTER INSERT OR UPDATE OF department_id OR DELETE ON teams
FOR EACH ROW EXECUTE FUNCTION sync_team_department_membership();

CREATE OR REPLACE FUNCTION assign_usage_component_organization_snapshot()
RETURNS TRIGGER AS $$
DECLARE
    resolved_team_id UUID;
    resolved_department_id UUID;
BEGIN
    SELECT membership.team_id
    INTO resolved_team_id
    FROM user_team_memberships membership
    WHERE membership.user_id = NEW.user_id
      AND membership.effective_from <= NEW.occurred_at
      AND (membership.effective_to IS NULL OR membership.effective_to > NEW.occurred_at)
    ORDER BY membership.effective_from DESC
    LIMIT 1;

    SELECT membership.department_id
    INTO resolved_department_id
    FROM user_department_memberships membership
    WHERE membership.user_id = NEW.user_id
      AND membership.effective_from <= NEW.occurred_at
      AND (membership.effective_to IS NULL OR membership.effective_to > NEW.occurred_at)
    ORDER BY membership.effective_from DESC
    LIMIT 1;

    NEW.team_id_snapshot := resolved_team_id;
    IF resolved_department_id IS NOT NULL THEN
        NEW.department_id_snapshot := resolved_department_id;
        NEW.department_attribution_source := 'direct';
        RETURN NEW;
    END IF;

    IF resolved_team_id IS NOT NULL THEN
        SELECT membership.department_id
        INTO resolved_department_id
        FROM team_department_memberships membership
        WHERE membership.team_id = resolved_team_id
          AND membership.effective_from <= NEW.occurred_at
          AND (membership.effective_to IS NULL OR membership.effective_to > NEW.occurred_at)
        ORDER BY membership.effective_from DESC
        LIMIT 1;
    END IF;

    NEW.department_id_snapshot := resolved_department_id;
    NEW.department_attribution_source := CASE
        WHEN resolved_department_id IS NULL THEN 'unknown'
        ELSE 'via_team'
    END;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_usage_components_assign_organization
BEFORE INSERT ON session_usage_components
FOR EACH ROW EXECUTE FUNCTION assign_usage_component_organization_snapshot();

CREATE TABLE price_books (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL UNIQUE,
    pricing_basis    TEXT NOT NULL DEFAULT 'official_api_equivalent',
    source_currency  TEXT NOT NULL DEFAULT 'USD',
    display_currency TEXT NOT NULL DEFAULT 'CNY',
    status           TEXT NOT NULL DEFAULT 'draft',
    created_by       BIGINT REFERENCES users(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT price_books_basis_check CHECK (pricing_basis = 'official_api_equivalent'),
    CONSTRAINT price_books_currency_check CHECK (source_currency = 'USD' AND display_currency = 'CNY'),
    CONSTRAINT price_books_status_check CHECK (status IN ('draft', 'active', 'archived'))
);

CREATE UNIQUE INDEX idx_price_books_one_active
    ON price_books((status)) WHERE status = 'active';

CREATE TABLE model_aliases (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider          TEXT NOT NULL,
    raw_model_pattern TEXT NOT NULL,
    canonical_model   TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'pending',
    reviewed_by       BIGINT REFERENCES users(id),
    reviewed_at       TIMESTAMPTZ,
    created_by        BIGINT REFERENCES users(id),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT model_alias_status_check CHECK (status IN ('pending', 'reviewed', 'rejected')),
    CONSTRAINT model_alias_review_check CHECK (
        (status = 'reviewed' AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL)
        OR status <> 'reviewed'
    ),
    UNIQUE(provider, raw_model_pattern)
);

CREATE INDEX idx_model_aliases_reviewed_lookup
    ON model_aliases(provider, raw_model_pattern) WHERE status = 'reviewed';

CREATE TABLE model_price_versions (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    price_book_id              UUID NOT NULL REFERENCES price_books(id),
    canonical_model            TEXT NOT NULL,
    billing_variant            TEXT NOT NULL DEFAULT 'any',
    input_per_million          NUMERIC(24, 12) NOT NULL,
    cache_read_per_million     NUMERIC(24, 12) NOT NULL,
    cache_write_5m_per_million NUMERIC(24, 12) NOT NULL,
    cache_write_1h_per_million NUMERIC(24, 12) NOT NULL,
    output_per_million         NUMERIC(24, 12) NOT NULL,
    effective_from             DATE NOT NULL,
    effective_to               DATE,
    source_url                 TEXT,
    source_checked_at          TIMESTAMPTZ,
    notes                      TEXT,
    status                     TEXT NOT NULL DEFAULT 'draft',
    published_by               BIGINT REFERENCES users(id),
    published_at               TIMESTAMPTZ,
    supersedes_id              UUID REFERENCES model_price_versions(id),
    superseded_at              TIMESTAMPTZ,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT model_price_values_check CHECK (
        input_per_million >= 0 AND cache_read_per_million >= 0 AND
        cache_write_5m_per_million >= 0 AND cache_write_1h_per_million >= 0 AND
        output_per_million >= 0
    ),
    CONSTRAINT model_price_range_check CHECK (effective_to IS NULL OR effective_to > effective_from),
    CONSTRAINT model_price_status_check CHECK (status IN ('draft', 'published', 'archived')),
    CONSTRAINT model_price_publish_check CHECK (
        (status = 'published' AND published_by IS NOT NULL AND published_at IS NOT NULL)
        OR status <> 'published'
    ),
    EXCLUDE USING gist (
        price_book_id WITH =,
        canonical_model WITH =,
        billing_variant WITH =,
        daterange(effective_from, effective_to, '[)') WITH &&
    ) WHERE (status = 'published' AND superseded_at IS NULL)
);

CREATE INDEX idx_model_price_versions_lookup
    ON model_price_versions(price_book_id, canonical_model, billing_variant, effective_from)
    WHERE status = 'published' AND superseded_at IS NULL;

CREATE TABLE usd_cny_rate_versions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rate              NUMERIC(18, 8) NOT NULL,
    effective_from    DATE NOT NULL,
    effective_to      DATE,
    source_url        TEXT,
    source_checked_at TIMESTAMPTZ,
    notes             TEXT,
    status            TEXT NOT NULL DEFAULT 'draft',
    published_by      BIGINT REFERENCES users(id),
    published_at      TIMESTAMPTZ,
    supersedes_id     UUID REFERENCES usd_cny_rate_versions(id),
    superseded_at     TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT usd_cny_rate_value_check CHECK (rate > 0),
    CONSTRAINT usd_cny_rate_range_check CHECK (effective_to IS NULL OR effective_to > effective_from),
    CONSTRAINT usd_cny_rate_status_check CHECK (status IN ('draft', 'published', 'archived')),
    CONSTRAINT usd_cny_rate_publish_check CHECK (
        (status = 'published' AND published_by IS NOT NULL AND published_at IS NOT NULL)
        OR status <> 'published'
    ),
    EXCLUDE USING gist (
        daterange(effective_from, effective_to, '[)') WITH &&
    ) WHERE (status = 'published' AND superseded_at IS NULL)
);

CREATE INDEX idx_usd_cny_rate_versions_lookup
    ON usd_cny_rate_versions(effective_from) WHERE status = 'published' AND superseded_at IS NULL;

CREATE OR REPLACE FUNCTION reject_published_pricing_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status = 'published' THEN
        IF TG_OP = 'UPDATE'
           AND OLD.superseded_at IS NULL
           AND NEW.superseded_at IS NOT NULL
           AND (to_jsonb(NEW) - 'superseded_at') = (to_jsonb(OLD) - 'superseded_at') THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'published pricing versions are immutable';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_model_price_versions_immutable
BEFORE UPDATE OR DELETE ON model_price_versions
FOR EACH ROW EXECUTE FUNCTION reject_published_pricing_mutation();

CREATE TRIGGER trg_usd_cny_rate_versions_immutable
BEFORE UPDATE OR DELETE ON usd_cny_rate_versions
FOR EACH ROW EXECUTE FUNCTION reject_published_pricing_mutation();

CREATE TABLE session_activity_costs (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    usage_component_id       UUID NOT NULL REFERENCES session_usage_components(id) ON DELETE CASCADE,
    price_version_id         UUID REFERENCES model_price_versions(id),
    rate_version_id          UUID REFERENCES usd_cny_rate_versions(id),
    calculator_version       TEXT NOT NULL,
    unit_price_snapshot_json JSONB NOT NULL DEFAULT '{}',
    usd_cny_rate_snapshot    NUMERIC(18, 8),
    estimated_cost_usd       NUMERIC(30, 12),
    estimated_cost_cny       NUMERIC(30, 12),
    pricing_status           TEXT NOT NULL,
    confidence               TEXT NOT NULL,
    assumptions_json         JSONB NOT NULL DEFAULT '{}',
    calculation_reason       TEXT,
    calculated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    supersedes_id            UUID REFERENCES session_activity_costs(id),
    superseded_at            TIMESTAMPTZ,
    CONSTRAINT session_activity_cost_status_check
        CHECK (pricing_status IN ('pricing_pending', 'priced', 'partially_priced', 'unpriced')),
    CONSTRAINT session_activity_cost_confidence_check
        CHECK (confidence IN ('exact', 'estimated', 'unknown')),
    CONSTRAINT session_activity_cost_amount_check CHECK (
        (pricing_status = 'priced' AND estimated_cost_usd IS NOT NULL AND estimated_cost_cny IS NOT NULL)
        OR (pricing_status <> 'priced')
    )
);

CREATE UNIQUE INDEX idx_session_activity_cost_one_active
    ON session_activity_costs(usage_component_id, calculator_version)
    WHERE superseded_at IS NULL;

CREATE INDEX idx_session_activity_cost_lookup
    ON session_activity_costs(usage_component_id, calculated_at DESC);

CREATE TABLE pricing_recalculation_runs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requested_by   BIGINT NOT NULL REFERENCES users(id),
    filter_json    JSONB NOT NULL,
    result_json    JSONB NOT NULL,
    reason         TEXT NOT NULL,
    calculator_version TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_pricing_recalculation_runs_created
    ON pricing_recalculation_runs(created_at DESC);

CREATE TABLE token_query_snapshots (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash          CHAR(64) NOT NULL UNIQUE,
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scope               TEXT NOT NULL,
    search_mode         TEXT NOT NULL DEFAULT 'filtered',
    filters_json        JSONB NOT NULL,
    metrics_snapshot_at TIMESTAMPTZ NOT NULL,
    component_count     BIGINT NOT NULL DEFAULT 0,
    pending_source_count BIGINT NOT NULL DEFAULT 0,
    pricing_pending_source_count BIGINT NOT NULL DEFAULT 0,
    expires_at          TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT token_query_snapshot_scope_check CHECK (scope IN ('mine', 'management')),
    CONSTRAINT token_query_snapshot_search_mode_check CHECK (search_mode IN ('filtered', 'exact_session_ref')),
    CONSTRAINT token_query_snapshot_expiry_check CHECK (expires_at > created_at),
    CONSTRAINT token_query_snapshot_count_check CHECK (
        component_count >= 0 AND pending_source_count >= 0 AND pricing_pending_source_count >= 0
    )
);

CREATE INDEX idx_token_query_snapshots_user_expiry
    ON token_query_snapshots(user_id, expires_at DESC);

CREATE TABLE token_query_snapshot_items (
    id                        BIGSERIAL PRIMARY KEY,
    snapshot_id               UUID NOT NULL REFERENCES token_query_snapshots(id) ON DELETE CASCADE,
    usage_component_id        UUID NOT NULL,
    cost_id                   UUID,
    session_id                UUID NOT NULL,
    session_ref               TEXT NOT NULL,
    agent_type                TEXT,
    user_id                   BIGINT NOT NULL,
    user_display_name         TEXT NOT NULL,
    user_current_enabled      BOOLEAN NOT NULL,
    team_id_snapshot          UUID,
    team_name_snapshot        TEXT,
    department_id_snapshot    UUID,
    department_name_snapshot  TEXT,
    activity_date             DATE NOT NULL,
    occurred_at               TIMESTAMPTZ NOT NULL,
    provider                  TEXT NOT NULL,
    canonical_model           TEXT,
    billing_variant           TEXT NOT NULL,
    uncached_input_tokens     BIGINT NOT NULL,
    cache_read_tokens         BIGINT NOT NULL,
    cache_write_5m_tokens     BIGINT NOT NULL,
    cache_write_1h_tokens     BIGINT NOT NULL,
    output_tokens             BIGINT NOT NULL,
    normalized_total_tokens   BIGINT NOT NULL,
    quality_status            TEXT NOT NULL,
    is_estimated              BOOLEAN NOT NULL,
    pricing_status            TEXT NOT NULL,
    estimated_cost_usd        NUMERIC(30, 12),
    estimated_cost_cny        NUMERIC(30, 12),
    UNIQUE(snapshot_id, usage_component_id)
);

CREATE INDEX idx_token_query_snapshot_items_date
    ON token_query_snapshot_items(snapshot_id, activity_date, occurred_at);

CREATE INDEX idx_token_query_snapshot_items_session
    ON token_query_snapshot_items(snapshot_id, session_id);

CREATE INDEX idx_token_query_snapshot_items_rankings
    ON token_query_snapshot_items(snapshot_id, department_id_snapshot, team_id_snapshot, user_id);

CREATE TABLE token_query_snapshot_members (
    snapshot_id              UUID NOT NULL REFERENCES token_query_snapshots(id) ON DELETE CASCADE,
    user_id                  BIGINT NOT NULL,
    user_display_name        TEXT NOT NULL,
    team_id                  UUID,
    team_name                TEXT,
    department_id            UUID,
    department_name          TEXT,
    PRIMARY KEY(snapshot_id, user_id)
);

CREATE INDEX idx_token_query_snapshot_members_scope
    ON token_query_snapshot_members(snapshot_id, department_id, team_id, user_id);


-- Consolidated from 023_session_daily_usage_org_snapshot_key.sql

DROP INDEX IF EXISTS idx_session_daily_usage_one_current_group;

CREATE UNIQUE INDEX idx_session_daily_usage_one_current_group
    ON session_daily_usage(
        revision_id, session_id, user_id,
        COALESCE(team_id_snapshot, '00000000-0000-0000-0000-000000000000'::UUID),
        COALESCE(department_id_snapshot, '00000000-0000-0000-0000-000000000000'::UUID),
        activity_date, provider, COALESCE(canonical_model, ''), billing_variant
    ) WHERE valid_to IS NULL;


-- Consolidated from 024_report_source_read_completion.sql

ALTER TABLE report_source_selections
    ADD COLUMN IF NOT EXISTS read_completed_at TIMESTAMPTZ;


-- Consolidated from 025_session_content_slices.sql

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
