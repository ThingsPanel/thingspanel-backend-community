package service

import (
	"context"
	"testing"

	"project/internal/model"
)

func TestValidateTemplateDeviceBindingsRequiresAllRequiredRoles(t *testing.T) {
	service := &DashboardTemplateService{}
	_, err := service.validateTemplateDeviceBindings(
		context.Background(),
		"tenant-1",
		[]*model.LocalDashboardTemplateBinding{
			{
				BindingKey:            "temperature_sensor",
				Required:              true,
				LocalDeviceTemplateID: "template-1",
			},
		},
		nil,
	)
	if err == nil {
		t.Fatal("expected missing required binding error")
	}
}

func TestValidateTemplateDeviceBindingsRejectsUnknownRoleBeforeDeviceLookup(t *testing.T) {
	service := &DashboardTemplateService{}
	_, err := service.validateTemplateDeviceBindings(
		context.Background(),
		"tenant-1",
		[]*model.LocalDashboardTemplateBinding{
			{
				BindingKey:            "temperature_sensor",
				Required:              true,
				LocalDeviceTemplateID: "template-1",
			},
		},
		[]model.DeviceBindingInput{
			{BindingKey: "unknown", LocalDeviceID: "device-1"},
		},
	)
	if err == nil {
		t.Fatal("expected unknown binding error")
	}
}
