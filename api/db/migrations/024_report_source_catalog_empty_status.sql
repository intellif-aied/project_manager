ALTER TABLE report_source_slice_catalog
    DROP CONSTRAINT report_source_slice_catalog_status_check,
    ADD CONSTRAINT report_source_slice_catalog_status_check CHECK (
        status IN ('building', 'ready', 'empty', 'failed', 'superseded', 'cleared')
    );

UPDATE report_source_slice_catalog
SET status = 'empty', updated_at = now()
WHERE status = 'failed'
  AND event_count = 0
  AND activity_start_at IS NULL
  AND activity_end_at IS NULL;
