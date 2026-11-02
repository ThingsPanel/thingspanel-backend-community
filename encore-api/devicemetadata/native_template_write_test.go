package devicemetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"encore.app/devicemetadata/accessconfig"
	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

const nativeTemplateMQTTConfiguration = `{"deviceKind":"standalone","model":null,"access":{"mode":"direct","protocol":"mqtt","providerId":null,"parameters":{},"credentialPolicy":{"kind":"none","autoRegister":false},"onlinePolicy":{"mode":"transport","timeoutSeconds":60},"connectionDirection":"device_to_platform","serviceInstanceId":null},"topicMappings":[],"processingScripts":[],"customFieldDefinitions":[]}`

func nativeTemplateWriteTestIdentity(tenantID string) nativeTemplateWriteIdentity {
	return nativeTemplateWriteIdentity{nativeTemplateReadIdentity: nativeTemplateReadIdentity{tenantID: tenantID}, subjectID: "template-write-test"}
}

func nativeTemplateWriteTestBody(t *testing.T, name, configuration string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"name": name, "configuration": json.RawMessage(configuration)})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func nativeTemplateWriteTestResult(t *testing.T, result *metadataWriteResponse) NativeDeviceAccessTemplate {
	t.Helper()
	var response NativeDeviceAccessTemplateResponse
	if result == nil || json.Unmarshal(result.Body, &response) != nil || response.Data.ID == "" {
		t.Fatal("invalid template mutation response")
	}
	return response.Data
}

