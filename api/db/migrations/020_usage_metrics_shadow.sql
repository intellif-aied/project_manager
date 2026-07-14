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
