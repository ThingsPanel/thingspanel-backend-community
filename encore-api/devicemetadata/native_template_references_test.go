package devicemetadata

import (
	"context"
	"net/url"
	"testing"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

func TestDecodeNativeReferenceQuery(t *testing.T) {
	page, size, err := decodeNativeReferenceQuery(url.Values{})
	if err != nil || page != 1 || size != 20 {
		t.Fatalf("defaults: %d %d %v", page, size, err)
	}
	if page, size, err = decodeNativeReferenceQuery(url.Values{"page": {"3"}, "pageSize": {"100"}}); err != nil || page != 3 || size != 100 {
		t.Fatalf("explicit: %d %d %v", page, size, err)
	}
	for _, bad := range []url.Values{{"page": {"0"}}, {"pageSize": {"101"}}, {"pageSize": {"x"}}} {
		if _, _, err := decodeNativeReferenceQuery(bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}

func TestNativeTemplateReferencesListLiveDevicesWithPaging(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	identity := nativeTemplateWriteTestIdentity(tenantID)
	created, err := createNativeTemplate(ctx, identity, "template-ref-create-"+uuid.NewString(), nativeTemplateWriteTestBody(t, "referenced-"+uuid.NewString(), nativeTemplateMQTTConfiguration), "ref-create-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatal(err)
	}
	template := nativeTemplateWriteTestResult(t, created)
	read := nativeTemplateReadIdentity{tenantID: tenantID}

	empty, err := readNativeTemplateReferences(ctx, read, template.ID, 1, 20)
	if err != nil || empty.Total != 0 || len(empty.Items) != 0 {
		t.Fatalf("unused template: %+v %v", empty, err)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 3)
	for i := range ids {
		ids[i] = uuid.NewString()
		if err := insertNativeReadDevice(ctx, tx, tenantID, ids[i], "ref-"+uuid.NewString()[:8], []string{"alpha", "bravo", "charlie"}[i], `{}`, true); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE device_access_bindings SET template_id=$3,configuration_generation=configuration_generation+1 WHERE tenant_id=$1 AND device_id=$2`, tenantID, ids[i], template.ID); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	all, err := readNativeTemplateReferences(ctx, read, template.ID, 1, 20)
	if err != nil || all.Total != 3 || len(all.Items) != 3 {
		t.Fatalf("three devices: %+v %v", all, err)
	}
	for _, item := range all.Items {
		if item.Kind != "device" || item.ID == "" || item.DisplayName == "" {
			t.Fatalf("bad item %+v", item)
		}
	}
	first, err := readNativeTemplateReferences(ctx, read, template.ID, 1, 2)
	second, err2 := readNativeTemplateReferences(ctx, read, template.ID, 2, 2)
	if err != nil || err2 != nil || len(first.Items) != 2 || len(second.Items) != 1 || first.Total != 3 || second.Total != 3 || first.Items[0].ID == second.Items[0].ID {
		t.Fatalf("paging: %+v %+v %v %v", first, second, err, err2)
	}

	// A deleted device stops counting; another tenant gets NotFound, not an empty list.
	if _, err := db.Exec(ctx, `UPDATE devices SET deleted_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantID, ids[0]); err != nil {
		t.Fatal(err)
	}
	after, err := readNativeTemplateReferences(ctx, read, template.ID, 1, 20)
	if err != nil || after.Total != 2 {
		t.Fatalf("after delete: %+v %v", after, err)
	}
	if _, err := readNativeTemplateReferences(ctx, nativeTemplateReadIdentity{tenantID: testTenantID()}, template.ID, 1, 20); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant: %v", err)
	}
}
