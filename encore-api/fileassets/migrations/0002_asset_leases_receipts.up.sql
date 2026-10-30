CREATE FUNCTION fileassets_valid_envelope(envelope_value JSONB) RETURNS BOOLEAN AS $$
DECLARE
    key_count INTEGER;
BEGIN
    IF jsonb_typeof(envelope_value) <> 'object' THEN
        RETURN FALSE;
    END IF;
    SELECT count(*) INTO key_count FROM jsonb_object_keys(envelope_value);
    IF key_count <> 4 THEN
        RETURN FALSE;
    END IF;
    RETURN envelope_value ?& ARRAY['version', 'keyId', 'nonce', 'ciphertext']
       AND jsonb_typeof(envelope_value->'version') = 'number'
       AND (envelope_value->>'version') = '1'
       AND jsonb_typeof(envelope_value->'keyId') = 'string'
       AND length(envelope_value->>'keyId') BETWEEN 1 AND 64
       AND jsonb_typeof(envelope_value->'nonce') = 'string'
       AND length(envelope_value->>'nonce') = 16
       AND jsonb_typeof(envelope_value->'ciphertext') = 'string'
       AND length(envelope_value->>'ciphertext') >= 24;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

CREATE TABLE file_asset_upload_intents (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    object_key TEXT NOT NULL CHECK (object_key <> '' AND btrim(object_key) <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    state VARCHAR(32) NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'committed', 'cleaned', 'cleanup_pending')),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id),
    UNIQUE (object_key)
);

CREATE INDEX file_asset_upload_intents_recovery_idx
    ON file_asset_upload_intents(state, created_at, id)
    WHERE state IN ('pending', 'cleanup_pending');

CREATE FUNCTION file_asset_upload_intents_transition() RETURNS trigger AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.object_key IS DISTINCT FROM OLD.object_key OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'file asset upload intent identity is immutable' USING ERRCODE = '23514';
    END IF;
    IF NEW.state IS DISTINCT FROM OLD.state AND NOT (
        (OLD.state = 'pending' AND NEW.state IN ('committed', 'cleaned', 'cleanup_pending'))
        OR (OLD.state = 'cleanup_pending' AND NEW.state IN ('committed', 'cleaned'))
    ) THEN
        RAISE EXCEPTION 'file asset upload intent transition is invalid' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER file_asset_upload_intents_transition
    BEFORE UPDATE ON file_asset_upload_intents
    FOR EACH ROW EXECUTE FUNCTION file_asset_upload_intents_transition();

CREATE FUNCTION file_asset_upload_intents_no_delete() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'file asset upload intents are retained' USING ERRCODE = '23514';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER file_asset_upload_intents_no_delete
    BEFORE DELETE ON file_asset_upload_intents
    FOR EACH ROW EXECUTE FUNCTION file_asset_upload_intents_no_delete();

CREATE TABLE file_asset_url_leases (
    tenant_id UUID NOT NULL,
    asset_id UUID NOT NULL,
    encrypted_envelope JSONB NOT NULL CHECK (fileassets_valid_envelope(encrypted_envelope)),
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, asset_id),
    FOREIGN KEY (tenant_id, asset_id) REFERENCES file_assets(tenant_id, id) ON DELETE RESTRICT
);

CREATE FUNCTION file_asset_url_leases_identity_immutable() RETURNS trigger AS $$
BEGIN
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.asset_id IS DISTINCT FROM OLD.asset_id THEN
        RAISE EXCEPTION 'file asset lease identity is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER file_asset_url_leases_identity_immutable
    BEFORE UPDATE ON file_asset_url_leases
    FOR EACH ROW EXECUTE FUNCTION file_asset_url_leases_identity_immutable();

CREATE TABLE file_asset_write_receipts (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    subject_id VARCHAR(128) NOT NULL CHECK (btrim(subject_id) <> '' AND octet_length(subject_id) <= 128),
    operation VARCHAR(128) NOT NULL CHECK (btrim(operation) <> '' AND octet_length(operation) <= 128),
    canonical_path VARCHAR(2048) NOT NULL CHECK (canonical_path LIKE '/%' AND position('?' in canonical_path) = 0 AND position('#' in canonical_path) = 0 AND octet_length(canonical_path) <= 2048),
    idempotency_key VARCHAR(128) NOT NULL CHECK (length(idempotency_key) BETWEEN 8 AND 128 AND idempotency_key ~ '^[A-Za-z0-9._:-]+$'),
    request_hash CHAR(64) NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    location VARCHAR(2048) NOT NULL CHECK (location <> '' AND octet_length(location) <= 2048),
    request_id VARCHAR(128) NOT NULL CHECK (btrim(request_id) <> '' AND octet_length(request_id) <= 128),
    encrypted_response JSONB NOT NULL CHECK (fileassets_valid_envelope(encrypted_response)),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, subject_id, operation, canonical_path, idempotency_key)
);

CREATE FUNCTION file_asset_write_receipts_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'file asset write receipts are immutable' USING ERRCODE = '23514';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER file_asset_write_receipts_immutable
    BEFORE UPDATE OR DELETE ON file_asset_write_receipts
    FOR EACH ROW EXECUTE FUNCTION file_asset_write_receipts_immutable();

CREATE TABLE file_asset_audit_events (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    asset_id UUID NOT NULL,
    actor VARCHAR(128) NOT NULL CHECK (btrim(actor) <> '' AND actor !~ '[[:cntrl:]]' AND octet_length(actor) <= 128),
    operation VARCHAR(64) NOT NULL CHECK (operation = 'file_asset.created'),
    request_id VARCHAR(128) NOT NULL CHECK (btrim(request_id) <> '' AND octet_length(request_id) <= 128),
    content_type VARCHAR(32) NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg', 'image/webp')),
    size_bytes BIGINT NOT NULL CHECK (size_bytes BETWEEN 1 AND 10485760),
    checksum CHAR(64) NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (tenant_id, asset_id) REFERENCES file_assets(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX file_asset_audit_events_tenant_asset_idx ON file_asset_audit_events(tenant_id, asset_id, created_at DESC);

CREATE FUNCTION file_asset_audit_events_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'file asset audit events are immutable' USING ERRCODE = '23514';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER file_asset_audit_events_immutable
    BEFORE UPDATE OR DELETE ON file_asset_audit_events
    FOR EACH ROW EXECUTE FUNCTION file_asset_audit_events_immutable();
