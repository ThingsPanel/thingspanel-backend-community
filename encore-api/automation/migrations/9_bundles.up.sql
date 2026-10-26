-- A21: importing a package is a state machine, not a button.
--
-- An import touches more than one owner's tables, and there is no distributed
-- transaction between them. The only honest way to handle that is to write down
-- what was intended, then record what actually happened, so a half-finished
-- import can be resumed instead of guessed at.

-- The business key a package knows a rule by. A row id only means something in
-- the database that minted it; a resource key means the same thing everywhere.
ALTER TABLE auto_rules ADD COLUMN IF NOT EXISTS resource_key text;

-- The hash of the package form that produced this rule. Stored rather than
-- recomputed because the stored definition is not the package definition: it
-- has local ids and resolved notification targets. Comparing those two hashes
-- would make every re-import look like a change.
ALTER TABLE auto_rules ADD COLUMN IF NOT EXISTS bundle_hash text;

-- One resource key per tenant. Partial, because rules created by hand have no
-- key and must not collide with each other.
CREATE UNIQUE INDEX IF NOT EXISTS auto_rules_resource_key
    ON auto_rules (tenant_id, resource_key)
 WHERE resource_key IS NOT NULL AND deleted_at IS NULL;

-- One row per attempted import of one content hash. Re-importing the same bytes
-- finds this row instead of starting over, which is what makes "import twice"
-- idempotent even when the two attempts are days apart.
CREATE TABLE IF NOT EXISTS auto_bundle_imports (
    id             text PRIMARY KEY,
    tenant_id      text NOT NULL,
    bundle_key     text NOT NULL,
    version        text NOT NULL,
    content_hash   text NOT NULL,
    -- staged -> activating -> activated
    --                    \-> failed (resumable: the mapping is kept)
    status         text NOT NULL,
    manifest       jsonb NOT NULL,
    role_mapping   jsonb,
    notify_choices jsonb,
    -- resourceKey -> local rule id, written as each rule is created. This is the
    -- resume point: a retry skips what is already in here.
    created_rules  jsonb NOT NULL DEFAULT '{}'::jsonb,
    report         jsonb,
    reason_code    text,
    reason_message text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    activated_at   timestamptz
);

CREATE UNIQUE INDEX IF NOT EXISTS auto_bundle_imports_identity
    ON auto_bundle_imports (tenant_id, bundle_key, version, content_hash);

CREATE INDEX IF NOT EXISTS auto_bundle_imports_recent
    ON auto_bundle_imports (tenant_id, created_at DESC);
