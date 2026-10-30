package devicemetadata

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
)

func TestDeviceTemplateCreateUpdateUseTenantTransactions(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	otherTenantID := testTenantID()
	userID := "device-template-write-" + uuid.NewString()
	et.OverrideAuthInfo(auth.UID(userID), &authn.Data{UserID: userID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)

	model, snapshot := createPublishedThingModel(t, ctx, tenantID)
	otherModel, otherSnapshot := createPublishedThingModel(t, ctx, tenantID)
	foreignModel, foreignSnapshot := createPublishedThingModel(t, ctx, otherTenantID)
	var templateIDs []string
	t.Cleanup(func() {
		for _, id := range templateIDs {
			_, _ = db.Exec(ctx, `DELETE FROM device_access_templates WHERE tenant_id=$1 AND id=$2`, tenantID, id)
		}
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantID, model.ID)
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantID, otherModel.ID)
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, otherTenantID, foreignModel.ID)
	})

	service := &Service{}
	createReq := &DeviceTemplate{
		Name: "tx-create-" + uuid.NewString(), ThingModelID: model.ID, ThingModelSnapshotID: snapshot.ID,
		ProtocolType: "MQTT", ProtocolConfig: json.RawMessage(`{"broker":"create"}`), Status: "DRAFT",
	}
	probeCreateTx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	var createTxBefore, createTxAfter int64
	var createTenant string
	if err := probeCreateTx.QueryRow(ctx, `SELECT txid_current()`).Scan(&createTxBefore); err != nil {
		_ = probeCreateTx.Rollback()
		t.Fatal(err)
	}
	probeCreated, err := createDeviceTemplateTx(ctx, probeCreateTx, tenantID, createReq)
	if err != nil {
		_ = probeCreateTx.Rollback()
		t.Fatal(err)
	}
	if err := probeCreateTx.QueryRow(ctx, `SELECT txid_current(), current_setting('app.current_tenant_id')`).Scan(&createTxAfter, &createTenant); err != nil {
		_ = probeCreateTx.Rollback()
		t.Fatal(err)
	}
	if createTxBefore != createTxAfter || createTenant != tenantID {
		_ = probeCreateTx.Rollback()
		t.Fatalf("create transaction context changed: before=%d after=%d tenant=%q", createTxBefore, createTxAfter, createTenant)
	}
	if err := probeCreateTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var probeCount int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_access_templates WHERE tenant_id=$1 AND id=$2`, tenantID, probeCreated.ID).Scan(&probeCount); err != nil || probeCount != 0 {
		t.Fatalf("rolled back create helper persisted row: count=%d err=%v", probeCount, err)
	}

	created, err := service.CreateTemplate(ctx, createReq)
	if err != nil {
		t.Fatalf("create template through service: %v", err)
	}
	templateIDs = append(templateIDs, created.ID)
	if created.ID == "" || created.TenantID != tenantID || created.Name != createReq.Name || created.Status != "DRAFT" || created.ThingModelID != model.ID || created.ThingModelSnapshotID != snapshot.ID {
		t.Fatalf("create RETURNING result mismatch: %#v", created)
	}
	readback, err := service.GetTemplate(ctx, created.ID)
	if err != nil || readback.ID != created.ID || readback.ProtocolConfig == nil {
		t.Fatalf("created template readback mismatch: template=%#v err=%v", readback, err)
	}

	validUpdate := &DeviceTemplate{
		Name: "tx-updated-" + uuid.NewString(), ThingModelID: model.ID, ThingModelSnapshotID: snapshot.ID,
		ProtocolType: "HTTP", ProtocolConfig: json.RawMessage(`{"endpoint":"https://example.test"}`), Status: "DRAFT",
	}
	probeUpdateTx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	var updateTxBefore, updateTxAfter int64
	if err := probeUpdateTx.QueryRow(ctx, `SELECT txid_current()`).Scan(&updateTxBefore); err != nil {
		_ = probeUpdateTx.Rollback()
		t.Fatal(err)
	}
	probeUpdated, err := updateDeviceTemplateTx(ctx, probeUpdateTx, tenantID, created.ID, validUpdate)
	if err != nil {
		_ = probeUpdateTx.Rollback()
		t.Fatal(err)
	}
	var updateTenant string
	if err := probeUpdateTx.QueryRow(ctx, `SELECT txid_current(), current_setting('app.current_tenant_id')`).Scan(&updateTxAfter, &updateTenant); err != nil {
		_ = probeUpdateTx.Rollback()
		t.Fatal(err)
	}
	if updateTxBefore != updateTxAfter || updateTenant != tenantID {
		_ = probeUpdateTx.Rollback()
		t.Fatalf("update transaction context changed: before=%d after=%d tenant=%q", updateTxBefore, updateTxAfter, updateTenant)
	}
	if err := probeUpdateTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	unchanged, err := service.GetTemplate(ctx, created.ID)
	if err != nil || unchanged.Name != created.Name {
		t.Fatalf("rolled back update helper changed template: template=%#v err=%v", unchanged, err)
	}
	if probeUpdated.Name != validUpdate.Name {
		t.Fatalf("update helper RETURNING result mismatch: %#v", probeUpdated)
	}

	updated, err := service.UpdateTemplate(ctx, created.ID, validUpdate)
	if err != nil {
		t.Fatalf("legal update through service: %v", err)
	}
	if updated.ID != created.ID || updated.Name != validUpdate.Name || updated.ProtocolType != "HTTP" || updated.ThingModelID != model.ID {
		t.Fatalf("update RETURNING result mismatch: %#v", updated)
	}

	foreignReference := *validUpdate
	foreignReference.Name = "foreign-model-" + uuid.NewString()
	foreignReference.ThingModelID = foreignModel.ID
	foreignReference.ThingModelSnapshotID = foreignSnapshot.ID
	if _, err := service.UpdateTemplate(ctx, created.ID, &foreignReference); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant ThingModel update code = %v, want NotFound", errs.Code(err))
	}
	wrongSnapshot := *validUpdate
	wrongSnapshot.Name = "wrong-snapshot-" + uuid.NewString()
	wrongSnapshot.ThingModelSnapshotID = otherSnapshot.ID
	if _, err := service.UpdateTemplate(ctx, created.ID, &wrongSnapshot); errs.Code(err) != errs.InvalidArgument {
		t.Fatalf("wrong ThingModel snapshot update code = %v, want InvalidArgument", errs.Code(err))
	}
	if _, err := service.UpdateTemplate(ctx, created.ID, &DeviceTemplate{
		Name: strings.Repeat("x", 129), ThingModelID: model.ID, ThingModelSnapshotID: snapshot.ID,
	}); err == nil {
		t.Fatal("database-invalid update unexpectedly succeeded")
	}
	unchanged, err = service.GetTemplate(ctx, created.ID)
	if err != nil || unchanged.Name != validUpdate.Name {
		t.Fatalf("rejected updates changed template: template=%#v err=%v", unchanged, err)
	}

	badCreate := &DeviceTemplate{
		Name: strings.Repeat("y", 129), ThingModelID: model.ID, ThingModelSnapshotID: snapshot.ID,
		ProtocolType: "MQTT", ProtocolConfig: json.RawMessage(`{"broker":"rollback"}`),
	}
	if _, err := service.CreateTemplate(ctx, badCreate); err == nil {
		t.Fatal("database-invalid create unexpectedly succeeded")
	}
	var failedCreateCount int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_access_templates WHERE tenant_id=$1 AND name=$2`, tenantID, badCreate.Name).Scan(&failedCreateCount); err != nil || failedCreateCount != 0 {
		t.Fatalf("failed create left template rows: count=%d err=%v", failedCreateCount, err)
	}

	if _, err := db.Exec(ctx, `UPDATE device_access_templates SET status='ARCHIVED' WHERE tenant_id=$1 AND id=$2`, tenantID, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateTemplate(ctx, created.ID, validUpdate); errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("archived update code = %v, want FailedPrecondition", errs.Code(err))
	}
	archived, err := service.GetTemplate(ctx, created.ID)
	if err != nil || archived.Status != "ARCHIVED" {
		t.Fatalf("archived template changed after rejected update: template=%#v err=%v", archived, err)
	}
}
