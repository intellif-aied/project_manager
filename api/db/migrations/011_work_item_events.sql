CREATE TABLE IF NOT EXISTS work_item_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_type     TEXT NOT NULL CHECK (target_type IN ('requirement', 'task')),
    target_id       UUID NOT NULL,
    requirement_id  UUID,
    task_id         UUID,
    actor_id        BIGINT REFERENCES users(id),
    actor_name      TEXT NOT NULL DEFAULT '',
    actor_role      TEXT NOT NULL DEFAULT '',
    event_type      TEXT NOT NULL,
    event_title     TEXT NOT NULL,
    before_data     JSONB NOT NULL DEFAULT '{}'::jsonb,
    after_data      JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_work_item_events_requirement
    ON work_item_events(requirement_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_work_item_events_task
    ON work_item_events(task_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_work_item_events_target
    ON work_item_events(target_type, target_id, created_at DESC);
