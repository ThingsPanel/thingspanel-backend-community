package devicemetadata

import (
	"context"
	"testing"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

func TestNativeTemplateDeleteRefusesUsedTemplatesAndIsConditionalAndIdempotent(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, otherTenant := testTenantID(), testTenantID()
	identity := nativeTemplateWriteTestIdentity(tenantID)
	created, err := createNativeTemplate(ctx, identity, "template-del-create-"+uuid.NewString(), nativeTemplateWriteTestBody(t, "delete-me-"+uuid.NewString(), nativeTemplateMQTTConfiguration), "delete-create-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatal(err)
	}
	template := nativeTemplateWriteTestResult(t, created)

	// A stale ETag is refused before anything is checked or changed.
	if _, err := deleteNativeTemplate(ctx, identity, template.ID, "template-del-stale-"+uuid.NewString(), `"sha256-stale"`, "stale-request"); errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("stale delete: %v", err)
	}

	// A live device that uses the template blocks the delete.
	deviceID := uuid.NewString()
	fixtureTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertNativeReadDevice(ctx, fixtureTx, tenantID, deviceID, "tpl-del-"+uuid.NewString()[:8], "uses template", `{}`, true); err != nil {
		fixtureTx.Rollback()
		t.Fatal(err)
	}
	if _, err := fixtureTx.Exec(ctx, `UPDATE device_access_bindings SET template_id=$3,configuration_generation=configuration_generation+1 WHERE tenant_id=$1 AND device_id=$2`, tenantID, deviceID, template.ID); err != nil {
		fixtureTx.Rollback()
		t.Fatal(err)
	}
	if err := fixtureTx.Commit(); err != nil {
		t.Fatal(err)
	}
	boundTemplate, err := readNativeTemplate(ctx, nativeTemplateReadIdentity{tenantID: tenantID}, template.ID)
	if err != nil {
		t.Fatal(err)
	}
	boundETag, err := nativeTemplateETag(boundTemplate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deleteNativeTemplate(ctx, identity, template.ID, "template-del-used-"+uuid.NewString(), boundETag, "used-request"); err == nil || !errorsIsInUse(err) {
		t.Fatalf("used template must be refused: %v", err)
	}
	if _, err := readNativeTemplate(ctx, nativeTemplateReadIdentity{tenantID: tenantID}, template.ID); err != nil {
		t.Fatalf("refused delete must leave the template readable: %v", err)
	}

	// (the tenant-scoped delete below reuses the bound ETag)
	// Another tenant cannot see it, let alone delete it.
	if _, err := deleteNativeTemplate(ctx, nativeTemplateWriteTestIdentity(otherTenant), template.ID, "template-del-cross-"+uuid.NewString(), boundETag, "cross-request"); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant delete: %v", err)
	}

	// Once the device no longer uses it, the delete succeeds and replays safely.
	if _, err := db.Exec(ctx, `UPDATE devices SET deleted_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantID, deviceID); err != nil {
		t.Fatal(err)
	}
	freeTemplate, err := readNativeTemplate(ctx, nativeTemplateReadIdentity{tenantID: tenantID}, template.ID)
	if err != nil {
		t.Fatal(err)
	}
	freeETag, err := nativeTemplateETag(freeTemplate)
	if err != nil {
		t.Fatal(err)
	}
	key := "template-del-ok-" + uuid.NewString()
	deleted, err := deleteNativeTemplate(ctx, identity, template.ID, key, freeETag, "delete-request")
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Status != 200 {
		t.Fatalf("delete status %d", deleted.Status)
	}
	replay, err := deleteNativeTemplate(ctx, identity, template.ID, key, freeETag, "other-request-id")
	if err != nil || !replay.Replayed || string(replay.Body) != string(deleted.Body) {
		t.Fatalf("delete replay: %+v %v", replay, err)
	}
	if _, err := readNativeTemplate(ctx, nativeTemplateReadIdentity{tenantID: tenantID}, template.ID); errs.Code(err) != errs.NotFound {
		t.Fatalf("deleted template must read as not found: %v", err)
	}
	var audits int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM metadata_audit_events WHERE tenant_id=$1 AND resource_id=$2 AND operation_name=$3`, tenantID, template.ID, deleteNativeTemplateOperation).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("delete audit events=%d, want 1 (replay writes none)", audits)
	}
}

func errorsIsInUse(err error) bool {
	return err == errNativeTemplateInUse || (errs.Code(err) == errs.FailedPrecondition && err.Error() == errNativeTemplateInUse.Error())
}
