-- Product / OTA menus. Frontend routes already exist; stock sql/1-20 never inserted them.
INSERT INTO public.sys_ui_elements
    (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path)
VALUES
    ('f46c5c1c-f528-4001-860f-b90c12d364e3', '0', 'product', 1, 111, '/product', 'mdi:cube-outline', '0', '["SYS_ADMIN","TENANT_ADMIN"]'::json, '产品管理', NOW(), '', 'route.product', 'layout.base'),
    ('54cb5dc5-9d15-489d-8d83-e46a44d967fd', 'f46c5c1c-f528-4001-860f-b90c12d364e3', 'product_list', 3, 1, '/product/list', 'mdi:format-list-bulleted', '0', '["SYS_ADMIN","TENANT_ADMIN"]'::json, '产品列表', NOW(), '', 'route.product_list', ''),
    ('869d0656-6a8c-4bc4-a9ae-96c24a1efd68', 'f46c5c1c-f528-4001-860f-b90c12d364e3', 'product_update-package', 3, 2, '/product/update-package', 'mdi:package-variant', '0', '["SYS_ADMIN","TENANT_ADMIN"]'::json, '升级包管理', NOW(), '', 'route.product_update-package', ''),
    ('3377e6fe-f3ac-4ae1-b71a-1cb96f7f3e86', 'f46c5c1c-f528-4001-860f-b90c12d364e3', 'product_update-ota', 3, 3, '/product/update-ota', 'mdi:rocket-launch-outline', '0', '["SYS_ADMIN","TENANT_ADMIN"]'::json, 'OTA升级', NOW(), '', 'route.product_update-ota', '')
ON CONFLICT (id) DO NOTHING;
