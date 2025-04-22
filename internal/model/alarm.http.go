// internal/model/alarm.http.go
package model

// AlarmDeviceCountsResponse 告警设备数量统计响应
type AlarmDeviceCountsResponse struct {
	AlarmDeviceTotal int64 `json:"alarm_device_total"` // 告警设备数量
}
