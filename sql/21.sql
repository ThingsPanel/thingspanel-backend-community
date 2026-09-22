-- Rename the device third-party service entry menu to integration.
-- Keep the same id so existing tenant permissions remain attached to the menu.
UPDATE public.sys_ui_elements
SET element_code = 'device_integration',
    param1 = '/device/integration',
    description = '三方集成',
    multilingual = 'route.device_integration'
WHERE id = '075d9f19-5618-bb9b-6ccd-f382bfd3292b';
