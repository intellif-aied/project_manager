CREATE TABLE team_sync_paths (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id         UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    normalized_path TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT team_sync_paths_absolute_check CHECK (left(normalized_path, 1) = '/'),
    CONSTRAINT team_sync_paths_root_check CHECK (normalized_path <> '/'),
    UNIQUE (team_id, normalized_path)
);

CREATE INDEX idx_team_sync_paths_team_user
    ON team_sync_paths(team_id, user_id, normalized_path);

ALTER TABLE sessions
    ADD COLUMN team_upload_team_id UUID REFERENCES teams(id) ON DELETE SET NULL,
    ADD COLUMN team_sync_path_id UUID REFERENCES team_sync_paths(id) ON DELETE SET NULL,
    ADD COLUMN team_uploaded_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX idx_sessions_team_agent_ref
    ON sessions(team_upload_team_id, agent_type, session_ref)
    WHERE team_upload_team_id IS NOT NULL;

ALTER TABLE session_source_generations
    ADD COLUMN upload_team_id UUID REFERENCES teams(id) ON DELETE SET NULL,
    ADD COLUMN prepared_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN team_sync_path_id UUID REFERENCES team_sync_paths(id) ON DELETE SET NULL;

CREATE INDEX idx_session_source_generations_upload_team
    ON session_source_generations(upload_team_id, id)
    WHERE upload_team_id IS NOT NULL;
