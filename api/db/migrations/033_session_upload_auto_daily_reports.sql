CREATE TABLE auto_daily_report_config (
    id            SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    enabled       BOOLEAN NOT NULL DEFAULT false,
    enabled_since TIMESTAMPTZ,
    updated_by    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT auto_daily_report_config_enabled_since_check
        CHECK ((enabled AND enabled_since IS NOT NULL) OR (NOT enabled AND enabled_since IS NULL))
);

INSERT INTO auto_daily_report_config (id, enabled)
VALUES (1, false);

CREATE TABLE auto_daily_report_config_events (
    id            BIGSERIAL PRIMARY KEY,
    enabled       BOOLEAN NOT NULL,
    enabled_since TIMESTAMPTZ,
    changed_by    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    changed_at    TIMESTAMPTZ NOT NULL,
    CONSTRAINT auto_daily_report_config_event_enabled_since_check
        CHECK ((enabled AND enabled_since IS NOT NULL) OR (NOT enabled AND enabled_since IS NULL))
);

CREATE INDEX idx_auto_daily_report_config_events_changed
    ON auto_daily_report_config_events(changed_at DESC, id DESC);

CREATE INDEX idx_report_source_slice_catalog_auto_daily
    ON report_source_slice_catalog(activity_end_at, activity_start_at, user_id, ready_at)
    WHERE status = 'ready';

CREATE TABLE auto_daily_report_states (
    user_id                            BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    report_date                        DATE NOT NULL,
    desired_source_fingerprint         TEXT NOT NULL,
    desired_source_slice_keys          UUID[] NOT NULL DEFAULT '{}',
    last_source_ready_at               TIMESTAMPTZ NOT NULL,
    due_at                             TIMESTAMPTZ,
    status                             TEXT NOT NULL DEFAULT 'pending',
    claimed_source_fingerprint         TEXT,
    claimed_source_slice_keys          UUID[],
    active_source_fingerprint          TEXT,
    active_run_id                      UUID REFERENCES ai_runs(id),
    last_completed_source_fingerprint  TEXT,
    lease_owner                        TEXT,
    lease_until                        TIMESTAMPTZ,
    last_error                         TEXT,
    created_at                         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, report_date),
    CONSTRAINT auto_daily_report_state_status_check CHECK (
        status IN ('pending', 'submitting', 'running', 'idle', 'blocked', 'failed', 'suppressed')
    ),
    CONSTRAINT auto_daily_report_state_desired_fingerprint_check
        CHECK (desired_source_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT auto_daily_report_state_claimed_fingerprint_check
        CHECK (claimed_source_fingerprint IS NULL OR claimed_source_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT auto_daily_report_state_active_fingerprint_check
        CHECK (active_source_fingerprint IS NULL OR active_source_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT auto_daily_report_state_completed_fingerprint_check
        CHECK (last_completed_source_fingerprint IS NULL OR last_completed_source_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT auto_daily_report_state_active_pair_check
        CHECK ((active_run_id IS NULL) = (active_source_fingerprint IS NULL)),
    CONSTRAINT auto_daily_report_state_claim_pair_check
        CHECK ((claimed_source_fingerprint IS NULL) = (claimed_source_slice_keys IS NULL)),
    CONSTRAINT auto_daily_report_state_nonempty_sources_check
        CHECK (cardinality(desired_source_slice_keys) > 0)
);

CREATE INDEX idx_auto_daily_report_states_due
    ON auto_daily_report_states(due_at, report_date, user_id)
    WHERE status IN ('pending', 'submitting');

CREATE INDEX idx_auto_daily_report_states_active_run
    ON auto_daily_report_states(active_run_id)
    WHERE active_run_id IS NOT NULL;
