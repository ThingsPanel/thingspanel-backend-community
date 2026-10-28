-- A realistic slice of an old installation, written to the real schema so the
-- upgrade command is exercised against the tables it will actually meet.
--
-- Tenant "t-e2e". Three devices, one device config, three scenes, three 联动, two
-- alarm configurations, two alarms still open.
--
-- What each row is here to prove:
--   sc-1  ordered actions: a device write, then "raise alarm ac-1"
--   sc-2  activates sc-1, so the activation graph is two levels deep
--   sc-3  points at a device that no longer exists -> refused, with reason
--   au-1  two AND conditions, string operators -> copied unchanged
--   au-2  device-config trigger and action on the same config -> converts
--   au-3  disabled, and a time-range trigger -> off stays off, time range refused
--   ac-1  enabled alarm configuration with no condition -> event-only policy
--   ac-2  disabled alarm configuration -> skipped, not re-enabled
--   al-1  open alarm whose content carries the device id -> adopted
--   al-2  open alarm with no device id -> refused, not invented
--   al-3  already processed -> not touched

BEGIN;

INSERT INTO device_configs (id, name, device_type, tenant_id, created_at, updated_at)
VALUES ('cfg-1', '温控器', '1', 't-e2e', now(), now());

INSERT INTO devices (id, voucher, tenant_id, is_enabled, activate_flag, device_number, is_online, device_config_id)
VALUES ('dev-1', 'v1', 't-e2e', 'Y', '1', '1', 1, 'cfg-1'),
       ('dev-2', 'v2', 't-e2e', 'Y', '1', '2', 1, 'cfg-1'),
       ('dev-3', 'v3', 't-e2e', 'Y', '1', '3', 0, 'cfg-1');

-- 场景 1: 一个设备状态写入，然后触发告警。动作有先后。
INSERT INTO scene_info (id, name, description, tenant_id, creator, created_at)
VALUES ('sc-1', '着火处置', '先开阀再报警', 't-e2e', 'u1', now() - interval '3 days');

INSERT INTO scene_action_info (id, scene_id, action_target, action_type, action_param_type, action_param, action_value, created_at, tenant_id)
VALUES ('sc-1-a1', 'sc-1', 'dev-1', '10', 'TEL', 'switch', '1',
        now() - interval '3 days', 't-e2e'),
       ('sc-1-a2', 'sc-1', 'ac-1', '30', NULL, NULL, NULL,
        now() - interval '3 days' + interval '1 second', 't-e2e');

-- 场景 2: 激活另一个场景。
INSERT INTO scene_info (id, name, description, tenant_id, creator, created_at)
VALUES ('sc-2', '总开关', '', 't-e2e', 'u1', now() - interval '2 days');

INSERT INTO scene_action_info (id, scene_id, action_target, action_type, action_param_type, action_param, action_value, created_at, tenant_id)
VALUES ('sc-2-a1', 'sc-2', 'sc-1', '20', NULL, NULL, NULL,
        now() - interval '2 days', 't-e2e');

-- 场景 3: 引用了一个已经不存在的设备。
INSERT INTO scene_info (id, name, description, tenant_id, creator, created_at)
VALUES ('sc-3', '坏场景', '设备已删除', 't-e2e', 'u1', now() - interval '1 day');

INSERT INTO scene_action_info (id, scene_id, action_target, action_type, action_param_type, action_param, action_value, created_at, tenant_id)
VALUES ('sc-3-a1', 'sc-3', 'dev-gone', '10', 'TEL', 'switch', '1',
        now() - interval '1 day', 't-e2e');

-- 联动 1: 两条 AND 条件，一个动作。运算符是字符串，必须原样搬过去。
INSERT INTO scene_automations (id, name, description, enabled, tenant_id, creator, updator, created_at)
VALUES ('au-1', '高温开风机', '', 'Y', 't-e2e', 'u1', 'u1', now() - interval '4 days');

INSERT INTO device_trigger_condition (id, scene_automation_id, enabled, group_id, trigger_condition_type, trigger_source, trigger_param_type, trigger_param, trigger_operator, trigger_value, tenant_id)
VALUES ('au-1-c1', 'au-1', 'Y', 'g1', '10', 'dev-1', 'TEL', 'temperature', '>', '30', 't-e2e'),
       ('au-1-c2', 'au-1', 'Y', 'g1', '10', 'dev-1', 'TEL', 'humidity', '>=', '80', 't-e2e');

INSERT INTO action_info (id, scene_automation_id, action_target, action_type, action_param_type, action_param, action_value)
VALUES ('au-1-a1', 'au-1', 'dev-2', '10', 'CMD', 'fan', '{"method":"turn_on","params":{}}');

-- 联动 2: 整类设备触发 + 整类设备动作，目标集一致。
INSERT INTO scene_automations (id, name, description, enabled, tenant_id, creator, updator, created_at)
VALUES ('au-2', '整类设备联动', '', 'Y', 't-e2e', 'u1', 'u1', now() - interval '4 days');

INSERT INTO device_trigger_condition (id, scene_automation_id, enabled, group_id, trigger_condition_type, trigger_source, trigger_param_type, trigger_param, trigger_operator, trigger_value, tenant_id)
VALUES ('au-2-c1', 'au-2', 'Y', 'g1', '11', 'cfg-1', 'TEL', 'temperature', '>', '40', 't-e2e');

INSERT INTO action_info (id, scene_automation_id, action_target, action_type, action_param_type, action_param, action_value)
VALUES ('au-2-a1', 'au-2', 'cfg-1', '11', 'TEL', 'switch', '0');

-- 联动 3: 停用 + 时间范围触发。
INSERT INTO scene_automations (id, name, description, enabled, tenant_id, creator, updator, created_at)
VALUES ('au-3', '只在白天', '', 'N', 't-e2e', 'u1', 'u1', now() - interval '4 days');

INSERT INTO device_trigger_condition (id, scene_automation_id, enabled, group_id, trigger_condition_type, trigger_source, trigger_param_type, trigger_param, trigger_operator, trigger_value, tenant_id)
VALUES ('au-3-c1', 'au-3', 'Y', 'g1', '22', NULL, NULL, NULL, NULL,
        '137|08:00:00+00:00|18:00:00+00:00', 't-e2e');

INSERT INTO action_info (id, scene_automation_id, action_target, action_type, action_param_type, action_param, action_value)
VALUES ('au-3-a1', 'au-3', 'dev-1', '10', 'TEL', 'switch', '1');

-- 告警配置: 只有名字/等级/通知组，没有触发条件。
INSERT INTO alarm_config (id, name, description, alarm_level, notification_group_id, created_at, updated_at, tenant_id, enabled)
VALUES ('ac-1', '高温告警', '', 'H', 'ng-1', now(), now(), 't-e2e', 'Y'),
       ('ac-2', '已停用的告警', '', 'L', 'ng-2', now(), now(), 't-e2e', 'N');

-- 三条告警信息，两条还没处理。
INSERT INTO alarm_info (id, alarm_config_id, name, alarm_time, description, content, processing_result, tenant_id, alarm_level)
VALUES ('al-1', 'ac-1', '高温告警', now() - interval '2 hours', '',
        '{"device_id":"dev-1","temperature":42}', 'UND', 't-e2e', 'H'),
       ('al-2', 'ac-1', '高温告警', now() - interval '1 hour', '',
        '温度 41 度', 'UND', 't-e2e', 'H'),
       ('al-3', 'ac-1', '已处理过的', now() - interval '5 hours', '',
        '{"device_id":"dev-2"}', 'DOP', 't-e2e', 'H');

COMMIT;
