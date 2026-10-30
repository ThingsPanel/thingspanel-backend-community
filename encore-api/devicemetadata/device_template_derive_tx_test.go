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

func TestDeriveDeviceTemplateDraftTenantTransaction(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, otherTenantID := testTenantID(), testTenantID()
	userID := "device-template-derive-" + uuid.NewString()
	et.OverrideAuthInfo(auth.UID(userID), &authn.Data{UserID: userID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)

	model, snapshot := createPublishedThingModel(t, ctx, tenantID)
	otherModel, otherSnapshot := createPublishedThingModel(t, ctx, otherTenantID)
	source := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://derive.example.test"}`)
	draftSource := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://draft.example.test"}`)
	foreignSource := createPublishTestTemplate(t, ctx, otherTenantID, otherModel.ID, otherSnapshot.ID, `{"broker_url":"mqtt://foreign.example.test"}`)
	probeSource := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://probe.example.test"}`)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantID, model.ID)
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, otherTenantID, otherModel.ID)
	})
	cleanupPublishTestTemplates(t, ctx, tenantID, source.ID, draftSource.ID, probeSource.ID)
	cleanupPublishTestTemplates(t, ctx, otherTenantID, foreignSource.ID)
	if _, err := publishDeviceTemplate(ctx, tenantID, source.ID); err != nil {
		t.Fatalf("publish source: %v", err)
	}
	if _, err := publishDeviceTemplate(ctx, tenantID, probeSource.ID); err != nil {
		t.Fatalf("publish transaction probe source: %v", err)
	}
	if _, err := publishDeviceTemplate(ctx, otherTenantID, foreignSource.ID); err != nil {
		t.Fatalf("publish foreign source: %v", err)
	}

	service := &Service{}
	derived, err := service.DeriveTemplateDraft(ctx, source.ID)
	if err != nil {
		t.Fatalf("derive through authenticated service: %v", err)
	}
	if derived.TenantID != tenantID || derived.Name != source.Name+" (draft)" || derived.Status != "DRAFT" || derived.ThingModelID != source.ThingModelID || derived.ThingModelSnapshotID != source.ThingModelSnapshotID || string(derived.ProtocolConfig) != string(source.ProtocolConfig) {
		t.Fatalf("derived field mapping mismatch: source=%#v derived=%#v", source, derived)
	}
	readback, err := service.GetTemplate(ctx, derived.ID)
	if err != nil || readback.ID != derived.ID || readback.Status != "DRAFT" {
		t.Fatalf("derived readback = %#v, %v", readback, err)
	}
	if _, err := service.DeriveTemplateDraft(ctx, draftSource.ID); errs.Code(err) != errs.FailedPrecondition || !strings.Contains(err.Error(), "current status is DRAFT") {
		t.Fatalf("derive from DRAFT = %v (code %v), want state precondition", err, errs.Code(err))
	}
	if _, err := service.DeriveTemplateDraft(ctx, foreignSource.ID); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant derive = %v (code %v), want NotFound", err, errs.Code(err))
	}

	// Exercise the helper in a caller-owned transaction to verify source lock,
	// model/snapshot validation, insert, and tenant setting share one txid.
	probeTx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	var txidBefore int64
	if err := probeTx.QueryRow(ctx, `SELECT txid_current()`).Scan(&txidBefore); err != nil {
		_ = probeTx.Rollback()
		t.Fatal(err)
	}
	probe, err := deriveDeviceTemplateDraftAdvancedTx(ctx, probeTx, tenantID, probeSource.ID)
	if err != nil {
		_ = probeTx.Rollback()
		t.Fatalf("derive in probe transaction: %v", err)
	}
	var txidAfter int64
	var configuredTenant string
	if err := probeTx.QueryRow(ctx, `SELECT txid_current(), current_setting('app.current_tenant_id')`).Scan(&txidAfter, &configuredTenant); err != nil {
		_ = probeTx.Rollback()
		t.Fatal(err)
	}
	if txidBefore != txidAfter || configuredTenant != tenantID {
		_ = probeTx.Rollback()
		t.Fatalf("derive transaction context changed: before=%d after=%d tenant=%q", txidBefore, txidAfter, configuredTenant)
	}
	if _, err := probeTx.Exec(ctx, `SELECT 1 / 0`); err == nil {
		_ = probeTx.Rollback()
		t.Fatal("injected SQL failure unexpectedly succeeded")
	}
	if err := probeTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var probeCount int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_access_templates WHERE tenant_id=$1 AND id=$2`, tenantID, probe.ID).Scan(&probeCount); err != nil || probeCount != 0 {
		t.Fatalf("rolled back derive persisted row: count=%d err=%v", probeCount, err)
	}
}

func TestDeriveDeviceTemplateDraftSerializesWithArchive(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	model, snapshot := createPublishedThingModel(t, ctx, tenantID)
	source := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://derive-race.example.test"}`)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantID, model.ID)
	})
	cleanupPublishTestTemplates(t, ctx, tenantID, source.ID)
	if _, err := publishDeviceTemplate(ctx, tenantID, source.ID); err != nil {
		t.Fatal(err)
	}

	lockTx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err := lockTx.QueryRow(ctx, `SELECT status FROM device_access_templates WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, source.ID).Scan(&status); err != nil {
		_ = lockTx.Rollback()
		t.Fatal(err)
	}
	if status != "PUBLISHED" {
		_ = lockTx.Rollback()
		t.Fatalf("source state=%q, want PUBLISHED", status)
	}
	start := make(chan struct{})
	started := make(chan struct{}, 2)
	type result struct {
		derived *DeviceTemplate
		err     error
	}
	deriveResults := make(chan result, 1)
	archiveResults := make(chan error, 1)
	go func() {
		started <- struct{}{}
		<-start
		d, e := deriveDeviceTemplateDraftAdvanced(ctx, tenantID, source.ID)
		deriveResults <- result{d, e}
	}()
	go func() {
		started <- struct{}{}
		<-start
		_, e := archiveDeviceTemplate(ctx, tenantID, source.ID)
		archiveResults <- e
	}()
	<-started
	<-started
	close(start)
	if err := waitForDeviceTemplateArchiveLockWaiters(t, 2); err != nil {
		_ = lockTx.Rollback()
		<-deriveResults
		<-archiveResults
		t.Fatal(err)
	}
	if err := lockTx.Commit(); err != nil {
		t.Fatal(err)
	}
	d := <-deriveResults
	archiveErr := <-archiveResults
	if archiveErr != nil {
		t.Fatalf("archive concurrent with derive: %v", archiveErr)
	}
	if d.err != nil {
		if errs.Code(d.err) != errs.FailedPrecondition || !strings.Contains(d.err.Error(), "current status is ARCHIVED") {
			t.Fatalf("derive after archive = %v (code %v), want archived precondition", d.err, errs.Code(d.err))
		}
	} else if d.derived == nil || d.derived.Status != "DRAFT" || d.derived.TenantID != tenantID {
		t.Fatalf("derive before archive returned %#v, want tenant DRAFT", d.derived)
	}
	assertPublishTemplateUnchanged(t, ctx, tenantID, source.ID, "ARCHIVED")
	if d.derived != nil {
		cleanupPublishTestTemplates(t, ctx, tenantID, d.derived.ID)
		var count int
		if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_access_templates WHERE tenant_id=$1 AND id=$2 AND status='DRAFT'`, tenantID, d.derived.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("derived row after serialized race count=%d err=%v", count, err)
		}
	}
}
