CREATE UNIQUE INDEX idx_session_content_events_source_range
    ON session_content_events (
        content_projection_revision_id, chunk_id, source_start_cursor, source_end_cursor
    );

CREATE UNIQUE INDEX idx_session_processing_one_rebuild_per_revision
    ON session_processing_jobs(job_type, target_revision_id)
    WHERE job_type = 'rebuild_content_revision' AND target_revision_id IS NOT NULL;

CREATE UNIQUE INDEX idx_session_processing_one_metering_per_generation_epoch
    ON session_processing_jobs(job_type, generation_id, content_epoch)
    WHERE job_type = 'build_metering_envelope' AND generation_id IS NOT NULL AND content_epoch IS NOT NULL;
