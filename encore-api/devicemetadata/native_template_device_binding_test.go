package devicemetadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

func TestNativeTemplateModelSelectionIsInheritedByExistingAndNewDevices(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	fixtureTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer fixtureTx.Rollback()
	modelA, snapshotsA := seedNativeTemplateBindingModel(t, ctx, fixtureTx, tenantA, "template-binding-model-a-", 7)
	modelB, _ := seedNativeTemplateBindingModel(t, ctx, fixtureTx, tenantB, "template-binding-model-b-", 1)
	if _, err := fixtureTx.Exec(ctx, `UPDATE thing_models SET published_revision=7,current_snapshot_id=$1 WHERE tenant_id=$2 AND id=$3`, snapshotsA[7], tenantA, modelA); err != nil {
		t.Fatalf("set initial published model version 7: %v", err)
	}
	if err := fixtureTx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupNativeTemplateBindingMutableRows(t, ctx, tenantA) })

	identity := nativeTemplateWriteTestIdentity(tenantA)
	foreignName := "cross-tenant-template-" + uuid.NewString()
	foreignKey := "template-cross-model-" + uuid.NewString()
	foreignBody := nativeTemplateWriteTestBody(t, foreignName, nativeTemplateBindingConfiguration(modelB, 1))
	if _, err := createNativeTemplate(ctx, identity, foreignKey, foreignBody, "cross-tenant-template-request", validateNativeDeviceConfiguration, nil); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant ThingModel selection error=%v, want NotFound", err)
	}
	var foreignTemplateCount, foreignReceiptCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM device_access_templates WHERE tenant_id=$1 AND name=$2`, tenantA, foreignName).Scan(&foreignTemplateCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM metadata_idempotency WHERE tenant_id=$1 AND operation_name='createDeviceAccessTemplate' AND idempotency_key=$2`, tenantA, foreignKey).Scan(&foreignReceiptCount); err != nil {
		t.Fatal(err)
	}
	if foreignTemplateCount != 0 || foreignReceiptCount != 0 {
		t.Fatalf("cross-tenant model rejection persisted template/receipt=%d/%d", foreignTemplateCount, foreignReceiptCount)
	}

	created, err := createNativeTemplate(ctx, identity, "template-model-create-"+uuid.NewString(), nativeTemplateWriteTestBody(t, "snapshot-binding-"+uuid.NewString(), nativeTemplateBindingConfiguration(modelA, 7)), "template-model-create-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatalf("create current-configuration template at model version 7: %v", err)
	}
	template := nativeTemplateWriteTestResult(t, created)
	if template.ResourceVersion != 1 {
		t.Fatalf("new template resource version=%d, want 1", template.ResourceVersion)
	}

	firstDevice := createNativeTemplateBoundDevice(t, ctx, tenantA, template.ID, "bound-before-template-edit")
	assertNativeDeviceModelSnapshot(t, ctx, tenantA, firstDevice.ID, template.ID, modelA, snapshotsA[7], 7)
	currentTemplate, err := readNativeTemplate(ctx, nativeTemplateReadIdentity{tenantID: tenantA}, template.ID)
	if err != nil {
		t.Fatalf("read template after creating first device: %v", err)
	}
	if currentTemplate.ResourceVersion != 1 || currentTemplate.DeviceCount != 1 {
		t.Fatalf("device creation should change only the template representation count: resourceVersion=%d deviceCount=%d", currentTemplate.ResourceVersion, currentTemplate.DeviceCount)
	}
	currentETag, err := nativeTemplateETag(currentTemplate)
	if err != nil {
		t.Fatalf("compute current template ETag: %v", err)
	}
	if currentETag == created.ETag {
		t.Fatal("template ETag did not reflect the updated deviceCount representation")
	}

	// Publishing model version 8 alone must not move the template's explicit version-7 selection.
	if _, err := db.Exec(ctx, `UPDATE thing_models SET published_revision=8,current_snapshot_id=$1 WHERE tenant_id=$2 AND id=$3`, snapshotsA[8], tenantA, modelA); err != nil {
		t.Fatalf("publish ThingModel version 8: %v", err)
	}
	assertNativeDeviceModelSnapshot(t, ctx, tenantA, firstDevice.ID, template.ID, modelA, snapshotsA[7], 7)
	snapshotCount, snapshotFingerprint := nativeThingModelSnapshotFingerprint(t, ctx, tenantA, modelA)

	patch, err := json.Marshal(map[string]json.RawMessage{"configuration": json.RawMessage(nativeTemplateBindingConfiguration(modelA, 8))})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := updateNativeTemplate(ctx, identity, template.ID, "template-model-update-"+uuid.NewString(), currentETag, patch, "template-model-update-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatalf("update current template selection to ThingModel version 8: %v", err)
	}
	updatedTemplate := nativeTemplateWriteTestResult(t, updated)
	if updatedTemplate.ResourceVersion != 2 {
		t.Fatalf("updated template resource version=%d, want 2", updatedTemplate.ResourceVersion)
	}

	secondDevice := createNativeTemplateBoundDevice(t, ctx, tenantA, template.ID, "bound-after-template-edit")
	assertNativeDeviceModelSnapshot(t, ctx, tenantA, firstDevice.ID, template.ID, modelA, snapshotsA[8], 8)
	assertNativeDeviceModelSnapshot(t, ctx, tenantA, secondDevice.ID, template.ID, modelA, snapshotsA[8], 8)
	if afterCount, afterFingerprint := nativeThingModelSnapshotFingerprint(t, ctx, tenantA, modelA); afterCount != snapshotCount || afterFingerprint != snapshotFingerprint {
		t.Fatalf("editing template changed immutable ThingModel snapshots: before=%d/%s after=%d/%s", snapshotCount, snapshotFingerprint, afterCount, afterFingerprint)
	}

	readback, err := readNativeTemplate(ctx, nativeTemplateReadIdentity{tenantID: tenantA}, template.ID)
	if err != nil {
		t.Fatalf("read updated template: %v", err)
	}
	if readback.DeviceCount != 2 {
		t.Fatalf("template device count=%d, want 2", readback.DeviceCount)
	}
	currentETag, err = nativeTemplateETag(readback)
	if err != nil {
		t.Fatalf("compute template ETag before clearing model: %v", err)
	}
	clearPatch, err := json.Marshal(map[string]json.RawMessage{"configuration": json.RawMessage(nativeTemplateMQTTConfiguration)})
	if err != nil {
		t.Fatal(err)
	}
	cleared, err := updateNativeTemplate(ctx, identity, template.ID, "template-model-clear-"+uuid.NewString(), currentETag, clearPatch, "template-model-clear-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatalf("clear current template model selection: %v", err)
	}
	assertNativeDeviceHasNoTemplateModel(t, ctx, tenantA, firstDevice.ID, template.ID)
	if afterClearCount, afterClearFingerprint := nativeThingModelSnapshotFingerprint(t, ctx, tenantA, modelA); afterClearCount != snapshotCount || afterClearFingerprint != snapshotFingerprint {
		t.Fatalf("clearing template model changed immutable ThingModel snapshots: before=%d/%s after=%d/%s", snapshotCount, snapshotFingerprint, afterClearCount, afterClearFingerprint)
	}
	clearedTemplate := nativeTemplateWriteTestResult(t, cleared)
	clearedETag, err := nativeTemplateETag(&clearedTemplate)
	if err != nil {
		t.Fatal(err)
	}
	reselectPatch, err := json.Marshal(map[string]json.RawMessage{"configuration": json.RawMessage(nativeTemplateBindingConfiguration(modelA, 7))})
	if err != nil {
		t.Fatal(err)
	}
	_, err = updateNativeTemplate(ctx, identity, template.ID, "template-model-reselect-"+uuid.NewString(), clearedETag, reselectPatch, "template-model-reselect-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatalf("reselect ThingModel version 7: %v", err)
	}
	assertNativeDeviceModelSnapshot(t, ctx, tenantA, firstDevice.ID, template.ID, modelA, snapshotsA[7], 7)
	assertNativeDeviceModelSnapshot(t, ctx, tenantA, secondDevice.ID, template.ID, modelA, snapshotsA[7], 7)
	if afterReselectCount, afterReselectFingerprint := nativeThingModelSnapshotFingerprint(t, ctx, tenantA, modelA); afterReselectCount != snapshotCount || afterReselectFingerprint != snapshotFingerprint {
		t.Fatalf("reselecting template model changed immutable ThingModel snapshots: before=%d/%s after=%d/%s", snapshotCount, snapshotFingerprint, afterReselectCount, afterReselectFingerprint)
	}
	var revisionTable, draftColumn bool
	if err := db.QueryRow(ctx, `SELECT to_regclass('device_access_template_revisions') IS NOT NULL,
		EXISTS(SELECT FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='device_access_templates' AND column_name='draft')`).Scan(&revisionTable, &draftColumn); err != nil {
		t.Fatal(err)
	}
	if revisionTable || draftColumn {
		t.Fatalf("withdrawn template lifecycle schema remains: revision table=%v draft column=%v", revisionTable, draftColumn)
	}
}

