ALTER TABLE sessions
    ADD COLUMN repository_key TEXT;

ALTER TABLE sessions
    ADD CONSTRAINT sessions_repository_key_check
        CHECK (repository_key IS NULL OR repository_key ~ '^[0-9a-f]{64}$');

CREATE INDEX idx_sessions_user_repository_key
    ON sessions(user_id, repository_key, last_activity_at DESC)
    WHERE repository_key IS NOT NULL;

CREATE TABLE report_workspaces (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    first_seen_at    TIMESTAMPTZ NOT NULL,
    last_seen_at     TIMESTAMPTZ NOT NULL,
    resolver_version TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_workspace_identity_time_check
        CHECK (last_seen_at >= first_seen_at),
    CONSTRAINT report_workspace_identity_resolver_check
        CHECK (btrim(resolver_version) <> '')
);

CREATE INDEX idx_report_workspaces_user_recent
    ON report_workspaces(user_id, last_seen_at DESC, id);

CREATE TABLE report_workspace_keys (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id  UUID NOT NULL REFERENCES report_workspaces(id) ON DELETE CASCADE,
    key_kind      TEXT NOT NULL,
    key_hash      TEXT NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at  TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_workspace_key_kind_check
        CHECK (key_kind IN ('cwd', 'git_repository')),
    CONSTRAINT report_workspace_key_hash_check
        CHECK (key_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT report_workspace_key_time_check
        CHECK (last_seen_at >= first_seen_at),
    UNIQUE (user_id, key_kind, key_hash)
);

CREATE INDEX idx_report_workspace_keys_workspace
    ON report_workspace_keys(workspace_id, key_kind, id);

CREATE TABLE report_workspace_evidence (
    id                             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id                   UUID NOT NULL REFERENCES report_workspaces(id) ON DELETE CASCADE,
    evidence_hash                  TEXT NOT NULL,
    evidence_type                  TEXT NOT NULL,
    source_selection_id            UUID REFERENCES report_source_selections(id) ON DELETE SET NULL,
    source_session_id              UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source_slice_id                UUID REFERENCES session_content_slices(id) ON DELETE SET NULL,
    content_projection_revision_id UUID NOT NULL REFERENCES session_content_projection_revisions(id) ON DELETE CASCADE,
    start_cursor                   BIGINT NOT NULL,
    end_cursor                     BIGINT NOT NULL,
    observed_from                  TIMESTAMPTZ NOT NULL,
    observed_to                    TIMESTAMPTZ NOT NULL,
    created_at                     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_workspace_evidence_hash_check
        CHECK (evidence_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT report_workspace_evidence_type_check
        CHECK (evidence_type IN ('cwd', 'git_remote', 'repo_root', 'session_continuity')),
    CONSTRAINT report_workspace_evidence_cursor_check
        CHECK (start_cursor >= 0 AND end_cursor > start_cursor),
    CONSTRAINT report_workspace_evidence_time_check
        CHECK (observed_to >= observed_from),
    UNIQUE (user_id, evidence_hash)
);

CREATE INDEX idx_report_workspace_evidence_workspace_time
    ON report_workspace_evidence(workspace_id, observed_to DESC, id);

CREATE INDEX idx_report_workspace_evidence_session_cursor
    ON report_workspace_evidence(source_session_id, start_cursor, end_cursor);
