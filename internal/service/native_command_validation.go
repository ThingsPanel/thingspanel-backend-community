package service

import (
	"context"
	"encoding/json"
	"project/internal/brokersemantic"
	"project/internal/dal"
	"project/internal/model"
)

// Native bindings are exact published snapshots. Legacy and unbound devices
// retain their existing command contract; a partial native binding fails closed.
func validateDeviceCommand(ctx context.Context, authorization string, device *model.Device, identifier string, value *string) error {
	if device.DeviceConfigID == nil || *device.DeviceConfigID == "" {
		return nil
	}
	config, err := dal.GetDeviceConfigByIDForTenant(ctx, *device.DeviceConfigID, device.TenantID)
	if err != nil {
		return err
	}
	if !hasNativeDeviceConfigBinding(config) {
		return nil
	}
	if authorization == "" {
		var params json.RawMessage
		if value != nil {
			params = json.RawMessage(*value)
		}
		return validateOwnerNativeCommand(ctx, device.TenantID, device.ID, config, identifier, params)
	}
	return validateNativeDeviceCommand(ctx, authorization, config, identifier, value)
}

func validateNativeDeviceCommand(ctx context.Context, authorization string, config *model.DeviceConfig, identifier string, value *string) error {
	snapshot, err := readNativeBindingSnapshot(ctx, authorization, config)
	if err != nil {
		return err
	}
	return brokersemantic.ValidateCommand(brokersemantic.Description{Model: snapshot.Model}, identifier, value)
}
