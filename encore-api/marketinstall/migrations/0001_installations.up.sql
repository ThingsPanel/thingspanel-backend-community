-- A08 coordinator-owned intent only. Business data and provider migration
-- ledgers remain with their existing owners. Fixed PostgreSQL first profile.
CREATE TABLE market_install_operations (
 id text PRIMARY KEY,
 tenant_id text NOT NULL,
 subject_id text NOT NULL,
 authorization_ref text NOT NULL,
 request_key varchar(128) NOT NULL,
 request_digest char(64) NOT NULL,
 artifact_digest varchar(71) NOT NULL,
 plan jsonb NOT NULL,
 state text NOT NULL CHECK(state IN('queued','running','paused','completed','rejected')),
 generation bigint NOT NULL DEFAULT 0,
 lease_until timestamptz,
 error_code varchar(80) NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT NOW(),
 updated_at timestamptz NOT NULL DEFAULT NOW(),
 UNIQUE(tenant_id,request_key)
);
CREATE TABLE market_install_steps (
 operation_id text NOT NULL REFERENCES market_install_operations(id),
 ordinal integer NOT NULL CHECK(ordinal>=0),
 step_id text NOT NULL,
 idempotency_key char(64) NOT NULL,
 payload_hash char(64) NOT NULL,
 state text NOT NULL CHECK(state IN('pending','dispatched','confirmed','unknown','rejected')),
 receipt jsonb,
 updated_at timestamptz NOT NULL DEFAULT NOW(),
 PRIMARY KEY(operation_id,ordinal),
 UNIQUE(operation_id,step_id),
 UNIQUE(idempotency_key)
);
CREATE INDEX market_install_claim ON market_install_operations(state,lease_until,created_at);
