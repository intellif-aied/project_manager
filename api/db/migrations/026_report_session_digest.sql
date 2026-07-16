CREATE TABLE session_slice_digest_revisions (
    id                             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_content_slice_id       UUID NOT NULL REFERENCES session_content_slices(id) ON DELETE CASCADE,
    content_projection_revision_id UUID NOT NULL REFERENCES session_content_projection_revisions(id) ON DELETE CASCADE,
    generation_id                  UUID NOT NULL REFERENCES session_source_generations(id) ON DELETE CASCADE,
    content_epoch                  BIGINT NOT NULL,
    digest_version                 TEXT NOT NULL,
    redaction_version              TEXT NOT NULL,
    status                         TEXT NOT NULL DEFAULT 'pending',
    digest_json                    JSONB,
    source_event_count             BIGINT NOT NULL DEFAULT 0,
    included_event_count           BIGINT NOT NULL DEFAULT 0,
    omitted_event_count            BIGINT NOT NULL DEFAULT 0,
    source_bytes                   BIGINT NOT NULL DEFAULT 0,
    digest_bytes                   INTEGER NOT NULL DEFAULT 0,
    truncated                      BOOLEAN NOT NULL DEFAULT false,
    source_sha256                  TEXT,
    digest_sha256                  TEXT,
    error_code                     TEXT,
    created_at                     TIMESTAMPTZ NOT NULL DEFAULT now(),
    build_started_at               TIMESTAMPTZ,
    ready_at                       TIMESTAMPTZ,
    superseded_at                  TIMESTAMPTZ,
    CONSTRAINT session_slice_digest_status_check
        CHECK (status IN ('pending', 'building', 'ready', 'failed', 'superseded')),
    CONSTRAINT session_slice_digest_epoch_check CHECK (content_epoch >= 0),
    CONSTRAINT session_slice_digest_count_check CHECK (
        source_event_count >= 0 AND included_event_count >= 0 AND omitted_event_count >= 0 AND
        included_event_count + omitted_event_count = source_event_count AND
        source_bytes >= 0 AND digest_bytes >= 0
    ),
    CONSTRAINT session_slice_digest_hash_check CHECK (
        (source_sha256 IS NULL OR source_sha256 ~ '^[0-9a-f]{64}$') AND
        (digest_sha256 IS NULL OR digest_sha256 ~ '^[0-9a-f]{64}$')
    ),
    CONSTRAINT session_slice_digest_ready_check CHECK (
        status <> 'ready' OR (
            digest_json IS NOT NULL AND source_sha256 IS NOT NULL AND digest_sha256 IS NOT NULL AND
            digest_bytes > 0 AND ready_at IS NOT NULL AND error_code IS NULL
        )
    ),
    CONSTRAINT session_slice_digest_json_object_check CHECK (
        digest_json IS NULL OR jsonb_typeof(digest_json) = 'object'
    ),
    CONSTRAINT session_slice_digest_failed_check CHECK (
        status <> 'failed' OR error_code IS NOT NULL
    ),
    UNIQUE (
        session_content_slice_id, content_projection_revision_id, content_epoch,
        digest_version, redaction_version
    )
);

