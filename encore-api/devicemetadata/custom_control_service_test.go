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

func TestBeginTenantScopedSQLTxSetsTenantAndReadOnlyMode(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()

	for _, readOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "write", true: "read"}[readOnly], func(t *testing.T) {
			tx, err := beginTenantScopedSQLTx(ctx, tenantID, readOnly)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()

			var configured, readOnlyValue, isolation string
			if err := tx.QueryRow(ctx, `SELECT current_setting('app.current_tenant_id'), current_setting('transaction_read_only'), current_setting('transaction_isolation')`).Scan(&configured, &readOnlyValue, &isolation); err != nil {
				t.Fatal(err)
			}
			if configured != tenantID {
				t.Fatalf("tenant setting = %q, want %q", configured, tenantID)
			}
			wantReadOnly := map[bool]string{false: "off", true: "on"}[readOnly]
			if readOnlyValue != wantReadOnly {
				t.Fatalf("transaction_read_only = %q, want %q", readOnlyValue, wantReadOnly)
			}
			if readOnly && isolation != "repeatable read" {
				t.Fatalf("read isolation = %q, want repeatable read", isolation)
			}
		})
	}
}

func TestBeginTenantScopedSQLTxRejectsInvalidTenantBeforeOpeningTx(t *testing.T) {
	for _, tenantID := range []string{"", uuid.Nil.String(), strings.ToUpper(testTenantID()), " tenant" + testTenantID()} {
		t.Run(tenantID, func(t *testing.T) {
			if tx, err := beginTenantScopedSQLTx(context.Background(), tenantID, false); err == nil {
				_ = tx.Rollback()
				t.Fatalf("tenant %q unexpectedly opened a transaction", tenantID)
			}
		})
	}
}

func TestCustomCommandControlCRUDUsesTenantScopedTransactions(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	userID := "custom-control-test-" + uuid.NewString()
	et.OverrideAuthInfo(auth.UID(userID), &authn.Data{UserID: userID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)

	model := createTestThingModel(t, ctx, tenantID)
	service := &Service{}
	var controlID string
	t.Cleanup(func() {
		if controlID != "" {
			_, _ = db.Exec(ctx, `DELETE FROM custom_command_controls WHERE tenant_id=$1 AND id=$2`, tenantID, controlID)
		}
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantID, model.ID)
	})
	control, err := service.CreateCustomCommandControl(ctx, &CustomCommandControl{
		ThingModelID: model.ID,
		Name:         "control-" + uuid.NewString(),
		Description:  "created in tenant transaction",
		Content:      "{\"command\":1}",
		ControlType:  "WRITE",
	})
	if err != nil {
		t.Fatalf("create custom control: %v", err)
	}
	if control.ID == "" || control.TenantID != tenantID || control.ThingModelID != model.ID || control.EnableStatus != "enable" {
		t.Fatalf("create response mismatch: %#v", control)
	}
	controlID = control.ID

	listed, err := service.ListCustomCommandControls(ctx, &CustomCommandControlListReq{ThingModelID: model.ID})
	if err != nil {
		t.Fatalf("list custom controls: %v", err)
	}
	if listed.Total != 1 || len(listed.Items) != 1 || listed.Items[0].ID != control.ID {
		t.Fatalf("list response mismatch: %#v", listed)
	}
	if _, err := service.UpdateCustomCommandControl(ctx, &CustomCommandControl{
		ID: control.ID, Name: strings.Repeat("x", 129), Content: "invalid update",
	}); err == nil {
		t.Fatal("database-invalid update unexpectedly succeeded")
	}
	readTx, err := beginTenantScopedSQLTx(ctx, tenantID, true)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := getCustomCommandControlTx(ctx, readTx, tenantID, control.ID)
	_ = readTx.Rollback()
	if err != nil || unchanged.Name != control.Name {
		t.Fatalf("failed update changed persisted control: control=%#v err=%v", unchanged, err)
	}

	updated, err := service.UpdateCustomCommandControl(ctx, &CustomCommandControl{
		ID: control.ID, Name: "updated-" + uuid.NewString(), Description: "updated description",
		Content: "{\"command\":2}", EnableStatus: "disable", ControlType: "READ",
	})
	if err != nil {
		t.Fatalf("update custom control: %v", err)
	}
	if updated.Name == control.Name || updated.Description != "updated description" || updated.Content != "{\"command\":2}" || updated.EnableStatus != "disable" || updated.ControlType != "READ" {
		t.Fatalf("update response mismatch: %#v", updated)
	}

	if err := service.DeleteCustomCommandControl(ctx, control.ID); err != nil {
		t.Fatalf("delete custom control: %v", err)
	}
	listed, err = service.ListCustomCommandControls(ctx, &CustomCommandControlListReq{ThingModelID: model.ID})
	if err != nil || listed.Total != 0 || len(listed.Items) != 0 {
		t.Fatalf("list after delete mismatch: response=%#v err=%v", listed, err)
	}
}

func TestCustomCommandControlCreateEnforcesTenantOwnershipAndRollsBack(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	modelTenant := testTenantID()
	requestTenant := testTenantID()
	model := createTestThingModel(t, ctx, modelTenant)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, modelTenant, model.ID)
	})
	userID := "custom-control-owner-test-" + uuid.NewString()
	et.OverrideAuthInfo(auth.UID(userID), &authn.Data{UserID: userID, TenantID: requestTenant, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)

	_, err := (&Service{}).CreateCustomCommandControl(ctx, &CustomCommandControl{
		ThingModelID: model.ID, Name: "foreign", Content: "should not persist",
	})
	if err == nil {
		t.Fatal("create accepted a ThingModel owned by another tenant")
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM custom_command_controls WHERE tenant_id=$1 AND thing_model_id=$2`, requestTenant, model.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("foreign model check left %d custom controls", count)
	}

	et.OverrideAuthInfo(auth.UID(userID), &authn.Data{UserID: userID, TenantID: modelTenant, Roles: []string{"TENANT_ADMIN"}})
	_, err = (&Service{}).CreateCustomCommandControl(ctx, &CustomCommandControl{
		ThingModelID: model.ID, Name: strings.Repeat("x", 129), Content: "insert must roll back",
	})
	if err == nil {
		t.Fatal("create with a database-invalid name unexpectedly succeeded")
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM custom_command_controls WHERE tenant_id=$1 AND thing_model_id=$2`, modelTenant, model.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed create left %d controls", count)
	}
}

func TestCustomCommandControlRejectsNilAuthenticatedTenant(t *testing.T) {
	userID := "custom-control-invalid-tenant-" + uuid.NewString()
	et.OverrideAuthInfo(auth.UID(userID), &authn.Data{UserID: userID, TenantID: uuid.Nil.String(), Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)

	if _, err := (&Service{}).ListCustomCommandControls(context.Background(), &CustomCommandControlListReq{ThingModelID: uuid.NewString()}); err == nil {
		t.Fatal("list accepted nil authenticated tenant")
	} else if errs.Code(err) != errs.Unauthenticated {
		t.Fatalf("nil authenticated tenant error code = %v, want unauthenticated", errs.Code(err))
	}
}
