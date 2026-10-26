-- Bounded lookup for trusted telemetry evidence by tenant, logical device and
-- the broker-observed timestamp stored in receipts.created_at.
CREATE INDEX IF NOT EXISTS broker_ingress_receipts_trusted_telemetry_device_time_idx
    ON broker_ingress_receipts (tenant_id, (metadata->>'logical_device_id'), created_at, id)
    WHERE stream = 'telemetry_sample'
      AND state = 'processed'
      AND metadata->>'source' = 'trusted_telemetry_batch';
