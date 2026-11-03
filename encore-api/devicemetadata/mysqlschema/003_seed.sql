-- Built-in metadata keys from migrations/0002_seed_meta_item_keys.up.sql.
-- Stable IDs plus the unique (tenant_id, key) index make this safe to apply more than once.
INSERT INTO `meta_item_keys`
    (`id`, `tenant_id`, `key`, `label_i18n`, `scope`, `value_schema`, `is_builtin`, `description_i18n`)
VALUES
    ('00000000-0000-4000-8000-000000000001', NULL, 'chart_hint', '{"default":"Chart Hint"}', 'UI', '{"type":"string","enum":["line","bar","gauge","switch","text"]}', 1, '{"default":"Recommended widget chart style"}'),
    ('00000000-0000-4000-8000-000000000002', NULL, 'widget_hint', '{"default":"Widget Hint"}', 'UI', '{"type":"string","enum":["slider","select","toggle","input","number_input"]}', 1, '{"default":"Recommended control widget"}'),
    ('00000000-0000-4000-8000-000000000003', NULL, 'unit', '{"default":"Unit"}', 'UI', '{"type":"string"}', 1, '{"default":"Engineering unit"}'),
    ('00000000-0000-4000-8000-000000000004', NULL, 'range', '{"default":"Range"}', 'STORAGE', '{"type":"object","properties":{"min":{"type":"number"},"max":{"type":"number"},"step":{"type":"number"}}}', 1, '{"default":"Value range metadata"}'),
    ('00000000-0000-4000-8000-000000000005', NULL, 'icon', '{"default":"Icon"}', 'UI', '{"type":"string"}', 1, '{"default":"Icon identifier"}'),
    ('00000000-0000-4000-8000-000000000006', NULL, 'store_policy', '{"default":"Store Policy"}', 'STORAGE', '{"type":"string","enum":["history","latest_only"]}', 1, '{"default":"Storage strategy hint"}')
ON DUPLICATE KEY UPDATE
    `label_i18n` = VALUES(`label_i18n`),
    `scope` = VALUES(`scope`),
    `value_schema` = VALUES(`value_schema`),
    `is_builtin` = VALUES(`is_builtin`),
    `description_i18n` = VALUES(`description_i18n`);
