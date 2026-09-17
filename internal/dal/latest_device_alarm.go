package dal

import (
	"context"

	"project/internal/query"
)

// LatestDeviceAlarmQuery 设备告警查询结构体
type LatestDeviceAlarmQuery struct{}

// CountDevicesByTenantAndStatus 根据租户ID和状态统计设备数量
func (q *LatestDeviceAlarmQuery) CountDevicesByTenantAndStatus(ctx context.Context, tenantID string) (int64, error) {
	lda := query.LatestDeviceAlarm

	// 查询指定状态的设备数量，alarm_status不等于N
	count, err := lda.WithContext(ctx).
		Where(lda.TenantID.Eq(tenantID)).
		Where(lda.AlarmStatus.Neq("N")).
		Distinct(lda.DeviceID).
		Count()

	return count, err
}

// CountDevicesByTenantAndStatusForUser counts only devices in a user's scope.
func (q *LatestDeviceAlarmQuery) CountDevicesByTenantAndStatusForUser(ctx context.Context, tenantID string, deviceIDs []string) (int64, error) {
	if len(deviceIDs) == 0 {
		return 0, nil
	}
	lda := query.LatestDeviceAlarm
	return lda.WithContext(ctx).
		Where(lda.TenantID.Eq(tenantID), lda.DeviceID.In(deviceIDs...)).
		Where(lda.AlarmStatus.Neq("N")).
		Distinct(lda.DeviceID).
		Count()
}
