-- Immutable authorization snapshot for legacy protocol/service MQTT plugins.
-- The ID is derived from principal, broker scope, connection and target scope;
-- repeated identical grants may only extend expiry.
CREATE TABLE IF NOT EXISTS broker_plugin_contexts (
    id varchar(64) PRIMARY KEY,
    principal_json jsonb NOT NULL,
    broker_cell varchar(128) NOT NULL,
    broker_instance varchar(128) NOT NULL,
    connection_id varchar(256) NOT NULL,
    targets_json jsonb NOT NULL,
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    CHECK (expires_at > issued_at)
);

CREATE INDEX IF NOT EXISTS broker_plugin_contexts_expiry_idx
    ON broker_plugin_contexts (expires_at);
