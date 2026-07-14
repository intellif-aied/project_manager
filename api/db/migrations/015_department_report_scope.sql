ALTER TABLE department_reports ADD COLUMN department_id UUID REFERENCES departments(id);
ALTER TABLE department_weekly_reports ADD COLUMN department_id UUID REFERENCES departments(id);

UPDATE department_reports r
SET department_id = d.id
FROM ai_runs ar JOIN departments d ON d.director_user_id = ar.user_id
WHERE r.managed_agent_run_id = ar.id;

UPDATE department_weekly_reports r
SET department_id = d.id
FROM ai_runs ar JOIN departments d ON d.director_user_id = ar.user_id
WHERE r.managed_agent_run_id = ar.id;

ALTER TABLE department_reports DROP CONSTRAINT department_reports_report_date_key;
ALTER TABLE department_weekly_reports DROP CONSTRAINT department_weekly_reports_week_start_key;
CREATE UNIQUE INDEX uq_department_reports_scope_date ON department_reports(department_id, report_date) WHERE department_id IS NOT NULL;
CREATE UNIQUE INDEX uq_department_weekly_reports_scope_week ON department_weekly_reports(department_id, week_start) WHERE department_id IS NOT NULL;
CREATE INDEX idx_department_reports_department ON department_reports(department_id);
CREATE INDEX idx_department_weekly_reports_department ON department_weekly_reports(department_id);
