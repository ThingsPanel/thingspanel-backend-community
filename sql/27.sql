-- Stable target-environment identity for native thing models imported with a
-- device-template market version. A retry reuses the same native model and the
-- same local device config instead of creating duplicate records.
CREATE TABLE IF NOT EXISTS market_native_model_imports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id VARCHAR(100) NOT NULL,
    market_template_id VARCHAR(100) NOT NULL,
    market_version VARCHAR(128) NOT NULL,
    source_model_id VARCHAR(36) NOT NULL,
    source_snapshot_id VARCHAR(36) NOT NULL,
    source_revision INTEGER NOT NULL CHECK (source_revision > 0),
    dependency_checksum CHAR(64) NOT NULL,
    created_by VARCHAR(100) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'IMPORTING'
        CHECK (status IN ('IMPORTING', 'NATIVE_READY', 'COMPLETE')),
    local_model_id VARCHAR(36),
    local_snapshot_id VARCHAR(36),
    local_revision INTEGER,
    local_device_config_id VARCHAR(36),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_market_native_model_import_identity
        UNIQUE (tenant_id, market_template_id, market_version)
);

CREATE INDEX IF NOT EXISTS idx_market_native_model_import_source
    ON market_native_model_imports(tenant_id, source_model_id, source_snapshot_id);

COMMENT ON TABLE market_native_model_imports IS
    'Idempotent mapping from a pinned device-template market version to target-native model and local device config';
