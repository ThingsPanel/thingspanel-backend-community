-- MySQL form of the permanent device identity reservations (sql/31.sql). Same rules:
--   INSERT into devices needs an active reservation for the same id and tenant;
--   DELETE needs a tombstoned reservation for the same id and tenant;
--   id and tenant_id never change.
-- Statements are separated by lines containing only "-- @@" because trigger bodies contain ';'.
-- Requirements: MySQL 8.0.16+ (enforced CHECK constraints). Creating triggers needs the TRIGGER
-- privilege, and SUPER or log_bin_trust_function_creators=1 when binary logging is on.
-- The application account must not hold CREATE TEMPORARY TABLES: a session-local table with the
-- reservation table's name would shadow it inside the triggers (PostgreSQL pins pg_temp last
-- for the same reason).
-- @@
CREATE TABLE IF NOT EXISTS device_id_reservations (
    id VARCHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    tenant_id VARCHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    state VARCHAR(16) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    deleted_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    CONSTRAINT device_id_reservations_state_check CHECK (state IN ('active', 'tombstoned')),
    CONSTRAINT device_id_reservations_lifecycle_check CHECK ((state = 'active' AND deleted_at IS NULL) OR (state = 'tombstoned' AND deleted_at IS NOT NULL))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4
-- @@
INSERT IGNORE INTO device_id_reservations (id, tenant_id, state, created_at)
SELECT id, tenant_id, 'active', COALESCE(created_at, CURRENT_TIMESTAMP(6)) FROM devices
-- @@
DROP TRIGGER IF EXISTS devices_identity_fence_insert
-- @@
CREATE TRIGGER devices_identity_fence_insert BEFORE INSERT ON devices FOR EACH ROW
BEGIN
    DECLARE found INT DEFAULT 0;
    SELECT 1 INTO found FROM device_id_reservations
        WHERE id = NEW.id AND tenant_id = NEW.tenant_id AND state = 'active' FOR SHARE;
    IF found = 0 THEN
        SIGNAL SQLSTATE '23000' SET MESSAGE_TEXT = 'device INSERT requires an active tenant identity reservation';
    END IF;
END
-- @@
DROP TRIGGER IF EXISTS devices_identity_fence_update
-- @@
CREATE TRIGGER devices_identity_fence_update BEFORE UPDATE ON devices FOR EACH ROW
BEGIN
    IF NOT (NEW.id <=> OLD.id) OR NOT (NEW.tenant_id <=> OLD.tenant_id) THEN
        SIGNAL SQLSTATE '23000' SET MESSAGE_TEXT = 'device identity and tenant are immutable';
    END IF;
END
-- @@
DROP TRIGGER IF EXISTS devices_identity_fence_delete
-- @@
CREATE TRIGGER devices_identity_fence_delete BEFORE DELETE ON devices FOR EACH ROW
BEGIN
    DECLARE found INT DEFAULT 0;
    SELECT 1 INTO found FROM device_id_reservations
        WHERE id = OLD.id AND tenant_id = OLD.tenant_id AND state = 'tombstoned' FOR SHARE;
    IF found = 0 THEN
        SIGNAL SQLSTATE '23000' SET MESSAGE_TEXT = 'device DELETE requires a tenant identity tombstone';
    END IF;
END
