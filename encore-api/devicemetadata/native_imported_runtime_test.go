package devicemetadata

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestImportedRuntimeKeepsCredentialOwnership(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant, device := testTenantID(), uuid.NewString()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := insertNativeReadDevice(ctx, tx, tenant, device, "import-"+device[:8], "imported", `{}`, true); err != nil {
		t.Fatal(err)
	}
	if err := requireNativeCredentialOwnershipTx(ctx, tx, tenant, device); err != nil {
		t.Fatal("new device blocked", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE device_access_bindings SET runtime_provider_id=$3,runtime_namespace='community',runtime_external_id=$4 WHERE tenant_id=$1 AND device_id=$2`, tenant, device, ImportedCommunityRuntimeProviderID, device); err != nil {
		t.Fatal(err)
	}
	if err := requireNativeCredentialOwnershipTx(ctx, tx, tenant, device); err == nil {
		t.Fatal("import allowed credential overwrite")
	}
	for _, operation := range []string{"device deletion", "device renumbering", "gateway membership", "administrative state"} {
		if err := requireNativeRuntimeManagementTx(ctx, tx, tenant, device, operation); err == nil {
			t.Fatal("import allowed runtime mutation", operation)
		}
	}
	ref := &RuntimeRef{ProviderID: ImportedCommunityRuntimeProviderID, Namespace: "community", ExternalID: device}
	if !isImportedCommunityRuntimeRef(ref) {
		t.Fatal("import marker missing")
	}
	id, err := resolveRuntimeDeviceID(ref, device)
	if err != nil || id != device {
		t.Fatal("read/control identity lost", err)
	}
	ref.ProviderID = uuid.NewString()
	if isImportedCommunityRuntimeRef(ref) {
		t.Fatal("unrelated runtime misclassified")
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM device_credentials WHERE tenant_id=$1 AND device_id=$2`, tenant, device).Scan(&count); err != nil || count != 0 {
		t.Fatal("guard created credentials", err)
	}
}
