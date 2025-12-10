-- ✅2025/12/10 设备主题转换表添加数据标识符字段
ALTER TABLE public.device_topic_mappings ADD data_identifier varchar(500) NULL;
