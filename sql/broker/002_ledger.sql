-- Durable, auditable MQTT ingress receipts. Apply in the same schema as the
-- broker owner tables; tests isolate it with a per-run PostgreSQL search_path.
CREATE TABLE IF NOT EXISTS broker_ingress_receipts (
    id uuid PRIMARY KEY,
    tenant_id varchar(36) NOT NULL,
    device_id varchar(36) NOT NULL,
    stream varchar(64) NOT NULL,
    source_event_id varchar(256),
    broker_cell varchar(128) NOT NULL,
    route_epoch bigint NOT NULL CHECK (route_epoch > 0),
    payload bytea NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    canonical_sha256 bytea NOT NULL CHECK (octet_length(canonical_sha256) = 32),
    state varchar(16) NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'processed', 'dead', 'fenced')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    retry_after timestamptz NOT NULL DEFAULT now(),
    last_error varchar(512) NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz
);

CREATE UNIQUE INDEX IF NOT EXISTS broker_ingress_receipts_source_key
    ON broker_ingress_receipts (tenant_id, device_id, stream, source_event_id)
    WHERE source_event_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS broker_ingress_receipts_pending_idx
    ON broker_ingress_receipts (state, retry_after, created_at, id)
    WHERE state = 'pending';

CREATE INDEX IF NOT EXISTS broker_ingress_receipts_tenant_state_idx
    ON broker_ingress_receipts (tenant_id, state, created_at);
