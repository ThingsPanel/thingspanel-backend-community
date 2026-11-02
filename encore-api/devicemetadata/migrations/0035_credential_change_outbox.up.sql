-- Credential rotation and revocation are runtime-visible changes, so they use the
-- same durable outbox as the other device concerns. The operation kind
-- 'credential_change' already exists in metadata_operations (0025).
ALTER TABLE device_runtime_outbox DROP CONSTRAINT device_runtime_outbox_kind_valid;
ALTER TABLE device_runtime_outbox ADD CONSTRAINT device_runtime_outbox_kind_valid
    CHECK (kind IN ('administrative_state', 'template_binding', 'gateway_binding', 'device_deletion', 'credential_change'));

-- 0033 created the outbox without the tenant-immutability guard every other
-- tenant-bearing table in this package carries.
CREATE TRIGGER trg_tenant_id_immutable
    BEFORE UPDATE OF tenant_id ON device_runtime_outbox
    FOR EACH ROW EXECUTE FUNCTION reject_tenant_id_reassignment();