func seedNativeTemplateBindingModel(t *testing.T, ctx context.Context, tx *sqldb.Tx, tenantID, namePrefix string, firstVersion int) (string, map[int]string) {
	t.Helper()
	snapshots := []revisionSchemaSnapshot{}
	for version := firstVersion; version <= firstVersion+1; version++ {
		snapshots = append(snapshots, revisionSchemaSnapshot{
			version: version, canonical: true, canonicalContent: []byte(`{"fields":[]}`),
			semanticDigest: strings.Repeat("a", 64), resourcesDigest: strings.Repeat("b", 64),
		})
	}
	modelID, snapshotIDs := seedRevisionSchemaModel(t, ctx, tx, tenantID, namePrefix, "PUBLISHED", snapshots)
	return modelID, snapshotIDs
}

func nativeTemplateBindingConfiguration(modelID string, revision int) string {
	selection := fmt.Sprintf(`{"modelId":%q,"publishedRevision":%d}`, modelID, revision)
	return strings.Replace(nativeTemplateMQTTConfiguration, `"model":null`, `"model":`+selection, 1)
}

func createNativeTemplateBoundDevice(t *testing.T, ctx context.Context, tenantID, templateID, label string) NativeDevice {
	t.Helper()
	identity := nativeWriteIdentity(tenantID)
	body := nativeWriteBody(t, map[string]any{
		"name": label, "deviceNumber": "template-snapshot-" + uuid.NewString(), "deviceKind": "standalone",
		"templateId": templateID, "initialGroupIds": []string{},
	})
	result, err := createNativeDevice(ctx, identity, "template-device-"+uuid.NewString(), body, label+"-request", validateNativeDeviceConfiguration)
	if err != nil {
		t.Fatalf("create template-bound device: %v", err)
	}
	device := nativeWriteDevice(t, result)
	if result.Status != 201 || device.TemplateID == nil || *device.TemplateID != templateID || device.ModelBinding == nil {
		t.Fatalf("device did not retain template/model binding: status=%d device=%+v", result.Status, device)
	}
	return device
}

