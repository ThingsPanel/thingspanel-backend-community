-- Create sys_config table for persistent system configurations
CREATE TABLE public.sys_config (
	config_key varchar(255) NOT NULL,
	config_value text NOT NULL,
	remark varchar(255) NULL,
	created_at timestamptz(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at timestamptz(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
	CONSTRAINT sys_config_pkey PRIMARY KEY (config_key)
);

COMMENT ON TABLE public.sys_config IS '系统配置表';
COMMENT ON COLUMN public.sys_config.config_key IS '配置键';
COMMENT ON COLUMN public.sys_config.config_value IS '配置值';

-- Initial instance_id will be inserted by the application logic if not exists
