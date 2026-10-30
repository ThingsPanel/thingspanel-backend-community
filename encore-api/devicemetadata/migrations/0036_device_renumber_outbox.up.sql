-- Device number changes are runtime-visible (identification rules, reconnect),
-- so they use the same durable outbox. metadata_operations already allows
-- 'device_renumber' (0025).
ALTER TABLE device_runtime_outbox DROP CONSTRAINT device_runtime_outbox_kind_valid;
ALTER TABLE device_runtime_outbox ADD CONSTRAINT device_runtime_outbox_kind_valid
    CHECK (kind IN ('administrative_state', 'template_binding', 'gateway_binding', 'device_deletion',
                    'credential_change', 'device_renumber'));
