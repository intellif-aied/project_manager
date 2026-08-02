ALTER TABLE report_projects DROP CONSTRAINT report_projects_source_type_check;
ALTER TABLE report_project_aliases DROP CONSTRAINT report_project_alias_source_type_check;
ALTER TABLE report_project_occurrences DROP CONSTRAINT report_project_occurrence_source_type_check;

UPDATE report_projects SET canonical_source_type = 'explicit_saved'
WHERE canonical_source_type = 'ai_confirmed';
UPDATE report_project_aliases SET source_type = 'explicit_saved'
WHERE source_type = 'ai_confirmed';
UPDATE report_project_occurrences SET source_type = 'explicit_saved'
WHERE source_type = 'ai_confirmed';
UPDATE report_projects SET canonical_source_weight = 0.400
WHERE canonical_source_type = 'legacy';
UPDATE report_project_aliases SET source_weight = 0.400
WHERE source_type = 'legacy';
UPDATE report_project_occurrences SET source_weight = 0.400
WHERE source_type = 'legacy';

ALTER TABLE report_projects ALTER COLUMN canonical_source_weight SET DEFAULT 0.400;
ALTER TABLE report_project_aliases ALTER COLUMN source_weight SET DEFAULT 0.400;
ALTER TABLE report_project_occurrences ALTER COLUMN source_weight SET DEFAULT 0.400;

ALTER TABLE report_projects
    ADD CONSTRAINT report_projects_source_type_check
        CHECK (canonical_source_type IN ('manual_final', 'human_edited', 'explicit_saved', 'auto_carried', 'legacy'));
ALTER TABLE report_project_aliases
    ADD CONSTRAINT report_project_alias_source_type_check
        CHECK (source_type IN ('manual_final', 'human_edited', 'explicit_saved', 'auto_carried', 'legacy'));
ALTER TABLE report_project_occurrences
    ADD CONSTRAINT report_project_occurrence_source_type_check
        CHECK (source_type IN ('manual_final', 'human_edited', 'explicit_saved', 'auto_carried', 'legacy'));

CREATE TABLE report_project_memory_jobs (
    user_id                    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    report_date                DATE NOT NULL,
    report_id                  UUID NOT NULL REFERENCES daily_reports(id) ON DELETE CASCADE,
    desired_source_fingerprint TEXT NOT NULL,
    claimed_source_fingerprint TEXT,
    status                     TEXT NOT NULL DEFAULT 'pending',
    due_at                     TIMESTAMPTZ NOT NULL,
    attempts                   INTEGER NOT NULL DEFAULT 0,
    external_task_id           TEXT,
    input_json                 JSONB,
    input_token_estimate       INTEGER,
    output_token_estimate      INTEGER,
    resolver_version           TEXT,
    model_id                   TEXT,
    proposal_json              JSONB,
    snapshot_id                UUID,
    lease_owner                TEXT,
    lease_until                TIMESTAMPTZ,
    last_error                 TEXT,
    started_at                 TIMESTAMPTZ,
    finished_at                TIMESTAMPTZ,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, report_date),
    CONSTRAINT report_project_memory_job_status_check CHECK (
        status IN ('pending', 'submitting', 'running', 'succeeded', 'failed')
    ),
    CONSTRAINT report_project_memory_job_desired_hash_check
        CHECK (desired_source_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT report_project_memory_job_claimed_hash_check
        CHECK (claimed_source_fingerprint IS NULL OR claimed_source_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT report_project_memory_job_attempts_check CHECK (attempts >= 0),
    CONSTRAINT report_project_memory_job_input_check CHECK (input_json IS NULL OR jsonb_typeof(input_json) = 'object'),
    CONSTRAINT report_project_memory_job_proposal_check CHECK (proposal_json IS NULL OR jsonb_typeof(proposal_json) = 'object')
);

CREATE INDEX idx_report_project_memory_jobs_due
    ON report_project_memory_jobs(due_at, report_date, user_id)
    WHERE status IN ('pending', 'failed', 'submitting');
CREATE INDEX idx_report_project_memory_jobs_running
    ON report_project_memory_jobs(updated_at, user_id)
    WHERE status = 'running';

CREATE TABLE report_project_memory_snapshots (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id               BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    report_id             UUID NOT NULL REFERENCES daily_reports(id) ON DELETE CASCADE,
    report_date           DATE NOT NULL,
    source_fingerprint    TEXT NOT NULL,
    resolver_version      TEXT NOT NULL,
    model_id              TEXT,
    input_json            JSONB NOT NULL,
    proposal_json         JSONB NOT NULL,
    project_memory_json   JSONB NOT NULL,
    input_token_estimate  INTEGER NOT NULL,
    output_token_estimate INTEGER NOT NULL,
    external_task_id      TEXT,
    duration_ms           BIGINT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_project_memory_snapshot_hash_check
        CHECK (source_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT report_project_memory_snapshot_resolver_check CHECK (btrim(resolver_version) <> ''),
    CONSTRAINT report_project_memory_snapshot_input_check CHECK (jsonb_typeof(input_json) = 'object'),
    CONSTRAINT report_project_memory_snapshot_proposal_check CHECK (jsonb_typeof(proposal_json) = 'object'),
    CONSTRAINT report_project_memory_snapshot_memory_check CHECK (jsonb_typeof(project_memory_json) = 'object'),
    UNIQUE (user_id, source_fingerprint, resolver_version)
);

ALTER TABLE report_project_memory_jobs
    ADD CONSTRAINT report_project_memory_jobs_snapshot_fk
        FOREIGN KEY (snapshot_id) REFERENCES report_project_memory_snapshots(id) ON DELETE SET NULL;

CREATE INDEX idx_report_project_memory_snapshots_user_date
    ON report_project_memory_snapshots(user_id, report_date DESC, created_at DESC);
