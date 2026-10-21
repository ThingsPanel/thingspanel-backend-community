-- Keep the existing Data Services group for the Rule Engine.
-- The previous candidate version temporarily renamed this group; restore its
-- original label and scope so the new feature has its own navigation group.
UPDATE public.sys_ui_elements
SET description = '数据服务',
    multilingual = 'route.data-service',
    authority = '["SYS_ADMIN"]'::json
WHERE id = '9a11b3e4-9982-a0f0-996c-a9be6e738947'
  AND element_code = 'data-service';

-- Create a separate dynamic root menu for forwarding.
INSERT INTO public.sys_ui_elements
    (id, parent_id, element_code, element_type, orders, param1, param2, param3,
     authority, description, created_at, remark, multilingual, route_path)
VALUES
    ('db86f7c1-1055-4d63-bff8-ae868a1121a8',
     'db86f7c1-1055-4d63-bff8-ae868a1121a8',
     'data-forwarding', 1, 23,
     '/data-forwarding', 'mdi:transit-connection-variant', 'self',
     '["SYS_ADMIN","TENANT_ADMIN"]'::json,
     '数据转发', now(), '', 'route.data-forwarding', 'layout.base')
ON CONFLICT (id) DO UPDATE SET
    parent_id = excluded.parent_id,
    element_code = excluded.element_code,
    element_type = excluded.element_type,
    orders = excluded.orders,
    param1 = excluded.param1,
    param2 = excluded.param2,
    param3 = excluded.param3,
    authority = excluded.authority,
    description = excluded.description,
    multilingual = excluded.multilingual,
    route_path = excluded.route_path;

-- Migrate a route row created by the first candidate menu seed, if present.
UPDATE public.sys_ui_elements
SET parent_id = 'db86f7c1-1055-4d63-bff8-ae868a1121a8',
    element_code = 'data-forwarding_northbound',
    param1 = '/data-forwarding/northbound',
    param2 = 'mdi:transit-connection-variant',
    authority = '["SYS_ADMIN","TENANT_ADMIN"]'::json,
    description = '北向数据转发',
    multilingual = 'route.data-forwarding_northbound',
    route_path = 'view.data-forwarding_northbound'
WHERE element_code = 'data-service_northbound';

-- Add the dynamic child route for existing installations.
INSERT INTO public.sys_ui_elements
    (id, parent_id, element_code, element_type, orders, param1, param2, param3,
     authority, description, created_at, remark, multilingual, route_path)
SELECT
    'cf3eae79-dc26-42cc-a769-e92c664c90f1',
    'db86f7c1-1055-4d63-bff8-ae868a1121a8',
    'data-forwarding_northbound', 3, 1,
    '/data-forwarding/northbound', 'mdi:transit-connection-variant', '1',
    '["SYS_ADMIN","TENANT_ADMIN"]'::json,
    '北向数据转发', now(), '', 'route.data-forwarding_northbound',
    'view.data-forwarding_northbound'
WHERE NOT EXISTS (
    SELECT 1 FROM public.sys_ui_elements
    WHERE element_code = 'data-forwarding_northbound'
);
