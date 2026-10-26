-- Permanently rejected ingress frames only. This table is not an application
-- success path and contains no connection credentials or auth payload fields.
CREATE TABLE IF NOT EXISTS public.broker_ingress_quarantine (
    id varchar(64) PRIMARY KEY,
    tenant_id varchar(36) NOT NULL,
    device_id varchar(36) NOT NULL,
    broker_cell varchar(128) NOT NULL,
    route_epoch bigint NOT NULL CHECK (route_epoch > 0),
    topic text NOT NULL,
    body bytea NOT NULL,
    reason varchar(256) NOT NULL,
    occurrences bigint NOT NULL DEFAULT 1 CHECK (occurrences > 0),
    first_seen timestamptz NOT NULL,
    last_seen timestamptz NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS broker_ingress_quarantine_tenant_expiry
    ON public.broker_ingress_quarantine(tenant_id, expires_at);

-- Expiration never deletes live data automatically. An operator may inspect
-- counts/backup policy and explicitly purge expired rows with:
-- DELETE FROM public.broker_ingress_quarantine WHERE expires_at <= now();
