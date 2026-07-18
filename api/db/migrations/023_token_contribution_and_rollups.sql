ALTER TABLE session_usage_observations
    ADD COLUMN logical_usage_event_id UUID REFERENCES session_logical_usage_events(id) ON DELETE SET NULL;

CREATE INDEX idx_session_usage_observations_logical_event
    ON session_usage_observations(logical_usage_event_id, source_start_cursor, source_end_cursor);

CREATE TABLE session_usage_contributions (
    id                            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    revision_id                   UUID NOT NULL REFERENCES session_metrics_revisions(id) ON DELETE CASCADE,
    generation_id                 UUID NOT NULL REFERENCES session_source_generations(id) ON DELETE CASCADE,
    logical_usage_event_id        UUID NOT NULL REFERENCES session_logical_usage_events(id) ON DELETE CASCADE,
    from_observation_id           UUID REFERENCES session_usage_observations(id) ON DELETE RESTRICT,
    to_observation_id             UUID NOT NULL REFERENCES session_usage_observations(id) ON DELETE RESTRICT,
    contribution_kind             TEXT NOT NULL,
    member_session_id             UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id                       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id_snapshot              UUID,
    department_id_snapshot        UUID,
    department_attribution_source TEXT NOT NULL DEFAULT 'unknown',
    chunk_id                      UUID NOT NULL REFERENCES session_upload_chunks(id) ON DELETE CASCADE,
    activity_date                 DATE NOT NULL,
    occurred_at                   TIMESTAMPTZ NOT NULL,
    provider                      TEXT NOT NULL,
    raw_model                     TEXT,
    canonical_model               TEXT NOT NULL DEFAULT '',
    billing_variant               TEXT NOT NULL DEFAULT 'unknown',
    uncached_input_tokens         BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens             BIGINT NOT NULL DEFAULT 0,
    cache_write_5m_tokens         BIGINT NOT NULL DEFAULT 0,
    cache_write_1h_tokens         BIGINT NOT NULL DEFAULT 0,
    output_tokens                 BIGINT NOT NULL DEFAULT 0,
    total_tokens                  BIGINT NOT NULL DEFAULT 0,
    normalization_strategy        TEXT NOT NULL,
    quality_status                TEXT NOT NULL,
    is_estimated                  BOOLEAN NOT NULL DEFAULT false,
    assumptions_json              JSONB NOT NULL DEFAULT '{}',
    contribution_hash             CHAR(64) NOT NULL,
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT session_usage_contribution_kind_check CHECK (
        contribution_kind IN ('initial', 'advance', 'checkpoint_delta')
    ),
    CONSTRAINT session_usage_contribution_department_source_check CHECK (
        department_attribution_source IN ('direct', 'via_team', 'unknown')
    ),
    CONSTRAINT session_usage_contribution_quality_check CHECK (
        quality_status IN ('exact', 'estimated', 'incomplete', 'conflict')
    ),
    CONSTRAINT session_usage_contribution_tokens_check CHECK (
        uncached_input_tokens >= 0 AND cache_read_tokens >= 0 AND
        cache_write_5m_tokens >= 0 AND cache_write_1h_tokens >= 0 AND output_tokens >= 0 AND
        total_tokens = uncached_input_tokens + cache_read_tokens +
            cache_write_5m_tokens + cache_write_1h_tokens + output_tokens
    ),
    CONSTRAINT session_usage_contribution_hash_check CHECK (
        contribution_hash ~ '^[0-9a-f]{64}$'
    )
);

CREATE UNIQUE INDEX idx_session_usage_contribution_identity
    ON session_usage_contributions(
        revision_id, logical_usage_event_id, to_observation_id,
        canonical_model, billing_variant
    );

CREATE INDEX idx_session_usage_contributions_revision
    ON session_usage_contributions(revision_id, logical_usage_event_id, occurred_at);

CREATE INDEX idx_session_usage_contributions_user_date
    ON session_usage_contributions(user_id, activity_date, member_session_id);

CREATE INDEX idx_session_usage_contributions_chunk
    ON session_usage_contributions(chunk_id, activity_date, member_session_id);

CREATE TRIGGER trg_usage_contributions_assign_organization
BEFORE INSERT ON session_usage_contributions
FOR EACH ROW EXECUTE FUNCTION assign_usage_component_organization_snapshot();

