package dal

import (
	"project/pkg/global"
	"time"
)

type TenantLatestTelemetryRow struct {
	DeviceID        string    `gorm:"column:device_id"`
	DeviceName      *string   `gorm:"column:device_name"`
	IsOnline        int16     `gorm:"column:is_online"`
	DeviceTimestamp time.Time `gorm:"column:device_ts"`
	Key             string    `gorm:"column:telemetry_key"`
	Timestamp       time.Time `gorm:"column:telemetry_ts"`
	BoolV           *bool     `gorm:"column:bool_v"`
	NumberV         *float64  `gorm:"column:number_v"`
	StringV         *string   `gorm:"column:string_v"`
	Label           *string   `gorm:"column:label"`
	Unit            *string   `gorm:"column:unit"`
}

// ReadTenantLatestTelemetry uses one statement snapshot for current ownership,
// ranking, values and optional presentation metadata. It never consults an
// external current-data owner or an unscoped device/model cache.
func ReadTenantLatestTelemetry(tenantID string) ([]TenantLatestTelemetryRow, error) {
	rows := make([]TenantLatestTelemetryRow, 0)
	err := global.DB.Raw(`WITH latest_devices AS (
 SELECT d.id,d.name,d.is_online,d.device_config_id,MAX(t.ts) AS device_ts
 FROM devices d JOIN device_id_reservations reservation ON reservation.id=d.id AND reservation.tenant_id=d.tenant_id AND reservation.state='active'
 JOIN telemetry_current_datas t ON t.device_id=d.id AND t.tenant_id=d.tenant_id
 WHERE d.tenant_id=? AND t.tenant_id=?
 GROUP BY d.id,d.name,d.is_online,d.device_config_id
 ORDER BY MAX(t.ts) DESC,d.id ASC LIMIT 3
 )
 SELECT d.id AS device_id,d.name AS device_name,d.is_online,d.device_ts,
 t."key" AS telemetry_key,t.ts AS telemetry_ts,t.bool_v,t.number_v,t.string_v,
 (SELECT m.data_name FROM device_model_telemetry m WHERE m.device_template_id=template.id AND m.tenant_id=? AND m.data_identifier=t."key" ORDER BY m.id ASC LIMIT 1) AS label,
 (SELECT m.unit FROM device_model_telemetry m WHERE m.device_template_id=template.id AND m.tenant_id=? AND m.data_identifier=t."key" ORDER BY m.id ASC LIMIT 1) AS unit
 FROM latest_devices d JOIN telemetry_current_datas t ON t.device_id=d.id AND t.tenant_id=?
 LEFT JOIN device_configs config ON config.id=d.device_config_id AND config.tenant_id=?
 LEFT JOIN device_templates template ON template.id=config.device_template_id AND template.tenant_id=?
 ORDER BY d.device_ts DESC,d.id ASC,t.ts DESC,t."key" ASC`, tenantID, tenantID, tenantID, tenantID, tenantID, tenantID, tenantID).Scan(&rows).Error
	return rows, err
}
