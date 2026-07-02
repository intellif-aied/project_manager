CREATE TABLE IF NOT EXISTS session_activity_slices (
    session_id             UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id                BIGINT NOT NULL REFERENCES users(id),
    activity_date          DATE NOT NULL,
    activity_start_at      TIMESTAMPTZ NOT NULL,
    activity_end_at        TIMESTAMPTZ NOT NULL,
    timezone               TEXT NOT NULL DEFAULT 'Asia/Shanghai',

    agent_type             TEXT NOT NULL DEFAULT 'claude_code',
    model                  TEXT,
    models                 TEXT[] NOT NULL DEFAULT '{}',

    summary                TEXT,
    excerpt                TEXT,
    message_count          INTEGER NOT NULL DEFAULT 0,
    source_event_count     INTEGER NOT NULL DEFAULT 0,
    tool_calls_json        JSONB NOT NULL DEFAULT '{}',
    git_commits            TEXT[] NOT NULL DEFAULT '{}',

    task_id                UUID REFERENCES tasks(id),
    requirement_id         UUID REFERENCES requirements(id),

    input_tokens           BIGINT NOT NULL DEFAULT 0,
    output_tokens          BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens  BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens      BIGINT NOT NULL DEFAULT 0,
    total_tokens           BIGINT NOT NULL DEFAULT 0,

    source_has_raw_log     BOOLEAN NOT NULL DEFAULT false,
    token_slice_strategy   TEXT NOT NULL DEFAULT 'exact',
    summary_strategy       TEXT NOT NULL DEFAULT 'rule',
    parser_version         TEXT NOT NULL DEFAULT 'v1',
    slice_version          INTEGER NOT NULL DEFAULT 1,
    is_estimated           BOOLEAN NOT NULL DEFAULT false,

    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (session_id, activity_date)
);

CREATE INDEX IF NOT EXISTS idx_session_activity_user_date
    ON session_activity_slices(user_id, activity_date DESC);

CREATE INDEX IF NOT EXISTS idx_session_activity_date
    ON session_activity_slices(activity_date DESC);

CREATE INDEX IF NOT EXISTS idx_session_activity_task_date
    ON session_activity_slices(task_id, activity_date DESC)
    WHERE task_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_session_activity_requirement_date
    ON session_activity_slices(requirement_id, activity_date DESC)
    WHERE requirement_id IS NOT NULL;
