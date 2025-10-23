-- public.telemetry_datas definition

-- Drop table

-- DROP TABLE public.telemetry_datas;

CREATE TABLE public.telemetry_datas (
	device_id varchar(36) NOT NULL, -- 设备ID
	"key" varchar(255) NOT NULL, -- 数据标识符
	ts int8 NOT NULL, -- 上报时间
	bool_v bool NULL,
	number_v float8 NULL,
	string_v text NULL,
	tenant_id varchar(36) NULL,
	CONSTRAINT telemetry_datas_device_id_key_ts_key UNIQUE (device_id, key, ts)
);
CREATE INDEX telemetry_datas_ts_idx ON public.telemetry_datas USING btree (ts DESC);

-- Column comments

COMMENT ON COLUMN public.telemetry_datas.device_id IS '设备ID';
COMMENT ON COLUMN public.telemetry_datas."key" IS '数据标识符';
COMMENT ON COLUMN public.telemetry_datas.ts IS '上报时间';

-- Table Triggers

create trigger ts_insert_blocker before
insert
    on
    public.telemetry_datas for each row execute function _timescaledb_functions.insert_blocker();

-- public.telemetry_set_logs definition

-- Drop table

-- DROP TABLE public.telemetry_set_logs;

CREATE TABLE public.telemetry_set_logs (
	id varchar(36) NOT NULL,
	device_id varchar(36) NOT NULL, -- 设备id（外键-关联删除）
	operation_type varchar(255) NULL, -- 操作类型1-手动操作 2-自动触发
	"data" json NULL, -- 发送内容
	status varchar(2) NULL, -- 1-发送成功 2-失败
	error_message varchar(500) NULL, -- 错误信息
	created_at timestamptz(6) NOT NULL, -- 创建时间
	user_id varchar(36) NULL, -- 操作用户
	description varchar(255) NULL, -- 描述
	CONSTRAINT telemetry_set_logs_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.telemetry_set_logs.device_id IS '设备id（外键-关联删除）';
COMMENT ON COLUMN public.telemetry_set_logs.operation_type IS '操作类型1-手动操作 2-自动触发';
COMMENT ON COLUMN public.telemetry_set_logs."data" IS '发送内容';
COMMENT ON COLUMN public.telemetry_set_logs.status IS '1-发送成功 2-失败';
COMMENT ON COLUMN public.telemetry_set_logs.error_message IS '错误信息';
COMMENT ON COLUMN public.telemetry_set_logs.created_at IS '创建时间';
COMMENT ON COLUMN public.telemetry_set_logs.user_id IS '操作用户';
COMMENT ON COLUMN public.telemetry_set_logs.description IS '描述';


-- public.telemetry_set_logs foreign keys

ALTER TABLE public.telemetry_set_logs ADD CONSTRAINT telemetry_set_logs_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE CASCADE;

-- public.telemetry_current_datas definition

-- Drop table

-- DROP TABLE public.telemetry_current_datas;

CREATE TABLE public.telemetry_current_datas (
	device_id varchar(36) NOT NULL, -- 设备ID
	"key" varchar(255) NOT NULL, -- 数据标识符
	ts timestamptz(6) NOT NULL, -- 上报时间
	bool_v bool NULL,
	number_v float8 NULL,
	string_v text NULL,
	tenant_id varchar(36) NULL,
	CONSTRAINT telemetry_current_datas_unique UNIQUE (device_id, key)
);
CREATE INDEX telemetry_datas_ts_idx_copy1 ON public.telemetry_current_datas USING btree (ts DESC);

-- Column comments

COMMENT ON COLUMN public.telemetry_current_datas.device_id IS '设备ID';
COMMENT ON COLUMN public.telemetry_current_datas."key" IS '数据标识符';
COMMENT ON COLUMN public.telemetry_current_datas.ts IS '上报时间';

-- public.event_datas definition

-- Drop table

-- DROP TABLE public.event_datas;

CREATE TABLE public.event_datas (
	id varchar(36) NOT NULL,
	device_id varchar(36) NOT NULL, -- 设备id（外键-关联删除）
	identify varchar(255) NOT NULL, -- 数据标识符
	ts timestamptz(6) NOT NULL, -- 上报时间
	"data" json NULL, -- 数据
	tenant_id varchar(36) NULL,
	CONSTRAINT event_datas_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.event_datas.device_id IS '设备id（外键-关联删除）';
COMMENT ON COLUMN public.event_datas.identify IS '数据标识符';
COMMENT ON COLUMN public.event_datas.ts IS '上报时间';
COMMENT ON COLUMN public.event_datas."data" IS '数据';


-- public.event_datas foreign keys

ALTER TABLE public.event_datas ADD CONSTRAINT event_datas_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE RESTRICT;

-- public.attribute_datas definition

-- Drop table

-- DROP TABLE public.attribute_datas;

CREATE TABLE public.attribute_datas (
	id varchar(36) NOT NULL,
	device_id varchar(36) NOT NULL, -- 设备id（外键-关联删除）
	"key" varchar(255) NOT NULL, -- 数据标识符
	ts timestamptz(6) NOT NULL, -- 上报时间
	bool_v bool NULL,
	number_v float8 NULL,
	string_v text NULL,
	tenant_id varchar(36) NULL,
	CONSTRAINT attribute_datas_device_id_key_key UNIQUE (device_id, key)
);

-- Column comments

COMMENT ON COLUMN public.attribute_datas.device_id IS '设备id（外键-关联删除）';
COMMENT ON COLUMN public.attribute_datas."key" IS '数据标识符';
COMMENT ON COLUMN public.attribute_datas.ts IS '上报时间';


-- public.attribute_datas foreign keys

ALTER TABLE public.attribute_datas ADD CONSTRAINT attribute_datas_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE RESTRICT;

-- public.attribute_set_logs definition

-- Drop table

-- DROP TABLE public.attribute_set_logs;

CREATE TABLE public.attribute_set_logs (
	id varchar(36) NOT NULL,
	device_id varchar(36) NOT NULL, -- 设备id（外键-关联删除）
	operation_type varchar(255) NULL, -- 操作类型1-手动操作 2-自动触发
	message_id varchar(36) NULL, -- 消息ID
	"data" text NULL, -- 发送内容
	rsp_data text NULL, -- 返回内容
	status varchar(2) NULL, -- 1-发送成功 2-失败
	error_message varchar(500) NULL, -- 错误信息
	created_at timestamptz(6) NOT NULL, -- 创建时间
	user_id varchar(36) NULL, -- 操作用户
	description varchar(255) NULL, -- 描述
	CONSTRAINT attribute_set_logs_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.attribute_set_logs.device_id IS '设备id（外键-关联删除）';
COMMENT ON COLUMN public.attribute_set_logs.operation_type IS '操作类型1-手动操作 2-自动触发';
COMMENT ON COLUMN public.attribute_set_logs.message_id IS '消息ID';
COMMENT ON COLUMN public.attribute_set_logs."data" IS '发送内容';
COMMENT ON COLUMN public.attribute_set_logs.rsp_data IS '返回内容';
COMMENT ON COLUMN public.attribute_set_logs.status IS '1-发送成功 2-失败';
COMMENT ON COLUMN public.attribute_set_logs.error_message IS '错误信息';
COMMENT ON COLUMN public.attribute_set_logs.created_at IS '创建时间';
COMMENT ON COLUMN public.attribute_set_logs.user_id IS '操作用户';
COMMENT ON COLUMN public.attribute_set_logs.description IS '描述';


-- public.attribute_set_logs foreign keys

ALTER TABLE public.attribute_set_logs ADD CONSTRAINT attribute_set_logs_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE CASCADE;

-- public.command_set_logs definition

-- Drop table

-- DROP TABLE public.command_set_logs;

CREATE TABLE public.command_set_logs (
	id varchar(36) NOT NULL,
	device_id varchar(36) NOT NULL, -- 设备id（外键-关联删除）
	operation_type varchar(255) NULL, -- 操作类型1-手动操作 2-自动触发
	message_id varchar(36) NULL, -- 消息ID
	"data" text NULL, -- 发送内容
	rsp_data text NULL, -- 返回内容
	status varchar(2) NULL, -- 1-发送成功 2-失败
	error_message varchar(500) NULL, -- 错误信息
	created_at timestamptz(6) NOT NULL, -- 创建时间
	user_id varchar(36) NULL, -- 操作用户
	description varchar(255) NULL, -- 描述
	identify varchar(255) NULL, -- 数据标识符
	CONSTRAINT command_set_logs_pkey PRIMARY KEY (id)
);
COMMENT ON TABLE public.command_set_logs IS '命令下发记录';

-- Column comments

COMMENT ON COLUMN public.command_set_logs.device_id IS '设备id（外键-关联删除）';
COMMENT ON COLUMN public.command_set_logs.operation_type IS '操作类型1-手动操作 2-自动触发';
COMMENT ON COLUMN public.command_set_logs.message_id IS '消息ID';
COMMENT ON COLUMN public.command_set_logs."data" IS '发送内容';
COMMENT ON COLUMN public.command_set_logs.rsp_data IS '返回内容';
COMMENT ON COLUMN public.command_set_logs.status IS '1-发送成功 2-失败';
COMMENT ON COLUMN public.command_set_logs.error_message IS '错误信息';
COMMENT ON COLUMN public.command_set_logs.created_at IS '创建时间';
COMMENT ON COLUMN public.command_set_logs.user_id IS '操作用户';
COMMENT ON COLUMN public.command_set_logs.description IS '描述';
COMMENT ON COLUMN public.command_set_logs.identify IS '数据标识符';


-- public.command_set_logs foreign keys

ALTER TABLE public.command_set_logs ADD CONSTRAINT command_set_logs_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE CASCADE;

