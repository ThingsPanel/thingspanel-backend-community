package devicemetadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

func nativeWriteIdentity(tenantID string) nativeDeviceWriteIdentity {
	return nativeDeviceWriteIdentity{
		nativeDeviceReadIdentity: nativeDeviceReadIdentity{tenantID: tenantID},
		subjectID:                "native-device-write-test",
		canWriteGroups:           true,
	}
}

func nativeWriteBody(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func nativeCreateBody(number, name string) []byte {
	body, _ := json.Marshal(map[string]any{"name": name, "deviceNumber": number, "deviceKind": "standalone"})
	return body
}

func nativeWriteDevice(t *testing.T, result *metadataWriteResponse) NativeDevice {
	t.Helper()
	var response NativeDeviceResponse
	if err := json.Unmarshal(result.Body, &response); err != nil {
		t.Fatalf("decode write response: %v", err)
	}
	return response.Data
}

func cleanupNativeWriteTenants(t *testing.T, ctx context.Context, tenants ...string) {
	t.Helper()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Errorf("begin native write cleanup: %v", err)
		return
	}
	defer tx.Rollback()
	for _, tenant := range tenants {
		for _, table := range []string{"metadata_audit_events", "metadata_idempotency", "device_group_members", "gateway_memberships", "device_access_bindings", "device_groups", "devices", "products"} {
			if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant); err != nil {
				t.Errorf("cleanup %s tenant %s: %v", table, tenant, err)
				return
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("commit native write cleanup: %v", err)
	}
}

func insertNativeWriteRoot(t *testing.T, ctx context.Context, tenantID, id, name string) {
	t.Helper()
	if _, err := db.Exec(ctx, `INSERT INTO device_groups(id,tenant_id,name,created_by,updated_by) VALUES($1,$2,$3,'native-write-test','native-write-test')`, id, tenantID, name); err != nil {
		t.Fatalf("insert root group: %v", err)
	}
}

func TestCreateNativeDeviceWriteReceiptAndUnconfiguredProjection(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	t.Cleanup(func() { cleanupNativeWriteTenants(t, ctx, tenantA, tenantB) })
	identityA, identityB := nativeWriteIdentity(tenantA), nativeWriteIdentity(tenantB)
	key := "native-create-" + uuid.NewString()
	body := nativeCreateBody("shared-number", "Device A")
	first, err := createNativeDevice(ctx, identityA, key, body, "write-create-a", nil)
	if err != nil {
		t.Fatalf("create device: %v", err)
	}
	if first.Status != 201 || first.Location == "" || first.ETag == "" {
		t.Fatalf("create response status/location/etag = %d/%q/%q", first.Status, first.Location, first.ETag)
	}
	created := nativeWriteDevice(t, first)
	if created.ResourceVersion != 1 || created.ActivationState != "inactive" || created.AdministrativeState != "enabled" || created.TemplateID != nil || created.ModelBinding != nil {
		t.Fatalf("unexpected unconfigured create projection: %+v", created)
	}
	if created.DeviceNumber != "shared-number" {
		t.Fatalf("provided device number changed: got %q", created.DeviceNumber)
	}
	var legacyTemplate, legacySN, voucher any
	var bindingCount, deviceCount, auditCount, receiptCount int
	if err := db.QueryRow(ctx, `SELECT device_template_id,sn,voucher FROM devices WHERE tenant_id=$1 AND id=$2`, tenantA, created.ID).Scan(&legacyTemplate, &legacySN, &voucher); err != nil {
		t.Fatal(err)
	}
	if legacyTemplate != nil || legacySN != nil || voucher != nil {
		t.Fatalf("native create populated legacy columns: template=%v sn=%v voucher=%v", legacyTemplate, legacySN, voucher)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM device_access_bindings WHERE tenant_id=$1 AND device_id=$2 AND template_id IS NULL AND direct_access IS NULL AND device_parameters='{}'::jsonb`, tenantA, created.ID).Scan(&bindingCount); err != nil {
		t.Fatal(err)
	}
	replayed, err := createNativeDevice(ctx, identityA, key, body, "write-create-replay", nil)
	if err != nil {
		t.Fatalf("replay create: %v", err)
	}
	if !replayed.Replayed || nativeWriteDevice(t, replayed).ID != created.ID || replayed.ETag != first.ETag {
		t.Fatalf("replay did not reproduce original result: replay=%+v first=%+v", replayed, first)
	}
	if _, err := createNativeDevice(ctx, identityA, key, nativeCreateBody("shared-number", "Different body"), "write-create-conflict", nil); errs.Code(err) != errs.AlreadyExists {
		t.Fatalf("same key with different body error=%v, want AlreadyExists", err)
	}
	otherTenant, err := createNativeDevice(ctx, identityB, "native-create-"+uuid.NewString(), nativeCreateBody("shared-number", "Device B"), "write-create-b", nil)
	if err != nil {
		t.Fatalf("same device number in another tenant: %v", err)
	}
	if _, err := createNativeDevice(ctx, identityA, "native-create-"+uuid.NewString(), nativeCreateBody("shared-number", "Duplicate A"), "write-create-duplicate", nil); errs.Code(err) != errs.AlreadyExists {
		t.Fatalf("same-tenant duplicate number error=%v, want AlreadyExists", err)
	}
	for _, check := range []struct {
		query string
		count *int
	}{{`SELECT count(*) FROM devices WHERE tenant_id=$1 AND device_number='shared-number'`, &deviceCount}, {`SELECT count(*) FROM metadata_audit_events WHERE tenant_id=$1 AND resource_kind='device'`, &auditCount}, {`SELECT count(*) FROM metadata_idempotency WHERE tenant_id=$1 AND operation_name='createDevice'`, &receiptCount}} {
		if err := db.QueryRow(ctx, check.query, tenantA).Scan(check.count); err != nil {
			t.Fatal(err)
		}
	}
	if bindingCount != 1 || deviceCount != 1 || auditCount != 1 || receiptCount != 1 {
		t.Fatalf("tenant A persisted duplicate mutation: binding/device/audit/receipt=%d/%d/%d/%d", bindingCount, deviceCount, auditCount, receiptCount)
	}
	if nativeWriteDevice(t, otherTenant).TenantID != tenantB {
		t.Fatal("tenant B create returned a different tenant")
	}
}

func TestCreateNativeDeviceDefaultsOmittedNumberToIDAndReplaysSameValue(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	t.Cleanup(func() { cleanupNativeWriteTenants(t, ctx, tenant) })
	identity := nativeWriteIdentity(tenant)
	key := "native-create-default-number-" + uuid.NewString()
	body := nativeWriteBody(t, map[string]any{"name": "Device without supplied number", "deviceKind": "standalone"})
	first, err := createNativeDevice(ctx, identity, key, body, "write-create-default-number", nil)
	if err != nil {
		t.Fatalf("create with omitted deviceNumber: %v", err)
	}
	created := nativeWriteDevice(t, first)
	if created.ID == "" || created.DeviceNumber != created.ID {
		t.Fatalf("omitted device number should default to generated ID: id=%q number=%q", created.ID, created.DeviceNumber)
	}
	replayed, err := createNativeDevice(ctx, identity, key, body, "write-create-default-number-replay", nil)
	if err != nil {
		t.Fatalf("replay omitted-number create: %v", err)
	}
	replayDevice := nativeWriteDevice(t, replayed)
	if !replayed.Replayed || replayDevice.ID != created.ID || replayDevice.DeviceNumber != created.DeviceNumber {
		t.Fatalf("omitted-number replay changed generated identity: first=%+v replay=%+v", created, replayDevice)
	}
}

func TestCreateNativeDeviceGroupsGatewayAndConfiguredFailClosed(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantA, tenantB, tenantMulti := testTenantID(), testTenantID(), testTenantID()
	t.Cleanup(func() { cleanupNativeWriteTenants(t, ctx, tenantA, tenantB, tenantMulti) })
	rootA, rootB, rootMulti1, rootMulti2 := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	insertNativeWriteRoot(t, ctx, tenantA, rootA, "write-root-a")
	insertNativeWriteRoot(t, ctx, tenantB, rootB, "write-root-b")
	insertNativeWriteRoot(t, ctx, tenantMulti, rootMulti1, "write-root-multi-1")
	insertNativeWriteRoot(t, ctx, tenantMulti, rootMulti2, "write-root-multi-2")
	identityA := nativeWriteIdentity(tenantA)
	defaultKey := "native-group-default-" + uuid.NewString()
	defaultBody := nativeCreateBody("root-default", "Root default")
	defaulted, err := createNativeDevice(ctx, identityA, defaultKey, defaultBody, "write-default-root", nil)
	if err != nil {
		t.Fatal(err)
	}
	defaultDevice := nativeWriteDevice(t, defaulted)
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM device_group_members WHERE tenant_id=$1 AND group_id=$2 AND device_id=$3`, tenantA, rootA, defaultDevice.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("single-root default membership count=%d err=%v", count, err)
	}
	var groupVersion int64
	if err := db.QueryRow(ctx, `SELECT resource_version FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenantA, rootA).Scan(&groupVersion); err != nil || groupVersion != 2 {
		t.Fatalf("root version=%d err=%v, want 2", groupVersion, err)
	}
	var auditBefore, auditAfter int64
	var auditChanges []byte
	if err := db.QueryRow(ctx, `SELECT before_version,after_version,redacted_changes FROM metadata_audit_events
		WHERE tenant_id=$1 AND operation_name='createDevice' AND resource_kind='device_group' AND resource_id=$2`, tenantA, rootA).Scan(&auditBefore, &auditAfter, &auditChanges); err != nil {
		t.Fatalf("read initial group audit: %v", err)
	}
	var decodedAudit struct {
		AddedDeviceIDs []string `json:"addedDeviceIds"`
	}
	if err := json.Unmarshal(auditChanges, &decodedAudit); err != nil {
		t.Fatalf("decode initial group audit: %v", err)
	}
	if auditBefore != 1 || auditAfter != 2 || len(decodedAudit.AddedDeviceIDs) != 1 || decodedAudit.AddedDeviceIDs[0] != defaultDevice.ID {
		t.Fatalf("group audit before=%d after=%d changes=%s", auditBefore, auditAfter, auditChanges)
	}
	var groupAuditCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM metadata_audit_events WHERE tenant_id=$1 AND operation_name='createDevice' AND resource_kind='device_group' AND resource_id=$2`, tenantA, rootA).Scan(&groupAuditCount); err != nil || groupAuditCount != 1 {
		t.Fatalf("group audit count before replay=%d err=%v", groupAuditCount, err)
	}
	defaultReplay, err := createNativeDevice(ctx, identityA, defaultKey, defaultBody, "write-default-root-replay", nil)
	if err != nil {
		t.Fatal(err)
	}
	if nativeWriteDevice(t, defaultReplay).ID != defaultDevice.ID {
		t.Fatal("replayed default-root create returned a different device")
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM device_group_members WHERE tenant_id=$1 AND group_id=$2 AND device_id=$3`, tenantA, rootA, defaultDevice.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("replayed create changed default membership count=%d err=%v", count, err)
	}
	if err := db.QueryRow(ctx, `SELECT resource_version FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenantA, rootA).Scan(&groupVersion); err != nil || groupVersion != 2 {
		t.Fatalf("replayed create changed root version=%d err=%v, want 2", groupVersion, err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM metadata_audit_events WHERE tenant_id=$1 AND operation_name='createDevice' AND resource_kind='device_group' AND resource_id=$2`, tenantA, rootA).Scan(&groupAuditCount); err != nil || groupAuditCount != 1 {
		t.Fatalf("replay duplicated group audit count=%d err=%v", groupAuditCount, err)
	}
	emptyBody := nativeWriteBody(t, map[string]any{"name": "Explicit empty", "deviceNumber": "no-group", "deviceKind": "standalone", "initialGroupIds": []string{}})
	noGroup, err := createNativeDevice(ctx, identityA, "native-group-empty-"+uuid.NewString(), emptyBody, "write-empty-groups", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM device_group_members WHERE tenant_id=$1 AND device_id=$2`, tenantA, nativeWriteDevice(t, noGroup).ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("explicit empty groups count=%d err=%v", count, err)
	}
	identityMulti := nativeWriteIdentity(tenantMulti)
	noDefault, err := createNativeDevice(ctx, identityMulti, "native-group-multiple-"+uuid.NewString(), nativeCreateBody("multiple-roots", "No default"), "write-multiple-roots", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM device_group_members WHERE tenant_id=$1 AND device_id=$2`, tenantMulti, nativeWriteDevice(t, noDefault).ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("multiple roots unexpectedly defaulted membership count=%d err=%v", count, err)
	}
	beforeDevices := countTenantRows(t, ctx, "devices", tenantA)
	mixed := nativeWriteBody(t, map[string]any{"name": "Mixed group", "deviceNumber": "mixed-group", "deviceKind": "standalone", "initialGroupIds": []string{rootA, rootB}})
	if _, err := createNativeDevice(ctx, identityA, "native-group-mixed-"+uuid.NewString(), mixed, "write-mixed-groups", nil); errs.Code(err) != errs.NotFound {
		t.Fatalf("mixed-tenant group error=%v, want NotFound", err)
	}
	if got := countTenantRows(t, ctx, "devices", tenantA); got != beforeDevices {
		t.Fatalf("mixed group failure left device row: before=%d after=%d", beforeDevices, got)
	}
	var failedReceipt int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM metadata_idempotency WHERE tenant_id=$1 AND idempotency_key LIKE 'native-group-mixed-%'`, tenantA).Scan(&failedReceipt); err != nil || failedReceipt != 0 {
		t.Fatalf("mixed group failure left receipt count=%d err=%v", failedReceipt, err)
	}

	gatewayBody := nativeWriteBody(t, map[string]any{"name": "Gateway", "deviceNumber": "gateway", "deviceKind": "gateway", "initialGroupIds": []string{}})
	gateway, err := createNativeDevice(ctx, identityA, "native-gateway-"+uuid.NewString(), gatewayBody, "write-gateway", nil)
	if err != nil {
		t.Fatal(err)
	}
	gatewayID := nativeWriteDevice(t, gateway).ID
	childBody := nativeWriteBody(t, map[string]any{"name": "Child", "deviceNumber": "gateway-child", "deviceKind": "subdevice", "gatewayId": gatewayID, "subdeviceAddress": "child-1", "initialGroupIds": []string{}})
	child, err := createNativeDevice(ctx, identityA, "native-child-"+uuid.NewString(), childBody, "write-child", nil)
	if err != nil {
		t.Fatalf("create gateway child: %v", err)
	}
	childID := nativeWriteDevice(t, child).ID
	if err := db.QueryRow(ctx, `SELECT count(*) FROM gateway_memberships WHERE tenant_id=$1 AND gateway_device_id=$2 AND child_device_id=$3`, tenantA, gatewayID, childID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("gateway relation count=%d err=%v", count, err)
	}
	duplicateAddressBody := nativeWriteBody(t, map[string]any{"name": "Duplicate address", "deviceNumber": "gateway-child-duplicate", "deviceKind": "subdevice", "gatewayId": gatewayID, "subdeviceAddress": "child-1", "initialGroupIds": []string{}})
	beforeDevices = countTenantRows(t, ctx, "devices", tenantA)
	if _, err := createNativeDevice(ctx, identityA, "native-child-duplicate-address-"+uuid.NewString(), duplicateAddressBody, "write-child-duplicate-address", nil); errs.Code(err) != errs.AlreadyExists {
		t.Fatalf("duplicate gateway address error=%v, want AlreadyExists", err)
	}
	if got := countTenantRows(t, ctx, "devices", tenantA); got != beforeDevices {
		t.Fatalf("duplicate gateway address left device: before=%d after=%d", beforeDevices, got)
	}
	foreignGateway, err := createNativeDevice(ctx, nativeWriteIdentity(tenantB), "native-gateway-foreign-"+uuid.NewString(), nativeCreateBody("foreign-gateway", "Foreign gateway"), "write-foreign-gateway", nil)
	if err != nil {
		t.Fatal(err)
	}
	foreignBody := nativeWriteBody(t, map[string]any{"name": "Foreign child", "deviceNumber": "foreign-child", "deviceKind": "subdevice", "gatewayId": nativeWriteDevice(t, foreignGateway).ID, "subdeviceAddress": "child-2", "initialGroupIds": []string{}})
	beforeDevices = countTenantRows(t, ctx, "devices", tenantA)
	if _, err := createNativeDevice(ctx, identityA, "native-child-foreign-"+uuid.NewString(), foreignBody, "write-cross-tenant-gateway", nil); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant gateway error=%v, want NotFound", err)
	}
	if got := countTenantRows(t, ctx, "devices", tenantA); got != beforeDevices {
		t.Fatalf("cross-tenant gateway failure left device row: before=%d after=%d", beforeDevices, got)
	}
	direct := map[string]any{"name": "Configured", "deviceNumber": "configured", "deviceKind": "standalone", "initialGroupIds": []string{}, "directAccess": map[string]any{"access": map[string]any{"mode": "direct", "protocol": "MQTT", "providerId": uuid.NewString(), "providerVersion": "1", "schemaDigest": fmt.Sprintf("%064d", 0), "parameters": map[string]any{}, "credentialPolicy": map[string]any{"kind": "none", "autoRegister": false}, "onlinePolicy": map[string]any{"mode": "transport", "timeoutSeconds": 60}, "connectionDirection": "device_to_platform", "serviceInstanceId": nil}, "externalDeviceId": nil}}
	beforeDevices = countTenantRows(t, ctx, "devices", tenantA)
	failedKey := "native-config-unavailable-" + uuid.NewString()
	if _, err := createNativeDevice(ctx, identityA, failedKey, nativeWriteBody(t, direct), "write-config-unavailable", nil); errs.Code(err) != errs.Unavailable {
		t.Fatalf("configured create with absent validator error=%v, want Unavailable (HTTP handler maps this to 503)", err)
	}
	if got := countTenantRows(t, ctx, "devices", tenantA); got != beforeDevices {
		t.Fatalf("unavailable configuration validator left device: before=%d after=%d", beforeDevices, got)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM metadata_idempotency WHERE tenant_id=$1 AND idempotency_key=$2`, tenantA, failedKey).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unavailable configuration validator left receipt count=%d err=%v", count, err)
	}
}

func TestUpdateNativeDevicePatchAndFailClosedScope(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	t.Cleanup(func() { cleanupNativeWriteTenants(t, ctx, tenant) })
	identity := nativeWriteIdentity(tenant)
	created, err := createNativeDevice(ctx, identity, "native-update-create-"+uuid.NewString(), nativeCreateBody("patch-target", "Patch target"), "write-patch-create", nil)
	if err != nil {
		t.Fatal(err)
	}
	device := nativeWriteDevice(t, created)
	patch := func(key, etag, raw string) (*metadataWriteResponse, error) {
		return updateNativeDevice(ctx, identity, device.ID, key, etag, []byte(raw), "write-patch-"+key)
	}
	updated, err := patch("native-update-description-1", created.ETag, `{"description":""}`)
	if err != nil {
		t.Fatal(err)
	}
	device = nativeWriteDevice(t, updated)
	if device.Description == nil || *device.Description != "" || device.ResourceVersion != 2 {
		t.Fatalf("empty description patch = description:%v version:%d", device.Description, device.ResourceVersion)
	}
	cleared, err := patch("native-update-description-2", updated.ETag, `{"description":null}`)
	if err != nil {
		t.Fatal(err)
	}
	device = nativeWriteDevice(t, cleared)
	if device.Description != nil || device.ResourceVersion != 3 {
		t.Fatalf("null description patch = description:%v version:%d", device.Description, device.ResourceVersion)
	}
	unchanged, err := patch("native-update-empty", cleared.ETag, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if nativeWriteDevice(t, unchanged).ResourceVersion != 3 {
		t.Fatal("empty patch unexpectedly incremented resource version")
	}
	sameValues, err := patch("native-update-same", unchanged.ETag, `{"labels":{},"tags":[],"customFields":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	if nativeWriteDevice(t, sameValues).ResourceVersion != 3 {
		t.Fatal("same JSON values unexpectedly incremented resource version")
	}
	replaced, err := patch("native-update-replace", sameValues.ETag, `{"labels":{"area":"north"},"tags":["field"]}`)
	if err != nil {
		t.Fatal(err)
	}
	replacedDevice := nativeWriteDevice(t, replaced)
	var labels map[string]string
	if err := json.Unmarshal(replacedDevice.Labels, &labels); err != nil {
		t.Fatalf("decode replaced labels %s: %v", replacedDevice.Labels, err)
	}
	if replacedDevice.ResourceVersion != 4 || labels["area"] != "north" || len(labels) != 1 || len(replacedDevice.Tags) != 1 || replacedDevice.Tags[0] != "field" {
		t.Fatalf("labels/tags were not replaced atomically: %+v", replacedDevice)
	}
	if _, err := patch("native-update-stale", sameValues.ETag, `{"name":"stale"}`); errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("stale strong ETag error=%v, want FailedPrecondition (HTTP handler maps this to 412)", err)
	}
	if _, err := patch("native-update-readonly", replaced.ETag, `{"activationState":"active"}`); !isNativeInvalidInput(err) {
		t.Fatalf("readonly/unknown property error=%v, want NativeDeviceInputError", err)
	}
	limited := identity
	limited.deviceIDs = []string{uuid.NewString()}
	if _, err := updateNativeDevice(ctx, limited, device.ID, "native-update-out-of-scope", replaced.ETag, []byte(`{"name":"forbidden"}`), "write-out-of-scope"); errs.Code(err) != errs.NotFound {
		t.Fatalf("out-of-scope update error=%v, want NotFound", err)
	}
	var name string
	if err := db.QueryRow(ctx, `SELECT name FROM devices WHERE tenant_id=$1 AND id=$2`, tenant, device.ID).Scan(&name); err != nil || name != "Patch target" {
		t.Fatalf("out-of-scope update changed device name=%q err=%v", name, err)
	}
}

func TestNativeDeviceProductReferenceTenantFence(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	modelA, snapshotA := createPublishedThingModel(t, ctx, tenantA)
	modelB, snapshotB := createPublishedThingModel(t, ctx, tenantB)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantA, modelA.ID)
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantB, modelB.ID)
	})
	templateA := createPublishTestTemplate(t, ctx, tenantA, modelA.ID, snapshotA.ID, `{}`)
	templateB := createPublishTestTemplate(t, ctx, tenantB, modelB.ID, snapshotB.ID, `{}`)
	cleanupPublishTestTemplates(t, ctx, tenantA, templateA.ID)
	cleanupPublishTestTemplates(t, ctx, tenantB, templateB.ID)
	productA := createTestProduct(t, ctx, tenantA, templateA.ID)
	productB := createTestProduct(t, ctx, tenantB, templateB.ID)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM products WHERE tenant_id=$1 AND id=$2`, tenantA, productA.ID)
		_, _ = db.Exec(ctx, `DELETE FROM products WHERE tenant_id=$1 AND id=$2`, tenantB, productB.ID)
	})
	// Registered last so native device/binding/receipt/audit rows are removed
	// before products, templates, and the ThingModel fixtures.
	t.Cleanup(func() { cleanupNativeWriteTenants(t, ctx, tenantA, tenantB) })

	identityA := nativeWriteIdentity(tenantA)
	createWithProduct := func(productID string, key, number string) ([]byte, *metadataWriteResponse, error) {
		body := nativeWriteBody(t, map[string]any{"name": "Product device", "deviceNumber": number, "deviceKind": "standalone", "productId": productID, "initialGroupIds": []string{}})
		result, err := createNativeDevice(ctx, identityA, key, body, "write-product-"+key, nil)
		return body, result, err
	}
	_, created, err := createWithProduct(productA.ID, "native-product-same-"+uuid.NewString(), "product-same-tenant")
	if err != nil {
		t.Fatalf("same-tenant product reference: %v", err)
	}
	device := nativeWriteDevice(t, created)
	if device.ProductID == nil || *device.ProductID != productA.ID || device.TemplateID != nil || device.ModelBinding != nil {
		t.Fatalf("product reference unexpectedly bound access configuration: %+v", device)
	}
	beforeDevices := countTenantRows(t, ctx, "devices", tenantA)
	foreignKey := "native-product-cross-" + uuid.NewString()
	if _, _, err := createWithProduct(productB.ID, foreignKey, "product-cross-tenant"); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant product create error=%v, want NotFound", err)
	}
	if got := countTenantRows(t, ctx, "devices", tenantA); got != beforeDevices {
		t.Fatalf("cross-tenant product create left a device: before=%d after=%d", beforeDevices, got)
	}
	var receiptCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM metadata_idempotency WHERE tenant_id=$1 AND idempotency_key=$2`, tenantA, foreignKey).Scan(&receiptCount); err != nil || receiptCount != 0 {
		t.Fatalf("cross-tenant product create left receipt count=%d err=%v", receiptCount, err)
	}
	update := func(productID, key string) (*metadataWriteResponse, error) {
		body := nativeWriteBody(t, map[string]any{"productId": productID})
		return updateNativeDevice(ctx, identityA, device.ID, key, created.ETag, body, "write-product-update-"+key)
	}
	if _, err := update(productB.ID, "native-product-update-cross-"+uuid.NewString()); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant product update error=%v, want NotFound", err)
	}
	updated, err := update(productA.ID, "native-product-update-same-"+uuid.NewString())
	if err != nil {
		t.Fatalf("same-tenant product update: %v", err)
	}
	updatedDevice := nativeWriteDevice(t, updated)
	if updatedDevice.ProductID == nil || *updatedDevice.ProductID != productA.ID || updatedDevice.TemplateID != nil || updatedDevice.ResourceVersion != device.ResourceVersion || updated.ETag != created.ETag {
		t.Fatalf("same-tenant product patch changed unexpected fields: %+v", updatedDevice)
	}
	cleared, err := updateNativeDevice(ctx, identityA, device.ID, "native-product-clear-"+uuid.NewString(), updated.ETag, []byte(`{"productId":null}`), "write-product-clear")
	if err != nil {
		t.Fatal(err)
	}
	if got := nativeWriteDevice(t, cleared); got.ProductID != nil || got.ResourceVersion != device.ResourceVersion+1 {
		t.Fatalf("clearing product did not update the metadata version: %+v", got)
	}
	restoredBody := nativeWriteBody(t, map[string]any{"productId": productA.ID})
	restored, err := updateNativeDevice(ctx, identityA, device.ID, "native-product-restore-"+uuid.NewString(), cleared.ETag, restoredBody, "write-product-restore")
	if err != nil {
		t.Fatal(err)
	}
	if got := nativeWriteDevice(t, restored); got.ProductID == nil || *got.ProductID != productA.ID || got.ResourceVersion != device.ResourceVersion+2 || got.TemplateID != nil {
		t.Fatalf("restoring product did not update only the product association: %+v", got)
	}
}

