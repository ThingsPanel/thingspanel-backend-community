-- Northbound delivery substrate. One database, four owners of truth:
--   nb_connector_installations  fixed official plugin artifacts and health
--   nb_connections             tenant-owned destination configuration (no secrets)
--   nb_pipelines               versioned routing/transform configuration
--   nb_events / nb_deliveries  one payload copy, one ledger row per branch
-- Secrets live in nb_connection_secrets, encrypted with a key that is not in
-- this database. Nothing here stores a plaintext credential.

CREATE TABLE nb_connector_installations (
    id               text PRIMARY KEY,
    version          text NOT NULL,
    contract_version text NOT NULL,
    endpoint         text NOT NULL,
    artifact_digest  text NOT NULL,
    capabilities     jsonb NOT NULL,
    state            text NOT NULL DEFAULT 'ready',
    last_seen_at     timestamptz,
    last_error       text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT nb_installation_state CHECK (state IN ('ready', 'unreachable', 'revoked'))
);

CREATE TABLE nb_connections (
    id           uuid PRIMARY KEY,
    tenant_id    text NOT NULL,
    name         text NOT NULL,
    connector_id text NOT NULL REFERENCES nb_connector_installations (id),
    config       jsonb NOT NULL DEFAULT '{}'::jsonb,
    secret_ref   text,
    revision     bigint NOT NULL DEFAULT 1,
    state        text NOT NULL DEFAULT 'active',
    failure_code text,
    created_by   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT nb_connection_state CHECK (state IN ('active', 'needs_repair', 'disabled')),
    CONSTRAINT nb_connection_name UNIQUE (tenant_id, name)
);
CREATE INDEX nb_connections_tenant ON nb_connections (tenant_id, created_at DESC);

CREATE TABLE nb_connection_secrets (
    tenant_id     text NOT NULL,
    connection_id uuid NOT NULL REFERENCES nb_connections (id) ON DELETE CASCADE,
    secret_ref    text NOT NULL,
    key_version   int NOT NULL,
    ciphertext    bytea NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id)
);

CREATE TABLE nb_pipelines (
    id             uuid PRIMARY KEY,
    tenant_id      text NOT NULL,
    name           text NOT NULL,
    connection_id  uuid NOT NULL REFERENCES nb_connections (id),
    filter         jsonb NOT NULL,
    transform      jsonb NOT NULL,
    serialization  jsonb NOT NULL DEFAULT '{}'::jsonb,
    state          text NOT NULL DEFAULT 'draft',
    revision       bigint NOT NULL DEFAULT 1,
    active_version bigint,
    created_by     text NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT nb_pipeline_state CHECK (state IN ('draft', 'active', 'paused', 'disabled')),
    CONSTRAINT nb_pipeline_name UNIQUE (tenant_id, name)
);
CREATE INDEX nb_pipelines_tenant ON nb_pipelines (tenant_id, created_at DESC);

-- A version is immutable. Deliveries pin the version that produced them, so a
-- later edit or a plugin upgrade never silently changes queued meaning.
CREATE TABLE nb_pipeline_versions (
    pipeline_id uuid NOT NULL REFERENCES nb_pipelines (id) ON DELETE CASCADE,
    version     bigint NOT NULL,
    connection_id uuid NOT NULL,
    filter      jsonb NOT NULL,
    transform   jsonb NOT NULL,
    serialization jsonb NOT NULL,
    created_by  text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (pipeline_id, version)
);

CREATE TABLE nb_events (
    event_id       text PRIMARY KEY,
    tenant_id      text NOT NULL,
    device_id      text NOT NULL,
    event_type     text NOT NULL,
    occurred_at    timestamptz NOT NULL,
    received_at    timestamptz NOT NULL,
    payload        jsonb NOT NULL,
    payload_sha256 bytea NOT NULL,
    expires_at     timestamptz NOT NULL,
    CONSTRAINT nb_event_type CHECK (event_type IN ('telemetry', 'attribute', 'event'))
);
CREATE INDEX nb_events_expiry ON nb_events (expires_at);

-- state machine:
--   pending -> leased -> delivered
--                     -> retry_wait -> pending (next_attempt_at)
--                     -> unknown  (target may have applied it; never auto-retried)
--                     -> dead     (attempts exhausted or permanently rejected)
CREATE TABLE nb_deliveries (
    id                  uuid PRIMARY KEY,
    event_id            text NOT NULL REFERENCES nb_events (event_id) ON DELETE CASCADE,
    tenant_id           text NOT NULL,
    pipeline_id         uuid NOT NULL,
    pipeline_version    bigint NOT NULL,
    connection_id       uuid NOT NULL,
    connection_revision bigint NOT NULL,
    branch              text NOT NULL DEFAULT 'default',
    idempotency_key     text NOT NULL,
    order_key           text NOT NULL,
    state               text NOT NULL DEFAULT 'pending',
    ack_level           text,
    attempts            int NOT NULL DEFAULT 0,
    next_attempt_at     timestamptz NOT NULL DEFAULT now(),
    lease_token         uuid,
    lease_until         timestamptz,
    target_receipt      jsonb,
    failure_code        text,
    failure_detail      text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    delivered_at        timestamptz,
    CONSTRAINT nb_delivery_state CHECK (state IN ('pending', 'leased', 'retry_wait', 'delivered', 'unknown', 'dead')),
    CONSTRAINT nb_delivery_branch UNIQUE (pipeline_id, event_id, branch)
);
CREATE INDEX nb_deliveries_claim ON nb_deliveries (next_attempt_at)
    WHERE state IN ('pending', 'retry_wait');
CREATE INDEX nb_deliveries_lease ON nb_deliveries (lease_until) WHERE state = 'leased';
CREATE INDEX nb_deliveries_tenant_state ON nb_deliveries (tenant_id, state, created_at DESC);
CREATE INDEX nb_deliveries_pipeline ON nb_deliveries (pipeline_id, created_at DESC);

CREATE TABLE nb_delivery_attempts (
    id            bigserial PRIMARY KEY,
    delivery_id   uuid NOT NULL REFERENCES nb_deliveries (id) ON DELETE CASCADE,
    attempt       int NOT NULL,
    outcome       text NOT NULL,
    ack_level     text,
    error_code    text,
    retry_after_ms bigint,
    duration_ms   int NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX nb_delivery_attempts_delivery ON nb_delivery_attempts (delivery_id, attempt);

-- Per-tenant admission accounting. `rejected_total` is the visible record of
-- events that were NOT enrolled because the tenant was at capacity; those must
-- never be reported as "waiting for handoff".
CREATE TABLE nb_tenant_quota (
    tenant_id        text PRIMARY KEY,
    pending_events   bigint NOT NULL DEFAULT 0,
    rejected_total   bigint NOT NULL DEFAULT 0,
    last_reject_at   timestamptz,
    last_reject_code text,
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE nb_pipeline_quota (
    pipeline_id    uuid PRIMARY KEY REFERENCES nb_pipelines (id) ON DELETE CASCADE,
    pending        bigint NOT NULL DEFAULT 0,
    rejected_total bigint NOT NULL DEFAULT 0,
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- Facts the Community runtime reports about itself. The page must be able to
-- say "your ingest profile has no durable boundary" instead of implying one.
CREATE TABLE nb_runtime_facts (
    key        text PRIMARY KEY,
    value      text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
