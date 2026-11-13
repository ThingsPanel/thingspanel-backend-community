package devicemetadata

import (
	"context"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestImportedAccessProjectionAndBindingGuard(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	result, err := createNativeDevice(ctx, nativeWriteIdentity(tenant), "import-origin-"+uuid.NewString(), nativeCreateBody("import-origin-"+uuid.NewString(), "imported"), "import-origin-create", nil)
	if err != nil {
		t.Fatal(err)
	}
	device := nativeWriteDevice(t, result)
	_, err = db.Exec(ctx, `INSERT INTO imported_device_origins
 (tenant_id,device_id,source_owner,source_database,source_deployment_id,source_tenant_id,source_device_id,
 source_template_id,source_template_name,source_protocol,source_access_way,source_service_access_id,
 source_row_sha256,source_template_sha256,source_archive_sha256,imported_by)
 VALUES($1,$2,'community','source_db','deployment',$6,$3,$4,'Cloud template','CTWING','B','cloud-instance',$5,$5,$5,'test')`,
		tenant, device.ID, device.ID, uuid.NewString(), strings.Repeat("a", 64), tenant)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := readNativeDeviceTemplateBinding(ctx, nativeDeviceReadIdentity{tenantID: tenant}, device.ID, "read-import-origin")
	if err != nil {
		t.Fatal(err)
	}
	origin := binding.Data.ImportedAccess
	if origin == nil || origin.Protocol == nil || *origin.Protocol != "CTWING" || !origin.ReadOnly || origin.SourceDeviceID != device.ID {
		t.Fatalf("projection: %+v", origin)
	}
	if binding.Data.TemplateID != nil || binding.Data.DirectAccess != nil {
		t.Fatal("import invented a native configuration")
	}
	if _, err := readNativeDeviceTemplateBinding(ctx, nativeDeviceReadIdentity{tenantID: testTenantID()}, device.ID, "other-tenant"); err == nil {
		t.Fatal("cross tenant read allowed")
	}
	_, err = setNativeDeviceTemplateBinding(ctx, nativeWriteIdentity(tenant), device.ID, "binding-origin-"+uuid.NewString(), binding.ETag, []byte(`{"templateId":null}`), "bind-import", nil)
	if err == nil || !strings.Contains(err.Error(), "source owner") {
		t.Fatalf("binding not blocked: %v", err)
	}
	// Origin evidence cannot silently be changed or removed after import.
	if _, err = db.Exec(ctx, `UPDATE imported_device_origins SET source_protocol='mqtt' WHERE tenant_id=$1 AND device_id=$2`, tenant, device.ID); err == nil {
		t.Fatal("origin mutation allowed")
	}
	if _, err = db.Exec(ctx, `DELETE FROM imported_device_origins WHERE tenant_id=$1 AND device_id=$2`, tenant, device.ID); err == nil {
		t.Fatal("origin removal allowed")
	}
}
