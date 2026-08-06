CREATE TABLE report_run_fact_sources (
    run_id      UUID NOT NULL REFERENCES ai_runs(id) ON DELETE CASCADE,
    fact_ref    TEXT NOT NULL,
    session_ref TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, fact_ref, session_ref),
    CONSTRAINT report_run_fact_source_ref_check CHECK (btrim(fact_ref) <> '' AND btrim(session_ref) <> '')
);

CREATE INDEX idx_report_run_fact_sources_session
    ON report_run_fact_sources(run_id, session_ref, fact_ref);

CREATE TABLE report_project_workspace_links (
    project_id       UUID NOT NULL REFERENCES report_projects(id) ON DELETE CASCADE,
    workspace_id     UUID NOT NULL REFERENCES report_workspaces(id) ON DELETE CASCADE,
    confidence       NUMERIC(5,4) NOT NULL,
    source_weight    NUMERIC(5,4) NOT NULL,
    evidence_count   INTEGER NOT NULL DEFAULT 1,
    first_seen_on    DATE NOT NULL,
    last_seen_on     DATE NOT NULL,
    resolver_version TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, workspace_id),
    CONSTRAINT report_project_workspace_confidence_check CHECK (confidence >= 0 AND confidence <= 1),
    CONSTRAINT report_project_workspace_weight_check CHECK (source_weight >= 0 AND source_weight <= 1),
    CONSTRAINT report_project_workspace_time_check CHECK (last_seen_on >= first_seen_on)
);

CREATE INDEX idx_report_project_workspace_links_workspace
    ON report_project_workspace_links(workspace_id, last_seen_on DESC, confidence DESC);

CREATE TABLE report_project_workspace_link_evidence (
    project_id    UUID NOT NULL REFERENCES report_projects(id) ON DELETE CASCADE,
    workspace_id  UUID NOT NULL REFERENCES report_workspaces(id) ON DELETE CASCADE,
    report_id     UUID NOT NULL REFERENCES daily_reports(id) ON DELETE CASCADE,
    theme_ref     TEXT NOT NULL,
    report_date   DATE NOT NULL,
    confidence    NUMERIC(5,4) NOT NULL,
    source_weight NUMERIC(5,4) NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, workspace_id, report_id, theme_ref),
    CONSTRAINT report_project_workspace_evidence_confidence_check CHECK (confidence >= 0 AND confidence <= 1),
    CONSTRAINT report_project_workspace_evidence_weight_check CHECK (source_weight >= 0 AND source_weight <= 1)
);
