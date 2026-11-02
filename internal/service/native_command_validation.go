package service

import (
	"context"
	"project/internal/brokersemantic"
	"project/internal/model"
)

// Validate the exact published binding; never substitute draft or legacy metadata.
func validateNativeDeviceCommand(ctx context.Context, authorization string, config *model.DeviceConfig, identifier string, value *string) error {
	snapshot, err := readNativeBindingSnapshot(ctx, authorization, config)
	if err != nil {
		return err
	}
	return brokersemantic.ValidateCommand(brokersemantic.Description{Model: snapshot.Model}, identifier, value)
}
