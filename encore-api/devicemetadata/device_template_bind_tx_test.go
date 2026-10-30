package devicemetadata

import (
	"context"
	"strings"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
)

func TestBindDeviceTemplateVersionTenantTransaction(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, otherTenantID := testTenantID(), testTenantID()
	userID := "device-template-bind-" + uuid.NewString()
	et.OverrideAuthInfo(auth.UID(userID), &authn.Data{UserID: userID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)

	model, snapshot := createPublishedThingModel(t, ctx, tenantID)
	_, err := createThingModelItem(ctx, tenantID, model.ID, &ThingModelItem{
		Type: "PROPERTY", Identifier: "bind_extra", NameI18n: []byte(`{"default":"Bind extra"}`),
		ValueType: []byte(`{"kind":"bool"}`), Access: []byte(`{"read":true,"write":true}`),
	})
	if err != nil {
		t.Fatalf("add model item for second snapshot: %v", err)
	}
	secondSnapshot, err := publishThingModel(ctx, tenantID, model.ID, uuid.NewString(), &publishReq{Changelog: "second bind snapshot"})
	if err != nil {
		t.Fatalf("publish second snapshot: %v", err)
	}
	otherModel, otherSnapshot := createPublishedThingModel(t, ctx, tenantID)
	foreignModel, foreignSnapshot := createPublishedThingModel(t, ctx, otherTenantID)
	template := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://bind.example.test"}`)
	archived := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://archived.example.test"}`)
	raceTemplate := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://bind-race.example.test"}`)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantID, model.ID)
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantID, otherModel.ID)
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, otherTenantID, foreignModel.ID)
	})
	cleanupPublishTestTemplates(t, ctx, tenantID, template.ID, archived.ID, raceTemplate.ID)
	if _, err := archiveDeviceTemplate(ctx, tenantID, archived.ID); err != nil {
		t.Fatalf("archive fixture: %v", err)
	}

	service := &Service{}
	bound, err := service.BindThingModelVersion(ctx, template.ID, &bindThingModelReq{ThingModelSnapshotID: secondSnapshot.ID})
	if err != nil {
		t.Fatalf("bind through authenticated service: %v", err)
	}
	if bound.ID != template.ID || bound.TenantID != tenantID || bound.ThingModelID != model.ID || bound.ThingModelSnapshotID != secondSnapshot.ID {
		t.Fatalf("bind result mismatch: %#v", bound)
	}
	readback, err := service.GetTemplate(ctx, template.ID)
	if err != nil || readback.ThingModelSnapshotID != secondSnapshot.ID {
		t.Fatalf("bind readback=%#v err=%v", readback, err)
	}
	if _, err := service.BindThingModelVersion(ctx, archived.ID, &bindThingModelReq{ThingModelSnapshotID: snapshot.ID}); errs.Code(err) != errs.FailedPrecondition || !strings.Contains(err.Error(), "ARCHIVED templates cannot be modified") {
		t.Fatalf("bind archived template = %v (code %v), want existing precondition", err, errs.Code(err))
	}
	if _, err := service.BindThingModelVersion(ctx, template.ID, &bindThingModelReq{ThingModelSnapshotID: foreignSnapshot.ID}); errs.Code(err) != errs.NotFound || !strings.Contains(err.Error(), "thing model snapshot not found") {
		t.Fatalf("bind foreign snapshot = %v (code %v), want existing NotFound", err, errs.Code(err))
	}
	if _, err := service.BindThingModelVersion(ctx, template.ID, &bindThingModelReq{ThingModelSnapshotID: otherSnapshot.ID}); errs.Code(err) != errs.InvalidArgument || !strings.Contains(err.Error(), "snapshot does not belong") {
		t.Fatalf("bind wrong-model snapshot = %v (code %v), want existing InvalidArgument", err, errs.Code(err))
	}
	if _, err := service.BindThingModelVersion(ctx, template.ID, &bindThingModelReq{ThingModelSnapshotID: uuid.NewString()}); errs.Code(err) != errs.NotFound {
		t.Fatalf("bind missing snapshot = %v (code %v), want NotFound", err, errs.Code(err))
	}
	_ = foreignModel

	// Verify the locked update and validation remain in the tenant transaction,
	// then inject a subsequent SQL failure to prove the update rolls back.
	tx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := tx.QueryRow(ctx, `SELECT txid_current()`).Scan(&before); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	probe, err := bindThingModelVersionAdvancedTx(ctx, tx, tenantID, template.ID, snapshot.ID)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("bind in transaction: %v", err)
	}
	var after int64
	var configured string
	if err := tx.QueryRow(ctx, `SELECT txid_current(), current_setting('app.current_tenant_id')`).Scan(&after, &configured); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if before != after || configured != tenantID || probe.ThingModelSnapshotID != snapshot.ID {
		_ = tx.Rollback()
		t.Fatalf("bind tx context before=%d after=%d tenant=%q result=%#v", before, after, configured, probe)
	}
	if _, err := tx.Exec(ctx, `SELECT 1 / 0`); err == nil {
		_ = tx.Rollback()
		t.Fatal("injected SQL failure unexpectedly succeeded")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var afterRollback string
	if err := db.QueryRow(ctx, `SELECT thing_model_snapshot_id FROM device_access_templates WHERE tenant_id=$1 AND id=$2`, tenantID, template.ID).Scan(&afterRollback); err != nil || afterRollback != secondSnapshot.ID {
		t.Fatalf("snapshot after rollback=%q err=%v", afterRollback, err)
	}
}

