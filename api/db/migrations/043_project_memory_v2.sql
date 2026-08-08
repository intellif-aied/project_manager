CREATE TABLE report_project_signals (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES report_projects(id) ON DELETE CASCADE,
    signal_type       TEXT NOT NULL,
    normalized_value  TEXT NOT NULL,
    display_value     TEXT NOT NULL,
    authority         TEXT NOT NULL,
    confidence        NUMERIC(5,4) NOT NULL,
    evidence_count    INTEGER NOT NULL DEFAULT 1,
    first_seen_on     DATE NOT NULL,
    last_seen_on      DATE NOT NULL,
    status            TEXT NOT NULL DEFAULT 'active',
    last_agent_run_id TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_project_signals_type_check
        CHECK (signal_type IN ('alias', 'workstream_cue')),
    CONSTRAINT report_project_signals_authority_check
        CHECK (authority IN ('human_edited', 'manual_report', 'explicit_saved', 'ai_inferred', 'machine')),
    CONSTRAINT report_project_signals_confidence_check
        CHECK (confidence >= 0 AND confidence <= 1),
    CONSTRAINT report_project_signals_status_check
        CHECK (status IN ('active', 'retired', 'rejected')),
    CONSTRAINT report_project_signals_dates_check
        CHECK (first_seen_on <= last_seen_on),
    UNIQUE (project_id, signal_type, normalized_value)
);

CREATE INDEX idx_report_project_signals_active
    ON report_project_signals(project_id, signal_type, last_seen_on DESC)
    WHERE status = 'active';

ALTER TABLE report_projects
    ADD COLUMN memory_schema_version TEXT NOT NULL DEFAULT 'project-memory/v1';

ALTER TABLE report_projects
    DROP CONSTRAINT report_projects_user_id_normalized_name_key;

ALTER TABLE report_projects
    ADD CONSTRAINT report_projects_user_name_memory_schema_key
        UNIQUE (user_id, normalized_name, memory_schema_version);

ALTER TABLE report_project_workspace_links
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active';

ALTER TABLE report_project_workspace_links
    ADD CONSTRAINT report_project_workspace_links_status_check
        CHECK (status IN ('active', 'retired'));

ALTER TABLE report_project_memory_snapshots
    ADD COLUMN evidence_cutoff_date DATE,
    ADD COLUMN evidence_watermark BIGINT NOT NULL DEFAULT 0;

UPDATE report_project_memory_snapshots
SET evidence_cutoff_date = report_date
WHERE evidence_cutoff_date IS NULL;

ALTER TABLE report_project_memory_snapshots
    ALTER COLUMN evidence_cutoff_date SET NOT NULL;

CREATE INDEX idx_report_project_memory_snapshots_as_of
    ON report_project_memory_snapshots(user_id, evidence_cutoff_date DESC, created_at DESC);

ALTER TABLE report_project_memory_jobs
    ADD COLUMN dirty_from_date DATE,
    ADD COLUMN desired_evidence_watermark BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN claimed_evidence_watermark BIGINT,
    ADD COLUMN rebuild_required BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN last_event_fingerprint TEXT;

UPDATE report_project_memory_jobs
SET last_event_fingerprint = desired_source_fingerprint;

UPDATE report_project_memory_jobs SET dirty_from_date = report_date;

CREATE TEMP TABLE report_project_memory_job_keep ON COMMIT DROP AS
SELECT DISTINCT ON (user_id)
       user_id, report_date AS keep_report_date,
       min(report_date) OVER (PARTITION BY user_id) AS dirty_from_date
FROM report_project_memory_jobs
ORDER BY user_id, report_date DESC, updated_at DESC;

UPDATE report_project_memory_jobs job
SET dirty_from_date = keep.dirty_from_date,
    status = 'pending', due_at = LEAST(job.due_at, now()), attempts = 0,
    claimed_source_fingerprint = NULL, claimed_evidence_watermark = NULL,
    external_task_id = NULL, lease_owner = NULL, lease_until = NULL,
    proposal_json = NULL, snapshot_id = NULL, last_error = NULL
FROM report_project_memory_job_keep keep
WHERE job.user_id = keep.user_id AND job.report_date = keep.keep_report_date;

UPDATE report_project_memory_jobs job
SET dirty_from_date = COALESCE((
    SELECT min(report_date) FROM (
        SELECT report.report_date
        FROM daily_reports report
        WHERE report.user_id = job.user_id AND report.report_date <= job.report_date
          AND report.status IN ('saved', 'submitted')
          AND NULLIF(BTRIM(COALESCE(NULLIF(report.submitted_content, ''), report.content, '')), '') IS NOT NULL
        ORDER BY report.report_date DESC, report.updated_at DESC
        LIMIT 20
    ) recent
), job.dirty_from_date);

DELETE FROM report_project_memory_jobs job
USING report_project_memory_job_keep keep
WHERE job.user_id = keep.user_id AND job.report_date <> keep.keep_report_date;

ALTER TABLE report_project_memory_jobs
    DROP CONSTRAINT report_project_memory_jobs_pkey;

ALTER TABLE report_project_memory_jobs
    ADD PRIMARY KEY (user_id),
    ALTER COLUMN dirty_from_date SET NOT NULL;
