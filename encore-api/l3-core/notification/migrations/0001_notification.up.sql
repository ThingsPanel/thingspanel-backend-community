CREATE FUNCTION notification_reject_immutable_change() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'notification snapshot rows are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TABLE notification_plugin_registrations (
    id TEXT PRIMARY KEY,
    plugin_id TEXT NOT NULL,
    plugin_version TEXT NOT NULL,
    origin TEXT NOT NULL,
    manifest_digest CHAR(64) NOT NULL,
    manifest JSONB NOT NULL,
    auth_key_ref TEXT NOT NULL,
    auth_ciphertext BYTEA NOT NULL,
    auth_aad_version INTEGER NOT NULL CHECK (auth_aad_version > 0),
    registration_version BIGINT NOT NULL DEFAULT 1 CHECK (registration_version > 0),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    health TEXT NOT NULL DEFAULT 'unknown',
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (plugin_id, plugin_version)
);

CREATE TABLE notification_plugin_registration_states (
    registration_id TEXT NOT NULL REFERENCES notification_plugin_registrations(id),
    version BIGINT NOT NULL CHECK (version > 0),
    enabled BOOLEAN NOT NULL,
    health TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (registration_id, version)
);

CREATE TRIGGER notification_plugin_state_immutable
    BEFORE UPDATE OR DELETE ON notification_plugin_registration_states
    FOR EACH ROW EXECUTE FUNCTION notification_reject_immutable_change();

ALTER TABLE notification_plugin_registrations
    ADD CONSTRAINT notification_plugin_current_state_fk
    FOREIGN KEY (id, version)
    REFERENCES notification_plugin_registration_states(registration_id, version)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE notification_plugin_grants (
    tenant_id TEXT NOT NULL,
    registration_id TEXT NOT NULL REFERENCES notification_plugin_registrations(id),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, registration_id)
);

CREATE TABLE notification_instances (
    tenant_id TEXT NOT NULL,
    id TEXT NOT NULL,
    name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    version BIGINT NOT NULL CHECK (version > 0),
    current_config_version BIGINT NOT NULL CHECK (current_config_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, id)
);

CREATE TABLE notification_instance_configs (
    tenant_id TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    config_version BIGINT NOT NULL CHECK (config_version > 0),
    plugin_registration_id TEXT NOT NULL,
    channel TEXT NOT NULL CHECK (channel IN ('email', 'sms', 'voice', 'im', 'webhook')),
    name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL,
    config JSONB NOT NULL,
    provider_identity JSONB NOT NULL,
    secret_state JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, instance_id, config_version),
    FOREIGN KEY (tenant_id, instance_id)
        REFERENCES notification_instances(tenant_id, id),
    FOREIGN KEY (tenant_id, plugin_registration_id)
        REFERENCES notification_plugin_grants(tenant_id, registration_id)
);

CREATE TABLE notification_instance_secrets (
    tenant_id TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    config_version BIGINT NOT NULL CHECK (config_version > 0),
    slot TEXT NOT NULL,
    key_ref TEXT NOT NULL,
    ciphertext BYTEA NOT NULL,
    aad_version INTEGER NOT NULL CHECK (aad_version > 0),
    PRIMARY KEY (tenant_id, instance_id, config_version, slot),
    FOREIGN KEY (tenant_id, instance_id, config_version)
        REFERENCES notification_instance_configs(tenant_id, instance_id, config_version)
);

CREATE TRIGGER notification_instance_secret_immutable
    BEFORE UPDATE OR DELETE ON notification_instance_secrets
    FOR EACH ROW EXECUTE FUNCTION notification_reject_immutable_change();

CREATE TRIGGER notification_instance_config_immutable
    BEFORE UPDATE OR DELETE ON notification_instance_configs
    FOR EACH ROW EXECUTE FUNCTION notification_reject_immutable_change();

ALTER TABLE notification_instances
    ADD CONSTRAINT notification_instances_current_config_fk
    FOREIGN KEY (tenant_id, id, current_config_version)
    REFERENCES notification_instance_configs(tenant_id, instance_id, config_version)
    DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX notification_instance_configs_registration_idx
    ON notification_instance_configs(tenant_id, plugin_registration_id, config_version DESC);

