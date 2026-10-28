-- A28: registered external executors.
--
-- An external engine may only drive devices inside a contract it registered
-- here. Two things in this table are not bookkeeping:
--
--   epoch     the registration generation. A message carrying an older epoch is
--             refused, so re-registering with a narrower scope immediately
--             invalidates whatever the old deployment is still sending.
--   last_seq  the highest sequence number already accepted. It is advanced with a
--             conditional UPDATE, which is what makes "this message is not a
--             replay" a database decision instead of a race between two requests.
--
-- The contract itself is stored as jsonb because it is a document the operator
-- reviews, not a set of columns to query. Its hash is a column because every
-- message carries it and it is compared on every request.

CREATE TABLE IF NOT EXISTS auto_executors (
    id              text PRIMARY KEY,
    tenant_id       text        NOT NULL,
    name            text        NOT NULL,
    engine          text        NOT NULL,
    contract        jsonb       NOT NULL,
    contract_hash   text        NOT NULL,
    epoch           integer     NOT NULL DEFAULT 1,
    enabled         boolean     NOT NULL DEFAULT true,
    last_seq        bigint      NOT NULL DEFAULT 0,
    -- The credential the executor authenticates with. Stored as a fingerprint:
    -- the platform must be able to say "this is the credential that was issued",
    -- not hold a copy of the secret.
    credential_id   text,
    last_seen_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS auto_executors_tenant_name
    ON auto_executors (tenant_id, name);

CREATE INDEX IF NOT EXISTS auto_executors_tenant_enabled
    ON auto_executors (tenant_id, enabled);

-- One row per accepted message, so "did this intent already run?" survives a
-- restart and can be answered without replaying the sequence.
CREATE TABLE IF NOT EXISTS auto_executor_messages (
    tenant_id      text        NOT NULL,
    executor_id    text        NOT NULL,
    idempotency_key text       NOT NULL,
    direction      text        NOT NULL, -- intent | verdict
    epoch          integer     NOT NULL,
    seq            bigint      NOT NULL,
    outcome        text        NOT NULL,
    reason_code    text,
    subject        text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, executor_id, idempotency_key)
);
