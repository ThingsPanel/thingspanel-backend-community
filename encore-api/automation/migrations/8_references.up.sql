-- A20: references, tombstones and the reason a rule went away.
--
-- Deleting a rule used to be a soft delete with no reason. That is enough to
-- stop it running, but not enough to answer the only question an operator asks
-- afterwards: "why did this stop, and was it me?".
ALTER TABLE auto_rules ADD COLUMN IF NOT EXISTS delete_reason text;

-- The reconciliation walks device-scoped bindings; the index makes that a
-- range scan instead of a table scan on a tenant with many templates.
CREATE INDEX IF NOT EXISTS auto_bindings_device_scope
    ON auto_bindings (tenant_id, device_id) WHERE enabled AND device_id IS NOT NULL;