CREATE TABLE session_usage_contribution_costs (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contribution_id          UUID NOT NULL REFERENCES session_usage_contributions(id) ON DELETE CASCADE,
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
    supersedes_id            UUID REFERENCES session_usage_contribution_costs(id),
    superseded_at            TIMESTAMPTZ,
    CONSTRAINT session_usage_contribution_cost_status_check CHECK (
        pricing_status IN ('pricing_pending', 'priced', 'partially_priced', 'unpriced')
    ),
    CONSTRAINT session_usage_contribution_cost_confidence_check CHECK (
        confidence IN ('exact', 'estimated', 'unknown')
    ),
    CONSTRAINT session_usage_contribution_cost_amount_check CHECK (
        (pricing_status = 'priced' AND estimated_cost_usd IS NOT NULL AND estimated_cost_cny IS NOT NULL)
        OR (pricing_status <> 'priced')
    )
);

CREATE UNIQUE INDEX idx_session_usage_contribution_cost_one_active
    ON session_usage_contribution_costs(contribution_id, calculator_version)
    WHERE superseded_at IS NULL;

CREATE INDEX idx_session_usage_contribution_cost_lookup
    ON session_usage_contribution_costs(contribution_id, calculated_at DESC);

CREATE TABLE session_family_versions (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    root_session_id    UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    relation_hash      CHAR(64) NOT NULL,
    status             TEXT NOT NULL DEFAULT 'building',
    quality_status     TEXT NOT NULL DEFAULT 'exact',
    member_count       INTEGER NOT NULL DEFAULT 0,
    subagent_count     INTEGER NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at       TIMESTAMPTZ,
    superseded_at      TIMESTAMPTZ,
    failure_reason     TEXT,
    CONSTRAINT session_family_version_status_check CHECK (
        status IN ('building', 'active', 'failed', 'superseded')
    ),
    CONSTRAINT session_family_version_quality_check CHECK (
        quality_status IN ('exact', 'pending', 'conflict')
    ),
    CONSTRAINT session_family_version_count_check CHECK (
        member_count > 0 AND subagent_count >= 0 AND subagent_count < member_count
    ),
    CONSTRAINT session_family_version_hash_check CHECK (relation_hash ~ '^[0-9a-f]{64}$')
);

CREATE UNIQUE INDEX idx_session_family_version_one_active_root
    ON session_family_versions(root_session_id)
    WHERE status = 'active';

CREATE INDEX idx_session_family_versions_user_status
    ON session_family_versions(user_id, status, root_session_id);

CREATE TABLE session_family_memberships (
    family_version_id UUID NOT NULL REFERENCES session_family_versions(id) ON DELETE CASCADE,
    root_session_id   UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    member_session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    parent_session_id UUID REFERENCES sessions(id) ON DELETE SET NULL,
    depth             INTEGER NOT NULL,
    relation_source   TEXT NOT NULL,
    quality_status    TEXT NOT NULL,
    valid_from        TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_to          TIMESTAMPTZ,
    PRIMARY KEY (family_version_id, member_session_id),
    CONSTRAINT session_family_membership_depth_check CHECK (depth >= 0),
    CONSTRAINT session_family_membership_quality_check CHECK (
        quality_status IN ('exact', 'pending', 'conflict')
    ),
    CONSTRAINT session_family_membership_validity_check CHECK (
        valid_to IS NULL OR valid_to >= valid_from
    ),
    CONSTRAINT session_family_membership_root_depth_check CHECK (
        (root_session_id = member_session_id AND depth = 0 AND parent_session_id IS NULL)
        OR (root_session_id <> member_session_id AND depth > 0)
    )
);

CREATE UNIQUE INDEX idx_session_family_membership_one_active_member
    ON session_family_memberships(member_session_id)
    WHERE valid_to IS NULL;

CREATE INDEX idx_session_family_memberships_active_root
    ON session_family_memberships(root_session_id, depth, member_session_id)
    WHERE valid_to IS NULL;

