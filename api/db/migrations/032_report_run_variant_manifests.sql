CREATE TABLE report_run_variant_manifests (
    run_id          UUID PRIMARY KEY REFERENCES ai_runs(id) ON DELETE CASCADE,
    manifest_json   JSONB NOT NULL,
    manifest_sha256 TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT report_run_variant_manifest_object_check
        CHECK (jsonb_typeof(manifest_json) = 'object'),
    CONSTRAINT report_run_variant_manifest_hash_check
        CHECK (manifest_sha256 ~ '^[0-9a-f]{64}$')
);

INSERT INTO report_run_variant_manifests (run_id, manifest_json, manifest_sha256, created_at)
SELECT run_id, variant_manifest_json, variant_sha256, created_at
FROM report_generation_snapshots
ON CONFLICT (run_id) DO NOTHING;

CREATE INDEX idx_report_run_variant_manifests_hash
    ON report_run_variant_manifests(manifest_sha256, created_at, run_id);
