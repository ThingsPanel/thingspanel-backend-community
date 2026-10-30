-- Device metadata mutations that the runtime owner must apply are published
-- through this outbox instead of being assumed applied. One row per accepted
-- operation: the desired document is the complete target state for that
-- concern so a consumer can converge on the newest row per (device, kind).
--
-- This table records intent only. It does not implement a worker; DM-RT-007
-- owns consumption, runtime application and the operation transition to
-- succeeded. Until that consumer exists the related metadata operations stay
-- pending, which is the truthful state and is visible to clients.

CREATE TABLE device_runtime_outbox (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    device_id UUID NOT NULL,
    operation_id UUID NOT NULL,
    kind VARCHAR(40) NOT NULL,
    generation BIGINT NOT NULL,
    desired JSONB NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    applied_at TIMESTAMPTZ,
    updated_by VARCHAR(128) NOT NULL,

    CONSTRAINT device_runtime_outbox_operation_unique UNIQUE (tenant_id, operation_id),
    CONSTRAINT device_runtime_outbox_device_fk
        FOREIGN KEY (tenant_id, device_id) REFERENCES devices (tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT device_runtime_outbox_operation_fk
        FOREIGN KEY (tenant_id, operation_id) REFERENCES metadata_operations (tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT device_runtime_outbox_kind_valid
        CHECK (kind IN ('administrative_state', 'template_binding', 'gateway_binding', 'device_deletion')),
    CONSTRAINT device_runtime_outbox_state_valid
        CHECK (state IN ('pending', 'applied', 'failed')),
    CONSTRAINT device_runtime_outbox_generation_range
        CHECK (generation BETWEEN 1 AND 9007199254740991),
    CONSTRAINT device_runtime_outbox_desired_object
        CHECK (jsonb_typeof(desired) = 'object'),
    CONSTRAINT device_runtime_outbox_attempts_nonnegative
        CHECK (attempts >= 0),
    CONSTRAINT device_runtime_outbox_applied_pair
        CHECK ((state = 'applied') = (applied_at IS NOT NULL)),
    CONSTRAINT device_runtime_outbox_updated_by_nonempty
        CHECK (length(btrim(updated_by)) BETWEEN 1 AND 128)
);

CREATE INDEX idx_device_runtime_outbox_pending
    ON device_runtime_outbox (created_at, id)
    WHERE state IN ('pending', 'failed');
CREATE INDEX idx_device_runtime_outbox_tenant_device
    ON device_runtime_outbox (tenant_id, device_id, created_at DESC);

CREATE FUNCTION update_device_runtime_outbox_updated_at()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF (to_jsonb(NEW) - 'updated_at') IS DISTINCT FROM (to_jsonb(OLD) - 'updated_at') THEN
        NEW.updated_at := NOW();
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_device_runtime_outbox_updated_at
    BEFORE UPDATE ON device_runtime_outbox
    FOR EACH ROW EXECUTE FUNCTION update_device_runtime_outbox_updated_at();

COMMENT ON TABLE device_runtime_outbox IS
    'Desired runtime state for accepted device metadata operations; consumed by the device runtime owner (DM-RT-007), which owns the operation transition to succeeded.';
