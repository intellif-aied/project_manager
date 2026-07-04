CREATE TABLE IF NOT EXISTS requirement_owners (
    requirement_id UUID NOT NULL REFERENCES requirements(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (requirement_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_requirement_owners_user
    ON requirement_owners(user_id);

INSERT INTO requirement_owners (requirement_id, user_id)
SELECT id, owner_id
FROM requirements
WHERE owner_id IS NOT NULL
ON CONFLICT DO NOTHING;
