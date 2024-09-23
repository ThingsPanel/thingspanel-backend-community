INSERT INTO public.sys_function (id, "name", enable_flag, description, remark) VALUES('function_3', 'frontend_res', 'disable', '前端RSA加密', NULL);

ALTER TABLE "public"."casbin_rule"
ALTER COLUMN "v0" TYPE varchar(200) COLLATE "pg_catalog"."default",
  ALTER COLUMN "v1" TYPE varchar(200) COLLATE "pg_catalog"."default",
  ALTER COLUMN "v2" TYPE varchar(200) COLLATE "pg_catalog"."default",
  ALTER COLUMN "v3" TYPE varchar(200) COLLATE "pg_catalog"."default",
  ALTER COLUMN "v4" TYPE varchar(200) COLLATE "pg_catalog"."default",
  ALTER COLUMN "v5" TYPE varchar(200) COLLATE "pg_catalog"."default";


  -- public.device_model_custom_control definition

-- Drop table

-- DROP TABLE public.device_model_custom_control;

CREATE TABLE public.device_model_custom_control (
	id varchar(36) NOT NULL, -- id
	device_template_id varchar(36) NOT NULL, -- 设备模版ID
	"name" varchar(255) NOT NULL, -- 名称
	control_type varchar NOT NULL, -- 1.控制类型2.telemetry-遥测3.attributes-属性
	description varchar(500) NULL, -- 描述
	"content" text NULL, -- 指令内容
	enable_status varchar(10) NOT NULL, -- 启用状态enable-启用disable-禁用
	created_at timestamp NOT NULL, -- 创建时间
	updated_at timestamp NOT NULL, -- 更新时间
	remark varchar(255) NULL, -- 备注
	tenant_id varchar(36) NOT NULL,
	CONSTRAINT device_model_custom_control_pk PRIMARY KEY (id),
	CONSTRAINT device_model_custom_control_device_templates_fk FOREIGN KEY (device_template_id) REFERENCES public.device_templates(id) ON DELETE CASCADE

);

-- Column comments

COMMENT ON COLUMN public.device_model_custom_control.id IS 'id';
COMMENT ON COLUMN public.device_model_custom_control.device_template_id IS '设备模版ID';
COMMENT ON COLUMN public.device_model_custom_control."name" IS '名称';
COMMENT ON COLUMN public.device_model_custom_control.control_type IS '1.控制类型2.telemetry-遥测3.attributes-属性';
COMMENT ON COLUMN public.device_model_custom_control.description IS '描述';
COMMENT ON COLUMN public.device_model_custom_control."content" IS '指令内容';
COMMENT ON COLUMN public.device_model_custom_control.enable_status IS '启用状态enable-启用disable-禁用';
COMMENT ON COLUMN public.device_model_custom_control.created_at IS '创建时间';
COMMENT ON COLUMN public.device_model_custom_control.updated_at IS '更新时间';
COMMENT ON COLUMN public.device_model_custom_control.remark IS '备注';

-- 2024/08/12

-- public.expected_datas definition

-- Drop table

-- DROP TABLE public.expected_datas;

CREATE TABLE public.expected_datas (
	id varchar(36) NOT NULL, -- 指令唯一标识符(UUID)
	device_id varchar(36) NOT NULL, -- 目标设备ID
	send_type varchar(50) NOT NULL, -- 指令类型(e.g., telemetry, attribute, command)
	payload jsonb NOT NULL, -- 指令内容(具体指令参数)
	created_at timestamptz(6) NOT NULL, -- 指令生成时间
	send_time timestamptz(6) NULL, -- 指令实际发送时间(如果已发送)
	status varchar(50) NOT NULL DEFAULT 'pending'::character varying, -- 指令状态(pending, sent, expired)，默认待发送
	message text NULL, -- 状态附加信息(如发送失败的原因)
	expiry_time timestamptz(6) NULL, -- 指令过期时间(可选)
	"label" varchar(100) NULL, -- 指令标签(可选)
	tenant_id varchar(36) NOT NULL, -- 租户ID（用于多租户系统）
	CONSTRAINT expected_datas_pkey PRIMARY KEY (id)
);

-- Column comments

COMMENT ON COLUMN public.expected_datas.id IS '指令唯一标识符(UUID)';
COMMENT ON COLUMN public.expected_datas.device_id IS '目标设备ID';
COMMENT ON COLUMN public.expected_datas.send_type IS '指令类型(e.g., telemetry, attribute, command)';
COMMENT ON COLUMN public.expected_datas.payload IS '指令内容(具体指令参数)';
COMMENT ON COLUMN public.expected_datas.created_at IS '指令生成时间';
COMMENT ON COLUMN public.expected_datas.send_time IS '指令实际发送时间(如果已发送)';
COMMENT ON COLUMN public.expected_datas.status IS '指令状态(pending, sent, expired)，默认待发送';
COMMENT ON COLUMN public.expected_datas.message IS '状态附加信息(如发送失败的原因)';
COMMENT ON COLUMN public.expected_datas.expiry_time IS '指令过期时间(可选)';
COMMENT ON COLUMN public.expected_datas."label" IS '指令标签(可选)';
COMMENT ON COLUMN public.expected_datas.tenant_id IS '租户ID（用于多租户系统）';


-- public.expected_datas foreign keys

ALTER TABLE public.expected_datas ADD CONSTRAINT expected_datas_devices_fk FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE public.users ADD password_last_updated timestamptz(6) NULL;
