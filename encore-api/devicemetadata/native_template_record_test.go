package devicemetadata

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

func TestNativeTemplateRecordProjectionScopesCountsAndCallerTransaction(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	tx, err := beginTenantScopedSQLTx(ctx, tenantA, false)
	if err != nil {
		t.Fatal(err)
	}
	var txidBefore int64
	var currentTenant string
	if err := tx.QueryRow(ctx, `SELECT txid_current(),current_setting('app.current_tenant_id')`).Scan(&txidBefore, &currentTenant); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if currentTenant != tenantA {
		_ = tx.Rollback()
		t.Fatalf("test transaction tenant=%q, want %q", currentTenant, tenantA)
	}

	templateA, err := insertAccessTemplateConfigurationTx(ctx, tx, tenantA, "template-record-test", &accessTemplateConfigurationRecord{
		Name: "projection-a-" + uuid.NewString(), Brand: stringPtr("Maker"), Tags: []string{"field", "native"},
		Configuration: json.RawMessage(accessTemplateStoreFixtureConfiguration), AdministrativeState: "enabled",
	})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert tenant A template fixture: %v", err)
	}
	templateB, err := insertAccessTemplateConfigurationTx(ctx, tx, tenantB, "template-record-test", &accessTemplateConfigurationRecord{
		Name: "projection-b-" + uuid.NewString(), Tags: []string{},
		Configuration: json.RawMessage(accessTemplateStoreFixtureConfiguration), AdministrativeState: "disabled",
	})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert tenant B template fixture: %v", err)
	}
	deviceA1 := insertNativeTemplateRecordBoundDevice(t, ctx, tx, tenantA, templateA.ID, "projection-a-device-1", false)
	insertNativeTemplateRecordBoundDevice(t, ctx, tx, tenantA, templateA.ID, "projection-a-device-2", false)
	insertNativeTemplateRecordBoundDevice(t, ctx, tx, tenantA, templateA.ID, "projection-a-deleted", true)
	deviceB := insertNativeTemplateRecordBoundDevice(t, ctx, tx, tenantB, templateB.ID, "projection-b-device", false)

	projected, err := loadNativeDeviceAccessTemplateTx(ctx, tx, tenantA, templateA.ID, nil)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("load tenant A template: %v", err)
	}
	if projected.ID != templateA.ID || projected.TenantID != tenantA || projected.CreatedBy != "template-record-test" || projected.UpdatedBy != "template-record-test" || projected.ResourceVersion != 1 {
		t.Fatalf("template identity/actor/version projection mismatch: %+v", projected)
	}
	if projected.Description != nil || projected.Brand == nil || *projected.Brand != "Maker" || projected.ImageAssetID != nil || projected.AdministrativeState != "enabled" {
		t.Fatalf("nullable/display projection mismatch: %+v", projected)
	}
	if projected.DeviceCount != 2 {
		t.Fatalf("template device count=%d, want 2", projected.DeviceCount)
	}
	if !strings.Contains(string(projected.Configuration), "1.234567890123456789") || len(projected.Tags) != 2 {
		t.Fatalf("configuration precision/tags were lost: configuration=%s tags=%v", projected.Configuration, projected.Tags)
	}
	if _, offset := projected.CreatedAt.Zone(); offset != 0 {
		t.Fatalf("createdAt is not UTC: %s", projected.CreatedAt)
	}
	if _, offset := projected.UpdatedAt.Zone(); offset != 0 {
		t.Fatalf("updatedAt is not UTC: %s", projected.UpdatedAt)
	}

	for _, scopeCase := range []struct {
		name string
		ids  []string
		want int64
	}{
		{name: "single authorized device", ids: []string{deviceA1}, want: 1},
		{name: "authorized subset excludes other tenant", ids: []string{deviceA1, deviceB}, want: 1},
		{name: "empty authorized result", ids: []string{uuid.NewString()}, want: 0},
	} {
		t.Run(scopeCase.name, func(t *testing.T) {
			result, err := loadNativeDeviceAccessTemplateTx(ctx, tx, tenantA, templateA.ID, scopeCase.ids)
			if err != nil || result.DeviceCount != scopeCase.want {
				t.Fatalf("scoped device count=%v err=%v, want %d", result, err, scopeCase.want)
			}
		})
	}
	projectedB, err := loadNativeDeviceAccessTemplateTx(ctx, tx, tenantB, templateB.ID, nil)
	if err != nil || projectedB.DeviceCount != 1 {
		t.Fatalf("tenant B projection=%+v err=%v", projectedB, err)
	}
	if _, err := loadNativeDeviceAccessTemplateTx(ctx, tx, tenantB, templateA.ID, nil); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant template lookup error=%v, want NotFound", err)
	}
	var txidAfter int64
	if err := tx.QueryRow(ctx, `SELECT txid_current(),current_setting('app.current_tenant_id')`).Scan(&txidAfter, &currentTenant); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if txidAfter != txidBefore || currentTenant != tenantA {
		_ = tx.Rollback()
		t.Fatalf("projection escaped caller transaction: txid %d -> %d tenant=%q", txidBefore, txidAfter, currentTenant)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ tenantID, templateID string }{{tenantA, templateA.ID}, {tenantB, templateB.ID}} {
		var count int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM device_access_templates WHERE tenant_id=$1 AND id=$2`, check.tenantID, check.templateID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("rolled back fixture still visible for %s: count=%d err=%v", check.templateID, count, err)
		}
	}
}

func TestNativeTemplateRecordKeepsAssetIdentityAndRejectsIncompleteRows(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	tx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	assetID := uuid.NewString()
	imageTemplate, err := insertAccessTemplateConfigurationTx(ctx, tx, tenantID, "template-record-test", &accessTemplateConfigurationRecord{
		Name: "asset-" + uuid.NewString(), ImageAssetID: &assetID, Tags: []string{},
		Configuration: json.RawMessage(accessTemplateStoreFixtureConfiguration), AdministrativeState: "enabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := loadNativeDeviceAccessTemplateTx(ctx, tx, tenantID, imageTemplate.ID, nil)
	if err != nil || resolved.ImageAssetID == nil || *resolved.ImageAssetID != assetID {
		t.Fatalf("template must retain only its file asset identity: %+v err=%v", resolved, err)
	}
	encoded, err := json.Marshal(resolved)
	if err != nil || strings.Contains(string(encoded), "imageUrl") || strings.Contains(string(encoded), "downloadUrl") {
		t.Fatalf("template response contains a file capability")
	}

	incompleteID := uuid.NewString()
	if _, err := tx.Exec(ctx, `INSERT INTO device_access_templates(id,tenant_id,name) VALUES($1,$2,$3)`, incompleteID, tenantID, "incomplete-"+incompleteID); err != nil {
		t.Fatal(err)
	}
	if _, err := loadNativeDeviceAccessTemplateTx(ctx, tx, tenantID, incompleteID, nil); errs.Code(err) != errs.Unavailable {
		t.Fatalf("incomplete S3 row error=%v, want Unavailable", err)
	}
	invalidConfigurationID := uuid.NewString()
	if _, err := tx.Exec(ctx, `INSERT INTO device_access_templates(id,tenant_id,name,created_by,updated_by,configuration) VALUES($1,$2,$3,'template-record-test','template-record-test','{"model":null}'::jsonb)`, invalidConfigurationID, tenantID, "invalid-configuration-"+invalidConfigurationID); err != nil {
		t.Fatal(err)
	}
	if _, err := loadNativeDeviceAccessTemplateTx(ctx, tx, tenantID, invalidConfigurationID, nil); errs.Code(err) != errs.Unavailable {
		t.Fatalf("invalid stored configuration error=%v, want Unavailable", err)
	}
}

func insertNativeTemplateRecordBoundDevice(t *testing.T, ctx context.Context, tx *sqldb.Tx, tenantID, templateID, number string, deleted bool) string {
	t.Helper()
	row := validDeviceIdentitySchemaRow(tenantID)
	row.deviceNumber = deviceIdentityString(number + "-" + uuid.NewString())
	row.name = deviceIdentityString("bound-device")
	row.deviceKind = deviceIdentityString("standalone")
	row.activationState = deviceIdentityString("inactive")
	row.activationTimeSource = deviceIdentityString("not_activated")
	row.createdBy = deviceIdentityString("template-record-test")
	row.updatedBy = deviceIdentityString("template-record-test")
	if err := insertDeviceIdentitySchemaRow(ctx, tx, row); err != nil {
		t.Fatalf("insert bound device fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO device_access_bindings(tenant_id,device_id,template_id,updated_by) VALUES($1,$2,$3,'template-record-test')`, tenantID, row.id, templateID); err != nil {
		t.Fatalf("insert device binding fixture: %v", err)
	}
	if deleted {
		if _, err := tx.Exec(ctx, `UPDATE devices SET deleted_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantID, row.id); err != nil {
			t.Fatalf("soft delete bound device fixture: %v", err)
		}
	}
	return row.id
}
