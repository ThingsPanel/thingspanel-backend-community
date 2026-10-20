package service

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"project/internal/brokercommands"
	"project/internal/brokerplugin"
	"project/internal/model"
	"project/pkg/constant"
)

func TestDurableCommandCanonicalIdentityAndExpiry(t *testing.T) {
	params := `{"b":2,"a":1}`
	req := &model.PutMessageForCommand{DeviceID: "logical", Identify: "switch", Value: &params}
	first, err := canonicalizeCommand(req, "root")
	if err != nil {
		t.Fatal(err)
	}
	params = `{"a":1,"b":2}`
	req.Value = &params
	second, err := canonicalizeCommand(req, "root")
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("equivalent JSON params produced different canonical requests: %s vs %s", first, second)
	}
	var canonical canonicalCommandRequest
	if err := json.Unmarshal(first, &canonical); err != nil {
		t.Fatal(err)
	}
	if canonical.DeviceID != "logical" || canonical.ExpiresAt != nil {
		t.Fatalf("unexpected canonical request: %+v", canonical)
	}
	if caller, intent, err := durableCommandIdentity(strconv.Itoa(constant.Manual), "operator-7"); err != nil || caller != "operator-7" || intent != manualCommandIntent {
		t.Fatalf("manual identity=(%q,%q,%v)", caller, intent, err)
	}
	if _, _, err := durableCommandIdentity(strconv.Itoa(constant.Manual), ""); err == nil {
		t.Fatal("manual command without authenticated caller was accepted")
	}
	if _, _, err := durableCommandIdentity(strconv.Itoa(constant.Auto), "system"); err == nil {
		t.Fatal("automation command without policy context was accepted")
	}
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	if expiry := durableCommandExpiry(req, now); !expiry.Equal(now.Add(defaultCommandTTL)) {
		t.Fatalf("default expiry=%s, want %s", expiry, now.Add(defaultCommandTTL))
	}
	explicit := int64(1_800_000_000_000)
	req.ExpiresAt = &explicit
	if expiry := durableCommandExpiry(req, now); !expiry.Equal(time.UnixMilli(explicit)) {
		t.Fatalf("explicit expiry=%s", expiry)
	}
}

func TestDurableMultiLevelTransformPreservesPayload(t *testing.T) {
	payload := map[string]interface{}{"switch": true}
	path := []commandDeviceSnapshot{
		{ID: "child", DeviceType: "3", SubDeviceAddr: stringPtr("leaf-1"), ParentID: stringPtr("gateway")},
		{ID: "gateway", DeviceType: "2", SubDeviceAddr: stringPtr("gw-1"), ParentID: stringPtr("root")},
		{ID: "root", DeviceType: "1", DeviceNumber: "root-number"},
	}
	got, err := transformDurableCommandData(payload, path, "3")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"sub_gateway_data":{"gw-1":{"sub_device_data":{"leaf-1":{"switch":true}}}}}`
	if string(encoded) != want {
		t.Fatalf("transformed payload=%s, want %s", encoded, want)
	}
}

func TestDurableMultiLevelTransformFourLevelVectors(t *testing.T) {
	payload := map[string]interface{}{"method": "set", "params": map[string]interface{}{"value": 1}}
	childPath := []commandDeviceSnapshot{
		{ID: "child", DeviceType: "3", SubDeviceAddr: stringPtr("leaf"), ParentID: stringPtr("gateway-a")},
		{ID: "gateway-a", DeviceType: "2", SubDeviceAddr: stringPtr("branch-a"), ParentID: stringPtr("gateway-b")},
		{ID: "gateway-b", DeviceType: "2", SubDeviceAddr: stringPtr("branch-b"), ParentID: stringPtr("root")},
		{ID: "root", DeviceType: "1", DeviceNumber: "root-number"},
	}
	got, err := transformDurableCommandData(payload, childPath, "3")
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	const childWant = `{"sub_gateway_data":{"branch-b":{"sub_gateway_data":{"branch-a":{"sub_device_data":{"leaf":{"method":"set","params":{"value":1}}}}}}}}`
	if string(gotJSON) != childWant {
		t.Fatalf("four-level child command=%s, want %s", gotJSON, childWant)
	}

	gatewayPath := []commandDeviceSnapshot{
		{ID: "gateway-a", DeviceType: "2", SubDeviceAddr: stringPtr("branch-a"), ParentID: stringPtr("gateway-b")},
		{ID: "gateway-b", DeviceType: "2", SubDeviceAddr: stringPtr("branch-b"), ParentID: stringPtr("root")},
		{ID: "root", DeviceType: "1", DeviceNumber: "root-number"},
	}
	got, err = transformDurableCommandData(payload, gatewayPath, "2")
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err = json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	const gatewayWant = `{"sub_gateway_data":{"branch-b":{"sub_gateway_data":{"branch-a":{"gateway_data":{"method":"set","params":{"value":1}}}}}}}`
	if string(gotJSON) != gatewayWant {
		t.Fatalf("multi-level gateway command=%s, want %s", gotJSON, gatewayWant)
	}
}

func TestCommandFingerprintSnapshotLegacyJSONIsStable(t *testing.T) {
	type legacyFingerprint struct {
		PluginPath []commandDeviceSnapshot     `json:"plugin_path,omitempty"`
		Plugin     *brokerplugin.Route         `json:"plugin,omitempty"`
		Devices    []commandDeviceSnapshot     `json:"devices"`
		Configs    []commandConfigSnapshot     `json:"configs"`
		Commands   []commandDefinitionSnapshot `json:"commands"`
		Scripts    []commandScriptSnapshot     `json:"scripts"`
	}
	legacy := legacyFingerprint{
		Devices:  []commandDeviceSnapshot{{ID: "device-1", TenantID: "tenant-1", DeviceNumber: "number-1"}},
		Configs:  []commandConfigSnapshot{{ID: "config-1", TenantID: "tenant-1", DeviceType: "1"}},
		Commands: []commandDefinitionSnapshot{{ID: "command-1", DeviceTemplateID: "template-1", DataIdentifier: "switch", TenantID: "tenant-1"}},
		Scripts:  []commandScriptSnapshot{{ID: "script-1", DeviceConfigID: "config-1", ScriptType: "E", EnableFlag: "enabled"}},
	}
	current := commandFingerprintSnapshot{
		Devices: legacy.Devices, Configs: legacy.Configs, Commands: legacy.Commands, Scripts: legacy.Scripts,
	}
	oldJSON, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	newJSON, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if string(newJSON) != string(oldJSON) {
		t.Fatalf("standard fingerprint JSON changed: old=%s new=%s", oldJSON, newJSON)
	}
	if got := effectiveMappingVersion(""); got != "standard-v1" {
		t.Fatalf("legacy prepared command mapping version=%q, want standard-v1", got)
	}
	var prepared brokercommands.PreparedCommand
	if err := json.Unmarshal([]byte(`{"message":{"device_id":"device-1"}}`), &prepared); err != nil {
		t.Fatal(err)
	}
	if got := effectiveMappingVersion(prepared.MappingVersion); got != "standard-v1" {
		t.Fatalf("prepared command without new field maps to %q, want standard-v1", got)
	}
}
