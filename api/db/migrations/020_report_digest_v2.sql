ALTER TABLE session_processing_jobs
    DROP CONSTRAINT session_processing_jobs_type_check;

ALTER TABLE session_processing_jobs
    ADD CONSTRAINT session_processing_jobs_type_check CHECK (job_type IN (
        'index_content_chunk', 'parse_usage_chunk', 'rebuild_content_revision',
        'rebuild_metrics_revision', 'build_metering_envelope', 'delete_object', 'purge_session',
        'build_content_slice_digest', 'build_content_slice_digest_v2'
    ));

ALTER TABLE session_processing_jobs
    DROP CONSTRAINT session_processing_jobs_content_epoch_required;

ALTER TABLE session_processing_jobs
    ADD CONSTRAINT session_processing_jobs_content_epoch_required CHECK (
        job_type NOT IN (
            'index_content_chunk', 'rebuild_content_revision', 'build_metering_envelope',
            'delete_object', 'purge_session', 'build_content_slice_digest',
            'build_content_slice_digest_v2'
        ) OR content_epoch IS NOT NULL
    );

CREATE UNIQUE INDEX idx_session_processing_one_digest_v2_job_per_revision
    ON session_processing_jobs(job_type, target_digest_revision_id)
    WHERE job_type = 'build_content_slice_digest_v2'
        AND target_digest_revision_id IS NOT NULL;

ALTER TABLE report_source_selections
    DROP CONSTRAINT report_source_selection_required_read_mode_check,
    DROP CONSTRAINT report_source_selection_read_completed_mode_check,
    DROP CONSTRAINT report_source_selection_digest_payload_check;

ALTER TABLE report_source_selections
    ADD CONSTRAINT report_source_selection_required_read_mode_check
        CHECK (required_read_mode IN ('full', 'digest_v1', 'digest_v2')),
    ADD CONSTRAINT report_source_selection_read_completed_mode_check
        CHECK (read_completed_mode IS NULL OR read_completed_mode IN ('full', 'digest_v1', 'digest_v2')),
    ADD CONSTRAINT report_source_selection_digest_payload_check CHECK (
        required_read_mode NOT IN ('digest_v1', 'digest_v2') OR (
            selection_digest_payload IS NOT NULL AND selection_digest_sha256 IS NOT NULL AND
            selection_digest_bytes = octet_length(selection_digest_payload) AND
            selection_digest_compaction IS NOT NULL AND digest_version_snapshot IS NOT NULL AND
            redaction_version_snapshot IS NOT NULL AND digest_target_bytes_snapshot IS NOT NULL AND
            digest_hard_limit_bytes_snapshot IS NOT NULL
        )
    );
