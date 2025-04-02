-- ✅2025/3/20 v1.1.6

-- 为name字段创建lower索引
CREATE INDEX idx_lower_name ON public.devices (LOWER(name));

-- 为device_number字段创建lower索引
CREATE INDEX idx_lower_device_number ON public.devices (LOWER(device_number));