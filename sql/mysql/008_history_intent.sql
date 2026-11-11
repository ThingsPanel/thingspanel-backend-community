-- MySQL form of the telemetry ingest tables that the intent-mode history path needs: the current
-- snapshot, the durable history intents and the durable device deletions (sql/history_intent/001, 002).
-- Differences from PostgreSQL:
--  * There are no partial indexes: pending lookups use (state, claimed_until, id).
--  * writer_token marks the rows one call inserted. INSERT IGNORE cannot say which rows it skipped, and a
--    DO NOTHING upsert reports "1 row" for an existing row on clientFoundRows connections, so the call
--    reads back its own token to learn which identities were new.
--  * Identifier columns are utf8mb4_bin (PostgreSQL compares them case-sensitively).
-- @@
CREATE TABLE IF NOT EXISTS telemetry_current_datas (
    device_id VARCHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    `key` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    ts DATETIME(6) NOT NULL,
    bool_v TINYINT(1) NULL,
    number_v DOUBLE NULL,
    string_v LONGTEXT NULL,
    tenant_id VARCHAR(36) NULL,
    UNIQUE KEY telemetry_current_datas_unique (device_id, `key`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin
-- @@
CREATE TABLE IF NOT EXISTS telemetry_history_intents (
    id BIGINT NOT NULL AUTO_INCREMENT,
    tenant_id VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    device_id VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    `key` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    ts BIGINT NOT NULL,
    bool_v TINYINT(1) NULL,
    number_v DOUBLE NULL,
    string_v LONGTEXT NULL,
    generation BIGINT NOT NULL DEFAULT 0,
    state VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts INT NOT NULL DEFAULT 0,
    claimed_by VARCHAR(128) NULL,
    claimed_until DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    applied_at DATETIME(6) NULL,
    writer_token CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL,
    PRIMARY KEY (id),
    CONSTRAINT telemetry_history_intents_identity UNIQUE (device_id, `key`, ts, generation),
    KEY telemetry_history_intents_pending (state, claimed_until, id),
    KEY telemetry_history_intents_token (writer_token),
    CONSTRAINT telemetry_history_intents_state_check CHECK (state IN ('pending', 'applied')),
    CONSTRAINT telemetry_history_intents_tenant_check CHECK (CHAR_LENGTH(tenant_id) > 0),
    CONSTRAINT telemetry_history_intents_applied_check CHECK ((state = 'applied') = (applied_at IS NOT NULL))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin
-- @@
CREATE TABLE IF NOT EXISTS telemetry_history_deletions (
    id BIGINT NOT NULL AUTO_INCREMENT,
    device_id VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts INT NOT NULL DEFAULT 0,
    claimed_by VARCHAR(128) NULL,
    claimed_until DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    applied_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    KEY telemetry_history_deletions_pending (state, id),
    KEY telemetry_history_deletions_device (device_id),
    CONSTRAINT telemetry_history_deletions_state_check CHECK (state IN ('pending', 'applied')),
    CONSTRAINT telemetry_history_deletions_device_check CHECK (CHAR_LENGTH(device_id) > 0),
    CONSTRAINT telemetry_history_deletions_applied_check CHECK ((state = 'applied') = (applied_at IS NOT NULL))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin
