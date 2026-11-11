package service

import (
	"github.com/spf13/viper"
	"project/internal/dal"
	"project/pkg/errcode"
	"project/pkg/global"
	"strings"
)

// getTenantTelemetryDataCurrent preserves the tenant summary response while
// using only the local relationship/current-data owner. The owner query requires
// active permanent identity reservations; it does not manufacture an external
// TSDB deletion generation. Pre-cutover history needs writer fencing/drain.
func (*Device) getTenantTelemetryDataCurrent(tenantID string) ([]map[string]interface{}, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, errcode.New(errcode.CodeNoPermission)
	}
	profile := strings.ToUpper(strings.TrimSpace(viper.GetString("grpc.tptodb_type")))
	if profile != "" && profile != "NONE" {
		return nil, errcode.New(errcode.CodeServiceUnavailable)
	}
	if global.DB == nil {
		return nil, errcode.New(errcode.CodeDBError)
	}
	dialect := global.DB.Dialector.Name()
	if dialect != "postgres" && dialect != "mysql" && dialect != "sqlite" {
		return nil, errcode.New(errcode.CodeServiceUnavailable)
	}
	rows, err := dal.ReadTenantLatestTelemetry(tenantID)
	if err != nil {
		return nil, errcode.New(errcode.CodeDBError)
	}
	result := make([]map[string]interface{}, 0)
	indexes := map[string]int{}
	for _, row := range rows {
		index, exists := indexes[row.DeviceID]
		if !exists {
			index = len(result)
			indexes[row.DeviceID] = index
			result = append(result, map[string]interface{}{"device_id": row.DeviceID, "device_name": row.DeviceName, "is_online": row.IsOnline, "last_push_time": row.DeviceTimestamp, "telemetry_data": make([]map[string]interface{}, 0)})
		}
		item := map[string]interface{}{"key": row.Key, "label": row.Label, "unit": row.Unit}
		if row.BoolV != nil {
			item["value"] = *row.BoolV
		} else if row.NumberV != nil {
			item["value"] = *row.NumberV
		} else if row.StringV != nil {
			item["value"] = *row.StringV
		}
		result[index]["telemetry_data"] = append(result[index]["telemetry_data"].([]map[string]interface{}), item)
	}
	return result, nil
}
