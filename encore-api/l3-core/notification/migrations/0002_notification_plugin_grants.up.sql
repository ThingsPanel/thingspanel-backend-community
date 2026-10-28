ALTER TABLE notification_plugin_grants
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE TABLE notification_plugin_grant_states (
    tenant_id TEXT NOT NULL,
    registration_id TEXT NOT NULL,
    version BIGINT NOT NULL CHECK (version > 0),
    enabled BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, registration_id, version),
    FOREIGN KEY (tenant_id, registration_id)
        REFERENCES notification_plugin_grants(tenant_id, registration_id)
);

INSERT INTO notification_plugin_grant_states
    (tenant_id, registration_id, version, enabled, created_at, updated_at)
SELECT tenant_id, registration_id, version, enabled, created_at, updated_at
FROM notification_plugin_grants;

CREATE TRIGGER notification_plugin_grant_state_immutable
    BEFORE UPDATE OR DELETE ON notification_plugin_grant_states
    FOR EACH ROW EXECUTE FUNCTION notification_reject_immutable_change();

ALTER TABLE notification_plugin_grants
    ADD CONSTRAINT notification_plugin_grant_current_state_fk
    FOREIGN KEY (tenant_id, registration_id, version)
    REFERENCES notification_plugin_grant_states(tenant_id, registration_id, version)
    DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX notification_plugin_grants_registration_idx
    ON notification_plugin_grants(registration_id, tenant_id);
