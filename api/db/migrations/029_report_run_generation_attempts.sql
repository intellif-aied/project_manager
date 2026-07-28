-- Bound model self-correction loops for the two-pass managed report flow.

CREATE TABLE report_run_generation_attempts (
    run_id UUID PRIMARY KEY REFERENCES ai_runs(id) ON DELETE CASCADE,
    brief_invalid_attempts INTEGER NOT NULL DEFAULT 0,
    result_invalid_attempts INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_run_generation_attempts_brief_check CHECK (brief_invalid_attempts >= 0),
    CONSTRAINT report_run_generation_attempts_result_check CHECK (result_invalid_attempts >= 0)
);
