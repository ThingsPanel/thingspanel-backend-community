package devicemetadata

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
)

func TestDeviceTemplateDeleteSerializesLocalReferences(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, otherTenantID := testTenantID(), testTenantID()
	userID := "device-template-reference-" + uuid.NewString()
	et.OverrideAuthInfo(auth.UID(userID), &authn.Data{UserID: userID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)

	model, snapshot := createPublishedThingModel(t, ctx, tenantID)
	otherModel, otherSnapshot := createPublishedThingModel(t, ctx, otherTenantID)
	template := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://refs.example.test"}`)
	deviceTemplate := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://device-refs.example.test"}`)
	rollbackTemplate := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{}`)
	foreignTemplate := createPublishTestTemplate(t, ctx, otherTenantID, otherModel.ID, otherSnapshot.ID, `{}`)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantID, model.ID)
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, otherTenantID, otherModel.ID)
	})
	cleanupPublishTestTemplates(t, ctx, tenantID, template.ID, deviceTemplate.ID, rollbackTemplate.ID)
	cleanupPublishTestTemplates(t, ctx, otherTenantID, foreignTemplate.ID)
	if _, err := publishDeviceTemplate(ctx, tenantID, deviceTemplate.ID); err != nil {
		t.Fatal(err)
	}

	service := &Service{}
	product, err := service.CreateProduct(ctx, &Product{Name: "reference-product-" + uuid.NewString(), DeviceTemplateID: template.ID})
	if err != nil {
		t.Fatalf("create product reference: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(ctx, `DELETE FROM products WHERE tenant_id=$1 AND id=$2`, tenantID, product.ID) })
	if err := service.DeleteTemplate(ctx, template.ID); errs.Code(err) != errs.FailedPrecondition || !strings.Contains(err.Error(), "referenced by products") {
		t.Fatalf("delete product-referenced template = %v (code %v), want FailedPrecondition", err, errs.Code(err))
	}
	device, err := service.CreateDevice(ctx, &Device{DeviceTemplateID: deviceTemplate.ID, SN: "ref-device-" + uuid.NewString()})
	if err != nil {
		t.Fatalf("create device reference: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(ctx, `DELETE FROM devices WHERE tenant_id=$1 AND id=$2`, tenantID, device.ID) })
	if err := service.DeleteTemplate(ctx, deviceTemplate.ID); errs.Code(err) != errs.FailedPrecondition || !strings.Contains(err.Error(), "referenced by devices") {
		t.Fatalf("delete device-referenced template = %v (code %v), want FailedPrecondition", err, errs.Code(err))
	}
	if err := service.DeleteTemplate(ctx, foreignTemplate.ID); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant delete = %v (code %v), want NotFound", err, errs.Code(err))
	}

	// Exercise the soft delete in a caller-owned tenant transaction and prove a
	// later statement failure rolls the deletion back.
	tx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	var txidBefore int64
	if err := tx.QueryRow(ctx, `SELECT txid_current()`).Scan(&txidBefore); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := deleteDeviceTemplateTx(ctx, tx, tenantID, rollbackTemplate.ID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("delete within transaction: %v", err)
	}
	var txidAfter int64
	var configuredTenant string
	if err := tx.QueryRow(ctx, `SELECT txid_current(), current_setting('app.current_tenant_id')`).Scan(&txidAfter, &configuredTenant); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if txidBefore != txidAfter || configuredTenant != tenantID {
		_ = tx.Rollback()
		t.Fatalf("delete tx changed: before=%d after=%d tenant=%q", txidBefore, txidAfter, configuredTenant)
	}
	if _, err := tx.Exec(ctx, `SELECT 1 / 0`); err == nil {
		_ = tx.Rollback()
		t.Fatal("injected SQL failure unexpectedly succeeded")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetTemplate(ctx, rollbackTemplate.ID); err != nil {
		t.Fatalf("rolled-back delete made template invisible: %v", err)
	}

	// No live local references permits soft deletion and hides the row.
	noRef := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{}`)
	cleanupPublishTestTemplates(t, ctx, tenantID, noRef.ID)
	if err := service.DeleteTemplate(ctx, noRef.ID); err != nil {
		t.Fatalf("delete unreferenced template: %v", err)
	}
	if _, err := service.GetTemplate(ctx, noRef.ID); errs.Code(err) != errs.NotFound {
		t.Fatalf("read deleted template = %v (code %v), want NotFound", err, errs.Code(err))
	}
}

