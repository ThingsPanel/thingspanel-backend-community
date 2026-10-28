-- A15: the alarm policy gets a stable business identity, and an episode stops
-- being identified by a rule id that can be recreated.
--
-- Before this migration the active-alarm identity was (tenant, policy_id,
-- subject, dimension). Re-publishing a policy kept the rule id so it happened to
-- work, but migrating a legacy alarm_config produced a new rule id and therefore
-- a second active alarm for the same physical problem. The business key fixes
-- that: it is what the operator means, and it survives re-creation.

ALTER TABLE alarm_episodes ADD COLUMN IF NOT EXISTS policy_key   text NOT NULL DEFAULT '';
ALTER TABLE alarm_episodes ADD COLUMN IF NOT EXISTS level        text NOT NULL DEFAULT '';
ALTER TABLE alarm_episodes ADD COLUMN IF NOT EXISTS source       text NOT NULL DEFAULT '';
ALTER TABLE alarm_episodes ADD COLUMN IF NOT EXISTS first_seq    bigint NOT NULL DEFAULT 0;
ALTER TABLE alarm_episodes ADD COLUMN IF NOT EXISTS sample_count bigint NOT NULL DEFAULT 1;
ALTER TABLE alarm_episodes ADD COLUMN IF NOT EXISTS last_evidence jsonb;

-- Existing rows keep their current identity, which is the only answer that does
-- not re-open or duplicate an alarm that is already active.
UPDATE alarm_episodes SET policy_key = policy_id WHERE policy_key = '';
UPDATE alarm_episodes SET first_seq = last_seq WHERE first_seq = 0;

DROP INDEX IF EXISTS alarm_episodes_active;
CREATE UNIQUE INDEX IF NOT EXISTS alarm_episodes_active
    ON alarm_episodes (tenant_id, policy_key, subject_id, dimension) WHERE state = 'active';
CREATE INDEX IF NOT EXISTS alarm_episodes_history
    ON alarm_episodes (tenant_id, policy_key, subject_id, opened_at DESC);

-- Event-type intake dedupe. One logical action id may qualify a policy once; a
-- redelivered scene action must not raise a second alarm, and it must not be
-- able to change its mind by resending the same id with a different body.
CREATE TABLE IF NOT EXISTS alarm_event_intake (
    tenant_id     text NOT NULL,
    policy_key    text NOT NULL,
    action_id     text NOT NULL,
    source        text NOT NULL,
    subject_id    text NOT NULL DEFAULT '',
    dimension     text NOT NULL DEFAULT '',
    kind          text NOT NULL,
    payload_hash  text NOT NULL DEFAULT '',
    outcome       text NOT NULL,
    reason_code   text NOT NULL DEFAULT '',
    episode_id    text,
    transition_id text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, policy_key, action_id)
);
CREATE INDEX IF NOT EXISTS alarm_event_intake_recent
    ON alarm_event_intake (tenant_id, created_at DESC);

-- The event exit needs a relay cursor. Alarm transitions are published into
-- auto_events (the existing reliable outbox); the relay turns them into alarm
-- signals for A05 so a linked rule fires without anyone polling the alarm table.
ALTER TABLE auto_events ADD COLUMN IF NOT EXISTS relayed_at timestamptz;
CREATE INDEX IF NOT EXISTS auto_events_unrelayed
    ON auto_events (seq) WHERE relayed_at IS NULL;
