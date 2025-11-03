-- ✅2025/11/3 设备状态历史记录表
CREATE TABLE device_status_history (
    id BIGSERIAL PRIMARY KEY,
    tenant_id VARCHAR(36) NOT NULL,
    device_id VARCHAR(36) NOT NULL,
    status SMALLINT NOT NULL,
    change_time TIMESTAMPTZ(6) NOT NULL,
    
    CONSTRAINT fk_device FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE
);

CREATE INDEX idx_tenant_device_time ON device_status_history (tenant_id, device_id, change_time);

COMMENT ON TABLE device_status_history IS '设备状态历史表';
COMMENT ON COLUMN device_status_history.tenant_id IS '租户ID';
COMMENT ON COLUMN device_status_history.device_id IS '设备ID';
COMMENT ON COLUMN device_status_history.status IS '状态: 0-离线 1-在线';
COMMENT ON COLUMN device_status_history.change_time IS '状态变更时间';