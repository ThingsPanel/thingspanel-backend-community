-- Durable command intents for the community MQTT dispatcher.
CREATE TABLE IF NOT EXISTS public.broker_command_requests (
    id uuid PRIMARY KEY,
    tenant_id varchar(36) NOT NULL,
    caller_id varchar(128) NOT NULL,
    intent varchar(128) NOT NULL,
    idempotency_key varchar(256) NOT NULL,
    device_id varchar(36) NOT NULL,
    target_device_id varchar(36) NOT NULL,
    route_cell varchar(128) NOT NULL,
    route_epoch bigint NOT NULL CHECK (route_epoch > 0),
    canonical_request bytea NOT NULL,
    canonical_sha256 bytea NOT NULL CHECK (octet_length(canonical_sha256) = 32),
    payload bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    state varchar(32) NOT NULL CHECK (state IN (
        'accepted', 'dispatching', 'broker_acknowledged',
        'device_reported_success', 'device_reported_failure',
        'expired', 'failed', 'unknown', 'fenced'
    )),
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    dispatched_at timestamptz,
    completed_at timestamptz,
    CONSTRAINT broker_command_requests_idempotency_uq
        UNIQUE (tenant_id, caller_id, intent, idempotency_key)
);

CREATE INDEX IF NOT EXISTS broker_command_requests_claim_idx
    ON public.broker_command_requests (tenant_id, created_at, id)
    WHERE state = 'accepted';
CREATE INDEX IF NOT EXISTS broker_command_requests_stale_idx
    ON public.broker_command_requests (updated_at, id)
    WHERE state = 'dispatching';
CREATE INDEX IF NOT EXISTS broker_command_requests_device_idx
    ON public.broker_command_requests (tenant_id, target_device_id, created_at DESC);
