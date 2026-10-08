package service

import (
	"strings"
	"testing"

	"project/internal/model"
)

func TestUniqueSuggestedBindingKeysForHeatingStation(t *testing.T) {
	used := make(map[string]bool)
	roles := make([]model.DashboardBundleRole, 0, 3)
	for index, name := range []string{"供热站主控DEMO", "补水泵DEMO", "补水电动阀DEMO"} {
		deviceID := []string{"controller-1", "pump-1", "valve-1"}[index]
		roles = append(roles, model.DashboardBundleRole{
			SourceDeviceID: deviceID,
			BindingKey:     uniqueSuggestedBindingKey(name, deviceID, used),
			DisplayName:    name,
		})
	}
	if roles[0].BindingKey != "demo" {
		t.Fatalf("expected existing non-conflicting key to be preserved: %s", roles[0].BindingKey)
	}
	if _, err := validateDashboardBundleRoles(roles); err != nil {
		t.Fatalf("analysis must produce publishable roles: %v", err)
	}
}

func TestUniqueSuggestedBindingKeyHandlesExistingFallback(t *testing.T) {
	deviceID := "pump-1"
	used := map[string]bool{"demo": true, deviceRoleBindingKey(deviceID): true}
	key := uniqueSuggestedBindingKey("补水泵DEMO", deviceID, used)
	if key != deviceRoleBindingKey(deviceID)+"-2" {
		t.Fatalf("unexpected collision resolution: %s", key)
	}
}

func TestValidateDashboardBundleRoles(t *testing.T) {
	roles, err := validateDashboardBundleRoles([]model.DashboardBundleRole{
		{
			SourceDeviceID: "sensor-1",
			BindingKey:     "temperature-sensor",
			DisplayName:    "Temperature Sensor",
		},
		{
			SourceDeviceID: "switch-1",
			BindingKey:     "power-switch",
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

func TestValidateDashboardBundleRolesAllowsDashboardWithoutDevices(t *testing.T) {
	roles, err := validateDashboardBundleRoles(nil)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if len(roles) != 0 {
		t.Fatalf("expected no roles, got %d", len(roles))
	}
}

func TestValidateDashboardBundleRolesRejectsDuplicateBindingKey(t *testing.T) {
	_, err := validateDashboardBundleRoles([]model.DashboardBundleRole{
		{SourceDeviceID: "sensor-1", BindingKey: "shared-role", DisplayName: "Sensor"},
		{SourceDeviceID: "switch-1", BindingKey: "shared-role", DisplayName: "Switch"},
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

func TestNormalizeDashboardBundleRolesConvertsLegacyUnderscores(t *testing.T) {
	roles := normalizeDashboardBundleRoles([]model.DashboardBundleRole{{
		SourceDeviceID: "sensor-1",
		BindingKey:     "temperature_sensor",
		DisplayName:    "Temperature Sensor",
	}})
	if roles[0].BindingKey != "temperature-sensor" {
		t.Fatalf("unexpected canonical binding key: %s", roles[0].BindingKey)
	}
}
