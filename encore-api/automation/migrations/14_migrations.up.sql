-- A31: resumable migration runs.
--
-- A migration is a long operation over another database, so it cannot be one
-- transaction: the legacy rows are read in the old service, the rules are
-- written here, and the network sits between them. The two tables below are the
-- checkpoint that makes "it failed halfway" a resumable state instead of a
-- reason to start over.
--
-- Two decisions worth recording:
--
--   * The plan is stored, not recomputed. If the operator approved a diff, the
--     thing that gets applied is the thing that was approved. Recomputing at
--     apply time would let a scene edited in between change under the approval.
--
--   * Unsupported and ambiguous items are rows too, with their own status. They
--     must not be countable as "migrated", and a report that only counts
--     successes hides exactly the objects the customer will miss.

CREATE TABLE IF NOT EXISTS auto_migrations (
    id            uuid PRIMARY KEY,
    tenant_id     text        NOT NULL,
    name          text        NOT NULL,
    status        text        NOT NULL DEFAULT 'planned',
    snapshot_hash text        NOT NULL,
    total         int         NOT NULL DEFAULT 0,
    applied       int         NOT NULL DEFAULT 0,
    skipped       int         NOT NULL DEFAULT 0,
    failed        int         NOT NULL DEFAULT 0,
    unresolved    int         NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS auto_migrations_tenant
    ON auto_migrations (tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS auto_migration_items (
    migration_id uuid        NOT NULL REFERENCES auto_migrations(id) ON DELETE CASCADE,
    legacy_id    text        NOT NULL,
    kind         text        NOT NULL,
    name         text        NOT NULL DEFAULT '',
    status       text        NOT NULL,
    rule_id      uuid,
    -- episode_id is set only for an adopted alarm. It is a separate column from
    -- rule_id because the two identify different things: an alarm item's rule is
    -- the policy it belongs to, while the thing the migration actually created is
    -- the episode. Overloading rule_id would make rollback clear the policy.
    episode_id   text,
    reason_code  text,
    reason       text,
    -- definition is the rule body to publish (scenes only).
    definition   jsonb,
    -- payload is the input the item was planned from, for kinds that are not a
    -- rule body (an alarm adoption). Keeping it separate from definition is what
    -- lets apply be a pure replay of the approved plan: it never re-reads the
    -- legacy database, so the thing applied is the thing that was approved.
    payload      jsonb,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (migration_id, legacy_id)
);

-- The apply loop reads "the next batch of work" on every call, so the status is
-- part of the index rather than a filter applied after the fact.
CREATE INDEX IF NOT EXISTS auto_migration_items_status
    ON auto_migration_items (migration_id, status, legacy_id);
