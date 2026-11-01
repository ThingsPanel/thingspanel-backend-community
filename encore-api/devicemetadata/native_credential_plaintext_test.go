package devicemetadata

import (
	"context"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/et"
)

func TestDeviceCredentialListShowsPlaintextUntilRevoked(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, subjectID := testTenantID(), "credential-plaintext-test"
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	deviceID := insertCredentialSchemaDevice(t, ctx, tx, tenantID)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM metadata_idempotency WHERE tenant_id=$1`,
			`DELETE FROM metadata_audit_events WHERE tenant_id=$1`,
			`DELETE FROM device_credentials WHERE tenant_id=$1`,
			`DELETE FROM device_runtime_outbox WHERE tenant_id=$1`,
			`DELETE FROM metadata_operations WHERE tenant_id=$1`,
			`DELETE FROM devices WHERE tenant_id=$1`,
		} {
			if _, err := db.Exec(ctx, q, tenantID); err != nil {
				t.Errorf("cleanup %q: %v", q, err)
			}
		}
	})
	et.OverrideAuthInfo(auth.UID(subjectID), &authn.Data{UserID: subjectID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)
	writeIdentity, err := currentNativeCredentialWriteIdentity()
	if err != nil {
		t.Fatal(err)
	}
	protector := testCredentialProtector(t)
	created, err := createNativeDeviceCredential(ctx, writeIdentity, deviceID, lifecycleKey("plain"), []byte(`{"kind":"token"}`), "plain-create", protector)
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	secretResponse := decodeCredentialSecretResponse(t, created.Body)
	if secretResponse.Secret == nil || *secretResponse.Secret == "" {
		t.Fatal("create returned no secret")
	}
	credentialID := secretResponse.Credential.ID

	readIdentity := nativeDeviceReadIdentity{tenantID: tenantID}
	page, err := readNativeDeviceCredentials(ctx, readIdentity, deviceID, 1, 20, "plain-list", protector)
	if err != nil {
		t.Fatalf("list credentials: %v", err)
	}
	if len(page.Data.Items) != 1 || page.Data.Items[0].Secret == nil || *page.Data.Items[0].Secret != *secretResponse.Secret {
		t.Fatalf("list did not return the stored plaintext: %+v", page.Data.Items)
	}

	if _, err := db.Exec(ctx, `UPDATE device_credentials SET status='revoked',revoked_at=NOW(),revoked_by='test',revocation_reason='test' WHERE tenant_id=$1 AND id=$2`, tenantID, credentialID); err != nil {
		t.Fatal(err)
	}
	page, err = readNativeDeviceCredentials(ctx, readIdentity, deviceID, 1, 20, "plain-list-revoked", protector)
	if err != nil {
		t.Fatalf("list after revoke: %v", err)
	}
	if page.Data.Items[0].Secret != nil {
		t.Fatal("revoked credential must not expose its secret")
	}
}