func TestNativeTemplateCreateDirectSaveReplayConflictAndCAS(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	identity := nativeTemplateWriteTestIdentity(tenantID)
	body := nativeTemplateWriteTestBody(t, "direct-save-template", nativeTemplateMQTTConfiguration)
	created, err := createNativeTemplate(ctx, identity, "template-create-001", body, "template-create-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatal(err)
	}
	template := nativeTemplateWriteTestResult(t, created)
	if created.Status != 201 || created.Location != "/api/v2/device-access-templates/"+template.ID || template.ResourceVersion != 1 || created.ETag == "" {
		t.Fatalf("unexpected create: %+v", created)
	}
	if strings.Contains(string(created.Body), "draft") || strings.Contains(string(created.Body), "latestPublishedRevision") || strings.Contains(string(created.Body), "imageUrl") {
		t.Fatal("withdrawn lifecycle or capability field in template response")
	}
	replay, err := createNativeTemplate(ctx, identity, "template-create-001", body, "different-request-id", validateNativeDeviceConfiguration, nil)
	if err != nil || !replay.Replayed || string(replay.Body) != string(created.Body) || replay.RequestID != created.RequestID {
		t.Fatalf("create replay changed: replay=%+v err=%v", replay, err)
	}
	if _, err := createNativeTemplate(ctx, identity, "template-create-001", nativeTemplateWriteTestBody(t, "changed-input", nativeTemplateMQTTConfiguration), "changed-request", validateNativeDeviceConfiguration, nil); errs.Code(err) != errs.AlreadyExists {
		t.Fatalf("different input must conflict: %v", err)
	}
	if _, err := createNativeTemplate(ctx, identity, "template-create-002", body, "duplicate-name", validateNativeDeviceConfiguration, nil); errs.Code(err) != errs.AlreadyExists {
		t.Fatalf("same-tenant duplicate name must conflict: %v", err)
	}
	patch := []byte(`{"name":"saved-now","description":"current configuration"}`)
	updated, err := updateNativeTemplate(ctx, identity, template.ID, "template-update-001", created.ETag, patch, "template-update-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatal(err)
	}
	saved := nativeTemplateWriteTestResult(t, updated)
	if saved.Name != "saved-now" || saved.ResourceVersion != 2 || updated.ETag == created.ETag || saved.Description == nil || *saved.Description != "current configuration" {
		t.Fatalf("direct update not visible: %+v", saved)
	}
	replayedUpdate, err := updateNativeTemplate(ctx, identity, template.ID, "template-update-001", created.ETag, patch, "different-update-request", validateNativeDeviceConfiguration, nil)
	if err != nil || !replayedUpdate.Replayed || string(replayedUpdate.Body) != string(updated.Body) {
		t.Fatalf("patch replay changed: %+v %v", replayedUpdate, err)
	}
	if _, err := updateNativeTemplate(ctx, identity, template.ID, "template-stale-001", created.ETag, []byte(`{"name":"overwrite"}`), "stale-request", validateNativeDeviceConfiguration, nil); errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("stale patch must fail: %v", err)
	}
	noOp, err := updateNativeTemplate(ctx, identity, template.ID, "template-noop-001", updated.ETag, []byte(`{}`), "noop-request", validateNativeDeviceConfiguration, nil)
	if err != nil || nativeTemplateWriteTestResult(t, noOp).ResourceVersion != 2 || noOp.ETag != updated.ETag {
		t.Fatalf("empty patch changed resource: %+v %v", noOp, err)
	}
	changedKind, _ := json.Marshal(map[string]json.RawMessage{"configuration": json.RawMessage(strings.Replace(nativeTemplateMQTTConfiguration, `"deviceKind":"standalone"`, `"deviceKind":"gateway"`, 1))})
	if _, err := updateNativeTemplate(ctx, identity, template.ID, "template-kind-001", updated.ETag, changedKind, "kind-change-request", validateNativeDeviceConfiguration, nil); err == nil {
		t.Fatal("template editing must preserve the existing device kind")
	}
	var auditCount, receiptCount, failedReceipts int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM metadata_audit_events WHERE tenant_id=$1 AND resource_id=$2`, tenantID, template.ID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM metadata_idempotency WHERE tenant_id=$1 AND state='completed'`, tenantID).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM metadata_idempotency WHERE tenant_id=$1 AND idempotency_key='template-stale-001'`, tenantID).Scan(&failedReceipts); err != nil {
		t.Fatal(err)
	}
	if auditCount != 3 || receiptCount != 3 || failedReceipts != 0 {
		t.Fatalf("mutation/replay audit boundary: audits=%d receipts=%d failed=%d", auditCount, receiptCount, failedReceipts)
	}
	if _, err := readNativeTemplate(ctx, nativeTemplateReadIdentity{tenantID: testTenantID()}, template.ID); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant read: %v", err)
	}
}

func TestNativeTemplateAssetReferenceValidationAndSafeReceipt(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	identity := nativeTemplateWriteTestIdentity(testTenantID())
	assetID := uuid.NewString()
	var body map[string]any
	_ = json.Unmarshal(nativeTemplateWriteTestBody(t, "template-cover", nativeTemplateMQTTConfiguration), &body)
	body["configuration"] = json.RawMessage(nativeTemplateMQTTConfiguration)
	body["imageAssetId"] = assetID
	encoded, _ := json.Marshal(body)
	if _, err := createNativeTemplate(ctx, identity, "asset-reference-001", encoded, "missing-asset-owner", validateNativeDeviceConfiguration, nil); errs.Code(err) != errs.Unavailable {
		t.Fatalf("missing asset owner must refuse write: %v", err)
	}
	called := 0
	check := func(_ context.Context, tenant, id string) error {
		called++
		if tenant != identity.tenantID || id != assetID {
			t.Fatal("asset owner received untrusted scope")
		}
		return nil
	}
	result, err := createNativeTemplate(ctx, identity, "asset-reference-002", encoded, "asset-reference-request", validateNativeDeviceConfiguration, check)
	if err != nil {
		t.Fatal(err)
	}
	template := nativeTemplateWriteTestResult(t, result)
	if called != 1 || template.ImageAssetID == nil || *template.ImageAssetID != assetID {
		t.Fatal("validated asset identity not retained")
	}
	var storedReceipt string
	if err := db.QueryRow(ctx, `SELECT response::text FROM metadata_idempotency WHERE tenant_id=$1 AND idempotency_key='asset-reference-002'`, identity.tenantID).Scan(&storedReceipt); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"imageUrl", "downloadUrl", "X-Amz-", "https://"} {
		if strings.Contains(storedReceipt, forbidden) {
			t.Fatalf("capability in metadata receipt: %s", forbidden)
		}
	}
}

func TestNativeTemplateCustomFieldChangesProtectLiveDeviceValues(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	identity := nativeTemplateWriteTestIdentity(tenantID)
	configuration := strings.Replace(nativeTemplateMQTTConfiguration, `"customFieldDefinitions":[]`, `"customFieldDefinitions":[
		{"key":"count","name":"Count","type":"number","defaultValue":0,"description":null,"enabled":true,"enumValues":[]},
		{"key":"flag","name":"Flag","type":"boolean","defaultValue":false,"description":null,"enabled":true,"enumValues":[]},
		{"key":"nullable","name":"Nullable","type":"string","defaultValue":null,"description":null,"enabled":true,"enumValues":[]},
		{"key":"mode","name":"Mode","type":"enum","defaultValue":"legacy","description":null,"enabled":true,"enumValues":[{"value":"legacy","label":"Legacy"},{"value":"safe","label":"Safe"}]},
		{"key":"vacant","name":"Vacant","type":"string","defaultValue":"","description":null,"enabled":true,"enumValues":[]}]`, 1)
	created, err := createNativeTemplate(ctx, identity, "custom-field-create-"+uuid.NewString(), nativeTemplateWriteTestBody(t, "custom-fields-"+uuid.NewString(), configuration), "custom-field-create-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatal(err)
	}
	template := nativeTemplateWriteTestResult(t, created)
	deviceBody := nativeWriteBody(t, map[string]any{
		"name": "custom-field-device-" + uuid.NewString(), "deviceNumber": "custom-field-device-" + uuid.NewString(),
		"deviceKind": "standalone", "templateId": template.ID, "initialGroupIds": []string{},
		"customFields": map[string]any{"count": 0, "flag": false, "nullable": nil, "mode": "legacy"},
	})
	if _, err := createNativeDevice(ctx, nativeWriteIdentity(tenantID), "custom-field-device-"+uuid.NewString(), deviceBody, "custom-field-device-request", validateNativeDeviceConfiguration); err != nil {
		t.Fatal(err)
	}
	current, err := readNativeTemplate(ctx, nativeTemplateReadIdentity{tenantID: tenantID}, template.ID)
	if err != nil {
		t.Fatal(err)
	}
	etag, err := nativeTemplateETag(current)
	if err != nil {
		t.Fatal(err)
	}
	var auditBefore int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM metadata_audit_events WHERE tenant_id=$1 AND resource_id=$2`, tenantID, template.ID).Scan(&auditBefore); err != nil {
		t.Fatal(err)
	}

	for i, changes := range []string{
		`{"upsert":[],"remove":["count"]}`,
		`{"upsert":[],"remove":["flag"]}`,
		`{"upsert":[],"remove":["nullable"]}`,
		`{"upsert":[{"key":"count","type":"boolean","defaultValue":false}],"remove":[]}`,
		`{"upsert":[{"key":"mode","defaultValue":"safe","enumValues":[{"value":"safe","label":"Safe"}]}],"remove":[]}`,
	} {
		body := []byte(`{"customFieldChanges":` + changes + `}`)
		conflictKey := fmt.Sprintf("custom-field-conflict-%d-%s", i, uuid.NewString())
		_, err := updateNativeTemplate(ctx, identity, template.ID, conflictKey, etag, body, "custom-field-conflict-request", validateNativeDeviceConfiguration, nil)
		if errs.Code(err) != errs.FailedPrecondition {
			t.Fatalf("incompatible field change %d error=%v, want conflict", i, err)
		}
		var persistedReceipt int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM metadata_idempotency WHERE tenant_id=$1 AND idempotency_key=$2`, tenantID, conflictKey).Scan(&persistedReceipt); err != nil || persistedReceipt != 0 {
			t.Fatalf("failed custom-field change persisted a receipt: count=%d err=%v", persistedReceipt, err)
		}
		for _, value := range []string{"legacy", "false"} {
			if strings.Contains(err.Error(), value) {
				t.Fatalf("conflict exposed stored device value %q: %v", value, err)
			}
		}
	}
	var auditAfter int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM metadata_audit_events WHERE tenant_id=$1 AND resource_id=$2`, tenantID, template.ID).Scan(&auditAfter); err != nil {
		t.Fatal(err)
	}
	if auditAfter != auditBefore {
		t.Fatalf("failed custom-field changes persisted audit events: before=%d after=%d", auditBefore, auditAfter)
	}

	// Presentation/default edits and disabling are safe; existing false and
	// null values remain untouched. Removing an unassigned field is also safe.
	compatible := []byte(`{"customFieldChanges":{"upsert":[
		{"key":"count","name":"Total","defaultValue":0.5,"description":"renamed"},
		{"key":"flag","enabled":false}],"remove":["vacant"]}}`)
	compatibleKey := "custom-field-compatible-" + uuid.NewString()
	updated, err := updateNativeTemplate(ctx, identity, template.ID, compatibleKey, etag, compatible, "custom-field-compatible-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatal(err)
	}
	updatedTemplate := nativeTemplateWriteTestResult(t, updated)
	var next accessconfig.Configuration
	if err := json.Unmarshal(updatedTemplate.Configuration, &next); err != nil {
		t.Fatal(err)
	}
	if len(next.CustomFieldDefinitions) != 4 || next.CustomFieldDefinitions[0].Name != "Total" || next.CustomFieldDefinitions[0].Enabled != true || string(next.CustomFieldDefinitions[0].DefaultValue) != "0.5" || next.CustomFieldDefinitions[1].Enabled {
		t.Fatalf("compatible custom-field changes not applied as requested: %+v", next.CustomFieldDefinitions)
	}
	newETag, err := nativeTemplateETag(&updatedTemplate)
	if err != nil {
		t.Fatal(err)
	}
	fullConfigurationWithoutValueField, _, _, err := mergeNativeCustomFieldChanges(updatedTemplate.Configuration, NativeCustomFieldChanges{Remove: []string{"nullable"}})
	if err != nil {
		t.Fatal(err)
	}
	fullConfigurationPatch, err := json.Marshal(map[string]json.RawMessage{"configuration": fullConfigurationWithoutValueField})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updateNativeTemplate(ctx, identity, template.ID, "custom-field-full-config-"+uuid.NewString(), newETag, fullConfigurationPatch, "custom-field-full-config-request", validateNativeDeviceConfiguration, nil); errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("complete configuration bypassed existing-value protection: %v", err)
	}
	replay, err := updateNativeTemplate(ctx, identity, template.ID, compatibleKey, etag, compatible, "custom-field-replay-request", validateNativeDeviceConfiguration, nil)
	if err != nil || !replay.Replayed || string(replay.Body) != string(updated.Body) {
		t.Fatalf("same-key/same-body replay changed: replay=%+v err=%v", replay, err)
	}
	if _, err := updateNativeTemplate(ctx, identity, template.ID, "custom-field-stale-"+uuid.NewString(), etag, []byte(`{"customFieldChanges":{"upsert":[{"key":"count","name":"stale"}],"remove":[]}}`), "custom-field-stale-request", validateNativeDeviceConfiguration, nil); errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("stale ETag update error=%v, want precondition failure", err)
	}

	// Re-enabling after a stored value has become invalid must also be blocked.
	_, err = updateNativeTemplate(ctx, identity, template.ID, "custom-field-invalid-enum-"+uuid.NewString(), newETag,
		[]byte(`{"customFieldChanges":{"upsert":[{"key":"mode","defaultValue":"safe","enumValues":[{"value":"safe","label":"Safe"}]}],"remove":[]}}`),
		"custom-field-invalid-enum-request", validateNativeDeviceConfiguration, nil)
	if errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("enum contraction with live legacy value error=%v, want conflict", err)
	}
}

