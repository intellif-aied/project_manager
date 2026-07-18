CREATE TABLE report_run_contexts (
    run_id               UUID PRIMARY KEY REFERENCES ai_runs(id) ON DELETE CASCADE,
    schema_version       TEXT NOT NULL,
    source_selection_id  UUID REFERENCES report_source_selections(id) ON DELETE SET NULL,
    context_hash         TEXT NOT NULL,
    context_payload      JSONB NOT NULL,
    context_bytes        BIGINT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_run_context_schema_check CHECK (schema_version = 'report-context/v1'),
    CONSTRAINT report_run_context_hash_check CHECK (context_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT report_run_context_bytes_check CHECK (context_bytes > 0)
);

CREATE INDEX idx_report_run_contexts_selection
    ON report_run_contexts(source_selection_id)
    WHERE source_selection_id IS NOT NULL;
