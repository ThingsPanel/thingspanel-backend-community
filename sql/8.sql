-- ✅2025/5/6 新增模板秘钥和自动注册
ALTER TABLE public.device_configs
ADD COLUMN template_secret varchar(255) NULL,
ADD COLUMN auto_register int2 DEFAULT 0 NOT NULL;

-- ✅2025/5/27 设备模板增加图片地址字段
ALTER TABLE public.device_configs
ADD COLUMN image_url varchar(255) NULL;