func countTenantRows(t *testing.T, ctx context.Context, table, tenant string) int {
	t.Helper()
	allowed := map[string]bool{"devices": true, "metadata_audit_events": true, "metadata_idempotency": true, "device_group_members": true, "gateway_memberships": true, "device_access_bindings": true, "device_groups": true, "products": true}
	if !allowed[table] {
		t.Fatalf("unexpected table in fixture count: %s", table)
	}
	var count int
	if err := db.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s WHERE tenant_id=$1", table), tenant).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestValidateNativeCustomFieldsTxTypedAndHistoricalValues(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	definitions := json.RawMessage(`[{"key":"label","type":"string","enabled":true,"enumValues":[]},{"key":"count","type":"number","enabled":true,"enumValues":[]},{"key":"mode","type":"enum","enabled":true,"enumValues":[{"value":"safe","label":"Safe"}]},{"key":"legacy","type":"string","enabled":false,"enumValues":[]}]`)
	previous := json.RawMessage(`{"legacy":"historical"}`)
	if err := validateNativeCustomFieldsTx(ctx, tx, json.RawMessage(`{"label":"x","count":1.234567890123456789,"mode":"safe","legacy":"historical"}`), previous, definitions, true); err != nil {
		t.Fatalf("valid typed values and preserved disabled value: %v", err)
	}
	for _, values := range []string{`{"count":"1"}`, `{"mode":"unknown"}`, `{"missing":true}`, `{"legacy":"changed"}`, `{"legacy":null}`} {
		if err := validateNativeCustomFieldsTx(ctx, tx, json.RawMessage(values), previous, definitions, true); !isNativeInvalidInput(err) {
			t.Errorf("invalid custom fields %s error=%v, want NativeDeviceInputError", values, err)
		}
	}
	if err := validateNativeCustomFieldsTx(ctx, tx, json.RawMessage(`{"x":1}`), json.RawMessage(`{}`), json.RawMessage(`[]`), false); !isNativeInvalidInput(err) {
		t.Fatalf("custom fields without a template error=%v, want NativeDeviceInputError", err)
	}
}

func isNativeInvalidInput(err error) bool {
	var inputErr *NativeDeviceInputError
	return errors.As(err, &inputErr)
}

func TestNativeDeviceWriteRejectsInvalidTenantBeforeDatabase(t *testing.T) {
	identity := nativeWriteIdentity(uuid.Nil.String())
	if _, err := createNativeDevice(context.Background(), identity, "native-invalid-tenant", nativeCreateBody("invalid-tenant", "Invalid tenant"), "write-invalid-tenant", nil); errs.Code(err) != errs.Unauthenticated {
		t.Fatalf("zero tenant create error=%v, want Unauthenticated", err)
	}
	if _, err := updateNativeDevice(context.Background(), identity, uuid.NewString(), "native-invalid-tenant-update", `"etag"`, []byte(`{"name":"x"}`), "write-invalid-tenant-update"); errs.Code(err) != errs.Unauthenticated {
		t.Fatalf("zero tenant update error=%v, want Unauthenticated", err)
	}
}
