ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS creator_id BIGINT REFERENCES users(id);

CREATE TABLE IF NOT EXISTS requirement_responsibles (
    requirement_id UUID NOT NULL REFERENCES requirements(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (requirement_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_requirement_responsibles_user
    ON requirement_responsibles(user_id);

CREATE TABLE IF NOT EXISTS task_responsibles (
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_task_responsibles_user
    ON task_responsibles(user_id);