func TestBindDeviceTemplateVersionSerializesWithArchive(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	model, snapshot := createPublishedThingModel(t, ctx, tenantID)
	template := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://bind-archive-race.example.test"}`)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantID, model.ID)
	})
	cleanupPublishTestTemplates(t, ctx, tenantID, template.ID)

	lockTx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := lockTx.QueryRow(ctx, `SELECT status FROM device_access_templates WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, template.ID).Scan(new(string)); err != nil {
		_ = lockTx.Rollback()
		t.Fatal(err)
	}
	start := make(chan struct{})
	started := make(chan struct{}, 2)
	type bindResult struct {
		template *DeviceTemplate
		err      error
	}
	bindResults := make(chan bindResult, 1)
	archiveResults := make(chan error, 1)
	go func() {
		started <- struct{}{}
		<-start
		out, e := bindThingModelVersionAdvanced(ctx, tenantID, template.ID, snapshot.ID)
		bindResults <- bindResult{out, e}
	}()
	go func() {
		started <- struct{}{}
		<-start
		_, e := archiveDeviceTemplate(ctx, tenantID, template.ID)
		archiveResults <- e
	}()
	<-started
	<-started
	close(start)
	if err := waitForDeviceTemplateArchiveLockWaiters(t, 2); err != nil {
		_ = lockTx.Rollback()
		<-bindResults
		<-archiveResults
		t.Fatal(err)
	}
	if err := lockTx.Commit(); err != nil {
		t.Fatal(err)
	}
	b := <-bindResults
	if err := <-archiveResults; err != nil {
		t.Fatalf("archive concurrent with bind: %v", err)
	}
	if b.err != nil {
		if errs.Code(b.err) != errs.FailedPrecondition || !strings.Contains(b.err.Error(), "ARCHIVED templates cannot be modified") {
			t.Fatalf("bind after archive=%v (code %v), want archived precondition", b.err, errs.Code(b.err))
		}
	} else if b.template == nil || b.template.ThingModelSnapshotID != snapshot.ID {
		t.Fatalf("bind before archive returned %#v", b.template)
	}
	assertPublishTemplateUnchanged(t, ctx, tenantID, template.ID, "ARCHIVED")
}
