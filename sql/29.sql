-- Existing installations: register only the hidden rule editor.
-- The standalone rule list was removed from the frontend.
DO $$
DECLARE automation_parent varchar;
BEGIN
  SELECT id INTO automation_parent FROM public.sys_ui_elements
    WHERE element_code = 'automation' LIMIT 1;
  IF automation_parent IS NULL THEN
    RAISE EXCEPTION 'automation menu parent is missing';
  END IF;
  INSERT INTO public.sys_ui_elements
    (id, parent_id, element_code, element_type, orders, param1, param2, param3,
     authority, description, created_at, remark, multilingual, route_path)
  SELECT '4b1f2c7e-8a3d-4e15-9c62-0d7a5b3f8e21', automation_parent,
    'automation_rule-edit', 3, 1144, '/automation/rule-edit', '', '1',
    '["TENANT_ADMIN","SYS_ADMIN"]'::json, '规则编辑', now(), '',
    'route.automation_rule-edit', 'view.automation_rule-edit'
  WHERE NOT EXISTS (SELECT 1 FROM public.sys_ui_elements WHERE element_code='automation_rule-edit');
END $$;
