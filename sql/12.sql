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

-- ✅2025/11/11 设备主题转换

-- public.device_topic_mappings definition

-- Drop table

-- DROP TABLE public.device_topic_mappings;

CREATE TABLE public.device_topic_mappings (
	id bigserial NOT NULL,
	device_config_id uuid NOT NULL,
	"name" varchar(500) NOT NULL,
	direction varchar(50) NOT NULL,
	source_topic varchar(500) NOT NULL,
	target_topic varchar(500) NOT NULL,
	priority int4 DEFAULT 100 NOT NULL,
	enabled bool DEFAULT true NOT NULL,
	description text NULL,
	created_at timestamptz(6) DEFAULT now() NOT NULL,
	updated_at timestamptz(6) DEFAULT now() NOT NULL,
	CONSTRAINT device_topic_mappings_pkey PRIMARY KEY (id)
);
CREATE INDEX idx_device_topic_mapping_lookup ON public.device_topic_mappings USING btree (device_config_id, direction, enabled, priority);
CREATE UNIQUE INDEX ux_device_topic_mapping_unique ON public.device_topic_mappings USING btree (device_config_id, direction, source_topic, target_topic);

-- ✅2025/11/13 设备列表查询索引
CREATE INDEX idx_devices_tenant_active_created
    ON devices (tenant_id, activate_flag, created_at DESC);