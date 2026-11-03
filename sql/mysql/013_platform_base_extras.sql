-- Hand-written companions of the generated platform base schema (012): the one view and the one index the
-- generator cannot see because the slice tables were ported by hand.
CREATE OR REPLACE VIEW latest_device_alarms AS
WITH unnested_devices AS (
    SELECT ah.id, ah.alarm_config_id, ah.group_id, ah.scene_automation_id, ah.name, ah.description, ah.content,
           ah.alarm_status, ah.tenant_id, ah.remark, ah.create_at, jt.device_id
      FROM alarm_history ah,
           JSON_TABLE(ah.alarm_device_list, '$[*]' COLUMNS (device_id VARCHAR(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin PATH '$')) jt
), ranked_alarms AS (
    SELECT u.*, ROW_NUMBER() OVER (PARTITION BY u.device_id ORDER BY u.create_at DESC) AS rn
      FROM unnested_devices u
)
SELECT id, alarm_config_id, group_id, scene_automation_id, name, description, content, alarm_status, tenant_id, remark,
       create_at, device_id
  FROM ranked_alarms
 WHERE rn = 1
-- @@
SET @tp_ix_ts := (SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'telemetry_current_datas' AND index_name = 'telemetry_datas_ts_idx_copy1')
-- @@
SET @tp_ix_ts_sql := IF(@tp_ix_ts = 0, 'CREATE INDEX telemetry_datas_ts_idx_copy1 ON telemetry_current_datas (ts DESC)', 'SELECT 1')
-- @@
PREPARE tp_ix_ts_stmt FROM @tp_ix_ts_sql
-- @@
EXECUTE tp_ix_ts_stmt
-- @@
DEALLOCATE PREPARE tp_ix_ts_stmt
