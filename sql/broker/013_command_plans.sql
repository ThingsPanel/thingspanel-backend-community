-- One-use preview/authorization records in the existing command owner. This
-- table is not a second dispatch queue; all accepted actions use requests.
CREATE TABLE IF NOT EXISTS public.broker_command_plans (
    id uuid PRIMARY KEY,
    tenant_id varchar(36) NOT NULL,
    caller_id varchar(128) NOT NULL,
    intent varchar(128) NOT NULL,
    device_id varchar(36) NOT NULL,
    target_device_id varchar(36) NOT NULL,
    route_cell varchar(128) NOT NULL,
    route_epoch bigint NOT NULL CHECK (route_epoch > 0),
    canonical_request bytea NOT NULL,
    payload bytea NOT NULL,
    context_sha256 bytea NOT NULL CHECK (octet_length(context_sha256)=32),
    plan_sha256 bytea NOT NULL CHECK (octet_length(plan_sha256)=32),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    authorization_id uuid,
    authorized_at timestamptz,
    revoked_at timestamptz,
    command_id uuid UNIQUE REFERENCES public.broker_command_requests(id),
    CHECK ((authorization_id IS NULL) = (authorized_at IS NULL))
);
CREATE INDEX IF NOT EXISTS broker_command_plans_scope_idx
    ON public.broker_command_plans(tenant_id,caller_id,created_at DESC);
CREATE INDEX IF NOT EXISTS broker_command_plans_expiry_idx
    ON public.broker_command_plans(expires_at) WHERE command_id IS NULL;
