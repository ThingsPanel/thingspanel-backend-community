-- MySQL form of sql/auth/001..003: users.auth_version, external identity bindings (with the
-- self-unlink flag from 002), the audit trail and the provider settings.
-- Differences from PostgreSQL:
--  * UNIQUE(issuer, subject) cannot span TEXT columns. issuer is VARCHAR(512) and subject
--    VARCHAR(255), utf8mb4_bin: 767 characters * 4 bytes = 3068 bytes, inside InnoDB's 3072-byte
--    key limit. OpenID Connect caps subject at 255 ASCII characters, so no valid subject is lost.
--  * jsonb -> JSON, bytea -> LONGBLOB, boolean -> TINYINT(1), bigserial -> BIGINT AUTO_INCREMENT.
--  * ADD COLUMN has no IF NOT EXISTS: it is guarded through information_schema and a prepared
--    statement (session variables, so the runner's single pinned connection is required).
--  * app_push_devices.auth_version is not created: that table is not part of the MySQL slice yet.
-- @@
SET @tp_auth_col := (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'users' AND column_name = 'auth_version')
-- @@
SET @tp_auth_sql := IF(@tp_auth_col = 0, 'ALTER TABLE users ADD COLUMN auth_version BIGINT NOT NULL DEFAULT 0, ADD CONSTRAINT users_auth_version_check CHECK (auth_version >= 0)', 'SELECT 1')
-- @@
PREPARE tp_auth_stmt FROM @tp_auth_sql
-- @@
EXECUTE tp_auth_stmt
-- @@
DEALLOCATE PREPARE tp_auth_stmt
-- @@
CREATE TABLE IF NOT EXISTS auth_external_bindings (
    id VARCHAR(36) NOT NULL,
    user_id VARCHAR(36) NOT NULL,
    issuer VARCHAR(512) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    subject VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    provider VARCHAR(255) NOT NULL,
    client_id VARCHAR(255) NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    disabled_at DATETIME(6) NULL,
    rebind_until DATETIME(6) NULL,
    self_unlinked TINYINT(1) NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    CONSTRAINT auth_external_bindings_identity UNIQUE (issuer, subject),
    CONSTRAINT auth_external_bindings_user_fk FOREIGN KEY (user_id) REFERENCES users (id),
    KEY auth_external_bindings_user (user_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin
-- @@
CREATE TABLE IF NOT EXISTS auth_audit_events (
    id BIGINT NOT NULL AUTO_INCREMENT,
    actor_id VARCHAR(255) NOT NULL,
    user_id VARCHAR(255) NOT NULL,
    event VARCHAR(255) NOT NULL,
    provider VARCHAR(255) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin
-- @@
CREATE TABLE IF NOT EXISTS auth_provider_settings (
    id VARCHAR(64) NOT NULL,
    draft JSON NOT NULL,
    draft_secret LONGBLOB NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    validated_revision BIGINT NOT NULL DEFAULT 0,
    tested_revision BIGINT NOT NULL DEFAULT 0,
    tested_at DATETIME(6) NULL,
    published JSON NULL,
    published_secret LONGBLOB NULL,
    published_revision BIGINT NOT NULL DEFAULT 0,
    generation BIGINT NOT NULL DEFAULT 0,
    enabled TINYINT(1) NOT NULL DEFAULT 0,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT auth_provider_settings_counters_check CHECK (revision > 0 AND published_revision >= 0 AND generation >= 0),
    CONSTRAINT auth_provider_settings_enabled_check CHECK (NOT enabled OR (published IS NOT NULL AND published_secret IS NOT NULL AND published_revision > 0))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin
