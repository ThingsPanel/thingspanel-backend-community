ALTER TABLE public.users ADD password_last_updated timestamptz(6) NULL;
INSERT INTO public.sys_function (id, "name", enable_flag, description, remark) VALUES('function_4', 'shared_account', 'enable', '共享账号', NULL);

ALTER TABLE public.users ADD last_visit_time timestamptz NULL;
COMMENT ON COLUMN public.users.last_visit_time IS '上次访问时间';

INSERT INTO public.service_plugins (id, "name", service_identifier, service_type, last_active_time, "version", create_at, update_at, description, service_config, remark) VALUES('d073ba1d-445a-a07f-430b-cf6d154bc5e8', 'MODBUS-RTU', 'MODBUS_RTU', 1, '2024-12-25 10:30:43.678', 'v1.0.1', '2024-12-25 09:05:12.019', '2024-12-25 09:05:36.696', '', '{"http_address":"172.20.0.10:503","device_type":2,"sub_topic_prefix":"plugin/modbus/","access_address":":502"}'::json, '');
INSERT INTO public.service_plugins (id, "name", service_identifier, service_type, last_active_time, "version", create_at, update_at, description, service_config, remark) VALUES('4bec425e-c7e3-476a-0303-ee8193ddf4ca', 'MODBUS-TCP	', 'MODBUS_TCP', 1, '2024-12-25 10:30:43.678', 'v1.0.1', '2024-12-25 09:03:25.401', '2024-12-25 09:10:02.208', '', '{"http_address":"172.20.0.10:503","device_type":2,"sub_topic_prefix":"plugin/modbus/","access_address":":502"}'::json, '');
