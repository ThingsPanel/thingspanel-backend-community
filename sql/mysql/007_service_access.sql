-- MySQL form of service_access and its creation receipts (sql/service_access/001).
-- Differences from PostgreSQL:
--  * service_access_config is LONGTEXT, not JSON: the receipt stores a hash of the exact configuration
--    bytes, and MySQL's JSON type would reorder keys and rewrite whitespace on read, turning every
--    replay into a false RESOURCE_CHANGED. PostgreSQL's json type keeps the text as written.
--  * The foreign key to service_plugins is not created: that table is not part of the MySQL slice yet.
--  * The receipt format checks use REGEXP_LIKE(..., 'c') so they stay case-sensitive.
-- @@
CREATE TABLE IF NOT EXISTS service_access (
    id VARCHAR(36) NOT NULL,
    name VARCHAR(100) NOT NULL,
    service_plugin_id VARCHAR(36) NOT NULL,
    voucher VARCHAR(999) NOT NULL,
    description VARCHAR(255) NULL,
    service_access_config LONGTEXT NULL,
    remark VARCHAR(255) NULL,
    create_at DATETIME(6) NOT NULL,
    update_at DATETIME(6) NOT NULL,
    tenant_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_service_access_plugin (service_plugin_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin
-- @@
CREATE TABLE IF NOT EXISTS service_access_create_receipts (
    tenant_id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    operation_key VARCHAR(200) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    subject_id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    request_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    access_id VARCHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    resource_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL,
    PRIMARY KEY (tenant_id, operation_key),
    CONSTRAINT service_access_create_receipts_access_unique UNIQUE (access_id),
    CONSTRAINT service_access_create_receipts_tenant_check CHECK (CHAR_LENGTH(tenant_id) > 0),
    CONSTRAINT service_access_create_receipts_subject_check CHECK (CHAR_LENGTH(subject_id) > 0),
    CONSTRAINT service_access_create_receipts_key_check CHECK (REGEXP_LIKE(operation_key, '^[A-Za-z0-9][A-Za-z0-9._:-]{0,199}$', 'c')),
    CONSTRAINT service_access_create_receipts_request_hash_check CHECK (REGEXP_LIKE(request_hash, '^[0-9a-f]{64}$', 'c')),
    CONSTRAINT service_access_create_receipts_resource_hash_check CHECK (REGEXP_LIKE(resource_hash, '^[0-9a-f]{64}$', 'c'))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin
