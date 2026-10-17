-- OTA 升级任务的设备逐条真实上报记录，供运营台追溯下载、校验、重启确认过程。
CREATE TABLE IF NOT EXISTS ota_upgrade_progress_logs (
  id varchar(36) PRIMARY KEY,
  task_detail_id varchar(36) NOT NULL,
  device_id varchar(200) NOT NULL,
  steps smallint,
  status smallint NOT NULL,
  description varchar(500) NOT NULL DEFAULT '',
  payload text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE ota_upgrade_progress_logs IS 'OTA 升级任务详情的设备实时进度上报记录';
COMMENT ON COLUMN ota_upgrade_progress_logs.id IS '进度记录唯一标识';
COMMENT ON COLUMN ota_upgrade_progress_logs.task_detail_id IS '关联的 OTA 升级任务设备详情标识';
COMMENT ON COLUMN ota_upgrade_progress_logs.device_id IS 'ThingsPanel 设备内部标识';
COMMENT ON COLUMN ota_upgrade_progress_logs.steps IS '设备上报的升级进度或失败码';
COMMENT ON COLUMN ota_upgrade_progress_logs.status IS '映射后的 OTA 任务状态：3升级中、4成功、5失败';
COMMENT ON COLUMN ota_upgrade_progress_logs.description IS '设备上报的可读状态说明';
COMMENT ON COLUMN ota_upgrade_progress_logs.payload IS '设备原始 OTA 进度报文，供诊断追溯';
COMMENT ON COLUMN ota_upgrade_progress_logs.created_at IS '服务端接收并持久化时间';
CREATE INDEX IF NOT EXISTS idx_ota_progress_logs_detail ON ota_upgrade_progress_logs (task_detail_id, created_at);
COMMENT ON INDEX idx_ota_progress_logs_detail IS '按 OTA 任务详情时间顺序读取进度记录';
