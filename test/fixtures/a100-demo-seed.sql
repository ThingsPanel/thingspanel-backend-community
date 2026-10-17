-- 仅供测试环境手动初始化，不属于自动版本迁移。
-- 已存在相同产品类型时，从字典表读取实际 ID，避免固定 ID 与已有数据冲突。
INSERT INTO public.sys_dict (id, dict_code, dict_value, created_at, remark)
VALUES
    ('a1000001-0000-4000-8000-000000000001', 'PRODUCT_TYPE', 'direct', NOW(), '综合设备类'),
    ('a1000001-0000-4000-8000-000000000002', 'PRODUCT_TYPE', 'sensor', NOW(), '传感器类'),
    ('a1000001-0000-4000-8000-000000000003', 'PRODUCT_TYPE', 'gateway', NOW(), '网关类')
ON CONFLICT (dict_code, dict_value) DO NOTHING;

WITH language_seed(id, dict_code, dict_value, language_code, translation) AS (
    VALUES
        ('a1000002-0000-4000-8000-000000000001', 'PRODUCT_TYPE', 'direct', 'en_US', 'Integrated Device'),
        ('a1000002-0000-4000-8000-000000000002', 'PRODUCT_TYPE', 'direct', 'zh_CN', '综合设备类'),
        ('a1000002-0000-4000-8000-000000000003', 'PRODUCT_TYPE', 'sensor', 'en_US', 'Sensor'),
        ('a1000002-0000-4000-8000-000000000004', 'PRODUCT_TYPE', 'sensor', 'zh_CN', '传感器类'),
        ('a1000002-0000-4000-8000-000000000005', 'PRODUCT_TYPE', 'gateway', 'en_US', 'Gateway'),
        ('a1000002-0000-4000-8000-000000000006', 'PRODUCT_TYPE', 'gateway', 'zh_CN', '网关类')
)
INSERT INTO public.sys_dict_language (id, dict_id, language_code, "translation")
SELECT seed.id, dictionary.id, seed.language_code, seed.translation
FROM language_seed AS seed
JOIN public.sys_dict AS dictionary
  ON dictionary.dict_code = seed.dict_code
 AND dictionary.dict_value = seed.dict_value
ON CONFLICT (dict_id, language_code) DO NOTHING;

INSERT INTO public.device_configs
    (id, name, device_template_id, device_type, protocol_type, voucher_type,
     protocol_config, device_conn_type, additional_info, description, tenant_id,
     created_at, updated_at, remark, other_config, template_secret, auto_register, image_url)
VALUES
    ('69b559aa-5c61-bbf3-5c4a-2984239767ff', 'A100 电子吧唧模板 01', NULL, '1', 'MQTT', 'BASIC',
     '{}'::json, '', '{}'::json, '', 'd616bcbb', NOW(), NOW(), '', '{}'::json,
     'e0bd0c36-cc75-13cd-ed06-2b86d5b67e32', 0, '')
ON CONFLICT (id) DO NOTHING;

INSERT INTO public.products
    (id, name, description, product_type, product_key, product_model, image_url,
     created_at, remark, additional_info, tenant_id, device_config_id)
VALUES
    ('611d0948-5fa3-0632-e048-4033eca7eade', 'A100-demo', 'A100 设备', 'direct',
     '0096e44d-2666-9536-9135-274394c65823', 'A100', '', NOW(), '', '{}'::json,
     'd616bcbb', NULL)
ON CONFLICT (id) DO NOTHING;