CREATE TABLE session_family_rollup_versions (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family_version_id     UUID NOT NULL REFERENCES session_family_versions(id) ON DELETE CASCADE,
    root_session_id       UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    revision_set_hash     CHAR(64) NOT NULL,
    cost_set_hash         CHAR(64) NOT NULL,
    calculator_version    TEXT NOT NULL,
    status                TEXT NOT NULL DEFAULT 'building',
    quality_status        TEXT NOT NULL DEFAULT 'exact',
    member_count          INTEGER NOT NULL,
    source_count          INTEGER NOT NULL,
    contribution_count    BIGINT NOT NULL DEFAULT 0,
    data_through_at       TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at          TIMESTAMPTZ,
    superseded_at         TIMESTAMPTZ,
    failure_reason        TEXT,
    CONSTRAINT session_family_rollup_version_status_check CHECK (
        status IN ('building', 'active', 'failed', 'superseded')
    ),
    CONSTRAINT session_family_rollup_version_quality_check CHECK (
        quality_status IN ('exact', 'estimated', 'pending', 'conflict')
    ),
    CONSTRAINT session_family_rollup_version_count_check CHECK (
        member_count > 0 AND source_count >= 0 AND contribution_count >= 0
    ),
    CONSTRAINT session_family_rollup_version_revision_hash_check CHECK (
        revision_set_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT session_family_rollup_version_cost_hash_check CHECK (
        cost_set_hash ~ '^[0-9a-f]{64}$'
    )
);

CREATE UNIQUE INDEX idx_session_family_rollup_one_active_root
    ON session_family_rollup_versions(root_session_id)
    WHERE status = 'active';

CREATE UNIQUE INDEX idx_session_family_rollup_version_identity
    ON session_family_rollup_versions(
        family_version_id, revision_set_hash, cost_set_hash, calculator_version
    );

CREATE INDEX idx_session_family_rollup_versions_family_status
    ON session_family_rollup_versions(family_version_id, status, created_at DESC);

CREATE INDEX idx_session_family_rollup_versions_cleanup
    ON session_family_rollup_versions(superseded_at, id)
    WHERE status = 'superseded';

CREATE TABLE session_family_rollup_revision_refs (
    rollup_version_id        UUID NOT NULL REFERENCES session_family_rollup_versions(id) ON DELETE CASCADE,
    source_id                UUID NOT NULL REFERENCES session_sources(id)
        ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED,
    revision_id              UUID NOT NULL REFERENCES session_metrics_revisions(id)
        ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED,
    generation_id            UUID NOT NULL REFERENCES session_source_generations(id)
        ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED,
    validated_through_cursor BIGINT NOT NULL,
    source_high_water_cursor BIGINT NOT NULL,
    PRIMARY KEY (rollup_version_id, revision_id),
    UNIQUE (rollup_version_id, source_id),
    CONSTRAINT session_family_rollup_revision_ref_cursor_check CHECK (
        validated_through_cursor >= 0 AND
        source_high_water_cursor >= validated_through_cursor
    )
);

CREATE INDEX idx_session_family_rollup_revision_refs_revision
    ON session_family_rollup_revision_refs(revision_id, rollup_version_id);

CREATE TABLE session_family_token_totals (
    id                         BIGSERIAL PRIMARY KEY,
    rollup_version_id          UUID NOT NULL REFERENCES session_family_rollup_versions(id) ON DELETE CASCADE,
    root_session_id            UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id                    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id_snapshot           UUID,
    department_id_snapshot     UUID,
    provider                   TEXT NOT NULL,
    canonical_model            TEXT NOT NULL DEFAULT '',
    billing_variant            TEXT NOT NULL,
    uncached_input_tokens      BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens          BIGINT NOT NULL DEFAULT 0,
    cache_write_5m_tokens      BIGINT NOT NULL DEFAULT 0,
    cache_write_1h_tokens      BIGINT NOT NULL DEFAULT 0,
    output_tokens              BIGINT NOT NULL DEFAULT 0,
    total_tokens               BIGINT NOT NULL DEFAULT 0,
    self_total_tokens          BIGINT NOT NULL DEFAULT 0,
    subagent_total_tokens      BIGINT NOT NULL DEFAULT 0,
    estimated_cost_usd         NUMERIC(30, 12),
    estimated_cost_cny         NUMERIC(30, 12),
    pricing_status             TEXT NOT NULL,
    contribution_count         BIGINT NOT NULL DEFAULT 0,
    unpriced_contribution_count BIGINT NOT NULL DEFAULT 0,
    quality_status             TEXT NOT NULL,
    CONSTRAINT session_family_token_total_tokens_check CHECK (
        uncached_input_tokens >= 0 AND cache_read_tokens >= 0 AND
        cache_write_5m_tokens >= 0 AND cache_write_1h_tokens >= 0 AND output_tokens >= 0 AND
        total_tokens = uncached_input_tokens + cache_read_tokens +
            cache_write_5m_tokens + cache_write_1h_tokens + output_tokens AND
        self_total_tokens >= 0 AND subagent_total_tokens >= 0 AND
        total_tokens = self_total_tokens + subagent_total_tokens
    ),
    CONSTRAINT session_family_token_total_pricing_check CHECK (
        pricing_status IN ('pricing_pending', 'priced', 'partially_priced', 'unpriced')
    ),
    CONSTRAINT session_family_token_total_contribution_count_check CHECK (
        contribution_count >= 0 AND unpriced_contribution_count >= 0
    ),
    CONSTRAINT session_family_token_total_quality_check CHECK (
        quality_status IN ('exact', 'estimated', 'pending', 'conflict')
    )
);

CREATE UNIQUE INDEX idx_session_family_token_totals_identity
    ON session_family_token_totals(
        rollup_version_id, root_session_id, user_id,
        COALESCE(team_id_snapshot, '00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(department_id_snapshot, '00000000-0000-0000-0000-000000000000'::uuid),
        provider, canonical_model, billing_variant
    );

CREATE INDEX idx_session_family_token_totals_user
    ON session_family_token_totals(user_id, rollup_version_id, root_session_id);

CREATE TABLE session_family_daily_usage (
    id                         BIGSERIAL PRIMARY KEY,
    rollup_version_id          UUID NOT NULL REFERENCES session_family_rollup_versions(id) ON DELETE CASCADE,
    root_session_id            UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id                    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id_snapshot           UUID,
    department_id_snapshot     UUID,
    activity_date              DATE NOT NULL,
    activity_start_at          TIMESTAMPTZ NOT NULL,
    activity_end_at            TIMESTAMPTZ NOT NULL,
    provider                   TEXT NOT NULL,
    canonical_model            TEXT NOT NULL DEFAULT '',
    billing_variant            TEXT NOT NULL,
    uncached_input_tokens      BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens          BIGINT NOT NULL DEFAULT 0,
    cache_write_5m_tokens      BIGINT NOT NULL DEFAULT 0,
    cache_write_1h_tokens      BIGINT NOT NULL DEFAULT 0,
    output_tokens              BIGINT NOT NULL DEFAULT 0,
    total_tokens               BIGINT NOT NULL DEFAULT 0,
    self_total_tokens          BIGINT NOT NULL DEFAULT 0,
    subagent_total_tokens      BIGINT NOT NULL DEFAULT 0,
    estimated_cost_usd         NUMERIC(30, 12),
    estimated_cost_cny         NUMERIC(30, 12),
    pricing_status             TEXT NOT NULL,
    contribution_count         BIGINT NOT NULL DEFAULT 0,
    unpriced_contribution_count BIGINT NOT NULL DEFAULT 0,
    quality_status             TEXT NOT NULL,
    CONSTRAINT session_family_daily_usage_tokens_check CHECK (
        uncached_input_tokens >= 0 AND cache_read_tokens >= 0 AND
        cache_write_5m_tokens >= 0 AND cache_write_1h_tokens >= 0 AND output_tokens >= 0 AND
        total_tokens = uncached_input_tokens + cache_read_tokens +
            cache_write_5m_tokens + cache_write_1h_tokens + output_tokens AND
        self_total_tokens >= 0 AND subagent_total_tokens >= 0 AND
        total_tokens = self_total_tokens + subagent_total_tokens
    ),
    CONSTRAINT session_family_daily_usage_activity_range_check CHECK (
        activity_end_at >= activity_start_at
    ),
    CONSTRAINT session_family_daily_usage_pricing_check CHECK (
        pricing_status IN ('pricing_pending', 'priced', 'partially_priced', 'unpriced')
    ),
    CONSTRAINT session_family_daily_usage_contribution_count_check CHECK (
        contribution_count >= 0 AND unpriced_contribution_count >= 0
    ),
    CONSTRAINT session_family_daily_usage_quality_check CHECK (
        quality_status IN ('exact', 'estimated', 'pending', 'conflict')
    )
);

CREATE UNIQUE INDEX idx_session_family_daily_usage_identity
    ON session_family_daily_usage(
        rollup_version_id, root_session_id, user_id, activity_date,
        COALESCE(team_id_snapshot, '00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(department_id_snapshot, '00000000-0000-0000-0000-000000000000'::uuid),
        provider, canonical_model, billing_variant
    );

CREATE INDEX idx_session_family_daily_usage_user_date
    ON session_family_daily_usage(user_id, activity_date, rollup_version_id);

CREATE TABLE session_chunk_usage (
    id                         BIGSERIAL PRIMARY KEY,
    rollup_version_id          UUID NOT NULL REFERENCES session_family_rollup_versions(id) ON DELETE CASCADE,
    root_session_id            UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    member_session_id          UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    chunk_id                   UUID NOT NULL REFERENCES session_upload_chunks(id) ON DELETE CASCADE,
    user_id                    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id_snapshot           UUID,
    department_id_snapshot     UUID,
    activity_date              DATE NOT NULL,
    provider                   TEXT NOT NULL,
    canonical_model            TEXT NOT NULL DEFAULT '',
    billing_variant            TEXT NOT NULL,
    uncached_input_tokens      BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens          BIGINT NOT NULL DEFAULT 0,
    cache_write_5m_tokens      BIGINT NOT NULL DEFAULT 0,
    cache_write_1h_tokens      BIGINT NOT NULL DEFAULT 0,
    output_tokens              BIGINT NOT NULL DEFAULT 0,
    total_tokens               BIGINT NOT NULL DEFAULT 0,
    estimated_cost_usd         NUMERIC(30, 12),
    estimated_cost_cny         NUMERIC(30, 12),
    pricing_status             TEXT NOT NULL,
    contribution_count         BIGINT NOT NULL DEFAULT 0,
    unpriced_contribution_count BIGINT NOT NULL DEFAULT 0,
    quality_status             TEXT NOT NULL,
    CONSTRAINT session_chunk_usage_tokens_check CHECK (
        uncached_input_tokens >= 0 AND cache_read_tokens >= 0 AND
        cache_write_5m_tokens >= 0 AND cache_write_1h_tokens >= 0 AND output_tokens >= 0 AND
        total_tokens = uncached_input_tokens + cache_read_tokens +
            cache_write_5m_tokens + cache_write_1h_tokens + output_tokens
    ),
    CONSTRAINT session_chunk_usage_pricing_check CHECK (
        pricing_status IN ('pricing_pending', 'priced', 'partially_priced', 'unpriced')
    ),
    CONSTRAINT session_chunk_usage_contribution_count_check CHECK (
        contribution_count >= 0 AND unpriced_contribution_count >= 0
    ),
    CONSTRAINT session_chunk_usage_quality_check CHECK (
        quality_status IN ('exact', 'estimated', 'pending', 'conflict')
    )
);

CREATE UNIQUE INDEX idx_session_chunk_usage_identity
    ON session_chunk_usage(
        rollup_version_id, root_session_id, member_session_id, chunk_id,
        user_id, activity_date,
        COALESCE(team_id_snapshot, '00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(department_id_snapshot, '00000000-0000-0000-0000-000000000000'::uuid),
        provider, canonical_model, billing_variant
    );

CREATE INDEX idx_session_chunk_usage_user_date
    ON session_chunk_usage(user_id, activity_date, rollup_version_id, root_session_id);

CREATE INDEX idx_session_chunk_usage_chunk
    ON session_chunk_usage(chunk_id, rollup_version_id);

ALTER TABLE token_query_snapshots
    ADD COLUMN snapshot_version TEXT NOT NULL DEFAULT 'component-v1',
    ADD COLUMN rollup_count BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT token_query_snapshot_version_check CHECK (
        snapshot_version IN ('component-v1', 'rollup-v2')
    ),
    ADD CONSTRAINT token_query_snapshot_rollup_count_check CHECK (rollup_count >= 0);

CREATE INDEX idx_token_query_snapshots_expiry
    ON token_query_snapshots(expires_at, id);

CREATE TABLE token_query_snapshot_rollups (
    snapshot_id                UUID NOT NULL REFERENCES token_query_snapshots(id) ON DELETE CASCADE,
    rollup_version_id          UUID NOT NULL REFERENCES session_family_rollup_versions(id) ON DELETE RESTRICT,
    family_version_id          UUID NOT NULL REFERENCES session_family_versions(id) ON DELETE RESTRICT,
    root_session_id            UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    root_session_ref           TEXT NOT NULL,
    agent_type                 TEXT NOT NULL,
    user_id                    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_display_name          TEXT NOT NULL,
    summary_snapshot           TEXT,
    matched_member_session_id  UUID REFERENCES sessions(id) ON DELETE SET NULL,
    matched_member_session_ref TEXT,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (snapshot_id, rollup_version_id),
    UNIQUE (snapshot_id, root_session_id)
);

CREATE INDEX idx_token_query_snapshot_rollups_user
    ON token_query_snapshot_rollups(snapshot_id, user_id, root_session_id);
