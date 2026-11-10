package devicemetadata

import (
	"context"
	"testing"

	"encore.dev/beta/errs"
)

func TestNativeSubdevicesReadScopeAndPagination(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	f := seedNativeDeviceReadFixture(t, ctx)
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(ctx, `UPDATE devices SET device_kind='gateway' WHERE tenant_id=$1 AND id=$2`, f.tenantA, f.devices[0]); err != nil {
		t.Fatal(err)
	}
	for _, id := range f.devices[1:3] {
		if _, err = tx.Exec(ctx, `UPDATE devices SET device_kind='subdevice' WHERE tenant_id=$1 AND id=$2`, f.tenantA, id); err != nil {
			t.Fatal(err)
		}
		// $2 is both a uuid column value and the address text; pass the address
		// as its own parameter so PostgreSQL does not have to deduce one type
		// for a placeholder used in two incompatible contexts.
		if _, err = tx.Exec(ctx, `INSERT INTO gateway_memberships(tenant_id,child_device_id,gateway_device_id,subdevice_address,updated_by) VALUES($1,$2,$3,$4,'test')`, f.tenantA, id, f.devices[0], id); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	identity := nativeDeviceReadIdentity{tenantID: f.tenantA, deviceIDs: []string{f.devices[0], f.devices[1]}}
	result, err := readNativeDevicePageForGateway(ctx, identity, decodeNativeDeviceReadQuery(t, "page=1&pageSize=1"), "subdevices", f.devices[0])
	if err != nil {
		t.Fatal(err)
	}
	if result.Data.Total != 1 || len(result.Data.Items) != 1 || result.Data.Items[0].ID != f.devices[1] {
		t.Fatalf("scope not applied before pagination: %#v", result.Data)
	}
	if _, err = readNativeDevicePageForGateway(ctx, identity, decodeNativeDeviceReadQuery(t, ""), "subdevices", f.devices[3]); !hasErrCode(err, errs.NotFound) {
		t.Fatalf("out-of-scope parent: %v", err)
	}
	if _, err = readNativeDevicePageForGateway(ctx, nativeDeviceReadIdentity{tenantID: f.tenantA}, decodeNativeDeviceReadQuery(t, ""), "subdevices", f.tenantBDevice); !hasErrCode(err, errs.NotFound) {
		t.Fatalf("cross-tenant parent: %v", err)
	}
}
