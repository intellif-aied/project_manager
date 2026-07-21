ALTER TABLE session_sources
    ADD COLUMN source_format TEXT NOT NULL DEFAULT 'legacy_native_v1',
    ADD COLUMN ingestion_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT session_sources_source_format_check
        CHECK (source_format IN ('legacy_native_v1', 'aida_event_v1')),
    ADD CONSTRAINT session_sources_canonical_metadata_check CHECK (
        source_format <> 'aida_event_v1' OR
        (
            btrim(COALESCE(ingestion_metadata->>'adapter_version', '')) <> '' AND
            ingestion_metadata->>'usage_capability' IN ('unavailable', 'estimated', 'exact')
        )
    );

COMMENT ON COLUMN session_sources.source_format IS
    'Immutable parser contract. Existing legacy rows remain legacy_native_v1; canonical sources use aida_event_v1.';

ALTER TABLE session_metering_envelopes
    ADD COLUMN owner_session_ref TEXT;