CREATE FUNCTION protect_ready_session_slice_digest_revision() RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status = 'ready' AND (
        NEW.status NOT IN ('ready', 'superseded') OR
        NEW.session_content_slice_id IS DISTINCT FROM OLD.session_content_slice_id OR
        NEW.content_projection_revision_id IS DISTINCT FROM OLD.content_projection_revision_id OR
        NEW.generation_id IS DISTINCT FROM OLD.generation_id OR
        NEW.content_epoch IS DISTINCT FROM OLD.content_epoch OR
        NEW.digest_version IS DISTINCT FROM OLD.digest_version OR
        NEW.redaction_version IS DISTINCT FROM OLD.redaction_version OR
        NEW.digest_json IS DISTINCT FROM OLD.digest_json OR
        NEW.source_event_count IS DISTINCT FROM OLD.source_event_count OR
        NEW.included_event_count IS DISTINCT FROM OLD.included_event_count OR
        NEW.omitted_event_count IS DISTINCT FROM OLD.omitted_event_count OR
        NEW.source_bytes IS DISTINCT FROM OLD.source_bytes OR
        NEW.digest_bytes IS DISTINCT FROM OLD.digest_bytes OR
        NEW.truncated IS DISTINCT FROM OLD.truncated OR
        NEW.source_sha256 IS DISTINCT FROM OLD.source_sha256 OR
        NEW.digest_sha256 IS DISTINCT FROM OLD.digest_sha256 OR
        NEW.error_code IS DISTINCT FROM OLD.error_code OR
        NEW.created_at IS DISTINCT FROM OLD.created_at OR
        NEW.build_started_at IS DISTINCT FROM OLD.build_started_at OR
        NEW.ready_at IS DISTINCT FROM OLD.ready_at
    ) THEN
        RAISE EXCEPTION 'ready session digest revision is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_protect_ready_session_slice_digest_revision
BEFORE UPDATE ON session_slice_digest_revisions
FOR EACH ROW EXECUTE FUNCTION protect_ready_session_slice_digest_revision();

CREATE INDEX idx_session_slice_digest_reconcile
    ON session_slice_digest_revisions(status, digest_version, redaction_version, created_at);

ALTER TABLE session_processing_jobs
    ADD COLUMN target_digest_revision_id UUID
        REFERENCES session_slice_digest_revisions(id) ON DELETE CASCADE;

ALTER TABLE session_processing_jobs
    DROP CONSTRAINT session_processing_jobs_type_check;

ALTER TABLE session_processing_jobs
    ADD CONSTRAINT session_processing_jobs_type_check CHECK (job_type IN (
        'index_content_chunk', 'parse_usage_chunk', 'rebuild_content_revision',
        'rebuild_metrics_revision', 'build_metering_envelope', 'delete_object', 'purge_session',
        'build_content_slice_digest'
    ));

ALTER TABLE session_processing_jobs
    DROP CONSTRAINT session_processing_jobs_content_epoch_required;

ALTER TABLE session_processing_jobs
    ADD CONSTRAINT session_processing_jobs_content_epoch_required CHECK (
        job_type NOT IN (
            'index_content_chunk', 'rebuild_content_revision', 'build_metering_envelope',
            'delete_object', 'purge_session', 'build_content_slice_digest'
        ) OR content_epoch IS NOT NULL
    );

CREATE UNIQUE INDEX idx_session_processing_one_digest_job_per_revision
    ON session_processing_jobs(job_type, target_digest_revision_id)
    WHERE job_type = 'build_content_slice_digest' AND target_digest_revision_id IS NOT NULL;

ALTER TABLE report_source_selection_items
    ADD COLUMN session_content_slice_id UUID REFERENCES session_content_slices(id) ON DELETE SET NULL,
    ADD COLUMN digest_revision_id UUID REFERENCES session_slice_digest_revisions(id) ON DELETE SET NULL,
    ADD COLUMN digest_sha256_snapshot TEXT,
    ADD COLUMN digest_version_snapshot TEXT;

ALTER TABLE report_source_selection_items
    ADD CONSTRAINT report_source_selection_item_digest_hash_check CHECK (
        digest_sha256_snapshot IS NULL OR digest_sha256_snapshot ~ '^[0-9a-f]{64}$'
    );

CREATE INDEX idx_report_source_selection_items_digest
    ON report_source_selection_items(digest_revision_id)
    WHERE digest_revision_id IS NOT NULL;

