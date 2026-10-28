-- Tenant default notification policy. The pointer is kept separately from
-- alarm_config so future alarms resolve it at trigger time. The revision is
-- retained as a safe diagnostic target if a published route is later stopped.
CREATE TABLE IF NOT EXISTS public.notification_tenant_default_policies (
    source_deployment_id VARCHAR(128) NOT NULL,
    tenant_id VARCHAR(36) NOT NULL,
    native_group_id VARCHAR(128),
    alias_group_id VARCHAR(36),
    group_revision BIGINT NOT NULL DEFAULT 0 CHECK (group_revision >= 0),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (source_deployment_id, tenant_id),
    CHECK ((native_group_id IS NULL AND alias_group_id IS NULL AND group_revision = 0)
        OR (native_group_id IS NOT NULL AND alias_group_id IS NOT NULL AND group_revision > 0))
);

COMMENT ON TABLE public.notification_tenant_default_policies IS
	'Tenant-scoped default native notification policy pointer. Alarm events snapshot the resolved route into the source outbox.';

-- Register the exact API resource with Casbin. The handler independently
-- requires a tenant administrator and derives tenant scope from signed claims.
INSERT INTO public.casbin_rule (ptype, v0, v1, v2)
SELECT 'g2', 'api/v1/notification-default-policy', 'api/v1/notification-default-policy', ''
WHERE NOT EXISTS (
    SELECT 1 FROM public.casbin_rule
    WHERE ptype = 'g2' AND v0 = 'api/v1/notification-default-policy'
      AND v1 = 'api/v1/notification-default-policy'
);

INSERT INTO public.casbin_rule (ptype, v0, v1, v2)
SELECT 'p', roles.role_name, 'api/v1/notification-default-policy', 'allow'
FROM (VALUES ('TENANT_ADMIN'), ('SYS_ADMIN')) AS roles(role_name)
WHERE NOT EXISTS (
    SELECT 1 FROM public.casbin_rule existing
    WHERE existing.ptype = 'p' AND existing.v0 = roles.role_name
      AND existing.v1 = 'api/v1/notification-default-policy' AND existing.v2 = 'allow'
);
