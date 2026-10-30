-- Native record of device command submissions (CommandReceipt in the v2 contract).
--
-- A row is written in the same transaction as the idempotency receipt, before the
-- Community runtime is called. Its state only moves forward from what the runtime
-- reports; a submission whose outcome is unknown stays 'pending' until reconciled
-- to 'unknown' and is never presented as delivered. Parameters are the frozen
-- accepted input, validated against the device's pinned ThingModel snapshot.

CREATE TABLE device_command_executions (
    tenant_id UUID NOT NULL,
    id UUID NOT NULL,
    device_id UUID NOT NULL,
    identifier TEXT NOT NULL,
    snapshot_id UUID NOT NULL,
    parameters JSONB NOT NULL,
    label VARCHAR(100),
    delivery_mode VARCHAR(16) NOT NULL DEFAULT 'immediate',
    timeout_seconds INTEGER NOT NULL DEFAULT 30,
    expires_at TIMESTAMPTZ,
    state VARCHAR(16) NOT NULL DEFAULT 'pending',
    reason TEXT,
    runtime_message_id VARCHAR(64),
    accepted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivery_at TIMESTAMPTZ,
    acknowledged_at TIMESTAMPTZ,
    created_by VARCHAR(128) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (tenant_id, id),
    CONSTRAINT device_command_executions_device_fk
        FOREIGN KEY (tenant_id, device_id) REFERENCES devices (tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT device_command_executions_identifier_nonempty
        CHECK (length(identifier) >= 1),
    CONSTRAINT device_command_executions_parameters_object
        CHECK (jsonb_typeof(parameters) = 'object'),
    CONSTRAINT device_command_executions_delivery_mode_valid
        CHECK (delivery_mode IN ('immediate', 'when_online')),
    CONSTRAINT device_command_executions_timeout_range
        CHECK (timeout_seconds BETWEEN 1 AND 300),
    CONSTRAINT device_command_executions_state_valid
        CHECK (state IN ('pending', 'accepted', 'delivered', 'acknowledged', 'failed', 'timed_out', 'expired', 'cancelled', 'unknown')),
    CONSTRAINT device_command_executions_created_by_nonempty
        CHECK (length(btrim(created_by)) BETWEEN 1 AND 128),
    CONSTRAINT device_command_executions_message_id_nonempty
        CHECK (runtime_message_id IS NULL OR length(btrim(runtime_message_id)) > 0)
);

CREATE INDEX idx_device_command_executions_device
    ON device_command_executions (tenant_id, device_id, accepted_at DESC, id DESC);
CREATE UNIQUE INDEX uq_device_command_executions_message
    ON device_command_executions (tenant_id, runtime_message_id)
    WHERE runtime_message_id IS NOT NULL;

CREATE TRIGGER trg_tenant_id_immutable
    BEFORE UPDATE OF tenant_id ON device_command_executions
    FOR EACH ROW EXECUTE FUNCTION reject_tenant_id_reassignment();

COMMENT ON TABLE device_command_executions IS
    'Native receipts for device command submissions; delivery state is reported by the Community runtime and never invented.';
