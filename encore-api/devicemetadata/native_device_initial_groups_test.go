package devicemetadata

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

func TestResolveNativeInitialGroupsNoSQLBranchesAndScopeErrors(t *testing.T) {
	ctx := context.Background()
	validTenant := testTenantID()
	if got, err := resolveNativeInitialGroupsTx(ctx, nil, "bad-tenant", NativeDeviceField[[]string]{Present: true, Value: []string{}}, false); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("explicit [] must avoid SQL: groups=%v err=%v", got, err)
	}
	if got, err := resolveNativeInitialGroupsTx(ctx, nil, "bad-tenant", NativeDeviceField[[]string]{}, false); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("omitted/no group write permission must avoid SQL: groups=%v err=%v", got, err)
	}
	if _, err := resolveNativeInitialGroupsTx(ctx, nil, "bad-tenant", NativeDeviceField[[]string]{Present: true, Value: []string{uuid.NewString()}}, false); !hasErrCode(err, errs.PermissionDenied) {
		t.Fatalf("permission check must precede SQL/scope use: err=%v", err)
	}
	if _, err := resolveNativeInitialGroupsTx(ctx, nil, validTenant, NativeDeviceField[[]string]{}, true); !hasErrCode(err, errs.Internal) {
		t.Fatalf("SQL path with nil transaction error=%v, want Internal", err)
	}
	for _, tenant := range []string{"bad-tenant", "00000000-0000-0000-0000-000000000000", "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"} {
		if _, err := resolveNativeInitialGroupsTx(ctx, nil, tenant, NativeDeviceField[[]string]{}, true); !hasErrCode(err, errs.Unauthenticated) {
			t.Errorf("non-canonical/zero tenant %q error=%v, want Unauthenticated", tenant, err)
		}
	}
}

func TestResolveNativeInitialGroupsTx(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := resolveNativeInitialGroupsTx(ctx, tx, "bad-tenant", NativeDeviceField[[]string]{}, true); !hasErrCode(err, errs.Unauthenticated) {
		t.Fatalf("SQL path with malformed tenant error=%v, want Unauthenticated", err)
	}

	tenantA, tenantB, tenantMulti := testTenantID(), testTenantID(), testTenantID()
	rootA, childA, otherTenantGroup := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := insertInitialGroupFixture(ctx, tx, tenantA, rootA, nil); err != nil {
		t.Fatal(err)
	}
	if err := insertInitialGroupFixture(ctx, tx, tenantA, childA, &rootA); err != nil {
		t.Fatal(err)
	}
	if err := insertInitialGroupFixture(ctx, tx, tenantB, otherTenantGroup, nil); err != nil {
		t.Fatal(err)
	}
	if err := insertInitialGroupFixture(ctx, tx, tenantMulti, uuid.NewString(), nil); err != nil {
		t.Fatal(err)
	}
	if err := insertInitialGroupFixture(ctx, tx, tenantMulti, uuid.NewString(), nil); err != nil {
		t.Fatal(err)
	}

	noWriteOmitted, err := resolveNativeInitialGroupsTx(ctx, tx, tenantA, NativeDeviceField[[]string]{}, false)
	if err != nil || len(noWriteOmitted) != 0 {
		t.Fatalf("omitted/no group permission = %v err=%v, want no groups", noWriteOmitted, err)
	}
	explicitEmpty, err := resolveNativeInitialGroupsTx(ctx, tx, tenantA, NativeDeviceField[[]string]{Present: true, Value: []string{}}, false)
	if err != nil || explicitEmpty == nil || len(explicitEmpty) != 0 {
		t.Fatalf("explicit [] = %v err=%v, want empty group set", explicitEmpty, err)
	}
	defaultRoot, err := resolveNativeInitialGroupsTx(ctx, tx, tenantA, NativeDeviceField[[]string]{}, true)
	if err != nil || !reflect.DeepEqual(defaultRoot, []string{rootA}) {
		t.Fatalf("single-root default = %v err=%v, want %s", defaultRoot, err, rootA)
	}
	noDefaultForMultipleRoots, err := resolveNativeInitialGroupsTx(ctx, tx, tenantMulti, NativeDeviceField[[]string]{}, true)
	if err != nil || noDefaultForMultipleRoots == nil || len(noDefaultForMultipleRoots) != 0 {
		t.Fatalf("multiple-root default = %v err=%v, want no groups", noDefaultForMultipleRoots, err)
	}

	requested := []string{childA, rootA}
	sortedRequested := []string{rootA, childA}
	if sortedRequested[0] > sortedRequested[1] {
		sortedRequested[0], sortedRequested[1] = sortedRequested[1], sortedRequested[0]
	}
	resolved, err := resolveNativeInitialGroupsTx(ctx, tx, tenantA, NativeDeviceField[[]string]{Present: true, Value: requested}, true)
	if err != nil || !reflect.DeepEqual(resolved, sortedRequested) {
		t.Fatalf("explicit groups = %v err=%v, want canonical sorted IDs %v", resolved, err, sortedRequested)
	}
	if _, err := resolveNativeInitialGroupsTx(ctx, tx, tenantA, NativeDeviceField[[]string]{Present: true, Value: []string{rootA}}, false); !hasErrCode(err, errs.PermissionDenied) {
		t.Fatalf("explicit non-empty without write permission error=%v, want PermissionDenied", err)
	}
	if _, err := resolveNativeInitialGroupsTx(ctx, tx, tenantA, NativeDeviceField[[]string]{Present: true, Value: []string{otherTenantGroup}}, true); !hasErrCode(err, errs.NotFound) {
		t.Fatalf("cross-tenant group error=%v, want NotFound", err)
	}
	if _, err := resolveNativeInitialGroupsTx(ctx, tx, tenantA, NativeDeviceField[[]string]{Present: true, Value: []string{rootA, strings.ToUpper(rootA)}}, true); !hasErrCode(err, errs.InvalidArgument) {
		t.Fatalf("duplicate case-variant UUID error=%v, want InvalidArgument", err)
	}
	if _, err := resolveNativeInitialGroupsTx(ctx, tx, tenantA, NativeDeviceField[[]string]{Present: true, Null: true}, true); !hasErrCode(err, errs.InvalidArgument) {
		t.Fatalf("explicit null error=%v, want InvalidArgument", err)
	}
	if _, err := resolveNativeInitialGroupsTx(ctx, tx, tenantA, NativeDeviceField[[]string]{Present: true, Value: []string{rootA, rootA}}, true); !hasErrCode(err, errs.InvalidArgument) {
		t.Fatalf("duplicate UUID error=%v, want InvalidArgument", err)
	}
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = rootA
	}
	if _, err := resolveNativeInitialGroupsTx(ctx, tx, tenantA, NativeDeviceField[[]string]{Present: true, Value: tooMany}, true); !hasErrCode(err, errs.InvalidArgument) {
		t.Fatalf("more than 100 requested groups error=%v, want InvalidArgument", err)
	}
	var memberCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM device_group_members WHERE tenant_id=$1`, tenantA).Scan(&memberCount); err != nil || memberCount != 0 {
		t.Fatalf("resolver unexpectedly wrote group memberships: count=%d err=%v", memberCount, err)
	}
}

func insertInitialGroupFixture(ctx context.Context, tx *sqldb.Tx, tenantID, id string, parentID *string) error {
	_, err := tx.Exec(ctx, `INSERT INTO device_groups(id,tenant_id,parent_id,name,created_by,updated_by) VALUES($1,$2,$3,$4,'initial-groups-test','initial-groups-test')`, id, tenantID, parentID, "initial-group-"+id)
	return err
}
