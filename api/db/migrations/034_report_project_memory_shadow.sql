CREATE TABLE report_projects (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    canonical_name  TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'active',
    first_seen_on   DATE NOT NULL,
    last_seen_on    DATE NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_projects_name_check CHECK (btrim(canonical_name) <> '' AND btrim(normalized_name) <> ''),
    CONSTRAINT report_projects_status_check CHECK (status IN ('active', 'paused', 'ended')),
    CONSTRAINT report_projects_dates_check CHECK (first_seen_on <= last_seen_on),
    UNIQUE (user_id, normalized_name)
);

CREATE INDEX idx_report_projects_user_recent
    ON report_projects(user_id, last_seen_on DESC, id);

CREATE TABLE report_project_aliases (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id         UUID NOT NULL REFERENCES report_projects(id) ON DELETE CASCADE,
    alias              TEXT NOT NULL,
    normalized_alias   TEXT NOT NULL,
    alias_type         TEXT NOT NULL,
    source_report_id   UUID NOT NULL REFERENCES daily_reports(id) ON DELETE CASCADE,
    source_report_date DATE NOT NULL,
    confidence         NUMERIC(4,3) NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_project_alias_name_check CHECK (btrim(alias) <> '' AND btrim(normalized_alias) <> ''),
    CONSTRAINT report_project_alias_type_check CHECK (alias_type IN ('canonical', 'child_topic')),
    CONSTRAINT report_project_alias_confidence_check CHECK (confidence >= 0 AND confidence <= 1),
    UNIQUE (project_id, normalized_alias)
);

CREATE INDEX idx_report_project_aliases_lookup
    ON report_project_aliases(normalized_alias, project_id);

CREATE TABLE report_project_occurrences (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id         UUID NOT NULL REFERENCES report_projects(id) ON DELETE CASCADE,
    report_id          UUID NOT NULL REFERENCES daily_reports(id) ON DELETE CASCADE,
    report_date        DATE NOT NULL,
    observed_title     TEXT NOT NULL,
    child_topics_json  JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_project_occurrence_title_check CHECK (btrim(observed_title) <> ''),
    CONSTRAINT report_project_occurrence_children_check CHECK (jsonb_typeof(child_topics_json) = 'array'),
    UNIQUE (project_id, report_id)
);

CREATE INDEX idx_report_project_occurrences_recent
    ON report_project_occurrences(project_id, report_date DESC, report_id);

CREATE TABLE report_project_memory_states (
    user_id            BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    source_fingerprint TEXT NOT NULL,
    synced_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_project_memory_fingerprint_check CHECK (source_fingerprint ~ '^[0-9a-f]{64}$')
);

CREATE TABLE report_project_resolution_snapshots (
    run_id            UUID PRIMARY KEY REFERENCES ai_runs(id) ON DELETE CASCADE,
    user_id           BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    report_date       DATE NOT NULL,
    algorithm_version TEXT NOT NULL,
    snapshot_json     JSONB NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_project_resolution_algorithm_check CHECK (btrim(algorithm_version) <> ''),
    CONSTRAINT report_project_resolution_snapshot_check CHECK (jsonb_typeof(snapshot_json) = 'object')
);

CREATE INDEX idx_report_project_resolution_user_date
    ON report_project_resolution_snapshots(user_id, report_date DESC, run_id);
