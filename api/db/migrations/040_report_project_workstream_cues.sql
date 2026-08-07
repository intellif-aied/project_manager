ALTER TABLE report_project_occurrences
    ADD COLUMN workstream_cues_json JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE report_project_occurrences
    ADD CONSTRAINT report_project_occurrence_workstream_cues_check
    CHECK (jsonb_typeof(workstream_cues_json) = 'array');
