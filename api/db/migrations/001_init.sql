-- Aida fresh schema. Authentication is delegated to AIHub.
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE teams (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL UNIQUE,
    director_user_id BIGINT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id             BIGINT PRIMARY KEY,
    username       TEXT NOT NULL DEFAULT '',
    nickname       TEXT NOT NULL DEFAULT '',
    email          TEXT NOT NULL DEFAULT '',
    name           TEXT NOT NULL DEFAULT '',
    employee_id    TEXT NOT NULL DEFAULT '',
    app_role       TEXT NOT NULL DEFAULT 'employee' CHECK (app_role IN ('admin', 'director', 'team_leader', 'pm', 'employee')),
    role           TEXT NOT NULL DEFAULT 'employee' CHECK (role IN ('admin', 'director', 'team_leader', 'pm', 'employee')),
    team_id        UUID REFERENCES teams(id),
    local_enabled  BOOLEAN NOT NULL DEFAULT true,
    aida_enabled   BOOLEAN NOT NULL DEFAULT false,
    status         TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deactivated')),
    last_synced_at TIMESTAMPTZ,
    deactivated_at TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_users_employee_id ON users(employee_id) WHERE employee_id <> '';
CREATE INDEX idx_users_team ON users(team_id);
CREATE INDEX idx_users_app_role ON users(app_role);
CREATE INDEX idx_users_role ON users(role);
CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_nickname ON users(nickname);
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_local_enabled ON users(local_enabled);
CREATE INDEX idx_users_aida_enabled ON users(aida_enabled);
CREATE INDEX idx_users_status ON users(status);

ALTER TABLE teams
    ADD CONSTRAINT teams_director_user_fk
    FOREIGN KEY (director_user_id) REFERENCES users(id);

CREATE TABLE requirements (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title               TEXT NOT NULL,
    description         TEXT NOT NULL,
    feishu_doc_url      TEXT,
    acceptance_criteria TEXT[] NOT NULL DEFAULT '{}',
    creator_id          BIGINT NOT NULL REFERENCES users(id),
    creator_role        TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'todo' CHECK (status IN ('todo', 'review', 'active', 'completed', 'cancelled')),
    priority            TEXT NOT NULL DEFAULT 'medium' CHECK (priority IN ('low', 'medium', 'high', 'urgent')),
    progress            INTEGER NOT NULL DEFAULT 0 CHECK (progress >= 0 AND progress <= 100),
    deadline            DATE,
    completed_at        TIMESTAMPTZ,
    version             BIGINT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_requirements_status ON requirements(status);
CREATE INDEX idx_requirements_creator ON requirements(creator_id);
CREATE INDEX idx_requirements_deadline ON requirements(deadline);

CREATE TABLE requirement_teams (
    requirement_id UUID NOT NULL REFERENCES requirements(id) ON DELETE CASCADE,
    team_id        UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    PRIMARY KEY (requirement_id, team_id)
);

CREATE TABLE tasks (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requirement_id      UUID NOT NULL REFERENCES requirements(id),
    title               TEXT NOT NULL,
    acceptance_criteria TEXT[],
    assignee_id         BIGINT REFERENCES users(id),
    creator_tl_id       BIGINT NOT NULL REFERENCES users(id),
    status              TEXT NOT NULL DEFAULT 'todo' CHECK (status IN ('todo', 'in_progress', 'done')),
    priority            TEXT NOT NULL DEFAULT 'medium' CHECK (priority IN ('low', 'medium', 'high')),
    progress            INTEGER NOT NULL DEFAULT 0 CHECK (progress >= 0 AND progress <= 100),
    due_date            DATE,
    completed_at        TIMESTAMPTZ,
    version             BIGINT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_tasks_requirement ON tasks(requirement_id);
CREATE INDEX idx_tasks_assignee ON tasks(assignee_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_due_date ON tasks(due_date) WHERE status <> 'done';

CREATE TABLE task_dependencies (
    task_id       UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    depends_on_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    dep_type      TEXT NOT NULL DEFAULT 'finish_to_start',
    PRIMARY KEY (task_id, depends_on_id),
    CHECK (task_id <> depends_on_id)
);

CREATE TABLE user_follows (
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL CHECK (target_type IN ('requirement', 'task')),
    target_id   UUID NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, target_type, target_id)
);
CREATE INDEX idx_user_follows_target ON user_follows(target_type, target_id);

CREATE TABLE sessions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_ref      TEXT NOT NULL,
    user_id          BIGINT NOT NULL REFERENCES users(id),
    agent_type       TEXT NOT NULL DEFAULT 'claude_code',
    started_at       TIMESTAMPTZ NOT NULL,
    ended_at         TIMESTAMPTZ,
    duration_secs    INTEGER,
    model            TEXT,
    models           TEXT[] NOT NULL DEFAULT '{}',
    summary          TEXT,
    tool_calls_json  JSONB,
    git_commits      TEXT[],
    task_id          UUID REFERENCES tasks(id),
    requirement_id   UUID REFERENCES requirements(id),
    match_confidence FLOAT,
    raw_log_url      TEXT,
    uploaded_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (session_ref, user_id)
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_task ON sessions(task_id);
CREATE INDEX idx_sessions_requirement ON sessions(requirement_id);
CREATE INDEX idx_sessions_started ON sessions(started_at);
CREATE INDEX idx_sessions_user_date ON sessions(user_id, started_at DESC);

CREATE TABLE token_usage (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id            UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id               BIGINT NOT NULL REFERENCES users(id),
    task_id               UUID REFERENCES tasks(id),
    requirement_id        UUID REFERENCES requirements(id),
    agent_type            TEXT NOT NULL,
    model                 TEXT NOT NULL,
    models                TEXT[] NOT NULL DEFAULT '{}',
    input_tokens          BIGINT NOT NULL DEFAULT 0,
    output_tokens         BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens     BIGINT NOT NULL DEFAULT 0,
    total_tokens          BIGINT NOT NULL DEFAULT 0,
    recorded_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_token_session ON token_usage(session_id);
CREATE INDEX idx_token_user ON token_usage(user_id);
CREATE INDEX idx_token_task ON token_usage(task_id);
CREATE INDEX idx_token_requirement ON token_usage(requirement_id);
CREATE INDEX idx_token_recorded ON token_usage(recorded_at);
CREATE INDEX idx_token_model ON token_usage(model);

CREATE TABLE documents (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        BIGINT NOT NULL REFERENCES users(id),
    title          TEXT NOT NULL,
    url            TEXT NOT NULL,
    description    TEXT,
    task_id        UUID REFERENCES tasks(id),
    requirement_id UUID REFERENCES requirements(id),
    uploaded_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_documents_user ON documents(user_id);
CREATE INDEX idx_documents_uploaded ON documents(uploaded_at DESC);

CREATE TABLE daily_reports (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              BIGINT NOT NULL REFERENCES users(id),
    report_date          DATE NOT NULL,
    content              TEXT NOT NULL,
    edited               BOOLEAN NOT NULL DEFAULT false,
    feishu_doc_url       TEXT,
    session_ids          UUID[],
    generation_mode      TEXT NOT NULL DEFAULT 'default',
    managed_agent_run_id UUID,
    agent_id             TEXT,
    agent_version_id     INTEGER,
    model_id             TEXT,
    status               TEXT CHECK (status IS NULL OR status IN ('saved', 'submitted')),
    submitted_content    TEXT,
    saved_at             TIMESTAMPTZ,
    submitted_at         TIMESTAMPTZ,
    submitted_to         TEXT CHECK (submitted_to IS NULL OR submitted_to IN ('team_leader', 'director')),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, report_date)
);
CREATE INDEX idx_reports_user_date ON daily_reports(user_id, report_date DESC);
CREATE INDEX idx_daily_reports_status_date ON daily_reports(status, report_date DESC);

CREATE TABLE team_reports (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id                 UUID NOT NULL REFERENCES teams(id),
    leader_id               BIGINT NOT NULL REFERENCES users(id),
    report_date             DATE NOT NULL,
    content                 TEXT NOT NULL,
    feishu_doc_url          TEXT,
    member_report_ids       UUID[] NOT NULL DEFAULT '{}',
    source_daily_report_ids UUID[] NOT NULL DEFAULT '{}',
    session_ids             UUID[] NOT NULL DEFAULT '{}',
    generation_mode         TEXT NOT NULL DEFAULT 'default',
    managed_agent_run_id    UUID,
    agent_id                TEXT,
    agent_version_id        INTEGER,
    model_id                TEXT,
    edited                  BOOLEAN NOT NULL DEFAULT false,
    status                  TEXT CHECK (status IS NULL OR status IN ('saved', 'submitted')),
    submitted_content       TEXT,
    saved_at                TIMESTAMPTZ,
    submitted_at            TIMESTAMPTZ,
    submitted_to            TEXT CHECK (submitted_to IS NULL OR submitted_to IN ('director')),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, report_date)
);
CREATE INDEX idx_team_reports_team_date ON team_reports(team_id, report_date DESC);
CREATE INDEX idx_team_reports_status_date ON team_reports(status, report_date DESC);

CREATE TABLE department_reports (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_date            DATE NOT NULL,
    content                TEXT NOT NULL,
    source_team_report_ids UUID[] NOT NULL DEFAULT '{}',
    generation_mode        TEXT NOT NULL DEFAULT 'default',
    managed_agent_run_id   UUID,
    agent_id               TEXT,
    agent_version_id       INTEGER,
    model_id               TEXT,
    edited                 BOOLEAN NOT NULL DEFAULT false,
    status                 TEXT CHECK (status IS NULL OR status IN ('saved', 'archived')),
    saved_at               TIMESTAMPTZ,
    archived_at            TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (report_date)
);
CREATE INDEX idx_department_reports_date ON department_reports(report_date DESC);
CREATE INDEX idx_department_reports_status_date ON department_reports(status, report_date DESC);

CREATE TABLE personal_weekly_reports (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 BIGINT NOT NULL REFERENCES users(id),
    week_start              DATE NOT NULL,
    week_end                DATE NOT NULL,
    content                 TEXT NOT NULL,
    submitted_content       TEXT,
    status                  TEXT NOT NULL CHECK (status IN ('saved', 'submitted')),
    saved_at                TIMESTAMPTZ,
    submitted_at            TIMESTAMPTZ,
    submitted_to            TEXT CHECK (submitted_to IS NULL OR submitted_to IN ('team_leader', 'director')),
    source_daily_report_ids UUID[] NOT NULL DEFAULT '{}',
    source_session_ids      UUID[] NOT NULL DEFAULT '{}',
    source_task_ids         UUID[] NOT NULL DEFAULT '{}',
    generation_mode         TEXT NOT NULL DEFAULT 'default',
    managed_agent_run_id    UUID,
    agent_id                TEXT,
    agent_version_id        INTEGER,
    model_id                TEXT,
    edited                  BOOLEAN NOT NULL DEFAULT false,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, week_start)
);
CREATE INDEX idx_personal_weekly_reports_user_week ON personal_weekly_reports(user_id, week_start DESC);
CREATE INDEX idx_personal_weekly_reports_status_week ON personal_weekly_reports(status, week_start DESC);

CREATE TABLE team_weekly_reports (
    id                                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id                           UUID NOT NULL REFERENCES teams(id),
    leader_id                         BIGINT NOT NULL REFERENCES users(id),
    week_start                        DATE NOT NULL,
    week_end                          DATE NOT NULL,
    content                           TEXT NOT NULL,
    source_daily_report_ids           UUID[] NOT NULL DEFAULT '{}',
    source_team_report_ids            UUID[] NOT NULL DEFAULT '{}',
    source_task_ids                   UUID[] NOT NULL DEFAULT '{}',
    source_personal_weekly_report_ids UUID[] NOT NULL DEFAULT '{}',
    generation_mode                   TEXT NOT NULL DEFAULT 'default',
    managed_agent_run_id              UUID,
    agent_id                          TEXT,
    agent_version_id                  INTEGER,
    model_id                          TEXT,
    edited                            BOOLEAN NOT NULL DEFAULT false,
    submitted_at                      TIMESTAMPTZ,
    created_at                        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, week_start)
);
CREATE INDEX idx_team_weekly_reports_team_week ON team_weekly_reports(team_id, week_start DESC);
CREATE INDEX idx_team_weekly_reports_week_end ON team_weekly_reports(week_end DESC);

CREATE TABLE department_weekly_reports (
    id                            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    week_start                    DATE NOT NULL,
    week_end                      DATE NOT NULL,
    content                       TEXT NOT NULL,
    source_team_weekly_report_ids UUID[] NOT NULL DEFAULT '{}',
    generation_mode               TEXT NOT NULL DEFAULT 'default',
    managed_agent_run_id          UUID,
    agent_id                      TEXT,
    agent_version_id              INTEGER,
    model_id                      TEXT,
    edited                        BOOLEAN NOT NULL DEFAULT false,
    archived_at                   TIMESTAMPTZ,
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (week_start)
);
CREATE INDEX idx_department_weekly_reports_week ON department_weekly_reports(week_start DESC);
CREATE INDEX idx_department_weekly_reports_week_end ON department_weekly_reports(week_end DESC);

CREATE TABLE ai_runs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             BIGINT NOT NULL REFERENCES users(id),
    business_type       TEXT NOT NULL,
    business_id         UUID,
    runtime_type        TEXT NOT NULL,
    agent_id            TEXT NOT NULL,
    agent_version_id    INTEGER,
    external_task_id    TEXT,
    external_session_id TEXT,
    model_id            TEXT,
    status              TEXT NOT NULL DEFAULT 'pending',
    input_ref_json      JSONB NOT NULL DEFAULT '{}'::jsonb,
    output_ref_json     JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message       TEXT,
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ai_runs_user_created ON ai_runs(user_id, created_at DESC);
CREATE INDEX idx_ai_runs_external_task ON ai_runs(external_task_id);
CREATE INDEX idx_ai_runs_business ON ai_runs(business_type, business_id);

ALTER TABLE daily_reports
    ADD CONSTRAINT daily_reports_managed_agent_run_fk
    FOREIGN KEY (managed_agent_run_id) REFERENCES ai_runs(id);

ALTER TABLE personal_weekly_reports
    ADD CONSTRAINT personal_weekly_reports_run_fk
    FOREIGN KEY (managed_agent_run_id) REFERENCES ai_runs(id);

ALTER TABLE team_reports
    ADD CONSTRAINT team_reports_run_fk
    FOREIGN KEY (managed_agent_run_id) REFERENCES ai_runs(id);

ALTER TABLE team_weekly_reports
    ADD CONSTRAINT team_weekly_reports_run_fk
    FOREIGN KEY (managed_agent_run_id) REFERENCES ai_runs(id);

ALTER TABLE department_reports
    ADD CONSTRAINT department_reports_run_fk
    FOREIGN KEY (managed_agent_run_id) REFERENCES ai_runs(id);

ALTER TABLE department_weekly_reports
    ADD CONSTRAINT department_weekly_reports_run_fk
    FOREIGN KEY (managed_agent_run_id) REFERENCES ai_runs(id);

CREATE TABLE managed_agent_schedules (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name                     TEXT NOT NULL,
    agent_id                 TEXT NOT NULL,
    model_id                 TEXT,
    message                  TEXT NOT NULL,
    params_json              JSONB NOT NULL DEFAULT '{}'::jsonb,
    schedule_type            TEXT NOT NULL DEFAULT 'daily' CHECK (schedule_type IN ('daily', 'weekly')),
    weekdays_json            JSONB NOT NULL DEFAULT '[]'::jsonb,
    time_of_day              TEXT NOT NULL,
    timezone                 TEXT NOT NULL DEFAULT 'Asia/Shanghai',
    enabled                  BOOLEAN NOT NULL DEFAULT true,
    last_run_at              TIMESTAMPTZ,
    last_ai_run_id           UUID REFERENCES ai_runs(id),
    run_kind                 TEXT NOT NULL DEFAULT 'generic_agent'
        CHECK (run_kind IN ('generic_agent', 'report_agent')),
    start_prompt_values_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    report_config_json       JSONB NOT NULL DEFAULT '{}'::jsonb,
    next_run_at              TIMESTAMPTZ,
    last_error               TEXT,
    last_skip_reason         TEXT,
    last_skip_at             TIMESTAMPTZ,
    last_skipped_trigger_at  TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_managed_agent_schedules_user ON managed_agent_schedules(user_id, created_at DESC);
CREATE INDEX idx_managed_agent_schedules_enabled ON managed_agent_schedules(enabled, time_of_day);
CREATE INDEX idx_managed_agent_schedules_due
    ON managed_agent_schedules(enabled, next_run_at)
    WHERE enabled = true;

CREATE TABLE managed_agent_profiles (
    agent_id      TEXT NOT NULL,
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    business_type TEXT NOT NULL DEFAULT 'generic',
    report_types  JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, user_id),
    CONSTRAINT managed_agent_profiles_business_type_check
        CHECK (business_type IN ('generic', 'report'))
);
CREATE INDEX idx_managed_agent_profiles_user ON managed_agent_profiles(user_id);
