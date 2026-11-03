-- MySQL form of device_status_history (the device online/offline change log behind the dashboard trend).
-- Column list follows the effective PostgreSQL schema; the cascade to devices is kept.
-- @@
CREATE TABLE IF NOT EXISTS device_status_history (
    id BIGINT NOT NULL AUTO_INCREMENT,
    tenant_id VARCHAR(36) NOT NULL,
    device_id VARCHAR(36) NOT NULL,
    status SMALLINT NOT NULL,
    change_time DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_tenant_device_time (tenant_id, device_id, change_time),
    CONSTRAINT fk_device FOREIGN KEY (device_id) REFERENCES devices (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4
