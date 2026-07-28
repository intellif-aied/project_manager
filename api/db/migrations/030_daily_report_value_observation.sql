CREATE TABLE report_generation_snapshots (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id                  UUID NOT NULL UNIQUE REFERENCES ai_runs(id),
    report_id               UUID NOT NULL,
    user_id                 BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    report_date             DATE NOT NULL,
    generated_content       TEXT NOT NULL,
    generated_content_sha256 TEXT NOT NULL,
    summary_content         TEXT NOT NULL DEFAULT '',
    summary_sha256          TEXT NOT NULL,
    variant_manifest_json   JSONB NOT NULL,
    variant_sha256          TEXT NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_generation_snapshot_content_check CHECK (btrim(generated_content) <> ''),
    CONSTRAINT report_generation_snapshot_content_hash_check CHECK (generated_content_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT report_generation_snapshot_summary_hash_check CHECK (summary_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT report_generation_snapshot_variant_object_check CHECK (jsonb_typeof(variant_manifest_json) = 'object'),
    CONSTRAINT report_generation_snapshot_variant_hash_check CHECK (variant_sha256 ~ '^[0-9a-f]{64}$')
);

CREATE INDEX idx_report_generation_snapshots_date
    ON report_generation_snapshots(report_date, created_at, run_id);
CREATE INDEX idx_report_generation_snapshots_user_date
    ON report_generation_snapshots(user_id, report_date, created_at);
CREATE INDEX idx_report_generation_snapshots_variant
    ON report_generation_snapshots(variant_sha256, report_date);

CREATE TABLE report_user_outcome_events (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_id            UUID NOT NULL,
    user_id              BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    report_date          DATE NOT NULL,
    managed_agent_run_id UUID REFERENCES ai_runs(id),
    action               TEXT NOT NULL,
    content              TEXT,
    content_sha256       TEXT,
    action_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_user_outcome_action_check CHECK (action IN ('saved', 'submitted', 'deleted')),
    CONSTRAINT report_user_outcome_content_check CHECK (
        (action IN ('saved', 'submitted') AND content IS NOT NULL AND content_sha256 ~ '^[0-9a-f]{64}$') OR
        (action = 'deleted' AND content IS NULL AND content_sha256 IS NULL)
    )
);

CREATE INDEX idx_report_user_outcomes_user_date
    ON report_user_outcome_events(user_id, report_date, action_at, id);
CREATE INDEX idx_report_user_outcomes_date
    ON report_user_outcome_events(report_date, action_at, user_id, id);
CREATE INDEX idx_report_user_outcomes_run
    ON report_user_outcome_events(managed_agent_run_id, action_at, id)
    WHERE managed_agent_run_id IS NOT NULL;
CREATE INDEX idx_report_user_outcomes_report
    ON report_user_outcome_events(report_id, action_at, id);

CREATE INDEX idx_ai_runs_personal_daily_report_date
    ON ai_runs (
        (COALESCE(input_ref_json #>> '{period,date}', input_ref_json ->> 'report_date', '')),
        created_at,
        user_id
    )
    WHERE business_type = 'report_agent_run'
      AND input_ref_json ->> 'report_type' = 'personal_daily';

CREATE INDEX idx_daily_reports_report_date
    ON daily_reports(report_date, user_id);

CREATE INDEX idx_team_reports_source_daily
    ON team_reports USING GIN(source_daily_report_ids);
