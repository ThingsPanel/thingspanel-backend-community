INSERT INTO meta_item_keys (id, tenant_id, key, label_i18n, scope, value_schema, is_builtin, description_i18n)
VALUES
    (gen_random_uuid(), NULL, 'chart_hint', '{"default":"Chart Hint"}', 'UI', '{"type":"string","enum":["line","bar","gauge","switch","text"]}', true, '{"default":"Recommended widget chart style"}'),
    (gen_random_uuid(), NULL, 'widget_hint', '{"default":"Widget Hint"}', 'UI', '{"type":"string","enum":["slider","select","toggle","input","number_input"]}', true, '{"default":"Recommended control widget"}'),
    (gen_random_uuid(), NULL, 'unit', '{"default":"Unit"}', 'UI', '{"type":"string"}', true, '{"default":"Engineering unit"}'),
    (gen_random_uuid(), NULL, 'range', '{"default":"Range"}', 'STORAGE', '{"type":"object","properties":{"min":{"type":"number"},"max":{"type":"number"},"step":{"type":"number"}}}', true, '{"default":"Value range metadata"}'),
    (gen_random_uuid(), NULL, 'icon', '{"default":"Icon"}', 'UI', '{"type":"string"}', true, '{"default":"Icon identifier"}'),
    (gen_random_uuid(), NULL, 'store_policy', '{"default":"Store Policy"}', 'STORAGE', '{"type":"string","enum":["history","latest_only"]}', true, '{"default":"Storage strategy hint"}');
