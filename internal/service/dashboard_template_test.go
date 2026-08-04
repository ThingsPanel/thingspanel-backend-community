package service

import (
	"context"
	"encoding/json"
	"testing"

	"project/internal/model"
)

func TestResolveDashboardDataSourceBindingsPreservesReferences(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"id":"__platform_binding_room_sensor__",
			"type":"PLATFORM_FIELD",
			"config":{
				"source":"platform",
				"deviceBinding":{"$deviceBinding":"room_sensor"},
				"requestedFields":["temperature"]
			}
		}
	]`)

	result, err := resolveDashboardDataSourceBindings(raw, map[string]string{
		"room_sensor": "device-1",
	})
	if err != nil {
		t.Fatalf("resolveDashboardDataSourceBindings() error = %v", err)
	}

	var dataSources []map[string]interface{}
	if err := json.Unmarshal(result, &dataSources); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got := dataSources[0]["id"]; got != "__platform_binding_room_sensor__" {
		t.Fatalf("data source id = %v, want placeholder id preserved", got)
	}
	config := dataSources[0]["config"].(map[string]interface{})
	if got := config["deviceId"]; got != "device-1" {
		t.Fatalf("deviceId = %v, want device-1", got)
	}
	if _, exists := config["deviceBinding"]; exists {
		t.Fatal("deviceBinding placeholder was not removed")
	}
}

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
