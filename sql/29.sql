-- A22/A23: the two menus the new rule pages need.
--
-- The menu is database-driven (`GET /ui_elements/menu`), so a page that exists in
-- the frontend bundle is still unreachable until a row exists here. That is the
-- difference between "the route is registered" and "the customer can click it",
-- and it is why this file exists instead of relying on the generated router.
--
-- Both rows hang off the existing 自动化 parent (676e8f33…). No new parent menu is
-- created: the old "自动化 → 场景联动 / 场景管理" entries stay exactly as they are,
-- and the new list is a sibling, not a replacement.
--
-- param3 = '1' hides a row from the menu while keeping its route — that is how
-- the existing *-edit pages are registered, and the editor must not appear in the
-- sidebar.
--
-- Idempotent: re-running the upgrade must not duplicate rows.

INSERT INTO public.sys_ui_elements (
  id, parent_id, element_code, element_type, orders, param1, param2, param3,
  authority, description, created_at, remark, multilingual, route_path
)
SELECT
  'a1000000-0000-4000-8000-000000000001', '676e8f33-875a-0473-e9ca-c82fd09fef57',
  'automation_rule-manage', 3, 1143, '/automation/rule-manage', 'mdi:robot', 'self',
  '["TENANT_ADMIN","SYS_ADMIN"]'::json, '规则列表', NOW(), '',
  'route.automation_rule-manage', 'view.automation_rule-manage'
WHERE NOT EXISTS (
  SELECT 1 FROM public.sys_ui_elements WHERE element_code = 'automation_rule-manage'
);

INSERT INTO public.sys_ui_elements (
  id, parent_id, element_code, element_type, orders, param1, param2, param3,
  authority, description, created_at, remark, multilingual, route_path
)
SELECT
  'a1000000-0000-4000-8000-000000000002', '676e8f33-875a-0473-e9ca-c82fd09fef57',
  'automation_rule-edit', 3, 1144, '/automation/rule-edit', 'mdi:apps-box', '1',
  '["TENANT_ADMIN","SYS_ADMIN"]'::json, '规则编辑', NOW(), '',
  'route.automation_rule-edit', 'view.automation_rule-edit'
WHERE NOT EXISTS (
  SELECT 1 FROM public.sys_ui_elements WHERE element_code = 'automation_rule-edit'
);
