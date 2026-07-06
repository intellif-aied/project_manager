DROP TABLE IF EXISTS requirement_owners;

DROP INDEX IF EXISTS idx_requirements_owner;
ALTER TABLE requirements
    DROP COLUMN IF EXISTS owner_id;

DROP INDEX IF EXISTS idx_tasks_assignee;
ALTER TABLE tasks
    DROP COLUMN IF EXISTS assignee_id,
    DROP COLUMN IF EXISTS creator_tl_id;
