CREATE TABLE report_review_jobs (
    run_id                UUID PRIMARY KEY REFERENCES ai_runs(id) ON DELETE CASCADE,
    user_id               BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    brief_hash            TEXT NOT NULL,
    context_hash          TEXT NOT NULL,
    status                TEXT NOT NULL DEFAULT 'pending',
    attempts              INTEGER NOT NULL DEFAULT 0,
    due_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    external_task_id      TEXT,
    input_json            JSONB NOT NULL,
    decision_json         JSONB,
    final_brief_json      JSONB,
    finalization_mode     TEXT,
    model_id              TEXT,
    lease_owner           TEXT,
    lease_until           TIMESTAMPTZ,
    last_error            TEXT,
    started_at            TIMESTAMPTZ,
    finished_at           TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_review_jobs_status_check CHECK (
        status IN ('pending', 'submitting', 'running', 'finalizing', 'written', 'failed')
    ),
    CONSTRAINT report_review_jobs_brief_hash_check CHECK (brief_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT report_review_jobs_context_hash_check CHECK (context_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT report_review_jobs_attempts_check CHECK (attempts >= 0),
    CONSTRAINT report_review_jobs_input_check CHECK (jsonb_typeof(input_json) = 'object'),
    CONSTRAINT report_review_jobs_decision_check CHECK (
        decision_json IS NULL OR jsonb_typeof(decision_json) = 'object'
    ),
    CONSTRAINT report_review_jobs_final_brief_check CHECK (
        final_brief_json IS NULL OR jsonb_typeof(final_brief_json) = 'object'
    )
);

CREATE INDEX idx_report_review_jobs_pending
    ON report_review_jobs(due_at, created_at)
    WHERE status IN ('pending', 'submitting');

CREATE INDEX idx_report_review_jobs_running
    ON report_review_jobs(updated_at)
    WHERE status IN ('running', 'finalizing');
