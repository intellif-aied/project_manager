ALTER TABLE sessions
    DROP CONSTRAINT IF EXISTS sessions_content_status_check;

ALTER TABLE sessions
    ADD CONSTRAINT sessions_content_status_check CHECK (
        content_status IN (
            'uploading', 'upload_failed', 'available',
            'clearing', 'clearing_failed', 'cleared', 'deleted'
        )
    );
