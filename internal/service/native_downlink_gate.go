package service

import (
	"project/internal/dal"
	"project/internal/model"
	"project/pkg/errcode"
)

// Native controls belong to COMMAND. Raw telemetry/attribute endpoints must
// not provide a way around the pinned command schema, including service routes.
func rejectNativeLegacyControl(device *model.Device) error {
	if device == nil || device.DeviceConfigID == nil || *device.DeviceConfigID == "" {
		return nil
	}
	config, err := dal.GetDeviceConfigByID(*device.DeviceConfigID)
	if err != nil {
		return err
	}
	return nativeLegacyControlError(config)
}

func nativeLegacyControlError(config *model.DeviceConfig) error {
	if hasNativeDeviceConfigBinding(config) {
		return errcode.NewWithMessage(errcode.CodeParamError, "native device controls must use a defined COMMAND and the command endpoint")
	}
	return nil
}
