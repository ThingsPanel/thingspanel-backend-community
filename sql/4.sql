ALTER TABLE public.users ADD password_last_updated timestamptz(6) NULL;
INSERT INTO public.sys_function (id, "name", enable_flag, description, remark) VALUES('function_4', 'shared_account', 'enable', '共享账号', NULL);


ALTER TABLE "public"."action_info"
ALTER COLUMN "action_param_type" TYPE varchar(20) COLLATE "pg_catalog"."default";

ALTER TABLE public.users ADD last_visit_time timestamptz NULL;
COMMENT ON COLUMN public.users.last_visit_time IS '上次访问时间';

INSERT INTO public.service_plugins (id, "name", service_identifier, service_type, last_active_time, "version", create_at, update_at, description, service_config, remark) VALUES('d073ba1d-445a-a07f-430b-cf6d154bc5e8', 'MODBUS-RTU', 'MODBUS_RTU', 1, '2024-12-25 10:30:43.678', 'v1.0.1', '2024-12-25 09:05:12.019', '2024-12-25 09:05:36.696', '', '{"http_address":"172.20.0.10:503","device_type":2,"sub_topic_prefix":"plugin/modbus/","access_address":":502"}'::json, '');
INSERT INTO public.service_plugins (id, "name", service_identifier, service_type, last_active_time, "version", create_at, update_at, description, service_config, remark) VALUES('4bec425e-c7e3-476a-0303-ee8193ddf4ca', 'MODBUS-TCP	', 'MODBUS_TCP', 1, '2024-12-25 10:30:43.678', 'v1.0.1', '2024-12-25 09:03:25.401', '2024-12-25 09:10:02.208', '', '{"http_address":"172.20.0.10:503","device_type":2,"sub_topic_prefix":"plugin/modbus/","access_address":":502"}'::json, '');

UPDATE public.sys_dict_language SET dict_id='7162fb9e-e3be-95d4-9c96-f18d1f9ddfcd', language_code='zh_CN', "translation"='MQTT协议' WHERE id='001c3960-3067-536d-5c97-7645351a687c';
UPDATE public.sys_dict_language SET dict_id='0013fb9e-e3be-95d4-9c96-f18d1f9ddfcd', language_code='zh_CN', "translation"='MQTT协议(网关)' WHERE id='002c3960-3067-536d-5c97-7645351a687b';

INSERT INTO public.sys_dict_language (id, dict_id, language_code, "translation") VALUES('7162fb9e-e3be-95d4-9c96-f18d1f9ddfss', '7162fb9e-e3be-95d4-9c96-f18d1f9ddfcd', 'en_US', 'MQTT Protocol');
INSERT INTO public.sys_dict_language (id, dict_id, language_code, "translation") VALUES('7162fb9e-e3be-95d4-9c96-f18d1f9ddfff', '0013fb9e-e3be-95d4-9c96-f18d1f9ddfcd', 'en_US', 'MQTT Protocol(Gateway)');

ALTER TABLE public.scene_automation_log DROP CONSTRAINT scene_automation_log_scene_automation_id_fkey;
ALTER TABLE public.scene_automation_log ADD CONSTRAINT scene_automation_log_scene_automation_id_fkey FOREIGN KEY (scene_automation_id) REFERENCES public.scene_automations(id) ON DELETE CASCADE;

