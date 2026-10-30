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

func TestAutomationDirectoryQueryScopesNativeSelectors(t *testing.T) {
	tenant := "11111111-1111-4111-8111-111111111111"
	id := "22222222-2222-4222-8222-222222222222"
	query, args, err := automationDirectoryQuery(tenant, ResolveAutomationDirectoryRequest{Selector: "templates", IDs: []string{id}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"d.tenant_id=$1", "d.deleted_at IS NULL", "b.template_id=ANY($2::uuid[])", "LIMIT 10001"} {
		if !strings.Contains(query, part) {
			t.Fatalf("query missing %q: %s", part, query)
		}
	}
	if len(args) != 2 || args[0] != tenant {
		t.Fatalf("unexpected query args: %#v", args)
	}
}

func TestValidateAutomationScopePostgresTenantAndDeletionFence(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	fixture := seedNativeDeviceReadFixture(t, ctx)
	et.OverrideAuthInfo(auth.UID("native-directory-admin"), &authn.Data{UserID: "native-directory-admin", TenantID: fixture.tenantA, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)
	svc := &Service{}
	validate := func(tenant, kind, id string) error {
		t.Helper()
		_, err := svc.ValidateAutomationScope(ctx, &ValidateAutomationScopeRequest{TenantID: tenant, ScopeType: kind, ScopeID: id})
		return err
	}
	if err := validate(fixture.tenantA, "device", fixture.devices[0]); err != nil {
		t.Fatalf("tenant-owned live device rejected: %v", err)
	}
	for _, id := range []string{fixture.tenantBDevice, uuid.NewString()} {
		if err := validate(fixture.tenantA, "device", id); errs.Code(err) != errs.NotFound {
			t.Errorf("cross-tenant/missing id %s code=%v, want NotFound", id, err)
		}
	}
	if err := validate(fixture.tenantA, "device_config", uuid.NewString()); errs.Code(err) != errs.NotFound {
		t.Errorf("missing template code=%v, want NotFound", err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE devices SET deleted_at=now() WHERE tenant_id=$1 AND id=$2`, fixture.tenantA, fixture.devices[1]); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := validate(fixture.tenantA, "device", fixture.devices[1]); errs.Code(err) != errs.NotFound {
		t.Fatalf("deleted device code=%v, want NotFound", err)
	}
}

func TestAutomationDirectoryQueryLabelAndKindSemantics(t *testing.T) {
	tenant := "11111111-1111-4111-8111-111111111111"
	query, args, err := automationDirectoryQuery(tenant, ResolveAutomationDirectoryRequest{Selector: "labels", Labels: []string{"floor=1", "zone=a"}, MatchAll: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, " AND ") || len(args) != 3 {
		t.Fatalf("match-all labels not represented precisely: %s %#v", query, args)
	}
	query, args, err = automationDirectoryQuery(tenant, ResolveAutomationDirectoryRequest{Selector: "kind", DeviceKind: "3"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "d.device_kind=$2") || args[1] != "subdevice" {
		t.Fatalf("legacy kind was not mapped: %s %#v", query, args)
	}
}

func TestAutomationDirectoryQueryRejectsInvalidAndUnboundedSelectors(t *testing.T) {
	tenant := "11111111-1111-4111-8111-111111111111"
	for _, req := range []ResolveAutomationDirectoryRequest{
		{Selector: "ids", IDs: []string{"not-a-uuid"}},
		{Selector: "ids", IDs: make([]string, 1001)},
		{Selector: "labels", Labels: []string{"floor"}},
		{Selector: "kind", DeviceKind: "sensor"},
	} {
		_, _, err := automationDirectoryQuery(tenant, req, nil)
		if errs.Code(err) != errs.InvalidArgument {
			t.Errorf("request %+v error=%v", req, err)
		}
	}
	if _, err := canonicalAutomationTenant("tenant-short"); errs.Code(err) != errs.InvalidArgument {
		t.Fatalf("non-canonical tenant error=%v", err)
	}
}
