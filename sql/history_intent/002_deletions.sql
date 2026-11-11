-- Durable "remove this device's history" requests. A request is created in the same
-- relational transaction that deletes the device and is carried out against the
-- time-series backend afterwards; carrying it out twice is harmless.
CREATE TABLE IF NOT EXISTS telemetry_history_deletions (
    id BIGSERIAL PRIMARY KEY,
    device_id TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    claimed_by TEXT,
    claimed_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    applied_at TIMESTAMPTZ,
    CONSTRAINT telemetry_history_deletions_state_check CHECK (state IN ('pending', 'applied')),
    CONSTRAINT telemetry_history_deletions_device_check CHECK (length(device_id) > 0),
    CONSTRAINT telemetry_history_deletions_applied_check CHECK ((state = 'applied') = (applied_at IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS telemetry_history_deletions_pending
    ON telemetry_history_deletions (id) WHERE state = 'pending';
