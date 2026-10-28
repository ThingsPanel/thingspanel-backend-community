-- Trusted best-effort telemetry is fenced at its final batch commit. Activity
-- is an observation of that accepted sample, not a broker connection identity.
CREATE TABLE IF NOT EXISTS broker_telemetry_activity (
 tenant_id varchar(36) NOT NULL, device_id varchar(36) NOT NULL,
 transport_device_id varchar(36) NOT NULL, broker_cell varchar(128) NOT NULL,
 route_epoch bigint NOT NULL CHECK(route_epoch>0), receipt_id uuid NOT NULL REFERENCES broker_ingress_receipts(id),
 observed_at timestamptz NOT NULL, next_check_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,device_id)
);
CREATE INDEX IF NOT EXISTS broker_telemetry_activity_due ON broker_telemetry_activity(next_check_at);
