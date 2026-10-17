-- Super admin on yomitest was missing tenant operational menus
-- (设备接入 / 自动化 / 告警). Local tenant view already has them.
WITH roots AS (
    SELECT id
    FROM public.sys_ui_elements
    WHERE element_code IN ('device', 'automation', 'alarm')
)
UPDATE public.sys_ui_elements AS e
SET authority = '["SYS_ADMIN","TENANT_ADMIN"]'::json
WHERE e.authority::text NOT LIKE '%SYS_ADMIN%'
  AND (
    e.element_code IN (
        'device',
        'automation',
        'alarm',
        'system-management-user_system-log',
        'management_api'
    )
    OR e.parent_id IN (SELECT id FROM roots)
  );
