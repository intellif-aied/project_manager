ALTER TABLE requirements
    ADD COLUMN IF NOT EXISTS owner_id BIGINT REFERENCES users(id);

CREATE INDEX IF NOT EXISTS idx_requirements_owner ON requirements(owner_id);
