-- Plugin installation records (PLUGIN-UI). One row per (tenant, plugin); never stores secret values. Idempotent.
CREATE TABLE IF NOT EXISTS plugin_installations (
 id varchar(36) PRIMARY KEY,
 tenant_id varchar(36) NOT NULL,
 plugin_id varchar(64) NOT NULL,
 state varchar(24) NOT NULL,
 desired varchar(16) NOT NULL,
 generation bigint NOT NULL DEFAULT 0,
 catalog_version varchar(64) NOT NULL DEFAULT '',
 config_public text NOT NULL DEFAULT '{}',
 secret_keys text NOT NULL DEFAULT '[]',
 health varchar(16) NOT NULL DEFAULT '',
 op_id varchar(36) NOT NULL DEFAULT '',
 op_step varchar(32) NOT NULL DEFAULT '',
 op_attempts integer NOT NULL DEFAULT 0,
 error_code varchar(48) NOT NULL DEFAULT '',
 idempotency_key varchar(128) NOT NULL DEFAULT '',
 lease_owner varchar(64) NOT NULL DEFAULT '',
 lease_until timestamptz,
 maintained_at timestamptz,
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 CONSTRAINT uq_plugin_install_tenant_plugin UNIQUE (tenant_id, plugin_id)
);
CREATE TABLE IF NOT EXISTS plugin_install_events (
 id bigserial PRIMARY KEY,
 installation_id varchar(36) NOT NULL,
 op_id varchar(36) NOT NULL,
 step varchar(32) NOT NULL,
 outcome varchar(16) NOT NULL,
 code varchar(48) NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plugin_install_events_inst ON plugin_install_events (installation_id, id);
