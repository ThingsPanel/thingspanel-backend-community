-- Match the candidate lookup and keep completed effects out of the dispatch index.
CREATE INDEX IF NOT EXISTS broker_effects_automation_dispatch_idx
    ON broker_ingress_effects (updated_at, id)
    WHERE effect_kind = 'automation' AND state IN ('pending', 'dispatching');
