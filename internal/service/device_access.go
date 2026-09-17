package service

import (
	"project/internal/dal"
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
