-- A19: bindings that a device inherits from its template.
--
-- The binding row already existed but nothing wrote it. Two things were missing
-- for it to carry "this template's rule, with these values, for this device":
--
--   detached_at  a detach is a decision with a date, not a deletion. "Why did
--                this stop running" has to stay answerable, and the old
--                template association has to remain countable for the
--                reconciliation.
--   binding_params on the pending row
--                the pending window was opened with a set of parameter values.
--                Recomputing them at fire time would let an edit made during
--                the window move the deadline that the customer is waiting on.

ALTER TABLE auto_bindings ADD COLUMN IF NOT EXISTS detached_at timestamptz;
ALTER TABLE auto_bindings ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

-- The reconciliation and the per-device resolution both look up "which
-- bindings does this template have", enabled first.
CREATE INDEX IF NOT EXISTS auto_bindings_config
    ON auto_bindings (tenant_id, device_config_id) WHERE enabled;
CREATE INDEX IF NOT EXISTS auto_bindings_rule_scope
    ON auto_bindings (tenant_id, rule_id);

ALTER TABLE auto_pending ADD COLUMN IF NOT EXISTS binding_params jsonb;
ALTER TABLE auto_pending ADD COLUMN IF NOT EXISTS binding_source text;
