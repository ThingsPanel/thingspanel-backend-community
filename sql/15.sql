-- ✅ 2026/03/02 Task-02: 设备模板增加品牌和型号字段
ALTER TABLE public.device_templates ADD COLUMN IF NOT EXISTS brand VARCHAR(255) DEFAULT '';
ALTER TABLE public.device_templates ADD COLUMN IF NOT EXISTS model_number VARCHAR(255) DEFAULT '';

COMMENT ON COLUMN public.device_templates.brand IS '品牌';
COMMENT ON COLUMN public.device_templates.model_number IS '型号';
