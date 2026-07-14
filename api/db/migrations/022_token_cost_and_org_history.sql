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