ALTER TABLE report_source_selections
    ADD COLUMN required_read_mode TEXT NOT NULL DEFAULT 'full',
    ADD COLUMN read_completed_mode TEXT,
    ADD COLUMN selection_digest_payload BYTEA,
    ADD COLUMN selection_digest_sha256 TEXT,
    ADD COLUMN selection_digest_bytes INTEGER,
    ADD COLUMN selection_digest_compaction TEXT,
    ADD COLUMN digest_version_snapshot TEXT,
    ADD COLUMN redaction_version_snapshot TEXT,
    ADD COLUMN digest_target_bytes_snapshot INTEGER,
    ADD COLUMN digest_hard_limit_bytes_snapshot INTEGER;

UPDATE report_source_selections
SET read_completed_mode = 'full'
WHERE read_completed_at IS NOT NULL AND read_completed_mode IS NULL;

ALTER TABLE report_source_selections
    ADD CONSTRAINT report_source_selection_required_read_mode_check
        CHECK (required_read_mode IN ('full', 'digest_v1')),
    ADD CONSTRAINT report_source_selection_read_completed_mode_check
        CHECK (read_completed_mode IS NULL OR read_completed_mode IN ('full', 'digest_v1')),
    ADD CONSTRAINT report_source_selection_completed_required_mode_check
        CHECK (read_completed_mode IS NULL OR read_completed_mode = required_read_mode),
    ADD CONSTRAINT report_source_selection_digest_hash_check
        CHECK (selection_digest_sha256 IS NULL OR selection_digest_sha256 ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT report_source_selection_digest_bytes_check
        CHECK (selection_digest_bytes IS NULL OR selection_digest_bytes > 0),
    ADD CONSTRAINT report_source_selection_digest_compaction_check
        CHECK (selection_digest_compaction IS NULL OR selection_digest_compaction IN ('detailed', 'compact')),
    ADD CONSTRAINT report_source_selection_digest_budget_check CHECK (
        (digest_target_bytes_snapshot IS NULL AND digest_hard_limit_bytes_snapshot IS NULL) OR
        (digest_target_bytes_snapshot > 0 AND
         digest_hard_limit_bytes_snapshot >= digest_target_bytes_snapshot)
    ),
    ADD CONSTRAINT report_source_selection_digest_payload_check CHECK (
        required_read_mode <> 'digest_v1' OR (
            selection_digest_payload IS NOT NULL AND selection_digest_sha256 IS NOT NULL AND
            selection_digest_bytes = octet_length(selection_digest_payload) AND
            selection_digest_compaction IS NOT NULL AND digest_version_snapshot IS NOT NULL AND
            redaction_version_snapshot IS NOT NULL AND digest_target_bytes_snapshot IS NOT NULL AND
            digest_hard_limit_bytes_snapshot IS NOT NULL
        )
    );

CREATE FUNCTION protect_attached_report_source_digest_payload() RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status = 'attached' AND (
        NEW.required_read_mode IS DISTINCT FROM OLD.required_read_mode OR
        NEW.selection_digest_payload IS DISTINCT FROM OLD.selection_digest_payload OR
        NEW.selection_digest_sha256 IS DISTINCT FROM OLD.selection_digest_sha256 OR
        NEW.selection_digest_bytes IS DISTINCT FROM OLD.selection_digest_bytes OR
        NEW.selection_digest_compaction IS DISTINCT FROM OLD.selection_digest_compaction OR
        NEW.digest_version_snapshot IS DISTINCT FROM OLD.digest_version_snapshot OR
        NEW.redaction_version_snapshot IS DISTINCT FROM OLD.redaction_version_snapshot OR
        NEW.digest_target_bytes_snapshot IS DISTINCT FROM OLD.digest_target_bytes_snapshot OR
        NEW.digest_hard_limit_bytes_snapshot IS DISTINCT FROM OLD.digest_hard_limit_bytes_snapshot
    ) THEN
        RAISE EXCEPTION 'attached report source digest payload is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_protect_attached_report_source_digest_payload
BEFORE UPDATE ON report_source_selections
FOR EACH ROW EXECUTE FUNCTION protect_attached_report_source_digest_payload();
