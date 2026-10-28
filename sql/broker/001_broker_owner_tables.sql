-- Community Mosquitto broker owner state. This is intentionally a separate
-- migration from the existing ThingsPanel bootstrap SQL.
CREATE TABLE IF NOT EXISTS public.broker_client_ids (
    broker_cell varchar(128) NOT NULL,
    wire_client_id varchar(256) NOT NULL,
    tenant_id varchar(36) NOT NULL,
    device_id varchar(36) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT broker_client_ids_pkey PRIMARY KEY (broker_cell, wire_client_id)
);

CREATE INDEX IF NOT EXISTS broker_client_ids_owner_idx
    ON public.broker_client_ids (tenant_id, device_id);

CREATE TABLE IF NOT EXISTS public.broker_transport_bindings (
    tenant_id varchar(36) NOT NULL,
    device_id varchar(36) NOT NULL,
    broker_cell varchar(128) NOT NULL,
    route_epoch bigint NOT NULL DEFAULT 1 CHECK (route_epoch > 0),
    phase varchar(24) NOT NULL DEFAULT 'active'
        CHECK (phase IN ('active', 'connecting', 'draining')),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT broker_transport_bindings_pkey PRIMARY KEY (tenant_id, device_id)
);
