CREATE TABLE departments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    director_user_id BIGINT UNIQUE REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE teams ADD COLUMN department_id UUID REFERENCES departments(id);
ALTER TABLE users ADD COLUMN department_id UUID REFERENCES departments(id);
CREATE INDEX idx_teams_department ON teams(department_id);
CREATE INDEX idx_users_department ON users(department_id);

INSERT INTO departments (name, director_user_id)
SELECT '部门-' || director_user_id::text, director_user_id
FROM teams
WHERE director_user_id IS NOT NULL
GROUP BY director_user_id;

UPDATE teams t
SET department_id = d.id
FROM departments d
WHERE d.director_user_id = t.director_user_id;

UPDATE users u
SET department_id = d.id
FROM departments d
WHERE d.director_user_id = u.id;
