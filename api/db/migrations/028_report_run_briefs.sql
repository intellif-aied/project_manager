-- Persist the accepted semantic brief between the two model turns of a
-- managed personal daily report. Report Context remains immutable.

CREATE TABLE report_run_briefs (
    run_id UUID PRIMARY KEY REFERENCES ai_runs(id) ON DELETE CASCADE,
    schema_version TEXT NOT NULL,
    context_hash TEXT NOT NULL,
    brief_hash TEXT NOT NULL,
    brief_payload JSONB NOT NULL,
    model_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_run_briefs_schema_check CHECK (schema_version = 'report-brief/v1'),
    CONSTRAINT report_run_briefs_context_hash_check CHECK (context_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT report_run_briefs_brief_hash_check CHECK (brief_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT report_run_briefs_payload_check CHECK (jsonb_typeof(brief_payload) = 'object')
);

CREATE INDEX idx_report_run_briefs_context_hash
    ON report_run_briefs(context_hash);
