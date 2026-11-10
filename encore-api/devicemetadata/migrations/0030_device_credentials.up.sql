-- Native device and template credentials are separate from the legacy
-- devices.voucher projection. Fingerprints are summary metadata; verifiers,
-- encrypted materials, and public X.509 certificates are distinct modes.

CREATE TABLE device_credentials (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    device_id UUID,
    template_id UUID,
    credential_owner_kind VARCHAR(16) GENERATED ALWAYS AS (
        CASE WHEN device_id IS NULL THEN 'template' ELSE 'device' END
    ) STORED,
    credential_owner_id UUID GENERATED ALWAYS AS (COALESCE(device_id, template_id)) STORED,
    purpose VARCHAR(32) NOT NULL,
    kind VARCHAR(32) NOT NULL,
    protection_mode VARCHAR(24) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    public_identifier VARCHAR(256),

    verifier TEXT,
    fingerprint_key_id VARCHAR(128),
    fingerprint BYTEA NOT NULL,

    ciphertext BYTEA,
    key_id VARCHAR(128),
    cipher_suite VARCHAR(32),
    nonce BYTEA,
    public_certificate TEXT,

    rotation_operation_id UUID,
    supersedes_credential_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(128) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by VARCHAR(128) NOT NULL,
    resource_version BIGINT NOT NULL DEFAULT 1,
    activated_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revoked_by VARCHAR(128),
    revocation_reason VARCHAR(512),
    expires_at TIMESTAMPTZ,

    CONSTRAINT device_credentials_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT device_credentials_owner_id_unique UNIQUE (tenant_id, credential_owner_kind, credential_owner_id, purpose, id),
    CONSTRAINT device_credentials_owner_purpose_valid CHECK (
        (device_id IS NOT NULL AND template_id IS NULL AND purpose = 'device_access')
        OR (device_id IS NULL AND template_id IS NOT NULL AND purpose = 'template_enrollment')
    ),
    CONSTRAINT device_credentials_kind_valid CHECK (kind IN ('token', 'username_password', 'x509')),
    CONSTRAINT device_credentials_template_kind_valid CHECK (
        purpose <> 'template_enrollment' OR kind = 'token'
    ),
    CONSTRAINT device_credentials_template_status_valid CHECK (
        purpose <> 'template_enrollment' OR status IN ('active', 'revoked')
    ),
    CONSTRAINT device_credentials_public_identifier_length CHECK (
        public_identifier IS NULL OR length(public_identifier) BETWEEN 1 AND 256
    ),
    CONSTRAINT device_credentials_audit_actor_valid CHECK (
        length(btrim(created_by)) BETWEEN 1 AND 128
        AND length(btrim(updated_by)) BETWEEN 1 AND 128
    ),
    CONSTRAINT device_credentials_fingerprint_shape CHECK (
        octet_length(fingerprint) = 32
        AND ((kind = 'x509' AND protection_mode = 'public_certificate'
                AND (fingerprint_key_id IS NULL OR length(fingerprint_key_id) BETWEEN 1 AND 128))
            OR (fingerprint_key_id IS NOT NULL AND length(fingerprint_key_id) BETWEEN 1 AND 128))
    ),
    CONSTRAINT device_credentials_protection_shape_valid CHECK (
        (protection_mode = 'verifier'
            AND verifier IS NOT NULL AND octet_length(btrim(verifier)) BETWEEN 1 AND 8192
            AND ciphertext IS NULL AND key_id IS NULL AND cipher_suite IS NULL AND nonce IS NULL
            AND public_certificate IS NULL)
        OR (protection_mode = 'encrypted'
            AND verifier IS NULL
            AND ciphertext IS NOT NULL AND octet_length(ciphertext) BETWEEN 17 AND 8208
            AND key_id IS NOT NULL AND length(key_id) BETWEEN 1 AND 128
            AND cipher_suite = 'AES-256-GCM'
            AND nonce IS NOT NULL AND octet_length(nonce) = 12
            AND public_certificate IS NULL)
        OR (protection_mode = 'public_certificate'
            AND kind = 'x509'
            AND verifier IS NULL
            AND ciphertext IS NULL AND key_id IS NULL AND cipher_suite IS NULL AND nonce IS NULL
            AND public_certificate IS NOT NULL AND length(btrim(public_certificate)) > 0
            AND octet_length(public_certificate) BETWEEN 1 AND 32768
            AND position('PRIVATE KEY' IN upper(public_certificate)) = 0)
    ),
    CONSTRAINT device_credentials_x509_public_only CHECK (
        (kind = 'x509' AND protection_mode = 'public_certificate')
        OR (kind <> 'x509' AND protection_mode <> 'public_certificate')
    ),
    CONSTRAINT device_credentials_status_valid CHECK (
        (status = 'pending' AND activated_at IS NULL AND revoked_at IS NULL AND revoked_by IS NULL AND revocation_reason IS NULL)
        OR (status = 'active' AND activated_at IS NOT NULL AND revoked_at IS NULL AND revoked_by IS NULL AND revocation_reason IS NULL)
        OR (status = 'revoked' AND revoked_at IS NOT NULL
            AND revoked_by IS NOT NULL AND length(btrim(revoked_by)) BETWEEN 1 AND 128
            AND revocation_reason IS NOT NULL AND length(btrim(revocation_reason)) BETWEEN 1 AND 512
            AND (activated_at IS NULL OR revoked_at >= activated_at))
    ),
    CONSTRAINT device_credentials_lifecycle_time_valid CHECK (
        (activated_at IS NULL OR activated_at >= created_at)
        AND (revoked_at IS NULL OR revoked_at >= created_at)
    ),
    CONSTRAINT device_credentials_expiry_valid CHECK (
        expires_at IS NULL OR (expires_at > created_at AND (activated_at IS NULL OR expires_at > activated_at))
    ),
    CONSTRAINT device_credentials_resource_version_range CHECK (
        resource_version BETWEEN 1 AND 9007199254740991
    ),
    CONSTRAINT device_credentials_supersedes_not_self CHECK (
        supersedes_credential_id IS NULL OR supersedes_credential_id <> id
    ),
    CONSTRAINT device_credentials_rotation_pair_valid CHECK (
        (rotation_operation_id IS NULL) = (supersedes_credential_id IS NULL)
    ),
    CONSTRAINT device_credentials_device_fk
        FOREIGN KEY (tenant_id, device_id)
        REFERENCES devices (tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT device_credentials_template_fk
        FOREIGN KEY (tenant_id, template_id)
        REFERENCES device_access_templates (tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT device_credentials_rotation_operation_fk
        FOREIGN KEY (tenant_id, rotation_operation_id)
        REFERENCES metadata_operations (tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT device_credentials_supersedes_same_owner_fk
        FOREIGN KEY (tenant_id, credential_owner_kind, credential_owner_id, purpose, supersedes_credential_id)
        REFERENCES device_credentials (tenant_id, credential_owner_kind, credential_owner_id, purpose, id) ON DELETE RESTRICT
);

CREATE INDEX idx_device_credentials_owner_purpose_status
    ON device_credentials (tenant_id, credential_owner_id, purpose, status, id);
CREATE INDEX idx_device_credentials_fingerprint_lookup
    ON device_credentials (tenant_id, purpose, fingerprint_key_id, fingerprint)
    WHERE status = 'active';
CREATE INDEX idx_device_credentials_public_identifier
    ON device_credentials (tenant_id, purpose, public_identifier)
    WHERE public_identifier IS NOT NULL;
CREATE INDEX idx_device_credentials_supersedes
    ON device_credentials (tenant_id, credential_owner_kind, credential_owner_id, purpose, supersedes_credential_id)
    WHERE supersedes_credential_id IS NOT NULL;

CREATE FUNCTION reject_device_credential_identity_reassignment()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.device_id IS DISTINCT FROM OLD.device_id
       OR NEW.template_id IS DISTINCT FROM OLD.template_id
       OR NEW.purpose IS DISTINCT FROM OLD.purpose THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'device credential identity is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_device_credentials_identity_immutable
    BEFORE UPDATE OF id, tenant_id, device_id, template_id, purpose ON device_credentials
    FOR EACH ROW EXECUTE FUNCTION reject_device_credential_identity_reassignment();

CREATE FUNCTION reject_device_credential_revocation_reversal()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status = 'revoked' AND (
        NEW.status IS DISTINCT FROM 'revoked'
        OR NEW.revoked_at IS DISTINCT FROM OLD.revoked_at
        OR NEW.revoked_by IS DISTINCT FROM OLD.revoked_by
        OR NEW.revocation_reason IS DISTINCT FROM OLD.revocation_reason
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'device credential revocation is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_device_credentials_revocation_immutable
    BEFORE UPDATE OF status, revoked_at, revoked_by, revocation_reason ON device_credentials
    FOR EACH ROW EXECUTE FUNCTION reject_device_credential_revocation_reversal();

CREATE FUNCTION update_device_credentials_updated_at()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF (to_jsonb(NEW) - 'updated_at') IS DISTINCT FROM (to_jsonb(OLD) - 'updated_at')
       OR NEW.updated_at IS DISTINCT FROM OLD.updated_at THEN
        NEW.updated_at := NOW();
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_device_credentials_updated_at
    BEFORE UPDATE ON device_credentials
    FOR EACH ROW EXECUTE FUNCTION update_device_credentials_updated_at();

COMMENT ON TABLE device_credentials IS
    'Native device/template credential material with tenant and owner scope; separate from legacy devices.voucher and runtime state.';
COMMENT ON COLUMN device_credentials.verifier IS
    'Algorithm-encoded one-way verifier. HMAC fingerprint is separate summary metadata and is not a password verifier.';
COMMENT ON COLUMN device_credentials.fingerprint IS
    '32-byte credential summary fingerprint; secret HMAC fingerprints require fingerprint_key_id, while public X.509 digests may omit it.';
COMMENT ON COLUMN device_credentials.ciphertext IS
    'AES-256-GCM ciphertext only, limited to 17..8208 bytes (1..8192 secret bytes plus tag); nonce, key reference and suite are separate. Plaintext and private keys are never stored.';
COMMENT ON COLUMN device_credentials.public_certificate IS
    'Public X.509 certificate only, at most 32768 bytes. Private key material is never stored in this table.';
COMMENT ON COLUMN device_credentials.credential_owner_id IS
    'Generated owner identity used to enforce same-tenant, same-owner rotation chains.';
