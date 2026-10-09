-- Durable source-to-Encore notification relay. Provider secrets are never
-- stored in this outbox; request_body is the frozen notification source event.
CREATE TABLE IF NOT EXISTS public.notification_source_group_routes (
    source_deployment_id VARCHAR(128) NOT NULL,
    tenant_id VARCHAR(36) NOT NULL,
    legacy_group_id VARCHAR(36) NOT NULL,
    engine VARCHAR(16) NOT NULL CHECK (engine IN ('legacy', 'encore')),
    notification_group_id VARCHAR(36),
    bound_notification_group_id VARCHAR(36),
    group_revision BIGINT NOT NULL DEFAULT 0 CHECK (group_revision >= 0),
    route_version BIGINT NOT NULL DEFAULT 0 CHECK (route_version >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (source_deployment_id, tenant_id, legacy_group_id),
    UNIQUE (source_deployment_id, tenant_id, legacy_group_id, bound_notification_group_id),
    CHECK ((engine = 'legacy' AND notification_group_id IS NULL AND bound_notification_group_id IS NULL AND group_revision = 0)
        OR (engine = 'encore' AND notification_group_id IS NOT NULL AND bound_notification_group_id = notification_group_id AND group_revision > 0))
);

CREATE TABLE IF NOT EXISTS public.notification_source_group_route_revisions (
    source_deployment_id VARCHAR(128) NOT NULL,
    tenant_id VARCHAR(36) NOT NULL,
    legacy_group_id VARCHAR(36) NOT NULL,
    group_revision BIGINT NOT NULL CHECK (group_revision > 0),
    notification_group_id VARCHAR(36) NOT NULL,
    projection_key VARCHAR(128) NOT NULL,
    projected_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (source_deployment_id, tenant_id, legacy_group_id, group_revision),
    UNIQUE (source_deployment_id, tenant_id, legacy_group_id, group_revision, notification_group_id),
    FOREIGN KEY (source_deployment_id, tenant_id, legacy_group_id, notification_group_id)
        REFERENCES public.notification_source_group_routes (source_deployment_id, tenant_id, legacy_group_id, bound_notification_group_id)
);

CREATE TABLE IF NOT EXISTS public.notification_source_outbox (
    id UUID PRIMARY KEY,
    source_deployment_id VARCHAR(128) NOT NULL,
    tenant_id VARCHAR(36) NOT NULL,
    source_event_id VARCHAR(36) NOT NULL,
    source_action_id VARCHAR(36) NOT NULL,
    legacy_group_id VARCHAR(36) NOT NULL,
    notification_group_id VARCHAR(36) NOT NULL,
    group_revision BIGINT NOT NULL CHECK (group_revision > 0),
    idempotency_key VARCHAR(128) NOT NULL,
    request_body BYTEA NOT NULL,
    body_sha256 CHAR(64) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    state VARCHAR(24) NOT NULL CHECK (state IN ('pending', 'retry_wait', 'leased', 'handed_off', 'blocked', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_token UUID,
    lease_until TIMESTAMPTZ,
    handed_off_at TIMESTAMPTZ,
    failure_code VARCHAR(64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source_deployment_id, tenant_id, source_event_id, source_action_id),
    UNIQUE (source_deployment_id, tenant_id, idempotency_key),
    CHECK (expires_at > occurred_at),
    CHECK ((state = 'leased' AND lease_token IS NOT NULL AND lease_until IS NOT NULL)
        OR (state <> 'leased' AND lease_token IS NULL AND lease_until IS NULL)),
    CHECK ((state = 'handed_off' AND handed_off_at IS NOT NULL) OR state <> 'handed_off')
);

CREATE INDEX IF NOT EXISTS idx_notification_source_outbox_claim
    ON public.notification_source_outbox (next_attempt_at, created_at)
    WHERE state IN ('pending', 'retry_wait');

CREATE INDEX IF NOT EXISTS idx_notification_source_outbox_expired_lease
    ON public.notification_source_outbox (lease_until)
    WHERE state = 'leased';
