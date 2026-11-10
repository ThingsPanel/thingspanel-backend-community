package devicemetadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
)

func TestNativeDeviceTemplateBindingReadCurrentProjectionAndTenantScope(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	t.Cleanup(func() {
		cleanupNativeTemplateBindingMutableRows(t, ctx, tenantA)
		cleanupNativeTemplateBindingMutableRows(t, ctx, tenantB)
	})

	modelTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	modelID, snapshots := seedNativeTemplateBindingModel(t, ctx, modelTx, tenantA, "binding-read-model-", 7)
	if _, err := modelTx.Exec(ctx, `UPDATE thing_models SET published_revision=7,current_snapshot_id=$1 WHERE tenant_id=$2 AND id=$3`, snapshots[7], tenantA, modelID); err != nil {
		t.Fatalf("set initial published model version 7: %v", err)
	}
	if err := modelTx.Commit(); err != nil {
		t.Fatal(err)
	}

	templateCreate, err := createNativeTemplate(ctx, nativeTemplateWriteTestIdentity(tenantA), "binding-read-template-"+uuid.NewString(),
		nativeTemplateWriteTestBody(t, "binding-read-"+uuid.NewString(), nativeTemplateBindingConfiguration(modelID, 7)),
		"binding-read-template-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatalf("create model-bound template: %v", err)
	}
	template := nativeTemplateWriteTestResult(t, templateCreate)
	boundDevice := createNativeTemplateBoundDevice(t, ctx, tenantA, template.ID, "binding-read-bound")

	unconfiguredResult, err := createNativeDevice(ctx, nativeWriteIdentity(tenantA), "binding-read-unconfigured-"+uuid.NewString(),
		nativeCreateBody("binding-read-unconfigured-"+uuid.NewString(), "binding-read-unconfigured"), "binding-read-unconfigured-request", nil)
	if err != nil {
		t.Fatalf("create unconfigured device: %v", err)
	}
	unconfigured := nativeWriteDevice(t, unconfiguredResult)

	dataA := &authn.Data{UserID: "native-binding-read-a", TenantID: tenantA, Roles: []string{"TENANT_ADMIN"}}
	defer et.OverrideAuthInfo("", nil)
	et.OverrideAuthInfo(auth.UID(dataA.UserID), dataA)
	identityA, err := currentNativeDeviceReadIdentity()
	if err != nil {
		t.Fatalf("build tenant A read identity: %v", err)
	}

	plain, err := readNativeDeviceTemplateBinding(ctx, identityA, unconfigured.ID, "binding-read-plain")
	if err != nil {
		t.Fatalf("read unconfigured binding: %v", err)
	}
	if plain.Data.DeviceID != unconfigured.ID || plain.Data.TemplateID != nil || plain.Data.ModelBinding != nil ||
		plain.Data.ConfigurationGeneration != 1 || plain.Data.ResourceVersion != unconfigured.ResourceVersion ||
		plain.Data.DirectAccess != nil || string(plain.Data.DeviceParameters) != `{}` {
		t.Fatalf("unconfigured binding projection=%+v", plain.Data)
	}

	first, err := readNativeDeviceTemplateBinding(ctx, identityA, boundDevice.ID, "binding-read-bound")
	if err != nil {
		t.Fatalf("read template-bound device: %v", err)
	}
	if first.Data.TemplateID == nil || *first.Data.TemplateID != template.ID || first.Data.ModelBinding == nil ||
		first.Data.ModelBinding.ModelID != modelID || first.Data.ModelBinding.SnapshotID != snapshots[7] ||
		first.Data.ModelBinding.PublishedRevision != 7 || first.Data.DirectAccess != nil || string(first.Data.DeviceParameters) != `{}` {
		t.Fatalf("template/model binding projection=%+v", first.Data)
	}
	assertNativeDeviceTemplateBindingShape(t, first)

	// Advancing the ThingModel's published version alone must leave the explicit
	// version-7 selection stored on the template unchanged.
	if _, err := db.Exec(ctx, `UPDATE thing_models SET published_revision=8,current_snapshot_id=$1 WHERE tenant_id=$2 AND id=$3`, snapshots[8], tenantA, modelID); err != nil {
		t.Fatalf("publish ThingModel version 8: %v", err)
	}
	stillSelected, err := readNativeDeviceTemplateBinding(ctx, identityA, boundDevice.ID, "binding-read-after-model-publish")
	if err != nil || stillSelected.Data.ModelBinding == nil || stillSelected.Data.ModelBinding.PublishedRevision != 7 || stillSelected.ETag != first.ETag {
		t.Fatalf("publishing a model version moved template selection or changed binding ETag: binding=%+v err=%v", stillSelected, err)
	}

	// Changing the template's current selection changes the inherited device model.
	currentTemplate, err := readNativeTemplate(ctx, nativeTemplateReadIdentity{tenantID: tenantA}, template.ID)
	if err != nil {
		t.Fatal(err)
	}
	currentETag, err := nativeTemplateETag(currentTemplate)
	if err != nil {
		t.Fatal(err)
	}
	patch, err := json.Marshal(map[string]json.RawMessage{"configuration": json.RawMessage(nativeTemplateBindingConfiguration(modelID, 8))})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updateNativeTemplate(ctx, nativeTemplateWriteTestIdentity(tenantA), template.ID, "binding-read-template-edit-"+uuid.NewString(), currentETag, patch,
		"binding-read-template-edit-request", validateNativeDeviceConfiguration, nil); err != nil {
		t.Fatalf("update template model choice: %v", err)
	}
	second, err := readNativeDeviceTemplateBinding(ctx, identityA, boundDevice.ID, "binding-read-bound-after-template-edit")
	if err != nil || second.Data.ModelBinding == nil || second.Data.ModelBinding.SnapshotID != snapshots[8] || second.Data.ModelBinding.PublishedRevision != 8 {
		t.Fatalf("device did not inherit the current template model selection: binding=%+v err=%v", second, err)
	}
	if first.ETag == second.ETag {
		t.Fatalf("binding ETag did not change when the template model selection changed: %q", first.ETag)
	}

	foreignResult, err := createNativeDevice(ctx, nativeWriteIdentity(tenantB), "binding-read-foreign-"+uuid.NewString(),
		nativeCreateBody("binding-read-foreign-"+uuid.NewString(), "binding-read-foreign"), "binding-read-foreign-request", nil)
	if err != nil {
		t.Fatalf("create tenant B device: %v", err)
	}
	foreign := nativeWriteDevice(t, foreignResult)
	if _, err := readNativeDeviceTemplateBinding(ctx, identityA, foreign.ID, "binding-read-cross-tenant"); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant binding read error=%v, want NotFound", err)
	}
	scoped := identityA
	scoped.deviceIDs = []string{unconfigured.ID}
	if _, err := readNativeDeviceTemplateBinding(ctx, scoped, boundDevice.ID, "binding-read-out-of-scope"); errs.Code(err) != errs.NotFound {
		t.Fatalf("out-of-scope binding read error=%v, want NotFound", err)
	}
}

