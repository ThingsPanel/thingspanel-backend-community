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