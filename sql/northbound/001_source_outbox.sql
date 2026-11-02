-- Northbound source outbox.
--
-- This table lives in the Community main database on purpose. The rows are
-- written inside the same transaction that commits the trusted telemetry batch
-- or the reliable attribute/event write, so an event that the platform claims to
-- have accepted is always visible to the northbound handoff. There is no second
-- queue and no in-memory buffer in this path.
--
-- Two states must never be confused:
--   * a row here in state 'pending'/'leased'  = enrolled, waiting for handoff
--   * a row in northbound_source_rejects      = never enrolled (capacity)
-- Only the first may be described as "waiting to be sent".

CREATE TABLE IF NOT EXISTS northbound_source_outbox (
    id              uuid PRIMARY KEY,
    tenant_id       text NOT NULL,
    device_id       text NOT NULL,
    event_type      text NOT NULL,
    event_id        text NOT NULL,
    payload         jsonb NOT NULL,
    payload_bytes   integer NOT NULL,
    occurred_at     timestamptz NOT NULL,
    state           text NOT NULL DEFAULT 'pending',
    attempts        integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    lease_token     uuid,
    lease_until     timestamptz,
    handed_off_at   timestamptz,
    failure_code    text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz NOT NULL,
    CONSTRAINT northbound_source_outbox_state
        CHECK (state IN ('pending', 'leased', 'handed_off', 'failed')),
    CONSTRAINT northbound_source_outbox_event UNIQUE (tenant_id, event_id),
    CONSTRAINT northbound_source_outbox_type
        CHECK (event_type IN ('telemetry', 'attribute', 'event'))
);

CREATE INDEX IF NOT EXISTS northbound_source_outbox_claim
    ON northbound_source_outbox (next_attempt_at)
    WHERE state IN ('pending', 'leased');

CREATE INDEX IF NOT EXISTS northbound_source_outbox_tenant
    ON northbound_source_outbox (tenant_id, state);

CREATE INDEX IF NOT EXISTS northbound_source_outbox_expiry
    ON northbound_source_outbox (expires_at);

-- Rejected-before-enrolment accounting. One row per tenant per minute keeps the
-- table small while still showing an operator when rejection started.
CREATE TABLE IF NOT EXISTS northbound_source_rejects (
    tenant_id   text NOT NULL,
    bucket      timestamptz NOT NULL,
    reason      text NOT NULL,
    count       bigint NOT NULL DEFAULT 0,
    last_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, bucket, reason)
);

-- Derived cache of "which tenants have at least one active forwarding task".
-- It is refreshed from the northbound service and never edited by users. When a
-- refresh fails the previous values are kept, so an outage slows the handoff
-- but does not silently stop enrolment.
CREATE TABLE IF NOT EXISTS northbound_source_tenants (
    tenant_id       text PRIMARY KEY,
    active_pipelines integer NOT NULL DEFAULT 0,
    refreshed_at    timestamptz NOT NULL DEFAULT now()
);

-- Operational record of the last successful handoff, shown on the page so an
-- operator can tell "the relay is running" from "nothing is configured".
CREATE TABLE IF NOT EXISTS northbound_source_relay_state (
    id                integer PRIMARY KEY DEFAULT 1,
    last_success_at   timestamptz,
    last_error        text,
    last_error_at     timestamptz,
    CONSTRAINT northbound_source_relay_single CHECK (id = 1)
);
