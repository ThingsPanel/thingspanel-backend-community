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

func TestArchiveDeviceTemplateTenantTransactionAndContract(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	otherTenantID := testTenantID()
	userID := "device-template-archive-" + uuid.NewString()
	et.OverrideAuthInfo(auth.UID(userID), &authn.Data{UserID: userID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)

	model, snapshot := createPublishedThingModel(t, ctx, tenantID)
	otherModel, otherSnapshot := createPublishedThingModel(t, ctx, otherTenantID)
	archiveTemplate := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://archive.example.test"}`)
	raceTemplate := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://race.example.test"}`)
	rollbackTemplate := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://rollback.example.test"}`)
	foreignTemplate := createPublishTestTemplate(t, ctx, otherTenantID, otherModel.ID, otherSnapshot.ID, `{"broker_url":"mqtt://foreign.example.test"}`)
	cleanupPublishTestTemplates(t, ctx, tenantID, archiveTemplate.ID, raceTemplate.ID, rollbackTemplate.ID)
	cleanupPublishTestTemplates(t, ctx, otherTenantID, foreignTemplate.ID)

	service := &Service{}
	archived, err := service.ArchiveTemplate(ctx, archiveTemplate.ID)
	if err != nil {
		t.Fatalf("archive template through service: %v", err)
	}
	if archived.ID != archiveTemplate.ID || archived.TenantID != tenantID || archived.Status != "ARCHIVED" {
		t.Fatalf("archive result mismatch: %#v", archived)
	}
	if _, err := service.ArchiveTemplate(ctx, archiveTemplate.ID); errs.Code(err) != errs.FailedPrecondition || !strings.Contains(err.Error(), "ARCHIVED templates cannot be modified") {
		t.Fatalf("repeat archive = %v (code %v), want existing archived precondition", err, errs.Code(err))
	}
	if _, err := service.ArchiveTemplate(ctx, foreignTemplate.ID); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant archive = %v (code %v), want NotFound", err, errs.Code(err))
	}
	if _, err := service.ArchiveTemplate(ctx, uuid.NewString()); errs.Code(err) != errs.NotFound {
		t.Fatalf("missing template archive = %v (code %v), want NotFound", err, errs.Code(err))
	}

	// Prove the state check and archive UPDATE share the caller's tenant tx,
	// then inject a later statement failure and roll back the archived status.
	tx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	var txIDBefore int64
	if err := tx.QueryRow(ctx, `SELECT txid_current()`).Scan(&txIDBefore); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err := archiveDeviceTemplateTx(ctx, tx, tenantID, rollbackTemplate.ID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("archive within transaction: %v", err)
	}
	var txIDAfter int64
	var configuredTenant string
	if err := tx.QueryRow(ctx, `SELECT txid_current(), current_setting('app.current_tenant_id')`).Scan(&txIDAfter, &configuredTenant); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if txIDBefore != txIDAfter || configuredTenant != tenantID {
		_ = tx.Rollback()
		t.Fatalf("archive transaction context changed: before=%d after=%d tenant=%q", txIDBefore, txIDAfter, configuredTenant)
	}
	if _, err := tx.Exec(ctx, `SELECT 1 / 0`); err == nil {
		_ = tx.Rollback()
		t.Fatal("injected SQL failure unexpectedly succeeded")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertPublishTemplateUnchanged(t, ctx, tenantID, rollbackTemplate.ID, "DRAFT")

	// Hold the row lock until both concurrent requests are waiting. Exactly one
	// may archive; the second observes ARCHIVED after acquiring the lock.
	lockTx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	var lockedStatus string
	if err := lockTx.QueryRow(ctx, `SELECT status FROM device_access_templates WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, raceTemplate.ID).Scan(&lockedStatus); err != nil {
		_ = lockTx.Rollback()
		t.Fatal(err)
	}
	if lockedStatus != "DRAFT" {
		_ = lockTx.Rollback()
		t.Fatalf("race fixture status = %q, want DRAFT", lockedStatus)
	}
	start := make(chan struct{})
	started := make(chan struct{}, 2)
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			started <- struct{}{}
			<-start
			_, err := archiveDeviceTemplate(ctx, tenantID, raceTemplate.ID)
			results <- err
		}()
	}
	<-started
	<-started
	close(start)
	if err := waitForDeviceTemplateArchiveLockWaiters(t, 2); err != nil {
		_ = lockTx.Rollback()
		<-results
		<-results
		t.Fatal(err)
	}
	if err := lockTx.Commit(); err != nil {
		t.Fatal(err)
	}
	var successCount, preconditionCount int
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successCount++
			continue
		}
		if errs.Code(err) == errs.FailedPrecondition && strings.Contains(err.Error(), "ARCHIVED templates cannot be modified") {
			preconditionCount++
			continue
		}
		t.Fatalf("concurrent archive result = %v (code %v), want one success and one archived precondition", err, errs.Code(err))
	}
	if successCount != 1 || preconditionCount != 1 {
		t.Fatalf("concurrent archive outcomes success=%d failed-precondition=%d, want 1 each", successCount, preconditionCount)
	}
	assertPublishTemplateUnchanged(t, ctx, tenantID, raceTemplate.ID, "ARCHIVED")
}

func waitForDeviceTemplateArchiveLockWaiters(t *testing.T, want int) error {
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
			  AND query ILIKE '%FOR UPDATE%'
		`).Scan(&count)
		cancel()
		if err != nil {
			return fmt.Errorf("observe concurrent archive lock waiters: %w", err)
		}
		if count >= want {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %d concurrent template archive lock waiters", want)
}
