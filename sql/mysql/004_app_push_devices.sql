-- MySQL form of the app push device table (initialize/app_push_schema.go) including the
-- auth_version column that sql/auth/001 adds in PostgreSQL.
-- MySQL has no partial unique indexes ("... WHERE active"): uniqueness among ACTIVE rows is kept by
-- generated columns that are NULL for inactive rows. NULLs never collide in a unique index.
-- @@
CREATE TABLE IF NOT EXISTS app_push_devices (
    id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    tenant_id VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    user_id VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    app_id VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    installation_id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    cid VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    platform VARCHAR(64) NOT NULL,
    session_hash VARCHAR(255) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    active TINYINT(1) NOT NULL DEFAULT 1,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    auth_version BIGINT NOT NULL DEFAULT 0,
    active_cid VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin GENERATED ALWAYS AS (IF(active, cid, NULL)) STORED NULL,
    active_installation VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin GENERATED ALWAYS AS (IF(active, installation_id, NULL)) STORED NULL,
    PRIMARY KEY (id),
    UNIQUE KEY app_push_active_cid (app_id, active_cid),
    UNIQUE KEY app_push_active_installation (app_id, active_installation),
    KEY app_push_member (tenant_id, user_id, active)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin
