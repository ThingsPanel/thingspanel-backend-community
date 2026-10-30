package devicemetadata

import (
	"context"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/et"
	"github.com/google/uuid"
)

func TestDeviceTemplateReadPathsUseTenantScopedTransactions(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	userID := "device-template-read-" + uuid.NewString()
	et.OverrideAuthInfo(auth.UID(userID), &authn.Data{UserID: userID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)

	model, snapshot := createPublishedThingModel(t, ctx, tenantID)
	templateA, err := createDeviceTemplate(ctx, tenantID, &DeviceTemplate{
		Name: "template-read-a-" + uuid.NewString(), ThingModelID: model.ID, ThingModelSnapshotID: snapshot.ID,
		ProtocolType: "MQTT", ProtocolConfig: []byte(`{"broker":"a"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	templateB, err := createDeviceTemplate(ctx, tenantID, &DeviceTemplate{
		Name: "template-read-b-" + uuid.NewString(), ThingModelID: model.ID, ThingModelSnapshotID: snapshot.ID,
		ProtocolType: "MQTT", ProtocolConfig: []byte(`{"broker":"b"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM device_access_templates WHERE tenant_id=$1 AND id IN ($2,$3)`, tenantID, templateA.ID, templateB.ID)
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantID, model.ID)
	})
	if _, err := db.Exec(ctx, `UPDATE device_access_templates SET updated_at=NOW()-INTERVAL '1 hour' WHERE tenant_id=$1 AND id=$2`, tenantID, templateA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE device_access_templates SET updated_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantID, templateB.ID); err != nil {
		t.Fatal(err)
	}

	tx, err := beginTenantScopedSQLTx(ctx, tenantID, true)
	if err != nil {
		t.Fatal(err)
	}
	var configured, readOnly, isolation string
	if err := tx.QueryRow(ctx, `SELECT current_setting('app.current_tenant_id'), current_setting('transaction_read_only'), current_setting('transaction_isolation')`).Scan(&configured, &readOnly, &isolation); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if configured != tenantID || readOnly != "on" || isolation != "repeatable read" {
		t.Fatalf("tenant read transaction settings = tenant:%q readonly:%q isolation:%q", configured, readOnly, isolation)
	}

	service := &Service{}
	page, err := service.ListTemplates(ctx, &ListDeviceTemplatesReq{pageRequest: pageRequest{Page: 1, PageSize: 1}, ThingModelID: model.ID})
	if err != nil {
		t.Fatalf("list templates: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != templateB.ID || page.Page != 1 || page.PageSize != 1 {
		t.Fatalf("list templates should preserve total, pagination and updated_at ordering: %#v", page)
	}

	detail, err := service.GetTemplate(ctx, templateA.ID)
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	if detail.ID != templateA.ID || detail.TenantID != tenantID || detail.ThingModelID != model.ID {
		t.Fatalf("get template response mismatch: %#v", detail)
	}

	byModel, err := service.ListTemplatesByThingModel(ctx, model.ID)
	if err != nil {
		t.Fatalf("list templates by ThingModel: %v", err)
	}
	if len(byModel.Items) != 2 || byModel.Items[0].ID != templateB.ID || byModel.Items[1].ID != templateA.ID {
		t.Fatalf("ThingModel template list should preserve tenant filter and updated_at ordering: %#v", byModel)
	}
}
