-- ✅2025/5/6 新增模板秘钥和自动注册
ALTER TABLE public.device_configs
ADD COLUMN template_secret varchar(255) NULL,
ADD COLUMN auto_register int2 DEFAULT 0 NOT NULL;