func TestNativeDeviceTemplateBindingReadDirectAccessPreservesNumbersWithoutSecrets(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	t.Cleanup(func() { cleanupNativeTemplateBindingMutableRows(t, ctx, tenantID) })

	const exactNumber = "12345678901234567890.123456789"
	access := `{"mode":"direct","protocol":"mqtt","providerId":null,"parameters":{"precise":` + exactNumber + `,"ratio":1e-7},"credentialPolicy":{"kind":"none","autoRegister":false},"onlinePolicy":{"mode":"transport","timeoutSeconds":60},"connectionDirection":"device_to_platform","serviceInstanceId":null}`
	direct := `{"access":` + access + `,"externalDeviceId":"external-binding-read"}`
	body := `{"name":"direct-binding-read","deviceNumber":"direct-binding-read-` + uuid.NewString() + `","deviceKind":"standalone","directAccess":` + direct + `,"deviceParameters":{}}`
	created, err := createNativeDevice(ctx, nativeWriteIdentity(tenantID), "binding-read-direct-"+uuid.NewString(), []byte(body), "binding-read-direct-request", validateNativeDeviceConfiguration)
	if err != nil {
		t.Fatalf("create direct-access device: %v", err)
	}
	device := nativeWriteDevice(t, created)
	binding, err := readNativeDeviceTemplateBinding(ctx, nativeDeviceReadIdentity{tenantID: tenantID}, device.ID, "binding-read-direct")
	if err != nil {
		t.Fatalf("read direct-access binding: %v", err)
	}
	if binding.Data.TemplateID != nil || binding.Data.DirectAccess == nil || string(binding.Data.DeviceParameters) != `{}` {
		t.Fatalf("direct binding projection=%+v", binding.Data)
	}
	encoded, err := json.Marshal(binding.Data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), exactNumber) || !strings.Contains(string(encoded), `"externalDeviceId":"external-binding-read"`) {
		t.Fatalf("direct access lost exact numeric or external identity: %s", encoded)
	}
	for _, forbidden := range []string{"password", "sentinel-secret", "imageUrl", "signature="} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("binding response exposed forbidden field/value %q: %s", forbidden, encoded)
		}
	}
	assertNativeDeviceTemplateBindingShape(t, binding)
}