func TestDeviceTemplateDeleteRacesReferenceCreation(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	model, snapshot := createPublishedThingModel(t, ctx, tenantID)
	template := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://product-race.example.test"}`)
	deviceTemplate := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://device-race.example.test"}`)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantID, model.ID)
	})
	cleanupPublishTestTemplates(t, ctx, tenantID, template.ID, deviceTemplate.ID)
	if _, err := publishDeviceTemplate(ctx, tenantID, deviceTemplate.ID); err != nil {
		t.Fatal(err)
	}

	t.Run("product", func(t *testing.T) {
		productReq := &Product{Name: "race-product-" + uuid.NewString(), DeviceTemplateID: template.ID}
		var productID string
		deleteWinsReferenceRace(t, ctx, tenantID, template.ID, func() error {
			p, err := createProduct(ctx, tenantID, productReq)
			if err == nil {
				productID = p.ID
			}
			return err
		})
		if productID != "" {
			t.Cleanup(func() { _, _ = db.Exec(ctx, `DELETE FROM products WHERE tenant_id=$1 AND id=$2`, tenantID, productID) })
		}
	})
	t.Run("device", func(t *testing.T) {
		deviceReq := &Device{DeviceTemplateID: deviceTemplate.ID, SN: "race-device-" + uuid.NewString()}
		var deviceID string
		deleteWinsReferenceRace(t, ctx, tenantID, deviceTemplate.ID, func() error {
			d, err := createDevice(ctx, tenantID, deviceReq)
			if err == nil {
				deviceID = d.ID
			}
			return err
		})
		if deviceID != "" {
			t.Cleanup(func() { _, _ = db.Exec(ctx, `DELETE FROM devices WHERE tenant_id=$1 AND id=$2`, tenantID, deviceID) })
		}
	})
}

func deleteWinsReferenceRace(t *testing.T, ctx context.Context, tenantID, templateID string, create func() error) {
	t.Helper()
	lockTx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := lockTx.QueryRow(ctx, `SELECT id FROM device_access_templates WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, templateID).Scan(new(string)); err != nil {
		_ = lockTx.Rollback()
		t.Fatal(err)
	}
	start := make(chan struct{})
	started := make(chan struct{}, 2)
	type outcome struct{ err error }
	createResults := make(chan outcome, 1)
	deleteResults := make(chan error, 1)
	go func() { started <- struct{}{}; <-start; createResults <- outcome{create()} }()
	go func() {
		started <- struct{}{}
		<-start
		deleteResults <- deleteDeviceTemplate(ctx, tenantID, templateID)
	}()
	<-started
	<-started
	close(start)
	if err := waitForDeviceTemplateReferenceLockWaiters(t, 2); err != nil {
		_ = lockTx.Rollback()
		<-createResults
		<-deleteResults
		t.Fatal(err)
	}
	if err := lockTx.Commit(); err != nil {
		t.Fatal(err)
	}
	createErr, deleteErr := (<-createResults).err, <-deleteResults
	if createErr == nil && errs.Code(deleteErr) == errs.FailedPrecondition && strings.Contains(deleteErr.Error(), "referenced by") {
		return
	}
	if errs.Code(createErr) == errs.NotFound && deleteErr == nil {
		return
	}
	t.Fatalf("create/delete outcomes create=%v (code %v), delete=%v (code %v); want create wins with delete FailedPrecondition or delete wins with create NotFound", createErr, errs.Code(createErr), deleteErr, errs.Code(deleteErr))
}

func waitForDeviceTemplateReferenceLockWaiters(t *testing.T, want int) error {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		err := db.QueryRow(ctx, `
			SELECT COUNT(*) FROM pg_stat_activity
			WHERE datname = current_database()
			  AND state = 'active'
			  AND wait_event_type = 'Lock'
			  AND query ILIKE '%FROM device_access_templates%'
			  AND (query ILIKE '%FOR UPDATE%' OR query ILIKE '%FOR SHARE%')
		`).Scan(&count)
		cancel()
		if err != nil {
			return fmt.Errorf("observe local reference lock waiters: %w", err)
		}
		if count >= want {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %d concurrent template reference/delete lock waiters", want)
}
