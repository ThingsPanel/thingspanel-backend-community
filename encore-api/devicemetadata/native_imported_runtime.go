package devicemetadata

import (
	"context"
	"encore.app/pkg/dbx"
	"encore.dev/beta/errs"
)

// ImportedCommunityRuntimeProviderID marks retained Community-owned access.
// Import preserves the existing voucher and route; it does not transfer credentials.
const ImportedCommunityRuntimeProviderID = "760c2298-8b97-51f1-bd71-2737d6278f21"

func isImportedCommunityRuntimeRef(ref *RuntimeRef) bool {
	return ref != nil && ref.ProviderID == ImportedCommunityRuntimeProviderID && ref.Namespace == "community" && ref.ExternalID != ""
}
func requireNativeCredentialOwnershipTx(ctx context.Context, tx dbx.Tx, tenantID, deviceID string) error {
	return requireNativeRuntimeManagementTx(ctx, tx, tenantID, deviceID, "credential ownership")
}

func requireNativeRuntimeManagementTx(ctx context.Context, tx dbx.Tx, tenantID, deviceID, operation string) error {
	var imported bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_access_bindings WHERE tenant_id=$1 AND device_id=$2 AND runtime_provider_id=$3 AND runtime_namespace='community' AND runtime_external_id IS NOT NULL)`, tenantID, deviceID, ImportedCommunityRuntimeProviderID).Scan(&imported); err != nil {
		return err
	}
	if imported {
		return &errs.Error{Code: errs.FailedPrecondition, Message: "device runtime remains managed by the original Community connection; " + operation + " has not been transferred"}
	}
	return nil
}
