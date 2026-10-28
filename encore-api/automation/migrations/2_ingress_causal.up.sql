-- A05: input classification and causal context.
--
-- Every inbox row now records which adapter produced it, so an explain view can
-- name the input class instead of inferring it from the signal. Gateway-relayed
-- sub-device traffic keeps the relay id for the same reason.
ALTER TABLE auto_inbox ADD COLUMN IF NOT EXISTS input_class text;
ALTER TABLE auto_inbox ADD COLUMN IF NOT EXISTS gateway_id text;

-- The receipt itself must remember the chain, otherwise a crash between receipt
-- and evaluation would silently reset the hop count on recovery.
ALTER TABLE auto_inbox ADD COLUMN IF NOT EXISTS causal_root text;
ALTER TABLE auto_inbox ADD COLUMN IF NOT EXISTS causal_hop integer NOT NULL DEFAULT 0;

-- Latest-state probe used to recognise a repeated online notification. The
-- index is on the same key order as auto_state so the check stays a single
-- index lookup instead of a scan.
CREATE INDEX IF NOT EXISTS auto_inbox_state_probe
    ON auto_inbox (device_id, signal, key, observed_at DESC);

-- Causal chain for alarm-driven automation. causal_root is the transition that
-- started the chain; causal_hop is how many alarm-triggered runs deep we are.
-- A chain that exceeds the limit is stopped with a recorded reason instead of
-- looping forever.
ALTER TABLE auto_runs ADD COLUMN IF NOT EXISTS causal_root text;
ALTER TABLE auto_runs ADD COLUMN IF NOT EXISTS causal_hop integer NOT NULL DEFAULT 0;
ALTER TABLE auto_runs ADD COLUMN IF NOT EXISTS cause_event_id text;

CREATE INDEX IF NOT EXISTS auto_runs_causal
    ON auto_runs (tenant_id, causal_root) WHERE causal_root IS NOT NULL;

-- A pending window opened by an alarm-triggered rule must keep its chain
-- context across the wait, otherwise a loop that goes through a "for: 5m" rule
-- would reset the hop count at the deadline and never stop.
ALTER TABLE auto_pending ADD COLUMN IF NOT EXISTS causal_root text;
ALTER TABLE auto_pending ADD COLUMN IF NOT EXISTS causal_hop integer NOT NULL DEFAULT 0;
