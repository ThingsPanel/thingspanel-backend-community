package service

import (
	"strings"
	"testing"

	"project/internal/model"
)

func TestValidateDashboardBundleRoles(t *testing.T) {
	roles, err := validateDashboardBundleRoles([]model.DashboardBundleRole{
		{
			SourceDeviceID: "sensor-1",
			BindingKey:     "temperature_sensor",
			DisplayName:    "Temperature Sensor",
		},
		{
			SourceDeviceID: "switch-1",
			BindingKey:     "power_switch",
			DisplayName:    "Power Switch",
		},
	})
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("expected two roles, got %d", len(roles))
	}
}

func TestValidateDashboardBundleRolesRejectsDuplicateBindingKey(t *testing.T) {
	_, err := validateDashboardBundleRoles([]model.DashboardBundleRole{
		{SourceDeviceID: "sensor-1", BindingKey: "shared_role", DisplayName: "Sensor"},
		{SourceDeviceID: "switch-1", BindingKey: "shared_role", DisplayName: "Switch"},
	})
	if err == nil {
		t.Fatal("expected duplicate bindingKey to be rejected")
	}
}

func TestDashboardPublishIdempotencyKeySeparatesVersions(t *testing.T) {
	first := dashboardPublishIdempotencyKey("tenant-1", "temperature-dashboard", "1.0.0")
	repeated := dashboardPublishIdempotencyKey("tenant-1", "temperature-dashboard", "1.0.0")
	next := dashboardPublishIdempotencyKey("tenant-1", "temperature-dashboard", "1.0.1")
	if first != repeated {
		t.Fatal("same publish identity must produce the same idempotency key")
	}
	if first == next {
		t.Fatal("different versions must produce different idempotency keys")
	}
}

func TestNormalizeDashboardBundleRolesRemovesSourceDeviceIdentity(t *testing.T) {
	sourceDeviceID := "192f3fa4-1a93-bd91-e10f-36d621601e63"
	roles := normalizeDashboardBundleRoles([]model.DashboardBundleRole{{
		SourceDeviceID: sourceDeviceID,
		BindingKey:     "device_192f3fa4_1a93_bd91_e10f_36d621601e63",
		DisplayName:    "Device " + sourceDeviceID,
	}})

	if roles[0].DisplayName != "Device" {
		t.Fatalf("unexpected portable display name: %s", roles[0].DisplayName)
	}
	if roles[0].BindingKey != deviceRoleBindingKey(sourceDeviceID) {
		t.Fatalf("unexpected portable binding key: %s", roles[0].BindingKey)
	}
	if strings.Contains(roles[0].BindingKey, strings.ReplaceAll(sourceDeviceID, "-", "_")) {
		t.Fatalf("binding key still contains encoded source device ID: %s", roles[0].BindingKey)
	}
}

func TestSuggestBindingKeyUsesOpaqueFallback(t *testing.T) {
	sourceDeviceID := "6f48f02b-2f06-0bb8-da6a-722b0565dc00"
	bindingKey := suggestBindingKey("温湿度传感器", sourceDeviceID)
	if bindingKey != deviceRoleBindingKey(sourceDeviceID) {
		t.Fatalf("unexpected fallback binding key: %s", bindingKey)
	}
	if strings.Contains(bindingKey, strings.ReplaceAll(sourceDeviceID, "-", "_")) {
		t.Fatalf("fallback binding key exposes source device ID: %s", bindingKey)
	}
}