CREATE TABLE notification_groups (
    tenant_id TEXT NOT NULL,
    id TEXT NOT NULL,
    current_version BIGINT NOT NULL CHECK (current_version > 0),
    current_revision BIGINT NOT NULL CHECK (current_revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, id)
);

CREATE TABLE notification_group_revisions (
    tenant_id TEXT NOT NULL,
    group_id TEXT NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    version BIGINT NOT NULL CHECK (version > 0),
    name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, group_id, revision),
    FOREIGN KEY (tenant_id, group_id)
        REFERENCES notification_groups(tenant_id, id)
);

CREATE TRIGGER notification_group_revision_immutable
    BEFORE UPDATE OR DELETE ON notification_group_revisions
    FOR EACH ROW EXECUTE FUNCTION notification_reject_immutable_change();

ALTER TABLE notification_groups
    ADD CONSTRAINT notification_groups_current_revision_fk
    FOREIGN KEY (tenant_id, id, current_revision)
    REFERENCES notification_group_revisions(tenant_id, group_id, revision)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE notification_group_bindings (
    tenant_id TEXT NOT NULL,
    group_id TEXT NOT NULL,
    group_revision BIGINT NOT NULL,
    binding_id TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    recipient_source_key_ref TEXT NOT NULL,
    recipient_source_ciphertext BYTEA NOT NULL,
    recipient_source_aad_version INTEGER NOT NULL CHECK (recipient_source_aad_version > 0),
    content_binding_key_ref TEXT NOT NULL,
    content_binding_ciphertext BYTEA NOT NULL,
    content_binding_aad_version INTEGER NOT NULL CHECK (content_binding_aad_version > 0),
    PRIMARY KEY (tenant_id, group_id, group_revision, binding_id),
    FOREIGN KEY (tenant_id, group_id, group_revision)
        REFERENCES notification_group_revisions(tenant_id, group_id, revision),
    FOREIGN KEY (tenant_id, instance_id)
        REFERENCES notification_instances(tenant_id, id)
);

CREATE TRIGGER notification_group_binding_immutable
    BEFORE UPDATE OR DELETE ON notification_group_bindings
    FOR EACH ROW EXECUTE FUNCTION notification_reject_immutable_change();

CREATE INDEX notification_group_bindings_instance_idx
    ON notification_group_bindings(tenant_id, instance_id);

CREATE TABLE notification_source_group_mappings (
    source_deployment_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    legacy_group_id TEXT NOT NULL,
    notification_group_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (source_deployment_id, tenant_id, legacy_group_id),
    UNIQUE (source_deployment_id, tenant_id, legacy_group_id, notification_group_id),
    FOREIGN KEY (tenant_id, notification_group_id)
        REFERENCES notification_groups(tenant_id, id)
);

