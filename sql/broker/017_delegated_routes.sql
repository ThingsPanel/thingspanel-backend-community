-- Preserve the immutable service authorization and both route epochs with a
-- per-device observation. Native observation rows keep NULL proof.
ALTER TABLE broker_connection_observations
    ADD COLUMN IF NOT EXISTS delegated_route jsonb;

ALTER TABLE broker_telemetry_activity
    ADD COLUMN IF NOT EXISTS delegated_route jsonb;
