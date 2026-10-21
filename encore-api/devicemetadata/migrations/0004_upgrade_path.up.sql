-- T27: Upgrade path support
-- Add bound_thing_model columns to device_templates (the only allowed change to existing tables)
ALTER TABLE device_templates
    ADD COLUMN IF NOT EXISTS bound_thing_model_id UUID,
    ADD COLUMN IF NOT EXISTS bound_thing_model_snapshot_id UUID;

-- Add topic_profile column to devices
ALTER TABLE devices
    ADD COLUMN IF NOT EXISTS topic_profile VARCHAR(32) NOT NULL DEFAULT 'legacy';

-- migration_log table: records every upgrade action and enables rollback
CREATE TABLE IF NOT EXISTS migration_log (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                UUID NOT NULL,
    type                     VARCHAR(64) NOT NULL,    -- LEGACY_TEMPLATE_TO_THING_MODEL / DEVICE_TOPIC_TO_DEVICES
    source_id                VARCHAR(64),              -- v1 template_id or device_id (string to support old non-UUID ids)
    target_thing_model_id    UUID,
    target_template_id       UUID,
    payload                  JSONB NOT NULL DEFAULT '{}'::jsonb,  -- renames, warnings, items_count, etc.
    status                   VARCHAR(16) NOT NULL DEFAULT 'SUCCESS',  -- SUCCESS / ROLLBACKED / FAILED
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by               UUID,
    rolledback_at            TIMESTAMPTZ
);
CREATE INDEX idx_ml_tenant     ON migration_log(tenant_id);
CREATE INDEX idx_ml_source     ON migration_log(tenant_id, source_id);
CREATE INDEX idx_ml_type       ON migration_log(tenant_id, type);
CREATE INDEX idx_ml_created    ON migration_log(tenant_id, created_at DESC);