func TestNativeDeviceCustomFieldWriteSerializesWithDefinitionRemoval(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	identity := nativeTemplateWriteTestIdentity(tenantID)
	configuration := strings.Replace(nativeTemplateMQTTConfiguration, `"customFieldDefinitions":[]`, `"customFieldDefinitions":[{"key":"race","name":"Race","type":"string","defaultValue":"","description":null,"enabled":true,"enumValues":[]}]`, 1)
	created, err := createNativeTemplate(ctx, identity, "custom-field-race-template-"+uuid.NewString(), nativeTemplateWriteTestBody(t, "custom-field-race-"+uuid.NewString(), configuration), "custom-field-race-create", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatal(err)
	}
	template := nativeTemplateWriteTestResult(t, created)
	deviceBody := nativeWriteBody(t, map[string]any{"name": "custom-field-race-device", "deviceNumber": "custom-field-race-" + uuid.NewString(), "deviceKind": "standalone", "templateId": template.ID, "initialGroupIds": []string{}})
	deviceResult, err := createNativeDevice(ctx, nativeWriteIdentity(tenantID), "custom-field-race-device-"+uuid.NewString(), deviceBody, "custom-field-race-device-request", validateNativeDeviceConfiguration)
	if err != nil {
		t.Fatal(err)
	}
	device := nativeWriteDevice(t, deviceResult)

	lockTx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := loadAccessTemplateConfigurationTx(ctx, lockTx, tenantID, template.ID, true)
	if err != nil {
		_ = lockTx.Rollback()
		t.Fatal(err)
	}
	changes, err := decodeNativeCustomFieldChanges([]byte(`{"upsert":[],"remove":["race"]}`))
	if err != nil {
		_ = lockTx.Rollback()
		t.Fatal(err)
	}
	merged, _, _, err := mergeNativeCustomFieldChanges(locked.Configuration, changes)
	if err != nil {
		_ = lockTx.Rollback()
		t.Fatal(err)
	}
	locked.Configuration = merged
	if _, err := saveAccessTemplateConfigurationTx(ctx, lockTx, tenantID, identity.subjectID, template.ID, locked.ResourceVersion, locked); err != nil {
		_ = lockTx.Rollback()
		t.Fatal(err)
	}

	type writeResult struct {
		response *metadataWriteResponse
		err      error
	}
	finished := make(chan writeResult, 1)
	go func() {
		response, err := updateNativeDevice(ctx, nativeWriteIdentity(tenantID), device.ID, "custom-field-race-write-"+uuid.NewString(), deviceResult.ETag,
			[]byte(`{"customFields":{"race":"value"}}`), "custom-field-race-write-request")
		finished <- writeResult{response: response, err: err}
	}()

	// Confirm the device patch is waiting on its template FOR SHARE lock before
	// making the definition removal visible.
	deadline := time.Now().Add(3 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock'
			AND query ILIKE '%device_access_templates%' AND query ILIKE '%FOR SHARE%')`).Scan(&waiting); err != nil {
			_ = lockTx.Rollback()
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case result := <-finished:
			_ = lockTx.Rollback()
			t.Fatalf("device write finished before waiting on template lock: %v", result.err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !waiting {
		_ = lockTx.Rollback()
		t.Fatal("device patch did not wait for the template FOR SHARE lock")
	}
	if err := lockTx.Commit(); err != nil {
		t.Fatal(err)
	}
	result := <-finished
	if result.err == nil {
		t.Fatal("device write accepted a custom field after its definition was removed")
	}
	readTx, err := beginTenantScopedSQLTx(ctx, tenantID, true)
	if err != nil {
		t.Fatal(err)
	}
	readback, err := loadNativeDeviceEncoreTx(ctx, readTx, tenantID, device.ID)
	if err != nil {
		_ = readTx.Rollback()
		t.Fatal(err)
	}
	if string(readback.CustomFields) != `{}` {
		t.Fatalf("rejected racing device write changed stored fields: %s", readback.CustomFields)
	}
	if err := readTx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeTemplateMutationProblemKeepsConditionalAndBusinessConflictsSeparate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"stale ETag", errNativeTemplateETagMismatch, http.StatusPreconditionFailed},
		{"unavailable model binding", &errs.Error{Code: errs.FailedPrecondition, Message: "model is archived"}, http.StatusConflict},
		{"custom field value conflict", nativeCustomFieldConflict(map[string]int{"mode": 1}), http.StatusConflict},
		{"invalid kind", invalid("configuration.deviceKind", "device kind cannot be changed"), http.StatusUnprocessableEntity},
		{"structural input", invalidStructure("duplicate property"), http.StatusBadRequest},
		{"unexpected storage failure", fmt.Errorf("private database credential detail"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPatch, "/api/v2/device-access-templates/test", nil)
			writeNativeTemplateMutationError(w, r, "template-problem-request", tc.err, true)
			if w.Code != tc.status || w.Header().Get("Content-Type") != "application/problem+json" || strings.Contains(w.Body.String(), "private database credential detail") {
				t.Fatalf("unsafe or incorrect template Problem: status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
