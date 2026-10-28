-- A07: durable schedules.
--
-- A schedule is business data owned by the automation service, not a source-code
-- cron entry. There is one row per schedule, so adding a schedule never requires
-- a deployment and deleting one leaves no ghost job behind.

CREATE TABLE IF NOT EXISTS auto_schedules (
    id             text PRIMARY KEY,
    tenant_id      text NOT NULL,
    rule_id        text NOT NULL REFERENCES auto_rules(id) ON DELETE CASCADE,
    kind           text NOT NULL,
    cron_expr      text NOT NULL DEFAULT '',
    -- IANA name. Stored explicitly so the meaning of an expression does not
    -- depend on the server's local zone.
    timezone       text NOT NULL DEFAULT 'UTC',
    run_at         timestamptz,
    -- What to do when the process was not running at the scheduled instant.
    misfire_policy text NOT NULL DEFAULT 'skip',
    enabled        boolean NOT NULL DEFAULT true,
    -- next_fire_at is the durable due time. NULL means "nothing scheduled",
    -- which is how a one-off looks after it fired.
    next_fire_at   timestamptz,
    last_fire_at   timestamptz,
    -- A lease makes the claim visible: a worker that dies leaves an expired
    -- lease instead of an invisible lock.
    lease_owner    text,
    lease_until    timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    deleted_at     timestamptz
);

-- Due scans use this index; there is no full-table sweep.
CREATE INDEX IF NOT EXISTS auto_schedules_due
    ON auto_schedules (next_fire_at) WHERE enabled AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS auto_schedules_rule ON auto_schedules (rule_id);

-- One row per attempted occurrence.
--
-- The unique (schedule_id, scheduled_for) is the "same instant runs once"
-- guarantee: a duplicate wake-up, a retried claim or a restart all collide on
-- this index instead of producing a second control command.
CREATE TABLE IF NOT EXISTS auto_schedule_fires (
    id            text PRIMARY KEY,
    schedule_id   text NOT NULL REFERENCES auto_schedules(id) ON DELETE CASCADE,
    tenant_id     text NOT NULL,
    rule_id       text NOT NULL,
    scheduled_for timestamptz NOT NULL,
    -- claimed -> running -> done | failed | skipped
    -- "done" is only written after the run finished, so a crash can never leave
    -- a false "already executed".
    state         text NOT NULL,
    run_id        text,
    attempt       integer NOT NULL DEFAULT 0,
    lease_owner   text,
    lease_until   timestamptz,
    -- How many occurrences the misfire policy dropped for this attempt.
    missed        integer NOT NULL DEFAULT 0,
    error         text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (schedule_id, scheduled_for)
);

CREATE INDEX IF NOT EXISTS auto_schedule_fires_work
    ON auto_schedule_fires (state, lease_until);
CREATE INDEX IF NOT EXISTS auto_schedule_fires_schedule
    ON auto_schedule_fires (schedule_id, scheduled_for DESC);
