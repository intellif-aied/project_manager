ALTER TABLE report_projects
    ADD COLUMN canonical_source_type TEXT NOT NULL DEFAULT 'legacy',
    ADD COLUMN canonical_source_weight NUMERIC(4,3) NOT NULL DEFAULT 0.500;

ALTER TABLE report_projects
    ADD CONSTRAINT report_projects_source_type_check
        CHECK (canonical_source_type IN ('manual_final', 'human_edited', 'ai_confirmed', 'legacy')),
    ADD CONSTRAINT report_projects_source_weight_check
        CHECK (canonical_source_weight >= 0 AND canonical_source_weight <= 1);

ALTER TABLE report_project_aliases
    ADD COLUMN source_type TEXT NOT NULL DEFAULT 'legacy',
    ADD COLUMN source_weight NUMERIC(4,3) NOT NULL DEFAULT 0.500;

ALTER TABLE report_project_aliases
    ADD CONSTRAINT report_project_alias_source_type_check
        CHECK (source_type IN ('manual_final', 'human_edited', 'ai_confirmed', 'legacy')),
    ADD CONSTRAINT report_project_alias_source_weight_check
        CHECK (source_weight >= 0 AND source_weight <= 1);

ALTER TABLE report_project_occurrences
    ADD COLUMN source_type TEXT NOT NULL DEFAULT 'legacy',
    ADD COLUMN source_weight NUMERIC(4,3) NOT NULL DEFAULT 0.500;

ALTER TABLE report_project_occurrences
    ADD CONSTRAINT report_project_occurrence_source_type_check
        CHECK (source_type IN ('manual_final', 'human_edited', 'ai_confirmed', 'legacy')),
    ADD CONSTRAINT report_project_occurrence_source_weight_check
        CHECK (source_weight >= 0 AND source_weight <= 1);
