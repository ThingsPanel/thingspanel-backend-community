package dal

import (
	"errors"
	"time"

	model "project/internal/model"
	"project/pkg/global"

	"gorm.io/gorm"
)

const (
	DeviceAccessRead   = "read"
	DeviceAccessManage = "manage"
)

// GetUserDeviceAccess returns the effective access level for each device.
func GetUserDeviceAccess(userID, tenantID string) (map[string]string, error) {
	var rows []model.UserDevicePermission
	err := global.DB.Where("user_id = ? AND tenant_id = ?", userID, tenantID).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(rows))
	for _, row := range rows {
		result[row.DeviceID] = row.AccessLevel
	}
	return result, nil
}

// GetAccessibleDeviceIDs is intentionally tenant-scoped and only returns
// active assignments. The caller uses the result in the device list query,
// so pagination and total are calculated after permission filtering.
func GetAccessibleDeviceIDs(userID, tenantID string) ([]string, error) {
	var ids []string
	err := global.DB.Table("user_device_permissions").
		Where("user_id = ? AND tenant_id = ? AND access_level IN ?", userID, tenantID, []string{DeviceAccessRead, DeviceAccessManage}).
		Pluck("device_id", &ids).Error
	return ids, err
}

func HasUserDeviceAccess(userID, tenantID, deviceID, accessLevel string) (bool, error) {
	query := global.DB.Table("user_device_permissions").
		Where("user_id = ? AND tenant_id = ? AND device_id = ?", userID, tenantID, deviceID)
	if accessLevel == DeviceAccessManage {
		query = query.Where("access_level = ?", DeviceAccessManage)
	} else {
		query = query.Where("access_level IN ?", []string{DeviceAccessRead, DeviceAccessManage})
	}
	var count int64
	err := query.Count(&count).Error
	return count > 0, err
}

func GetUserDevicePermissionItems(userID, tenantID string) ([]model.UserDevicePermissionItem, error) {
	var rows []model.UserDevicePermissionItem
	err := global.DB.Table("devices AS d").
		Select("d.id AS device_id, COALESCE(d.name, '') AS device_name, d.device_number, COALESCE(p.access_level, '') AS access_level").
		Joins("LEFT JOIN user_device_permissions AS p ON p.device_id = d.id AND p.user_id = ? AND p.tenant_id = ?", userID, tenantID).
		Where("d.tenant_id = ? AND d.activate_flag = ?", tenantID, "active").
		Order("d.created_at DESC").
		Scan(&rows).Error
	return rows, err
}

func ReplaceUserDeviceAccess(userID, tenantID string, assignments []model.UserDevicePermissionAssignment) error {
	tx := global.DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	rollback := func(err error) error {
		tx.Rollback()
		return err
	}

	var deviceCount int64
	deviceIDs := make([]string, 0, len(assignments))
	seen := make(map[string]struct{}, len(assignments))
	for _, assignment := range assignments {
		if assignment.DeviceID == "" || (assignment.AccessLevel != DeviceAccessRead && assignment.AccessLevel != DeviceAccessManage) {
			return rollback(errors.New("invalid user device permission assignment"))
		}
		if _, ok := seen[assignment.DeviceID]; ok {
			return rollback(errors.New("duplicate device permission assignment"))
		}
		seen[assignment.DeviceID] = struct{}{}
		deviceIDs = append(deviceIDs, assignment.DeviceID)
	}
	if len(deviceIDs) > 0 {
		if err := tx.Table("devices").Where("tenant_id = ? AND activate_flag = ? AND id IN ?", tenantID, "active", deviceIDs).Count(&deviceCount).Error; err != nil {
			return rollback(err)
		}
		if deviceCount != int64(len(deviceIDs)) {
			return rollback(gorm.ErrRecordNotFound)
		}
	}

	if err := tx.Where("user_id = ? AND tenant_id = ?", userID, tenantID).Delete(&model.UserDevicePermission{}).Error; err != nil {
		return rollback(err)
	}
	now := time.Now().UTC()
	rows := make([]model.UserDevicePermission, 0, len(assignments))
	for _, assignment := range assignments {
		rows = append(rows, model.UserDevicePermission{
			UserID: userID, DeviceID: assignment.DeviceID, TenantID: tenantID,
			AccessLevel: assignment.AccessLevel, CreatedAt: now, UpdatedAt: now,
		})
	}
	if len(rows) > 0 {
		if err := tx.Create(&rows).Error; err != nil {
			return rollback(err)
		}
	}
	return tx.Commit().Error
}
