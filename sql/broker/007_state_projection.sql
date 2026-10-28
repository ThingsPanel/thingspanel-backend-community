-- Durable, bounded retry queue for projection of broker connection state into
-- the existing Community devices/status-history tables. One row per device.
CREATE TABLE IF NOT EXISTS public.broker_state_projection_queue (
 tenant_id varchar(256) NOT NULL,
 device_id varchar(256) NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, device_id)
);
CREATE INDEX IF NOT EXISTS broker_state_projection_queue_order
 ON public.broker_state_projection_queue(updated_at, tenant_id, device_id);