func assertNativeDeviceModelSnapshot(t *testing.T, ctx context.Context, tenantID, deviceID, templateID, modelID, snapshotID string, revision int) {
	t.Helper()
	tx, err := beginTenantScopedSQLTx(ctx, tenantID, true)
	if err != nil {
		t.Fatalf("begin tenant-scoped binding read: %v", err)
	}
	defer tx.Rollback()
	device, err := loadNativeDeviceEncoreTx(ctx, tx, tenantID, deviceID)
	if err != nil {
		t.Fatalf("read bound device %s: %v", deviceID, err)
	}
	if device.TemplateID == nil || *device.TemplateID != templateID || device.ModelBinding == nil ||
		device.ModelBinding.ModelID != modelID || device.ModelBinding.SnapshotID != snapshotID || int(device.ModelBinding.PublishedRevision) != revision {
		t.Fatalf("device %s model binding=%+v template=%v, want model=%s snapshot=%s revision=%d", deviceID, device.ModelBinding, device.TemplateID, modelID, snapshotID, revision)
	}
	var actualTemplate, actualModel, actualSnapshot string
	var actualRevision int
	if err := tx.QueryRow(ctx, `SELECT t.id::text,t.thing_model_id::text,t.thing_model_snapshot_id::text,s.version
		FROM device_access_templates t JOIN thing_model_snapshots s
		ON s.tenant_id=t.tenant_id AND s.thing_model_id=t.thing_model_id AND s.id=t.thing_model_snapshot_id
		WHERE t.tenant_id=$1 AND t.id=$2`, tenantID, templateID).Scan(&actualTemplate, &actualModel, &actualSnapshot, &actualRevision); err != nil {
		t.Fatal(err)
	}
	if actualTemplate != templateID || actualModel != modelID || actualSnapshot != snapshotID || actualRevision != revision {
		t.Fatalf("stored template model selection=%s/%s/%s/v%d, want %s/%s/%s/v%d", actualTemplate, actualModel, actualSnapshot, actualRevision, templateID, modelID, snapshotID, revision)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tenant-scoped binding read: %v", err)
	}
}

