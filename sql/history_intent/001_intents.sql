-- Durable "history to be written" intents, owned by the storage module.
-- Snapshot and receipt commit in the relational transaction together with the
-- intent; the time-series backend is written afterwards and can be replayed
-- safely because (device_id, key, ts, generation) is the idempotency identity.
CREATE TABLE IF NOT EXISTS telemetry_history_intents (
    id BIGSERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    device_id TEXT NOT NULL,
    "key" TEXT NOT NULL,
    ts BIGINT NOT NULL,
    bool_v BOOLEAN,
    number_v DOUBLE PRECISION,
    string_v TEXT,
    generation BIGINT NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    claimed_by TEXT,
    claimed_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    applied_at TIMESTAMPTZ,
    CONSTRAINT telemetry_history_intents_identity UNIQUE (device_id, "key", ts, generation),
    CONSTRAINT telemetry_history_intents_state_check CHECK (state IN ('pending', 'applied')),
    CONSTRAINT telemetry_history_intents_tenant_check CHECK (length(tenant_id) > 0),
    CONSTRAINT telemetry_history_intents_applied_check CHECK ((state = 'applied') = (applied_at IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS telemetry_history_intents_pending
    ON telemetry_history_intents (id) WHERE state = 'pending';
