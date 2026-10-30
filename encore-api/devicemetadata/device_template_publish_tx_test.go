package devicemetadata

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

func TestPublishDeviceTemplateCommitsValidatedDraft(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	model, snapshot := createPublishedThingModel(t, ctx, tenantID)
	template := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://broker.example.test"}`)
	cleanupPublishTestTemplates(t, ctx, tenantID, template.ID)

	published, err := publishDeviceTemplate(ctx, tenantID, template.ID)
	if err != nil {
		t.Fatalf("publish draft: %v", err)
	}
	if published.Status != "PUBLISHED" || published.ID != template.ID || published.TenantID != tenantID || published.ThingModelSnapshotID != snapshot.ID {
		t.Fatalf("publish result mismatch: %#v", published)
	}

	// A sequential retry keeps the existing FailedPrecondition contract and
	// names the observed state, just as a concurrent retry must after locking.
	if _, err := publishDeviceTemplate(ctx, tenantID, template.ID); errs.Code(err) != errs.FailedPrecondition || !strings.Contains(err.Error(), "cannot transition from PUBLISHED to PUBLISHED") {
		t.Fatalf("repeat publish = %v (code %v), want existing PUBLISHED transition failure", err, errs.Code(err))
	}
}

func TestPublishDeviceTemplateValidationFailuresDoNotChangeDraft(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	otherTenantID := testTenantID()
	model, snapshot := createPublishedThingModel(t, ctx, tenantID)
	_, otherSnapshot := createPublishedThingModel(t, ctx, tenantID)
	_, foreignSnapshot := createPublishedThingModel(t, ctx, otherTenantID)
	template := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{}`)
	cleanupPublishTestTemplates(t, ctx, tenantID, template.ID)

	// Protocol validation happens in the publishing transaction. Prove it sees
	// the tenant GUC and remains in one transaction, then roll back the failed call.
	tx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	var txIDBefore int64
	if err := tx.QueryRow(ctx, `SELECT txid_current()`).Scan(&txIDBefore); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err := publishDeviceTemplateTx(ctx, tx, tenantID, template.ID); errs.Code(err) != errs.InvalidArgument {
		_ = tx.Rollback()
		t.Fatalf("invalid MQTT config publish code = %v, want InvalidArgument", errs.Code(err))
	}
	var txIDAfter int64
	var configuredTenant string
	if err := tx.QueryRow(ctx, `SELECT txid_current(), current_setting('app.current_tenant_id')`).Scan(&txIDAfter, &configuredTenant); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if txIDBefore != txIDAfter || configuredTenant != tenantID {
		_ = tx.Rollback()
		t.Fatalf("publish validation left transaction context: before=%d after=%d tenant=%q", txIDBefore, txIDAfter, configuredTenant)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertPublishTemplateUnchanged(t, ctx, tenantID, template.ID, "DRAFT")

	// A foreign-tenant snapshot is invisible in the publishing transaction;
	// a same-tenant snapshot owned by a different model is rejected as invalid.
	for _, tc := range []struct {
		name       string
		modelID    string
		snapshotID string
		wantCode   errs.ErrCode
	}{
		{name: "cross-tenant snapshot", modelID: model.ID, snapshotID: foreignSnapshot.ID, wantCode: errs.NotFound},
		{name: "snapshot from another model", modelID: model.ID, snapshotID: otherSnapshot.ID, wantCode: errs.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validationTx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
			if err != nil {
				t.Fatal(err)
			}
			err = validateDeviceTemplateSnapshotTx(ctx, validationTx, tenantID, tc.modelID, tc.snapshotID)
			if errs.Code(err) != tc.wantCode {
				_ = validationTx.Rollback()
				t.Fatalf("snapshot validation code = %v, want %v (err %v)", errs.Code(err), tc.wantCode, err)
			}
			if rollbackErr := validationTx.Rollback(); rollbackErr != nil {
				t.Fatal(rollbackErr)
			}
			assertPublishTemplateUnchanged(t, ctx, tenantID, template.ID, "DRAFT")
		})
	}

	if _, err := publishDeviceTemplate(ctx, tenantID, template.ID); errs.Code(err) != errs.InvalidArgument {
		t.Fatalf("invalid protocol config publish code = %v, want InvalidArgument", errs.Code(err))
	}
	assertPublishTemplateUnchanged(t, ctx, tenantID, template.ID, "DRAFT")
}

func TestPublishDeviceTemplateSQLFailureRollsBackWrite(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	model, snapshot := createPublishedThingModel(t, ctx, tenantID)
	template := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://broker.example.test"}`)
	cleanupPublishTestTemplates(t, ctx, tenantID, template.ID)

	// The Encore test role does not own the migrated table, so inject a SQL
	// failure after the publication update rather than altering table DDL.
	tx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publishDeviceTemplateTx(ctx, tx, tenantID, template.ID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("publish in transaction before injected SQL error: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT 1 / 0`); err == nil {
		_ = tx.Rollback()
		t.Fatal("injected SQL error unexpectedly succeeded")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("roll back after SQL error: %v", err)
	}
	assertPublishTemplateUnchanged(t, ctx, tenantID, template.ID, "DRAFT")
}

