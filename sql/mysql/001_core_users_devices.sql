-- MySQL 8 schema for the first platform slice (users, devices). It mirrors the effective
-- PostgreSQL schema after sql/1.sql .. sql/32.sql, measured from a database those scripts built.
--
-- Deliberate differences from PostgreSQL:
--  * Identifier-like columns that are UNIQUE in PostgreSQL (email, device_number, voucher) use
--    utf8mb4_bin so uniqueness stays case-sensitive; MySQL's default collation would not.
--  * devices.voucher is VARCHAR(2048): a unique index over it would exceed InnoDB's 3072-byte key
--    limit under utf8mb4, so uniqueness is enforced on SHA2(voucher, 256) in a stored column.
--  * timestamptz -> DATETIME(6) holding the connection's configured zone (see MysqlConnect).
--  * json defaults need expressions in MySQL.
--  * Foreign keys to products, device_configs and service_access are not created here: those
--    tables are not part of this slice yet.
CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(36) NOT NULL,
    name VARCHAR(255) NULL,
    phone_number VARCHAR(50) NOT NULL,
    email VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    status VARCHAR(2) NULL,
    authority VARCHAR(50) NULL,
    password VARCHAR(255) NOT NULL,
    tenant_id VARCHAR(36) NULL,
    remark VARCHAR(255) NULL,
    additional_info JSON NULL DEFAULT (JSON_OBJECT()),
    created_at DATETIME(6) NULL,
    updated_at DATETIME(6) NULL,
    password_last_updated DATETIME(6) NULL,
    last_visit_time DATETIME(6) NULL,
    last_visit_ip VARCHAR(30) NULL,
    last_visit_device VARCHAR(200) NULL,
    organization VARCHAR(200) NULL,
    timezone VARCHAR(50) NULL,
    default_language VARCHAR(10) NULL,
    password_fail_count INT NULL DEFAULT 0,
    avatar_url VARCHAR(500) NULL,
    PRIMARY KEY (id),
    CONSTRAINT users_un UNIQUE (email)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

CREATE TABLE IF NOT EXISTS devices (
    id VARCHAR(36) NOT NULL,
    name VARCHAR(255) NULL,
    voucher VARCHAR(2048) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
    voucher_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin GENERATED ALWAYS AS (SHA2(voucher, 256)) STORED NOT NULL,
    tenant_id VARCHAR(36) NOT NULL DEFAULT '',
    is_enabled VARCHAR(36) NOT NULL DEFAULT '',
    activate_flag VARCHAR(36) NOT NULL DEFAULT '',
    created_at DATETIME(6) NULL,
    update_at DATETIME(6) NULL,
    device_number VARCHAR(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
    product_id VARCHAR(36) NULL,
    parent_id VARCHAR(36) NULL,
    protocol VARCHAR(36) NULL,
    label VARCHAR(255) NULL,
    location VARCHAR(100) NULL,
    sub_device_addr VARCHAR(36) NULL,
    current_version VARCHAR(36) NULL,
    additional_info JSON NULL DEFAULT (JSON_OBJECT()),
    protocol_config JSON NULL DEFAULT (JSON_OBJECT()),
    remark1 VARCHAR(255) NULL,
    remark2 VARCHAR(255) NULL,
    remark3 VARCHAR(255) NULL,
    device_config_id VARCHAR(36) NULL,
    batch_number VARCHAR(500) NULL,
    activate_at DATETIME(6) NULL,
    is_online SMALLINT NOT NULL DEFAULT 0,
    access_way VARCHAR(10) NULL,
    description VARCHAR(500) NULL,
    service_access_id VARCHAR(36) NULL,
    last_offline_time DATETIME(6) NULL,
    PRIMARY KEY (id),
    CONSTRAINT devices_unique UNIQUE (device_number),
    CONSTRAINT devices_unique_1 UNIQUE (voucher_digest),
    KEY idx_devices_tenant_active_created (tenant_id, activate_flag, created_at),
    KEY idx_devices_name (name),
    KEY idx_devices_parent (parent_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;
