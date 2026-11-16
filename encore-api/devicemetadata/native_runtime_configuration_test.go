package devicemetadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeConfigurationCanonicalPreservesNumbers(t *testing.T) {
	raw, digest, err := canonicalRuntimeConfiguration([]byte(`{"z":9007199254740993123,"nested":{"b":0,"a":-40.125}}`))
	if err != nil || !strings.Contains(string(raw), "9007199254740993123") {
		t.Fatalf("lost precision: %s %v", raw, err)
	}
	_, same, err := canonicalRuntimeConfiguration([]byte(`{ "nested": {"a":-40.125,"b":0}, "z":9007199254740993123 }`))
	if err != nil || same != digest {
		t.Fatal("key order changed digest")
	}
	for _, bad := range []string{`null`, `[]`, `{} {}`} {
		if _, _, err := canonicalRuntimeConfiguration([]byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestRuntimeConfigurationReceiptRequiresAppliedExactSnapshot(t *testing.T) {
	for _, mode := range []string{"applied", "pending", "wrong-digest", "wrong-generation", "missing"} {
		t.Run(mode, func(t *testing.T) {
			snapshot := runtimeConfigurationSnapshot{Generation: 7, Digest: strings.Repeat("a", 64), Configuration: json.RawMessage(`{"access":{"mode":"direct","protocol":"mqtt"}}`)}
			desired, _ := json.Marshal(map[string]any{"runtimeConfiguration": snapshot})
			item := runtimeOutboxItem{TenantID: "tenant", DeviceID: "device", EventID: "event", Generation: 7, Kind: operationKindTemplateBinding, Desired: desired}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					RuntimeConfiguration runtimeConfigurationSnapshot `json:"runtimeConfiguration"`
				}
				if json.NewDecoder(r.Body).Decode(&body) != nil || body.RuntimeConfiguration.Digest != snapshot.Digest {
					t.Error("snapshot not transmitted")
				}
				confirmed := snapshot
				if mode == "wrong-digest" {
					confirmed.Digest = strings.Repeat("b", 64)
				}
				if mode == "wrong-generation" {
					confirmed.Generation++
				}
				state := "applied"
				if mode == "pending" {
					state = "pending"
				}
				data := map[string]any{"deviceId": item.DeviceID, "eventId": item.EventID, "generation": item.Generation, "state": state, "runtimeConfiguration": confirmed}
				if mode == "missing" {
					delete(data, "runtimeConfiguration")
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": data})
			}))
			defer server.Close()
			t.Setenv("TP_COMMUNITY_BASE_URL", server.URL)
			t.Setenv("TP_AUTOMATION_SERVICE_TOKEN", strings.Repeat("x", 32))
			t.Setenv("TP_SOURCE_DEPLOYMENT_ID", "dep")
			if err := applyNativeRuntimeLifecycle(context.Background(), item, ""); (err == nil) != (mode == "applied") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
		})
	}
}

