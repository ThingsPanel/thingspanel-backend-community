-- Revocable A08 installation delegation. No user bearer or market token is stored.
CREATE TABLE IF NOT EXISTS market_install_authorizations (
 id varchar(36) PRIMARY KEY,
 tenant_id text NOT NULL,
 subject_id text NOT NULL,
 authority text NOT NULL,
 auth_version bigint NOT NULL,
 actor_id text NOT NULL DEFAULT '',
 actor_version bigint NOT NULL DEFAULT 0,
 delegated boolean NOT NULL DEFAULT false,
 purpose text NOT NULL CHECK(purpose='a08.install.v1'),
 request_key varchar(128) NOT NULL,
 request_digest varchar(67) NOT NULL,
 plan_digest char(64) NOT NULL,
 operation_id text NOT NULL DEFAULT '',
 plan jsonb NOT NULL,
 session_hash text NOT NULL,
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT NOW(),
 CONSTRAINT uq_market_install_authorization_request UNIQUE(tenant_id,request_key)
);
CREATE INDEX IF NOT EXISTS market_install_authorizations_subject ON market_install_authorizations(tenant_id,subject_id,expires_at);
-- Community-config owns this receipt. A08 never writes the configuration table.
CREATE TABLE IF NOT EXISTS market_install_config_receipts (
 tenant_id text NOT NULL,
 subject_id text NOT NULL,
 operation_key char(64) NOT NULL,
 payload_hash char(64) NOT NULL,
 instance_key text NOT NULL,
 resource_key varchar(128) NOT NULL,
 config_id varchar(36) NOT NULL,
 binding_revision bigint NOT NULL,
 result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT NOW(),
 PRIMARY KEY(tenant_id,subject_id,operation_key),
 CONSTRAINT uq_market_install_config_resource UNIQUE(tenant_id,instance_key,resource_key)
);