func TestNativeDeviceTemplateBindingReadStoredConnectorWithoutWriteOwner(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	t.Cleanup(func() { cleanupNativeTemplateBindingMutableRows(t, ctx, tenantID) })

	created, err := createNativeDevice(ctx, nativeWriteIdentity(tenantID), "binding-read-connector-"+uuid.NewString(),
		nativeCreateBody("binding-read-connector-"+uuid.NewString(), "binding-read-connector"), "binding-read-connector-request", nil)
	if err != nil {
		t.Fatalf("create unconfigured device: %v", err)
	}
	device := nativeWriteDevice(t, created)
	const directAccess = `{"access":{"mode":"protocol_connector","protocol":"custom","providerId":"11111111-2222-4333-8444-555555555555","providerVersion":"v1","schemaDigest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","parameters":{"ratio":1.2500},"credentialPolicy":{"kind":"none","autoRegister":false},"onlinePolicy":{"mode":"transport","timeoutSeconds":60},"connectionDirection":"device_to_platform","serviceInstanceId":null},"externalDeviceId":null}`
	tx, err := beginTenantGroupTx(ctx, tenantID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE device_access_bindings SET direct_access=$3::jsonb,configuration_generation=2 WHERE tenant_id=$1 AND device_id=$2`, tenantID, device.ID, directAccess); err != nil {
		_ = tx.Rollback()
		t.Fatalf("seed previously stored connector configuration: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	binding, err := readNativeDeviceTemplateBinding(ctx, nativeDeviceReadIdentity{tenantID: tenantID}, device.ID, "binding-read-connector")
	if err != nil {
		t.Fatalf("read stored connector configuration without a connector write owner: %v", err)
	}
	if binding.Data.DirectAccess == nil || binding.Data.ConfigurationGeneration != 2 || !strings.Contains(string(*binding.Data.DirectAccess), `"mode":"protocol_connector"`) || !strings.Contains(string(*binding.Data.DirectAccess), `1.2500`) {
		t.Fatalf("stored connector binding projection=%+v", binding.Data)
	}
	assertNativeDeviceTemplateBindingShape(t, binding)
}

func TestGetNativeDeviceTemplateBindingRejectsUnknownQuery(t *testing.T) {
	defer et.OverrideAuthInfo("", nil)
	data := &authn.Data{UserID: "native-binding-read-query", TenantID: testTenantID(), Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/devices/"+uuid.NewString()+"/template-binding?tenantId="+uuid.NewString(), nil)
	response := httptest.NewRecorder()
	(&Service{}).GetDeviceTemplateBinding(response, req)
	var problem GroupProblem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil || response.Code != http.StatusBadRequest || problem.Code != errs.InvalidArgument.String() {
		t.Fatalf("unknown query status=%d body=%s problem=%+v err=%v", response.Code, response.Body.String(), problem, err)
	}
}

func assertNativeDeviceTemplateBindingShape(t *testing.T, response *NativeDeviceTemplateBindingResponse) {
	t.Helper()
	if !strings.HasPrefix(response.ETag, `"sha256-`) || !strings.HasSuffix(response.ETag, `"`) {
		t.Fatalf("binding ETag is not strong: %q", response.ETag)
	}
	encoded, err := json.Marshal(response.Data)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil || len(fields) != 7 {
		t.Fatalf("binding OAS fields=%d json=%s err=%v", len(fields), encoded, err)
	}
}
