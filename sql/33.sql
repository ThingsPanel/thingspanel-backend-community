-- Managed plugin runtime identities (T4). Only the SHA-256 of the secret is stored; a higher generation of the same
-- (tenant, plugin instance) revokes lower ones in application code. Idempotent.
CREATE TABLE IF NOT EXISTS plugin_runtime_identities (
 instance_id varchar(64) PRIMARY KEY,
 plugin_instance varchar(128) NOT NULL,
 service_identifier varchar(100) NOT NULL,
 tenant_id varchar(36) NOT NULL,
 generation bigint NOT NULL CHECK (generation >= 1),
 secret_sha256 char(64) NOT NULL,
 created_at timestamptz NOT NULL DEFAULT NOW(),
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 CONSTRAINT uq_plugin_runtime_identity_generation UNIQUE (tenant_id, plugin_instance, generation)
);
