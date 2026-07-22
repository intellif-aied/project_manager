-- Digest on-demand report run coordination.
-- This migration keeps historical runs and digest payloads readable while
-- allowing new report runs to attach a Selection before its Digest is frozen.

ALTER TABLE ai_runs
    ADD COLUMN execution_stage TEXT,
    ADD COLUMN stage_updated_at TIMESTAMPTZ,
    ADD COLUMN next_attempt_at TIMESTAMPTZ,
    ADD COLUMN execution_lease_owner TEXT,
    ADD COLUMN execution_lease_until TIMESTAMPTZ,
    ADD COLUMN stage_attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN digest_wait_deadline_at TIMESTAMPTZ,
    ADD COLUMN idempotency_key TEXT,
    ADD COLUMN active_dedupe_key TEXT,
    ADD COLUMN source_identity_set_sha256 TEXT,
    ADD COLUMN request_fingerprint TEXT,
    ADD COLUMN error_code TEXT,
    ADD COLUMN failure_stage TEXT,
    ADD COLUMN execution_input_json JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE ai_runs
    ADD CONSTRAINT ai_runs_execution_stage_check CHECK (
        execution_stage IS NULL OR execution_stage IN (
            'waiting_digest', 'building_context', 'submitting_agent',
            'agent_running', 'writing_result', 'completed'
        )
    ),
    ADD CONSTRAINT ai_runs_stage_attempts_check CHECK (stage_attempts >= 0),
    ADD CONSTRAINT ai_runs_execution_lease_check CHECK (
        (execution_lease_owner IS NULL) = (execution_lease_until IS NULL)
    ),
    ADD CONSTRAINT ai_runs_source_identity_hash_check CHECK (
        source_identity_set_sha256 IS NULL OR source_identity_set_sha256 ~ '^[0-9a-f]{64}$'
    ),
    ADD CONSTRAINT ai_runs_request_fingerprint_check CHECK (
        request_fingerprint IS NULL OR request_fingerprint ~ '^[0-9a-f]{64}$'
    ),
    ADD CONSTRAINT ai_runs_active_dedupe_key_check CHECK (
        active_dedupe_key IS NULL OR active_dedupe_key ~ '^[0-9a-f]{64}$'
    ),
    ADD CONSTRAINT ai_runs_execution_input_object_check CHECK (
        jsonb_typeof(execution_input_json) = 'object'
    ),
    ADD CONSTRAINT ai_runs_report_stage_status_check CHECK (
        business_type <> 'report_agent_run' OR execution_stage IS NULL OR
        (execution_stage IN ('waiting_digest', 'building_context', 'submitting_agent') AND status = 'pending') OR
        (execution_stage IN ('agent_running', 'writing_result') AND status = 'running') OR
        (execution_stage = 'completed' AND status IN ('succeeded', 'failed', 'timeout'))
    );

