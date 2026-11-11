package devicemetadata

import (
	"context"
	"os"
	"testing"

	"encore.app/devicemetadata/mysqlschema"
	"encore.app/pkg/dbx"
	"encore.app/pkg/sqldialect"
	"github.com/google/uuid"
)

// TestMySQLThingModelTenantContract exercises create, point-read, and paginated list against MySQL itself.
// TP_DM_MYSQL_DSN must point to an isolated device_metadata test database.
func TestMySQLThingModelTenantContract(t *testing.T) {
	dsn := os.Getenv("TP_DM_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TP_DM_MYSQL_DSN required")
	}
	pool, err := sqldialect.OpenMySQL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	conn, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i, statement := range mysqlschema.Statements() {
		if _, err := conn.ExecContext(context.Background(), statement); err != nil {
			_ = conn.Close()
			t.Fatalf("apply MySQL metadata schema statement %d: %v", i+1, err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	previous := db
	UseDatabase(dbx.SQL(pool, "mysql"))
	t.Cleanup(func() { UseDatabase(previous) })

	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	model, err := createThingModel(ctx, tenantA, &ThingModel{Name: "mysql-contract-" + uuid.NewString(), Status: "DRAFT"})
	if err != nil {
		t.Fatalf("createThingModel on MySQL: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantA, model.ID)
	})
	updated, err := updateThingModel(ctx, tenantA, model.ID, &ThingModel{
		Name:            "mysql-contract-updated-" + uuid.NewString(),
		DescriptionI18n: model.DescriptionI18n,
		Category:        "updated-category",
	})
	if err != nil {
		t.Fatalf("updateThingModel for owning tenant on MySQL: %v", err)
	}
	if updated.ID != model.ID || updated.TenantID != tenantA || updated.Category != "updated-category" {
		t.Fatalf("owning tenant update returned unexpected model: %+v", updated)
	}
	model = updated
	if _, err := updateThingModel(ctx, tenantB, model.ID, &ThingModel{Name: "mysql-contract-cross-tenant-" + uuid.NewString()}); err == nil {
		t.Fatal("tenant B updated tenant A's thing model")
	}
	deviceID := uuid.NewString()
	deviceNumber := "mysql-device-" + uuid.NewString()
	deviceTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin device number fixture transaction on MySQL: %v", err)
	}
	if err := insertNativeReadDevice(ctx, deviceTx, tenantA, deviceID, deviceNumber, "mysql-contract-device", "{}", false); err != nil {
		_ = deviceTx.Rollback()
		t.Fatalf("insert device number fixture on MySQL: %v", err)
	}
	if err := deviceTx.Commit(); err != nil {
		t.Fatalf("commit device number fixture on MySQL: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(ctx, `DELETE FROM devices WHERE tenant_id=$1 AND id=$2`, tenantA, deviceID) })
	for _, tc := range []struct {
		name    string
		tenant  string
		exclude *string
		want    bool
	}{
		{name: "same tenant collision", tenant: tenantA, want: false},
		{name: "same tenant excludes current device", tenant: tenantA, exclude: &deviceID, want: true},
		{name: "other tenant is isolated", tenant: tenantB, want: true},
	} {
		got, err := checkNativeDeviceNumber(ctx, tc.tenant, deviceNumberCheckInput{DeviceNumber: deviceNumber, ExcludeDeviceID: tc.exclude})
		if err != nil {
			t.Fatalf("checkNativeDeviceNumber %s on MySQL: %v", tc.name, err)
		}
		if got.Available != tc.want {
			t.Errorf("checkNativeDeviceNumber %s on MySQL: available=%v, want %v", tc.name, got.Available, tc.want)
		}
	}
	if _, err := getThingModelByID(ctx, tenantB, model.ID); err == nil {
		t.Fatal("tenant B read a tenant A thing model")
	}
	tx, err := beginTenantScopedSQLTx(ctx, tenantA, true)
	if err != nil {
		t.Fatalf("begin tenant A read transaction on MySQL: %v", err)
	}
	var found int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantA, model.ID).Scan(&found); err != nil {
		_ = tx.Rollback()
		t.Fatalf("tenant A read transaction on MySQL: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tenant A read transaction on MySQL: %v", err)
	}
	tx, err = beginTenantScopedSQLTx(ctx, tenantB, false)
	if err != nil {
		t.Fatalf("begin tenant B write transaction on MySQL: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT 1 FROM thing_models WHERE tenant_id=$1 AND id=$2`, tenantB, model.ID).Scan(&found); !dbx.IsNoRows(err) {
		_ = tx.Rollback()
		t.Fatalf("tenant B transaction read error=%v, want no rows", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tenant B read transaction on MySQL: %v", err)
	}
	itemsA, totalA, err := listThingModels(ctx, tenantA, &ThingModelListReq{
		Search:      "MYSQL-CONTRACT",
		pageRequest: pageRequest{Page: 1, PageSize: 50},
	})
	if err != nil {
		t.Fatalf("listThingModels for tenant A on MySQL: %v", err)
	}
	if totalA != 1 || len(itemsA) != 1 || itemsA[0].ID != model.ID {
		t.Fatalf("tenant A list did not return its row with case-insensitive search: total=%d items=%+v", totalA, itemsA)
	}
	items, total, err := listThingModels(ctx, tenantB, &ThingModelListReq{
		pageRequest: pageRequest{Page: 1, PageSize: 50},
	})
	if err != nil {
		t.Fatalf("listThingModels for tenant B on MySQL: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("tenant B received tenant A rows: total=%d items=%+v", total, items)
	}
}