func TestPublishDeviceTemplateSerializesConcurrentDuplicateRequests(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	model, snapshot := createPublishedThingModel(t, ctx, tenantID)
	template := createPublishTestTemplate(t, ctx, tenantID, model.ID, snapshot.ID, `{"broker_url":"mqtt://broker.example.test"}`)
	cleanupPublishTestTemplates(t, ctx, tenantID, template.ID)

	lockTx, err := beginTenantScopedSQLTx(ctx, tenantID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer lockTx.Rollback()
	var lockedStatus string
	if err := lockTx.QueryRow(ctx, `SELECT status FROM device_access_templates WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, template.ID).Scan(&lockedStatus); err != nil {
		t.Fatal(err)
	}
	if lockedStatus != "DRAFT" {
		t.Fatalf("fixture status = %q, want DRAFT", lockedStatus)
	}

	start := make(chan struct{})
	started := make(chan struct{}, 2)
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			started <- struct{}{}
			<-start
			_, err := publishDeviceTemplate(ctx, tenantID, template.ID)
			results <- err
		}()
	}
	<-started
	<-started
	close(start)

	if err := waitForDeviceTemplatePublishLockWaiters(t, 2); err != nil {
		_ = lockTx.Rollback()
		<-results
		<-results
		t.Fatal(err)
	}
	if err := lockTx.Commit(); err != nil {
		t.Fatal(err)
	}

	var successCount, failedPreconditionCount int
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successCount++
			continue
		}
		if errs.Code(err) == errs.FailedPrecondition && strings.Contains(err.Error(), "cannot transition from PUBLISHED to PUBLISHED") {
			failedPreconditionCount++
			continue
		}
		t.Fatalf("concurrent publish result = %v (code %v), want one success and one published-state precondition", err, errs.Code(err))
	}
	if successCount != 1 || failedPreconditionCount != 1 {
		t.Fatalf("concurrent outcomes success=%d failed-precondition=%d, want 1 each", successCount, failedPreconditionCount)
	}
	assertPublishTemplateUnchanged(t, ctx, tenantID, template.ID, "PUBLISHED")
}

func createPublishTestTemplate(t *testing.T, ctx context.Context, tenantID, modelID, snapshotID, protocolConfig string) *DeviceTemplate {
	t.Helper()
	created, err := createDeviceTemplate(ctx, tenantID, &DeviceTemplate{
		Name:                 "publish-tx-" + uuid.NewString(),
		ThingModelID:         modelID,
		ThingModelSnapshotID: snapshotID,
		ProtocolType:         "MQTT",
		ProtocolConfig:       []byte(protocolConfig),
		Status:               "DRAFT",
	})
	if err != nil {
		t.Fatalf("create publish fixture: %v", err)
	}
	return created
}

func cleanupPublishTestTemplates(t *testing.T, ctx context.Context, tenantID string, ids ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, id := range ids {
			if _, err := db.Exec(ctx, `DELETE FROM device_access_templates WHERE tenant_id=$1 AND id=$2`, tenantID, id); err != nil {
				t.Errorf("delete publish test template %s: %v", id, err)
			}
		}
	})
}

func assertPublishTemplateUnchanged(t *testing.T, ctx context.Context, tenantID, id, wantStatus string) {
	t.Helper()
	var status string
	if err := db.QueryRow(ctx, `SELECT status FROM device_access_templates WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus {
		t.Fatalf("template status = %q, want %q", status, wantStatus)
	}
}

func waitForDeviceTemplatePublishLockWaiters(t *testing.T, want int) error {
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
			return fmt.Errorf("observe concurrent publish lock waiters: %w", err)
		}
		if count >= want {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %d concurrent template row-lock waiters", want)
}