CREATE TABLE notification_source_group_revisions (
    source_deployment_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    legacy_group_id TEXT NOT NULL,
    notification_group_id TEXT NOT NULL,
    group_revision BIGINT NOT NULL CHECK (group_revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (source_deployment_id, tenant_id, legacy_group_id, group_revision),
    FOREIGN KEY (source_deployment_id, tenant_id, legacy_group_id, notification_group_id)
        REFERENCES notification_source_group_mappings(source_deployment_id, tenant_id, legacy_group_id, notification_group_id),
    FOREIGN KEY (tenant_id, notification_group_id, group_revision)
        REFERENCES notification_group_revisions(tenant_id, group_id, revision)
);

CREATE TRIGGER notification_source_group_revision_immutable
    BEFORE UPDATE OR DELETE ON notification_source_group_revisions
    FOR EACH ROW EXECUTE FUNCTION notification_reject_immutable_change();

CREATE TABLE notification_idempotency (
    tenant_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_digest CHAR(64) NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    resource_version BIGINT NOT NULL CHECK (resource_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, operation, idempotency_key),
    CHECK (char_length(idempotency_key) BETWEEN 8 AND 128)
);

CREATE TABLE notification_credentials (
    id TEXT PRIMARY KEY,
    purpose TEXT NOT NULL CHECK (purpose IN ('source', 'projection', 'receipt')),
    token_hash CHAR(64) NOT NULL,
    tenant_id TEXT,
    deployment_id TEXT,
    allowed_tenant_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    instance_id TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    rotated_from TEXT REFERENCES notification_credentials(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (purpose, token_hash),
    CHECK ((purpose = 'receipt' AND tenant_id IS NOT NULL AND instance_id IS NOT NULL
                AND deployment_id IS NULL AND allowed_tenant_ids = '[]'::jsonb)
        OR (purpose IN ('source', 'projection') AND tenant_id IS NULL AND instance_id IS NULL
                AND deployment_id IS NOT NULL AND jsonb_typeof(allowed_tenant_ids) = 'array'
                AND jsonb_array_length(allowed_tenant_ids) > 0))
);

CREATE INDEX notification_credentials_active_idx
    ON notification_credentials(purpose, token_hash, expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE notification_credential_replays (
    tenant_id TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    operation TEXT NOT NULL CHECK (operation = 'receipt-credential-rotation'),
    idempotency_key TEXT NOT NULL CHECK (char_length(idempotency_key) BETWEEN 8 AND 128),
    request_digest CHAR(64) NOT NULL,
    credential_id TEXT NOT NULL REFERENCES notification_credentials(id),
    response_key_ref TEXT NOT NULL,
    response_ciphertext BYTEA NOT NULL,
    response_aad_version INTEGER NOT NULL CHECK (response_aad_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, instance_id, operation, idempotency_key),
    FOREIGN KEY (tenant_id, instance_id)
        REFERENCES notification_instances(tenant_id, id)
);

CREATE TABLE notification_requests (
    tenant_id TEXT NOT NULL,
    id TEXT NOT NULL,
    source_type TEXT NOT NULL,
    source_id TEXT NOT NULL,
    source_deployment_id TEXT,
    source_event_id TEXT,
    action_id TEXT,
    group_id TEXT,
    group_revision BIGINT,
    source_group_id TEXT,
    source_group_revision BIGINT,
    intent_digest CHAR(64) NOT NULL,
    intake_status TEXT NOT NULL CHECK (intake_status IN ('ready', 'blocked')),
    blocked_reason TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, id),
    CHECK ((intake_status = 'blocked' AND blocked_reason IS NOT NULL)
        OR (intake_status = 'ready' AND blocked_reason IS NULL)),
    CHECK ((source_deployment_id IS NULL AND source_event_id IS NULL AND action_id IS NULL)
        OR (source_deployment_id IS NOT NULL AND source_event_id IS NOT NULL AND action_id IS NOT NULL)),
    FOREIGN KEY (tenant_id, group_id, group_revision)
        REFERENCES notification_group_revisions(tenant_id, group_id, revision)
);

CREATE UNIQUE INDEX notification_source_event_action_uq
    ON notification_requests(tenant_id, source_deployment_id, source_event_id, action_id)
    WHERE source_deployment_id IS NOT NULL;

CREATE INDEX notification_requests_created_idx
    ON notification_requests(tenant_id, created_at DESC, id);

CREATE TABLE notification_deliveries (
    tenant_id TEXT NOT NULL,
    id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    instance_id TEXT NOT NULL,
    config_version BIGINT NOT NULL CHECK (config_version > 0),
    recipient_kind TEXT NOT NULL,
    recipient_display TEXT NOT NULL,
    recipient_key_ref TEXT NOT NULL,
    recipient_ciphertext BYTEA NOT NULL,
    recipient_aad_version INTEGER NOT NULL CHECK (recipient_aad_version > 0),
    content_key_ref TEXT NOT NULL,
    content_ciphertext BYTEA NOT NULL,
    content_aad_version INTEGER NOT NULL CHECK (content_aad_version > 0),
    expires_at TIMESTAMPTZ NOT NULL,
    dispatch_status TEXT NOT NULL CHECK (dispatch_status IN ('queued', 'sending', 'accepted', 'failed', 'unknown')),
    delivery_status TEXT NOT NULL CHECK (delivery_status IN ('unsupported', 'pending', 'delivered', 'failed', 'unknown')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at TIMESTAMPTZ,
    current_attempt_id TEXT,
    current_lease_hash TEXT,
    lease_expires_at TIMESTAMPTZ,
    provider_message_id TEXT,
    error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, request_id, ordinal),
    FOREIGN KEY (tenant_id, request_id)
        REFERENCES notification_requests(tenant_id, id),
    FOREIGN KEY (tenant_id, instance_id, config_version)
        REFERENCES notification_instance_configs(tenant_id, instance_id, config_version),
    CHECK ((dispatch_status = 'sending' AND current_attempt_id IS NOT NULL
                AND current_lease_hash IS NOT NULL AND lease_expires_at IS NOT NULL)
        OR (dispatch_status <> 'sending' AND current_attempt_id IS NULL
                AND current_lease_hash IS NULL AND lease_expires_at IS NULL))
);

CREATE INDEX notification_deliveries_claim_idx
    ON notification_deliveries(dispatch_status, next_attempt_at, created_at, tenant_id, instance_id)
    WHERE dispatch_status = 'queued';
CREATE INDEX notification_deliveries_tenant_status_idx
    ON notification_deliveries(tenant_id, dispatch_status, created_at DESC, id);
CREATE INDEX notification_deliveries_tenant_instance_status_idx
    ON notification_deliveries(tenant_id, instance_id, dispatch_status, created_at DESC);
CREATE INDEX notification_deliveries_provider_idx
    ON notification_deliveries(tenant_id, instance_id, provider_message_id)
    WHERE provider_message_id IS NOT NULL;

CREATE TABLE notification_delivery_attempts (
    tenant_id TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    lease_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('sending', 'accepted', 'rejected', 'unknown', 'superseded')),
    started_at TIMESTAMPTZ NOT NULL,
    lease_expires_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    outcome_code TEXT,
    provider_message_id TEXT,
    retry_at TIMESTAMPTZ,
    PRIMARY KEY (tenant_id, delivery_id, attempt_id),
    FOREIGN KEY (tenant_id, delivery_id)
        REFERENCES notification_deliveries(tenant_id, id)
);

ALTER TABLE notification_deliveries
    ADD CONSTRAINT notification_deliveries_current_attempt_fk
    FOREIGN KEY (tenant_id, id, current_attempt_id)
    REFERENCES notification_delivery_attempts(tenant_id, delivery_id, attempt_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE notification_receipt_inbox (
    tenant_id TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    event_digest CHAR(64) NOT NULL,
    key_ref TEXT NOT NULL,
    ciphertext BYTEA NOT NULL,
    aad_version INTEGER NOT NULL CHECK (aad_version > 0),
    processing_status TEXT NOT NULL CHECK (processing_status IN ('pending', 'matched', 'unmatched', 'conflict')),
    provider_message_id TEXT NOT NULL,
    requested_delivery_id TEXT,
    receipt_status TEXT NOT NULL CHECK (receipt_status IN ('delivered', 'failed', 'unknown')),
    occurred_at TIMESTAMPTZ NOT NULL,
    delivery_id TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, instance_id, event_id),
    FOREIGN KEY (tenant_id, instance_id)
        REFERENCES notification_instances(tenant_id, id),
    FOREIGN KEY (tenant_id, delivery_id)
        REFERENCES notification_deliveries(tenant_id, id)
);

CREATE TABLE notification_receipt_conflicts (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    stored_digest CHAR(64) NOT NULL,
    incoming_digest CHAR(64) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (tenant_id, instance_id, event_id)
        REFERENCES notification_receipt_inbox(tenant_id, instance_id, event_id)
);

CREATE INDEX notification_receipt_inbox_delivery_idx
    ON notification_receipt_inbox(tenant_id, delivery_id, received_at DESC)
    WHERE delivery_id IS NOT NULL;
CREATE INDEX notification_receipt_inbox_provider_idx
    ON notification_receipt_inbox(tenant_id, instance_id, provider_message_id, occurred_at DESC);