func TestRuntimeConfigurationFreezeAndTemplatePropagation(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	t.Cleanup(func() { cleanupNativeTemplateBindingMutableRows(t, ctx, tenant) })
	created, err := createNativeTemplate(ctx, nativeTemplateWriteTestIdentity(tenant), "runtime-config-create", nativeTemplateWriteTestBody(t, "runtime-config", nativeTemplateMQTTConfiguration), "request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatal(err)
	}
	template := nativeTemplateWriteTestResult(t, created)
	result, err := createNativeDevice(ctx, nativeWriteIdentity(tenant), "runtime-config-device-create", nativeWriteBody(t, map[string]any{"name": "runtime-config-device", "deviceNumber": "runtime-config-device", "deviceKind": "standalone", "templateId": template.ID, "initialGroupIds": []string{}}), "request", validateNativeDeviceConfiguration)
	if err != nil {
		t.Fatal(err)
	}
	device := nativeWriteDevice(t, result)
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = enqueueTemplateRuntimeConfigurationsTx(ctx, tx, tenant, "tester", template.ID, "request"); err != nil {
		t.Fatal(err)
	}
	var desired []byte
	var generation int64
	if err = tx.QueryRow(ctx, `SELECT desired,generation FROM device_runtime_outbox WHERE tenant_id=$1 AND device_id=$2 AND kind='template_binding'`, tenant, device.ID).Scan(&desired, &generation); err != nil {
		t.Fatal(err)
	}
	var event struct {
		Snapshot runtimeConfigurationSnapshot `json:"runtimeConfiguration"`
	}
	if json.Unmarshal(desired, &event) != nil || event.Snapshot.Generation != generation || event.Snapshot.Digest == "" {
		t.Fatalf("invalid frozen event: %s", desired)
	}
	_, digest, err := canonicalRuntimeConfiguration(event.Snapshot.Configuration)
	if err != nil || digest != event.Snapshot.Digest {
		t.Fatal("JSONB changed snapshot digest")
	}
	if needed, err := runtimeConfigurationNeedsSyncTx(ctx, tx, tenant, device.ID); err != nil || needed {
		t.Fatalf("duplicate pending event: %v %v", needed, err)
	}
	if _, err := freezeRuntimeConfigurationTx(ctx, tx, testTenantID(), device.ID); err == nil {
		t.Fatal("cross tenant resolved")
	}
	if _, err = tx.Exec(ctx, `UPDATE device_access_templates SET configuration=jsonb_set(configuration,'{access,onlinePolicy,timeoutSeconds}','90'::jsonb) WHERE tenant_id=$1 AND id=$2`, tenant, template.ID); err != nil {
		t.Fatal(err)
	}
	if needed, err := runtimeConfigurationNeedsSyncTx(ctx, tx, tenant, device.ID); err != nil || !needed {
		t.Fatalf("changed configuration not scheduled: %v %v", needed, err)
	}
	var retained []byte
	if err = tx.QueryRow(ctx, `SELECT desired FROM device_runtime_outbox WHERE tenant_id=$1 AND device_id=$2 AND kind='template_binding'`, tenant, device.ID).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	var oldEvent string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM device_runtime_outbox WHERE tenant_id=$1 AND device_id=$2 AND generation=$3`, tenant, device.ID, generation).Scan(&oldEvent); err != nil {
		t.Fatal(err)
	}
	if err = enqueueTemplateRuntimeConfigurationsTx(ctx, tx, tenant, "tester", template.ID, "next-request"); err != nil {
		t.Fatal(err)
	}
	oldDigest, err := runtimeDesiredDigest(desired)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = recordRuntimeReceiptTx(ctx, tx, runtimeReceipt{EventID: oldEvent, TenantID: tenant, DeviceID: device.ID, Generation: generation, ContentDigest: oldDigest, State: runtimeReceiptApplied, RequestID: "old-receipt"}); err != nil {
		t.Fatal(err)
	}
	var pending bool
	if err = tx.QueryRow(ctx, `SELECT pending_operation_id IS NOT NULL FROM device_access_bindings WHERE tenant_id=$1 AND device_id=$2`, tenant, device.ID).Scan(&pending); err != nil || !pending {
		t.Fatalf("old receipt cleared newer operation: %v %v", pending, err)
	}
	if _, err = tx.Exec(ctx, `UPDATE device_access_templates SET configuration=jsonb_set(configuration,'{access,protocol}','"http"'::jsonb) WHERE tenant_id=$1 AND id=$2`, tenant, template.ID); err != nil {
		t.Fatal(err)
	}
	if unsupported, err := freezeRuntimeConfigurationTx(ctx, tx, tenant, device.ID); err != nil || unsupported != nil {
		t.Fatalf("non MQTT was opted into snapshot: %v %v", unsupported, err)
	}
	if string(retained) != string(desired) {
		t.Fatal("template update mutated prior frozen event")
	}
}

func assertFrozenRuntimeModelMatchesDevice(t *testing.T, ctx context.Context, tenant, deviceID string) {
	t.Helper()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	device, err := loadNativeDeviceEncoreTx(ctx, tx, tenant, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := freezeRuntimeConfigurationTx(ctx, tx, tenant, deviceID)
	if err != nil || snapshot == nil {
		t.Fatalf("snapshot: %v %v", snapshot, err)
	}
	var config struct {
		Model *ThingModelBinding `json:"model"`
	}
	if err = json.Unmarshal(snapshot.Configuration, &config); err != nil {
		t.Fatal(err)
	}
	if config.Model == nil || device.ModelBinding == nil || config.Model.ModelID != device.ModelBinding.ModelID || config.Model.SnapshotID != device.ModelBinding.SnapshotID || config.Model.PublishedRevision != device.ModelBinding.PublishedRevision {
		t.Fatalf("frozen model disagrees with device: %+v %+v", config.Model, device.ModelBinding)
	}
}
