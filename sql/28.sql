-- A10: authoritative acceptance for automation-issued device commands.
--
-- The automation side can only check "is my run still current?" before it sends.
-- That check is not authoritative: a newer trigger can arrive between the check
-- and the send, and an old command can arrive after the new one. The only place
-- where "which of these two commands is the current one" can be decided is the
-- acceptance boundary itself — here, in front of the legacy publish endpoints.
--
-- Two tables make that decision possible and repeatable:
--
--   automation_scope_generation  the highest generation seen per run scope. A
--                                command carrying a lower number is superseded
--                                and is not sent.
--   automation_command_guard     one row per logical action (idempotency key),
--                                holding the exact response that was returned.
--                                A retry with the same key and the same content
--                                replays that response instead of sending again;
--                                a retry with the same key and different content
--                                is a conflict, not a new action.

CREATE TABLE IF NOT EXISTS automation_scope_generation (
    scope_key  text PRIMARY KEY,
    generation bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS automation_command_guard (
    idempotency_key text PRIMARY KEY,
    tenant_id       text NOT NULL DEFAULT '',
    scope_key       text NOT NULL DEFAULT '',
    generation      bigint NOT NULL DEFAULT 0,
    action_kind     text NOT NULL DEFAULT '',
    device_id       text NOT NULL DEFAULT '',
    -- sha256 of the exact request body, so "same key, different content" is
    -- detectable instead of silently replayed.
    payload_hash    text NOT NULL DEFAULT '',
    -- in_flight | accepted | failed | unknown | superseded
    state           text NOT NULL DEFAULT 'in_flight',
    http_status     integer NOT NULL DEFAULT 0,
    response_body   text NOT NULL DEFAULT '',
    -- The owner's own id for the send (message_id / command_id), so a late
    -- device result can be correlated back to this logical action.
    accepted_ref    text NOT NULL DEFAULT '',
    -- A second worker must not execute the same key while the first is in
    -- flight; an expired lease means the first died and the outcome is unknown.
    lease_until     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS automation_command_guard_scope
    ON automation_command_guard (scope_key, created_at DESC);
CREATE INDEX IF NOT EXISTS automation_command_guard_ref
    ON automation_command_guard (accepted_ref) WHERE accepted_ref <> '';
