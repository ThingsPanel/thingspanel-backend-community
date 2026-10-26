-- Preserve the authenticated creator context for ingress_v1 expected messages.
-- This table intentionally has no FK so broker migrations can be applied before
-- the core application schema. Runtime code validates the referenced rows.
CREATE TABLE IF NOT EXISTS public.broker_expected_context (
    expected_id varchar(36) PRIMARY KEY,
    tenant_id varchar(36) NOT NULL,
    device_id varchar(36) NOT NULL,
    caller_id varchar(128) NOT NULL,
    send_type varchar(32) NOT NULL CHECK (send_type IN ('telemetry', 'attribute', 'command')),
    payload_sha256 varchar(64) NOT NULL CHECK (length(payload_sha256) = 64),
    created_at timestamptz NOT NULL DEFAULT now(),
    dispatch_attempts integer NOT NULL DEFAULT 0 CHECK (dispatch_attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS broker_expected_context_owner_idx
    ON public.broker_expected_context (tenant_id, device_id, created_at, expected_id);
CREATE INDEX IF NOT EXISTS broker_expected_context_dispatch_idx
    ON public.broker_expected_context (dispatch_attempts, next_attempt_at, created_at, expected_id);
