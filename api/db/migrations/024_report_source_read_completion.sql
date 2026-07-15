ALTER TABLE report_source_selections
    ADD COLUMN IF NOT EXISTS read_completed_at TIMESTAMPTZ;
