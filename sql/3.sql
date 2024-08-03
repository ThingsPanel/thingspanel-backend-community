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