CREATE TABLE service_plugins (
     id VARCHAR(36) PRIMARY KEY,
     name VARCHAR(255) NOT NULL,
     service_identifier VARCHAR(100) NOT NULL,
     service_type INT NOT NULL CHECK (service_type IN (1, 2)),
     last_active_time TIMESTAMP,
     version VARCHAR(100),
     create_at TIMESTAMP NOT NULL,
     update_at TIMESTAMP NOT NULL,
     description VARCHAR(255),
     service_config JSON,
     remark VARCHAR(255)
);

ALTER TABLE service_plugins
    ADD CONSTRAINT unique_service_identifier UNIQUE (service_identifier);

ALTER TABLE service_plugins
    ADD CONSTRAINT unique_name UNIQUE (name);

ALTER TABLE "public"."service_plugins"
ALTER COLUMN "create_at" TYPE timestamptz USING "create_at"::timestamptz,
  ALTER COLUMN "update_at" TYPE timestamptz USING "update_at"::timestamptz;


CREATE TABLE service_access (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    service_plugin_id VARCHAR(36) NOT NULL,
    voucher VARCHAR(999) NOT NULL,
    description VARCHAR(255),
    service_access_config JSON,
    remark VARCHAR(255),
    CONSTRAINT fk_service_plugin
        FOREIGN KEY (service_plugin_id)
            REFERENCES service_plugins (id)
            ON DELETE RESTRICT
);

ALTER TABLE "public"."service_access"
    ADD COLUMN "create_at" timestamptz,
  ADD COLUMN "update_at" timestamptz,
  ADD COLUMN "tenant_id" varchar(36) NOT NULL;

ALTER TABLE "public"."service_access"
    ALTER COLUMN "create_at" SET NOT NULL,
ALTER COLUMN "update_at" SET NOT NULL;

ALTER TABLE public.devices ADD service_access_id varchar(36) NULL;
ALTER TABLE public.devices ADD CONSTRAINT devices_service_access_fk FOREIGN KEY (service_access_id) REFERENCES public.service_access(id) ON DELETE RESTRICT;

COMMENT ON TABLE service_plugins IS '服务管理';

COMMENT ON COLUMN service_plugins.id IS '服务ID';
COMMENT ON COLUMN service_plugins.name IS '服务名称';
COMMENT ON COLUMN service_plugins.service_identifier IS '服务标识符';
COMMENT ON COLUMN service_plugins.service_type IS '服务类型: 1-接入协议, 2-接入服务';
COMMENT ON COLUMN service_plugins.last_active_time IS '服务最后活跃时间';
COMMENT ON COLUMN service_plugins.version IS '版本号';
COMMENT ON COLUMN service_plugins.create_at IS '创建时间';
COMMENT ON COLUMN service_plugins.update_at IS '更新时间';
COMMENT ON COLUMN service_plugins.description IS '描述';
COMMENT ON COLUMN service_plugins.service_config IS '服务配置';
COMMENT ON COLUMN service_plugins.remark IS '备注';

COMMENT ON TABLE service_access IS '服务接入(租户端)';

COMMENT ON COLUMN service_access.id IS '接入ID';
COMMENT ON COLUMN service_access.name IS '名称';
COMMENT ON COLUMN service_access.service_plugin_id IS '服务ID';
COMMENT ON COLUMN service_access.voucher IS '凭证';
COMMENT ON COLUMN service_access.description IS '描述';
COMMENT ON COLUMN service_access.service_access_config IS '服务配置';
COMMENT ON COLUMN service_access.create_at IS '创建时间';
COMMENT ON COLUMN service_access.update_at IS '更新时间';
COMMENT ON COLUMN service_access.tenant_id IS '租户ID';
COMMENT ON COLUMN service_access.remark IS '备注';

COMMENT ON COLUMN service_plugins.service_config IS '服务配置: 接入协议和接入服务的配置';


ALTER TABLE "public"."scene_action_info"
ALTER COLUMN "action_param" TYPE varchar(50) COLLATE "pg_catalog"."default";

ALTER TABLE public.telemetry_set_logs DROP CONSTRAINT telemetry_set_logs_device_id_fkey;
ALTER TABLE public.telemetry_set_logs ADD CONSTRAINT telemetry_set_logs_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE CASCADE;
ALTER TABLE public.attribute_set_logs DROP CONSTRAINT attribute_set_logs_device_id_fkey;
ALTER TABLE public.attribute_set_logs ADD CONSTRAINT attribute_set_logs_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE CASCADE;
ALTER TABLE public.command_set_logs DROP CONSTRAINT command_set_logs_device_id_fkey;
ALTER TABLE public.command_set_logs ADD CONSTRAINT command_set_logs_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE CASCADE;

ALTER TABLE public.service_plugins ALTER COLUMN last_active_time TYPE timestamptz USING last_active_time::timestamptz;
DELETE FROM public.sys_ui_elements WHERE id='367dbdb9-f28b-7a49-b8cd-23a915015093';

INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('075d9f19-5618-bb9b-6ccd-f382bfd3292b', '5373a6a2-1861-af35-eb4c-adfd5ca55ecd', 'device_service-access', 3, 1129, '/device/service-access', 'mdi:ab-testing', '0', '["TENANT_ADMIN"]'::json, '服务接入点管理', '2024-07-01 21:52:09.402', '', 'route.device_service_access', '');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('f960c45c-6d5b-e67a-c4ff-1f0e869c1625', '5373a6a2-1861-af35-eb4c-adfd5ca55ecd', 'device_service-details', 3, 1130, '/device/service-details', 'ph:align-bottom', '1', '["TENANT_ADMIN"]'::json, '服务详情', '2024-07-01 23:16:56.668', '', 'route.device_service_details', '');
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path) VALUES('29a684f9-c2bb-1a6f-6045-314944bef580', 'a2c53126-029f-7138-4d7a-f45491f396da', 'plug_in', 3, 32, '/apply/plugin', 'mdi:emoticon', '0', '["SYS_ADMIN"]'::json, '插件管理', '2024-06-29 01:04:51.301', '', 'route.apply_in', '');