package service

import (
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
