-- 测试环境初始化：产品类型、A100 设备配置和 A100 产品。
-- 仅补齐缺失数据，不删除或覆盖已有业务数据。

INSERT INTO public.sys_dict (id, dict_code, dict_value, created_at, remark)
VALUES
    ('a1000001-0000-4000-8000-000000000001', 'PRODUCT_TYPE', 'direct', NOW(), '综合设备类'),
    ('a1000001-0000-4000-8000-000000000002', 'PRODUCT_TYPE', 'sensor', NOW(), '传感器类'),
    ('a1000001-0000-4000-8000-000000000003', 'PRODUCT_TYPE', 'gateway', NOW(), '网关类')
ON CONFLICT (dict_code, dict_value) DO NOTHING;

INSERT INTO public.sys_dict_language (id, dict_id, language_code, "translation")
VALUES
    ('a1000002-0000-4000-8000-000000000001', 'a1000001-0000-4000-8000-000000000001', 'en_US', 'Integrated Device'),
    ('a1000002-0000-4000-8000-000000000002', 'a1000001-0000-4000-8000-000000000001', 'zh_CN', '综合设备类'),
    ('a1000002-0000-4000-8000-000000000003', 'a1000001-0000-4000-8000-000000000002', 'en_US', 'Sensor'),
    ('a1000002-0000-4000-8000-000000000004', 'a1000001-0000-4000-8000-000000000002', 'zh_CN', '传感器类'),
    ('a1000002-0000-4000-8000-000000000005', 'a1000001-0000-4000-8000-000000000003', 'en_US', 'Gateway'),
    ('a1000002-0000-4000-8000-000000000006', 'a1000001-0000-4000-8000-000000000003', 'zh_CN', '网关类')
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
