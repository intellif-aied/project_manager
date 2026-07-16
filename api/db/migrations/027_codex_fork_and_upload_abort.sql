ALTER TABLE sessions
    ADD COLUMN forked_at TIMESTAMPTZ,
    ADD COLUMN fork_source TEXT;

ALTER TABLE sessions
    DROP CONSTRAINT sessions_content_status_check,
    ADD CONSTRAINT sessions_content_status_check CHECK (
        content_status IN (
            'uploading', 'upload_failed', 'available',
            'clearing', 'clearing_failed', 'cleared', 'deleted'
        )
    );

ALTER TABLE session_parser_checkpoints
    ADD COLUMN root_metadata_seen BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN fork_parent_session_ref TEXT,
    ADD COLUMN fork_source TEXT,
    ADD COLUMN forked_at TIMESTAMPTZ,
    ADD COLUMN fork_baseline_ready BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN fork_baseline_missing BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN fork_metadata_conflict BOOLEAN NOT NULL DEFAULT false;
