-- Plugin install menu entry for tenant administrators (PLUGIN-UI): "Device access > Plugin install".
-- Runtime identities can only be issued by a tenant administrator, so this page is a tenant menu, not part of the
-- system-administrator "plugin management" menu. Idempotent.
INSERT INTO public.sys_ui_elements (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path)
SELECT 'b7c1f0a2-3d4e-4f56-8a90-1c2d3e4f5a6b', '5373a6a2-1861-af35-eb4c-adfd5ca55ecd', 'device_plugin-install', 3, 1131, '/device/plugin-install', 'mdi:puzzle-outline', '0',
       '["TENANT_ADMIN"]', '插件安装', NOW(), '', 'route.device_plugin-install', ''
WHERE NOT EXISTS (SELECT 1 FROM public.sys_ui_elements WHERE element_code = 'device_plugin-install');
