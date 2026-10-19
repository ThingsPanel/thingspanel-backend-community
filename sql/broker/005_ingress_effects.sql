-- A business transaction persists automation intent together with domain rows.
-- The dispatch result is distinct from a device's physical execution result.
CREATE TABLE IF NOT EXISTS broker_ingress_effects (
 id uuid PRIMARY KEY, receipt_id uuid NOT NULL REFERENCES broker_ingress_receipts(id),
 tenant_id varchar(256) NOT NULL, device_id varchar(256) NOT NULL,
 effect_kind varchar(32) NOT NULL, payload jsonb NOT NULL,
 state varchar(24) NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','dispatching','completed','failed','unknown','fenced')),
 last_error text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(receipt_id,device_id,effect_kind)
);
CREATE INDEX IF NOT EXISTS broker_effects_pending ON broker_ingress_effects(created_at) WHERE state='pending';
