package devicemetadata

import (
	"context"
	"testing"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

// A device keeps the template it was created with: another template or an
// unbind is refused, repeating the same template is a no-op, and a device that
// has no template can still be bound once.
func TestDeviceTemplateBindingIsFixedAfterCreation(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	templateIdentity := nativeTemplateWriteTestIdentity(tenantID)
	makeTemplate := func(name string) string {
		res, err := createNativeTemplate(ctx, templateIdentity, "bind-lock-"+uuid.NewString(), nativeTemplateWriteTestBody(t, name+uuid.NewString(), nativeTemplateMQTTConfiguration), "bind-lock-request", validateNativeDeviceConfiguration, nil)
		if err != nil {
			t.Fatal(err)
		}
		return nativeTemplateWriteTestResult(t, res).ID
	}
	first, second := makeTemplate("first-"), makeTemplate("second-")

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deviceID := uuid.NewString()
	if err := insertNativeReadDevice(ctx, tx, tenantID, deviceID, "lock-"+uuid.NewString()[:8], "binding lock", `{}`, true); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	identity := nativeWriteIdentity(tenantID)
	etag := func() string {
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		binding, err := loadNativeDeviceTemplateBindingEncoreTx(ctx, tx, tenantID, deviceID, "etag-request")
		if err != nil {
			t.Fatal(err)
		}
		return binding.ETag
	}
	set := func(body string) (*metadataWriteResponse, error) {
		return setNativeDeviceTemplateBinding(ctx, identity, deviceID, "bind-"+uuid.NewString(), etag(), []byte(body), "bind-request-"+uuid.NewString(), validateNativeDeviceConfiguration)
	}

	if _, err := set(`{"templateId":"` + first + `"}`); err != nil {
		t.Fatalf("first binding of an unbound device must work: %v", err)
	}
	if _, err := set(`{"templateId":"` + second + `"}`); errs.Code(err) != errs.Aborted {
		t.Fatalf("changing the template must be refused as a conflict: %v", err)
	}
	if _, err := set(`{"templateId":null}`); errs.Code(err) != errs.Aborted {
		t.Fatalf("unbinding must be refused as a conflict: %v", err)
	}
	if _, err := set(`{"templateId":"` + first + `"}`); err != nil {
		t.Fatalf("repeating the same template: %v", err)
	}
}
