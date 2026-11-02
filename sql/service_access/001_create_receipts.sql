-- Owned by the existing ServiceAccess business module. Receipts outlive their
-- resources so response loss or later deletion cannot recreate an old intent.
CREATE TABLE IF NOT EXISTS service_access_create_receipts (
    tenant_id TEXT NOT NULL,
    operation_key VARCHAR(200) NOT NULL,
    subject_id TEXT NOT NULL,
    request_hash CHAR(64) NOT NULL,
    access_id VARCHAR(36) NOT NULL,
    resource_hash CHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, operation_key),
    CONSTRAINT service_access_create_receipts_access_unique UNIQUE (access_id),
    CONSTRAINT service_access_create_receipts_tenant_check CHECK (length(tenant_id) > 0),
    CONSTRAINT service_access_create_receipts_subject_check CHECK (length(subject_id) > 0),
    CONSTRAINT service_access_create_receipts_key_check CHECK (operation_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,199}$'),
    CONSTRAINT service_access_create_receipts_request_hash_check CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT service_access_create_receipts_resource_hash_check CHECK (resource_hash ~ '^[0-9a-f]{64}$')
);
