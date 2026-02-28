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

-- ✅2026/2/29 增加HTTP插件
INSERT INTO public.service_plugins (id, "name", service_identifier, service_type, last_active_time, "version", create_at, update_at, description, service_config, remark) VALUES ('a1c2d3e4-f5a6-4b7c-8d9e-0f1a2b3c45l5', 'HTTP', 'HTTP', 1, NOW(), 'v1.0.0', NOW(), NOW(), '官方标准 HTTP 协议接入组件', '{"http_address":"172.20.0.11:19090","device_type":1,"sub_topic_prefix":"plugin/http/","access_address":":19091"}'::json, '');

INSERT INTO public.sys_ui_elements
(id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path)
VALUES('a0848997-3a3c-7ffa-1ca2-0380c564d5d0', '95e2a961-382b-f4a6-87b3-1898123c95bc', 'visualization_thingsvis', 3, 1, '/visualization/thingsvis', 'icon-park-outline:analysis', '0', '["SYS_ADMIN","TENANT_ADMIN"]'::json, '新看板', '2026-02-06 16:40:03.021', '', 'route.visualization-thingsvis', '');
INSERT INTO public.sys_ui_elements
(id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path)
VALUES('3733d118-fe8a-d02e-3f89-85a754586f75', '95e2a961-382b-f4a6-87b3-1898123c95bc', 'visualization_thingsvis-dashboards', 3, 1, '/visualization/thingsvis-dashboards', 'icon-park-outline:workbench', '1', '["TENANT_ADMIN","SYS_ADMIN"]'::json, 'visualization_thingsvis-dashboards', '2026-02-06 18:43:38.941', '', 'route.visualization-thingsvis-dashboards', '');
INSERT INTO public.sys_ui_elements
(id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path)
VALUES('e84d450c-ebc8-eae2-da17-f2f1269f6732', '95e2a961-382b-f4a6-87b3-1898123c95bc', 'visualization_thingsvis-editor', 3, 2, '/visualization/thingsvis-editor', 'icon-park-outline:workbench', '1', '["SYS_ADMIN","TENANT_ADMIN"]'::json, 'thingsvis-editor', '2026-02-06 18:46:31.554', '', 'route.visualization-thingsvis-editor', '');



