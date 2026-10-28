-- Automation runtime schema (A04/A05/A09/A15).
--
-- Ownership: the automation service is the only writer of every table here.
-- The alarm lifecycle tables live in the same database because they share the
-- evaluator's state machine, but only the alarm code path writes them.

CREATE TABLE IF NOT EXISTS auto_rules (
    id              text PRIMARY KEY,
    tenant_id       text NOT NULL,
    name            text NOT NULL,
    kind            text NOT NULL,
    enabled         boolean NOT NULL DEFAULT false,
    revision        integer NOT NULL DEFAULT 0,
    definition      jsonb NOT NULL,
    definition_hash text NOT NULL,
    version         integer NOT NULL DEFAULT 1,
    legacy_id       text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    deleted_at      timestamptz
);
CREATE INDEX IF NOT EXISTS auto_rules_tenant ON auto_rules (tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS auto_rules_legacy ON auto_rules (legacy_id) WHERE legacy_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS auto_rule_revisions (
    rule_id         text NOT NULL REFERENCES auto_rules(id) ON DELETE CASCADE,
    revision        integer NOT NULL,
    definition      jsonb NOT NULL,
    definition_hash text NOT NULL,
    published_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (rule_id, revision)
);

-- A binding ties a shared definition to a concrete device/template scope.
CREATE TABLE IF NOT EXISTS auto_bindings (
    id               text PRIMARY KEY,
    binding_key      text NOT NULL UNIQUE,
    tenant_id        text NOT NULL,
    rule_id          text NOT NULL,
    revision         integer NOT NULL,
    source           text NOT NULL,
    device_id        text,
    device_config_id text,
    template_id      text,
    params           jsonb NOT NULL DEFAULT '{}'::jsonb,
    enabled          boolean NOT NULL DEFAULT true,
    created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS auto_bindings_device ON auto_bindings (tenant_id, device_id) WHERE enabled;
CREATE INDEX IF NOT EXISTS auto_bindings_rule ON auto_bindings (rule_id);

-- Reliable ingress. The unique event_id is the dedupe boundary: a replayed
-- broker message or a relay retry cannot produce a second business action.
CREATE TABLE IF NOT EXISTS auto_inbox (
    event_id     text PRIMARY KEY,
    tenant_id    text NOT NULL,
    device_id    text NOT NULL,
    signal       text NOT NULL,
    key          text NOT NULL,
    value        jsonb,
    unit         text,
    quality      text NOT NULL DEFAULT 'good',
    observed_at  timestamptz NOT NULL,
    received_at  timestamptz NOT NULL DEFAULT now(),
    seq          bigint NOT NULL,
    source       text NOT NULL DEFAULT 'uplink',
    params       jsonb,
    processed_at timestamptz
);
CREATE INDEX IF NOT EXISTS auto_inbox_unprocessed ON auto_inbox (received_at) WHERE processed_at IS NULL;
CREATE INDEX IF NOT EXISTS auto_inbox_tenant ON auto_inbox (tenant_id, received_at DESC);

-- Latest value projection used by the evaluator. Rebuildable from inbox.
CREATE TABLE IF NOT EXISTS auto_state (
    device_id   text NOT NULL,
    signal      text NOT NULL,
    key         text NOT NULL,
    tenant_id   text NOT NULL,
    value       jsonb,
    unit        text,
    quality     text NOT NULL DEFAULT 'good',
    observed_at timestamptz NOT NULL,
    seq         bigint NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (device_id, signal, key)
);

-- Persistent pending state for "condition held for N seconds".
CREATE TABLE IF NOT EXISTS auto_pending (
    scope_key                text PRIMARY KEY,
    tenant_id                text NOT NULL,
    rule_id                  text NOT NULL,
    revision                 integer NOT NULL,
    device_id                text NOT NULL,
    started_at               timestamptz NOT NULL,
    deadline                 timestamptz NOT NULL,
    last_evidence_at         timestamptz NOT NULL,
    evidence_seq             bigint NOT NULL,
    require_processed_before timestamptz NOT NULL,
    state                    text NOT NULL,
    defer_count              integer NOT NULL DEFAULT 0,
    updated_at               timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS auto_pending_due ON auto_pending (deadline) WHERE state = 'pending';

CREATE TABLE IF NOT EXISTS auto_runs (
    id              text PRIMARY KEY,
    tenant_id       text NOT NULL,
    rule_id         text NOT NULL,
    revision        integer NOT NULL,
    definition_hash text NOT NULL,
    scope_key       text NOT NULL,
    generation      bigint NOT NULL,
    device_id       text,
    trigger_event_id text,
    status          text NOT NULL,
    reason_code     text,
    reason          text,
    snapshot        jsonb,
    started_at      timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz,
    deadline        timestamptz
);
CREATE INDEX IF NOT EXISTS auto_runs_scope ON auto_runs (scope_key, status);
CREATE INDEX IF NOT EXISTS auto_runs_rule ON auto_runs (rule_id, started_at DESC);

CREATE TABLE IF NOT EXISTS auto_steps (
    id              text PRIMARY KEY,
    run_id          text NOT NULL REFERENCES auto_runs(id) ON DELETE CASCADE,
    idx             integer NOT NULL,
    kind            text NOT NULL,
    status          text NOT NULL,
    idempotency_key text NOT NULL,
    attempt         integer NOT NULL DEFAULT 0,
    detail          jsonb,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (run_id, idx)
);

CREATE TABLE IF NOT EXISTS auto_targets (
    run_id      text NOT NULL REFERENCES auto_runs(id) ON DELETE CASCADE,
    device_id   text NOT NULL,
    status      text NOT NULL,
    reason_code text,
    reason      text,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, device_id)
);

-- Durable intent. Written in the same transaction as the step that produced it,
-- dispatched after commit. A crash before dispatch leaves it claimable.
CREATE TABLE IF NOT EXISTS auto_outbox (
    id              text PRIMARY KEY,
    tenant_id       text NOT NULL,
    run_id          text,
    step_id         text,
    kind            text NOT NULL,
    scope_key       text NOT NULL,
    generation      bigint NOT NULL,
    idempotency_key text NOT NULL UNIQUE,
    payload         jsonb NOT NULL,
    state           text NOT NULL,
    attempt         integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    accepted_ref    text,
    last_error      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS auto_outbox_due ON auto_outbox (next_attempt_at)
    WHERE state IN ('pending', 'dispatched');

-- Business transitions published for downstream consumers (A05/A15).
CREATE TABLE IF NOT EXISTS auto_events (
    id         text PRIMARY KEY,
    tenant_id  text NOT NULL,
    kind       text NOT NULL,
    subject    text NOT NULL,
    payload    jsonb NOT NULL,
    seq        bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS auto_events_tenant ON auto_events (tenant_id, seq);

CREATE SEQUENCE IF NOT EXISTS auto_seq;

-- ---------------------------------------------------------------------------
-- Alarm lifecycle (A15). Only the alarm code path writes these tables.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS alarm_episodes (
    id          text PRIMARY KEY,
    tenant_id   text NOT NULL,
    policy_id   text NOT NULL,
    subject_id  text NOT NULL,
    dimension   text NOT NULL DEFAULT '',
    state       text NOT NULL,
    revision    integer NOT NULL,
    epoch       bigint NOT NULL,
    opened_at   timestamptz NOT NULL,
    last_seq    bigint NOT NULL,
    acked_at    timestamptz,
    acked_by    text,
    cleared_at  timestamptz,
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS alarm_episodes_active
    ON alarm_episodes (tenant_id, policy_id, subject_id, dimension) WHERE state = 'active';

CREATE TABLE IF NOT EXISTS alarm_transitions (
    id          text PRIMARY KEY,
    episode_id  text NOT NULL REFERENCES alarm_episodes(id) ON DELETE CASCADE,
    tenant_id   text NOT NULL,
    kind        text NOT NULL,
    seq         bigint NOT NULL,
    revision    integer NOT NULL,
    epoch       bigint NOT NULL,
    evidence    jsonb,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS alarm_transitions_dedupe
    ON alarm_transitions (episode_id, kind, seq);

-- Durable continuation for finite "wait" steps. A wait is a business timer, not
-- a goroutine: the run resumes from the recorded program counter.
CREATE TABLE IF NOT EXISTS auto_run_timers (
    run_id   text NOT NULL REFERENCES auto_runs(id) ON DELETE CASCADE,
    pc       integer NOT NULL,
    due_at   timestamptz NOT NULL,
    state    text NOT NULL DEFAULT 'pending',
    PRIMARY KEY (run_id, pc)
);
CREATE INDEX IF NOT EXISTS auto_run_timers_due ON auto_run_timers (due_at) WHERE state = 'pending';
