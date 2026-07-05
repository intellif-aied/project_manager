DROP TABLE IF EXISTS task_dependencies;

CREATE TABLE IF NOT EXISTS work_item_relations (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_type   TEXT NOT NULL CHECK (source_type IN ('requirement', 'task')),
    source_id     UUID NOT NULL,
    target_type   TEXT NOT NULL CHECK (target_type IN ('requirement', 'task')),
    target_id     UUID NOT NULL,
    relation_type TEXT NOT NULL DEFAULT 'depends_on' CHECK (relation_type = 'depends_on'),
    note          TEXT,
    created_by    BIGINT REFERENCES users(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (source_type <> target_type OR source_id <> target_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_work_item_relations_unique
    ON work_item_relations(source_type, source_id, target_type, target_id, relation_type);

CREATE INDEX IF NOT EXISTS idx_work_item_relations_source
    ON work_item_relations(source_type, source_id);

CREATE INDEX IF NOT EXISTS idx_work_item_relations_target
    ON work_item_relations(target_type, target_id);
