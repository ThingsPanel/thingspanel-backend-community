package service

import (
	"errors"
	"fmt"
	"strings"
)

var (
	errProductHasActiveDevices = errors.New("产品下存在已激活设备，无法删除")
)

func preRegisterActivateState(currentFlag string) (activateFlag string, isEnabled string, err error) {
	if currentFlag == "active" {
		return "", "", errors.New("设备已激活")
	}
	return "active", "enabled", nil
}

func canRetryOTAStatus(status int16) bool {
	return status == 5 || status == 6
}

func shouldBlockForUnfinishedOTA(otherInProgressCount int) bool {
	return otherInProgressCount > 0
}

func resolveOTAPackageID(taskID, packageID string) string {
	if packageID != "" {
		return packageID
	}
	return taskID
}

func skipTenantFilter(authority string) bool {
	return authority == "SYS_ADMIN"
}

func filterActiveDevicesOnly(hasDeviceConfigFilter bool) bool {
	return !hasDeviceConfigFilter
}

func enabledStatuses(requested string) []string {
	if requested == "enabled" {
		return []string{"enabled", ""}
	}
	if requested == "" {
		return nil
	}
	return []string{requested}
}

func guardDeleteProduct(activeCount, inactiveCount int) (cascadeInactive bool, err error) {
	if activeCount > 0 {
		return false, errProductHasActiveDevices
	}
	if inactiveCount > 0 {
		return true, nil
	}
	return false, nil
}

func mapDeviceInsertError(err error, deviceNumber string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "devices_unique_1") || strings.Contains(msg, "voucher") {
		return fmt.Errorf("凭证已存在，请修改模板后重试")
	}
	if strings.Contains(msg, "devices_unique") || strings.Contains(msg, "device_number") {
		if deviceNumber != "" {
			return fmt.Errorf("设备编号 %s 已存在，请修改模板后重试", deviceNumber)
		}
		return fmt.Errorf("设备编号已存在，请修改模板后重试")
	}
	return err
}
