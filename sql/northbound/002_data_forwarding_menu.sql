-- Show northbound forwarding in the dynamic production menu.
-- Keep the existing menu id so saved roles and bookmarks keep their parent.
UPDATE public.sys_ui_elements
SET description = '数据转发',
    multilingual = 'route.data-service',
    authority = '["SYS_ADMIN","TENANT_ADMIN"]'::json
WHERE id = '9a11b3e4-9982-a0f0-996c-a9be6e738947'
  AND element_code = 'data-service';

-- The frontend route exists locally, but production builds its menu from this table.
-- Insert the route under the existing menu so the same page is reachable after login.
INSERT INTO public.sys_ui_elements
    (id, parent_id, element_code, element_type, orders, param1, param2, param3,
     authority, description, created_at, remark, multilingual, route_path)
SELECT
    'cf3eae79-dc26-42cc-a769-e92c664c90f1',
    '9a11b3e4-9982-a0f0-996c-a9be6e738947',
    'data-service_northbound', 3, 22,
    '/data-service/northbound', 'mdi:transit-connection-variant', '1',
    '["SYS_ADMIN","TENANT_ADMIN"]'::json,
    '数据转发', now(), '', 'route.data-service_northbound',
    'view.data-service_northbound'
WHERE EXISTS (
    SELECT 1 FROM public.sys_ui_elements
    WHERE id = '9a11b3e4-9982-a0f0-996c-a9be6e738947'
      AND element_code = 'data-service'
)
AND NOT EXISTS (
    SELECT 1 FROM public.sys_ui_elements
    WHERE element_code = 'data-service_northbound'
);