CREATE UNIQUE INDEX uq_ai_runs_idempotency
    ON ai_runs(user_id, business_type, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE UNIQUE INDEX uq_ai_runs_active_report_dedupe
    ON ai_runs(active_dedupe_key)
    WHERE business_type = 'report_agent_run'
      AND status IN ('pending', 'running')
      AND active_dedupe_key IS NOT NULL;

CREATE INDEX idx_ai_runs_report_claim
    ON ai_runs(next_attempt_at, created_at, id)
    WHERE business_type = 'report_agent_run'
      AND status = 'pending'
      AND execution_stage IN ('waiting_digest', 'building_context', 'submitting_agent');

CREATE INDEX idx_ai_runs_report_reconcile
    ON ai_runs(execution_stage, digest_wait_deadline_at, execution_lease_until, stage_updated_at)
    WHERE business_type = 'report_agent_run'
      AND status IN ('pending', 'running')
      AND execution_stage IS NOT NULL;

ALTER TABLE report_source_selections
    ADD COLUMN digest_frozen_at TIMESTAMPTZ;

UPDATE report_source_selections
SET digest_frozen_at = COALESCE(attached_at, created_at)
WHERE required_read_mode IN ('digest_v1', 'digest_v2')
  AND selection_digest_payload IS NOT NULL
  AND selection_digest_sha256 IS NOT NULL
  AND selection_digest_bytes IS NOT NULL
  AND selection_digest_compaction IS NOT NULL
  AND digest_version_snapshot IS NOT NULL
  AND redaction_version_snapshot IS NOT NULL;

ALTER TABLE report_source_selections
    DROP CONSTRAINT report_source_selection_digest_payload_check,
    DROP CONSTRAINT report_source_selection_digest_compaction_check,
    DROP CONSTRAINT report_source_selection_digest_budget_check;

ALTER TABLE report_source_selections
    ADD CONSTRAINT report_source_selection_digest_compaction_check CHECK (
        selection_digest_compaction IS NULL OR
        selection_digest_compaction IN ('detailed', 'compact', 'none')
    ),
    ADD CONSTRAINT report_source_selection_digest_budget_check CHECK (
        (digest_target_bytes_snapshot IS NULL AND digest_hard_limit_bytes_snapshot IS NULL) OR
        (digest_target_bytes_snapshot > 0 AND
         digest_hard_limit_bytes_snapshot >= digest_target_bytes_snapshot)
    ),
    ADD CONSTRAINT report_source_selection_digest_payload_check CHECK (
        required_read_mode NOT IN ('digest_v1', 'digest_v2') OR
        digest_frozen_at IS NULL OR (
            selection_digest_payload IS NOT NULL AND
            selection_digest_sha256 IS NOT NULL AND
            selection_digest_bytes = octet_length(selection_digest_payload) AND
            selection_digest_compaction IS NOT NULL AND
            digest_version_snapshot IS NOT NULL AND
            redaction_version_snapshot IS NOT NULL AND (
                (digest_version_snapshot = 'session-digest/v2.10.0' AND
                 selection_digest_compaction = 'none' AND
                 digest_target_bytes_snapshot IS NULL AND
                 digest_hard_limit_bytes_snapshot IS NULL) OR
                (digest_version_snapshot <> 'session-digest/v2.10.0' AND
                 selection_digest_compaction IN ('detailed', 'compact') AND
                 digest_target_bytes_snapshot IS NOT NULL AND
                 digest_hard_limit_bytes_snapshot IS NOT NULL)
            )
        )
    );

CREATE OR REPLACE FUNCTION protect_attached_report_source_digest_payload() RETURNS TRIGGER AS $$
BEGIN
    IF OLD.digest_frozen_at IS NOT NULL AND (
        NEW.required_read_mode IS DISTINCT FROM OLD.required_read_mode OR
        NEW.selection_digest_payload IS DISTINCT FROM OLD.selection_digest_payload OR
        NEW.selection_digest_sha256 IS DISTINCT FROM OLD.selection_digest_sha256 OR
        NEW.selection_digest_bytes IS DISTINCT FROM OLD.selection_digest_bytes OR
        NEW.selection_digest_compaction IS DISTINCT FROM OLD.selection_digest_compaction OR
        NEW.digest_version_snapshot IS DISTINCT FROM OLD.digest_version_snapshot OR
        NEW.redaction_version_snapshot IS DISTINCT FROM OLD.redaction_version_snapshot OR
        NEW.digest_target_bytes_snapshot IS DISTINCT FROM OLD.digest_target_bytes_snapshot OR
        NEW.digest_hard_limit_bytes_snapshot IS DISTINCT FROM OLD.digest_hard_limit_bytes_snapshot OR
        NEW.digest_frozen_at IS DISTINCT FROM OLD.digest_frozen_at
    ) THEN
        RAISE EXCEPTION 'attached report source digest payload is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

ALTER TABLE report_source_selection_items
    ADD CONSTRAINT report_source_selection_item_digest_snapshot_check CHECK (
        (digest_revision_id IS NULL AND digest_sha256_snapshot IS NULL AND digest_version_snapshot IS NULL) OR
        (digest_revision_id IS NOT NULL AND digest_sha256_snapshot IS NOT NULL AND digest_version_snapshot IS NOT NULL)
    );

CREATE INDEX idx_report_source_selection_items_digest_identity
    ON report_source_selection_items(
        session_content_slice_id,
        source_generation_id,
        content_projection_revision_id,
        content_epoch_snapshot,
        selection_id
    )
    WHERE session_content_slice_id IS NOT NULL;

ALTER TABLE session_slice_digest_revisions
    ADD COLUMN failure_class TEXT,
    ADD COLUMN failed_at TIMESTAMPTZ,
    ADD COLUMN rebuild_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN last_rebuild_at TIMESTAMPTZ,
    ADD COLUMN last_rebuild_run_id UUID REFERENCES ai_runs(id);

UPDATE session_slice_digest_revisions
SET failure_class = CASE
        WHEN error_code IN (
            'digest_v2_source_unavailable', 'digest_v2_identity_mismatch',
            'digest_v2_version_unsupported', 'digest_v2_payload_corrupt'
        ) THEN 'permanent'
        ELSE 'retryable'
    END,
    failed_at = COALESCE(ready_at, build_started_at, created_at)
WHERE status = 'failed';

ALTER TABLE session_slice_digest_revisions
    ADD CONSTRAINT session_slice_digest_failure_class_check CHECK (
        failure_class IS NULL OR failure_class IN ('retryable', 'permanent')
    ),
    ADD CONSTRAINT session_slice_digest_rebuild_count_check CHECK (
        rebuild_count BETWEEN 0 AND 2
    ),
    ADD CONSTRAINT session_slice_digest_failure_state_check CHECK (
        status <> 'failed' OR
        (error_code IS NOT NULL AND failure_class IS NOT NULL AND failed_at IS NOT NULL)
    ),
    ADD CONSTRAINT session_slice_digest_rebuild_audit_check CHECK (
        rebuild_count = 0 OR
        (last_rebuild_at IS NOT NULL AND last_rebuild_run_id IS NOT NULL)
    );

ALTER TABLE session_processing_jobs
    ADD COLUMN urgency TEXT NOT NULL DEFAULT 'background',
    ADD COLUMN urgency_raised_at TIMESTAMPTZ;

ALTER TABLE session_processing_jobs
    ADD CONSTRAINT session_processing_jobs_urgency_check CHECK (
        urgency IN ('background', 'interactive')
    ),
    ADD CONSTRAINT session_processing_jobs_urgency_raised_check CHECK (
        urgency <> 'interactive' OR urgency_raised_at IS NOT NULL
    );

DROP INDEX idx_session_processing_one_digest_v2_job_per_revision;

CREATE UNIQUE INDEX idx_session_processing_one_active_digest_v2_job_per_revision
    ON session_processing_jobs(job_type, target_digest_revision_id)
    WHERE job_type = 'build_content_slice_digest_v2'
      AND target_digest_revision_id IS NOT NULL
      AND status IN ('pending', 'leased', 'retry_wait');

CREATE INDEX idx_session_processing_digest_claim_by_urgency
    ON session_processing_jobs(
        job_type, urgency, status, next_retry_at, lease_until, created_at, id
    )
    WHERE job_type = 'build_content_slice_digest_v2';
