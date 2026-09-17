package service

import (
	"encoding/json"

	"project/internal/dal"
	model "project/internal/model"
	"project/pkg/errcode"
	"project/pkg/utils"
)

func hasFullDeviceAccess(claims *utils.UserClaims) bool {
	return claims != nil && (claims.Authority == "TENANT_ADMIN" || claims.Authority == "SYS_ADMIN")
}

func ensureDeviceAccess(deviceID string, claims *utils.UserClaims, accessLevel string) error {
	if claims == nil || claims.TenantID == "" {
		return errcode.New(errcode.CodeNoPermission)
	}
	device, err := dal.GetDeviceByID(deviceID)
	if err != nil || device == nil || device.TenantID != claims.TenantID {
		return errcode.New(errcode.CodeNoPermission)
	}
	if hasFullDeviceAccess(claims) {
		return nil
	}
	allowed, err := dal.HasUserDeviceAccess(claims.ID, claims.TenantID, deviceID, accessLevel)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{"error": err.Error()})
	}
	if !allowed {
		return errcode.New(errcode.CodeNoPermission)
	}
	return nil
}

func ensureTenantDeviceAdministrator(claims *utils.UserClaims) error {
	if !hasFullDeviceAccess(claims) {
		return errcode.New(errcode.CodeNoPermission)
	}
	return nil
}

// ensureAlarmHistoryAccess keeps alarm history aligned with the device scope.
// A history may contain multiple devices, so a manage operation requires
// manage access to every device while a read operation requires read access
// to every device that will be returned to the caller.
func ensureAlarmHistoryAccess(historyID string, claims *utils.UserClaims, accessLevel string) (*model.AlarmHistory, error) {
	if claims == nil || claims.TenantID == "" {
		return nil, errcode.New(errcode.CodeNoPermission)
	}
	history, err := dal.GetAlarmHistoryByID(historyID, claims.TenantID)
	if err != nil || history == nil {
		return nil, errcode.New(errcode.CodeNoPermission)
	}
	if hasFullDeviceAccess(claims) {
		return history, nil
	}

	var deviceIDs []string
	if err := json.Unmarshal([]byte(history.AlarmDeviceList), &deviceIDs); err != nil || len(deviceIDs) == 0 {
		return nil, errcode.New(errcode.CodeNoPermission)
	}
	allowed := 0
	for _, deviceID := range deviceIDs {
		if err := ensureDeviceAccess(deviceID, claims, accessLevel); err == nil {
			allowed++
		}
	}
	if allowed != len(deviceIDs) {
		return nil, errcode.New(errcode.CodeNoPermission)
	}
	return history, nil
}