func assertNativeDeviceHasNoTemplateModel(t *testing.T, ctx context.Context, tenantID, deviceID, templateID string) {
	t.Helper()
	tx, err := beginTenantScopedSQLTx(ctx, tenantID, true)
	if err != nil {
		t.Fatalf("begin tenant-scoped binding read: %v", err)
	}
	defer tx.Rollback()
	device, err := loadNativeDeviceEncoreTx(ctx, tx, tenantID, deviceID)
	if err != nil {
		t.Fatalf("read device %s after clearing template model: %v", deviceID, err)
	}
	if device.TemplateID == nil || *device.TemplateID != templateID || device.ModelBinding != nil {
		t.Fatalf("device model binding after clearing template selection=%+v template=%v", device.ModelBinding, device.TemplateID)
	}
	var actualTemplate string
	var modelID, snapshotID sql.NullString
	if err := tx.QueryRow(ctx, `SELECT id::text,thing_model_id::text,thing_model_snapshot_id::text FROM device_access_templates WHERE tenant_id=$1 AND id=$2`, tenantID, templateID).Scan(&actualTemplate, &modelID, &snapshotID); err != nil {
		t.Fatal(err)
	}
	if actualTemplate != templateID || modelID.Valid || snapshotID.Valid {
		t.Fatalf("cleared template model references=%s/%+v/%+v, want null model and snapshot", actualTemplate, modelID, snapshotID)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tenant-scoped binding read: %v", err)
	}
}

func nativeThingModelSnapshotFingerprint(t *testing.T, ctx context.Context, tenantID, modelID string) (int, string) {
	t.Helper()
	var count int
	var fingerprint string
	err := db.QueryRow(ctx, `SELECT count(*),COALESCE(string_agg(
		id::text || ':' || version::text || ':' || content::text || ':' || COALESCE(encode(canonical_content,'hex'),'') || ':' || COALESCE(canonical_checksum,''),
		'|' ORDER BY version,id), '')
		FROM thing_model_snapshots WHERE tenant_id=$1 AND thing_model_id=$2`, tenantID, modelID).Scan(&count, &fingerprint)
	if err != nil {
		t.Fatalf("read ThingModel snapshot fingerprint: %v", err)
	}
	return count, fingerprint
}

func cleanupNativeTemplateBindingMutableRows(t *testing.T, ctx context.Context, tenantID string) {
	t.Helper()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Errorf("begin template binding fixture cleanup: %v", err)
		return
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`DELETE FROM metadata_audit_events WHERE tenant_id=$1`,
		`DELETE FROM metadata_idempotency WHERE tenant_id=$1`,
		`DELETE FROM device_access_bindings WHERE tenant_id=$1`,
		`DELETE FROM devices WHERE tenant_id=$1`,
		`DELETE FROM device_access_templates WHERE tenant_id=$1`,
	} {
		if _, err := tx.Exec(ctx, statement, tenantID); err != nil {
			t.Errorf("cleanup template binding fixture for tenant %s: %v", tenantID, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("commit template binding fixture cleanup: %v", err)
	}
}
