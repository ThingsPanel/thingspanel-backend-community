-- ✅2025/6/17 设备表增加上次离线时间字段
ALTER TABLE public.devices
ADD COLUMN last_offline_time timestamptz(6) NULL;
COMMENT ON COLUMN public.devices.last_offline_time IS '上次离线时间';

