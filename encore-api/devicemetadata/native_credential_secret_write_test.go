package devicemetadata

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
)

func credentialReceiptFixture(t *testing.T) (context.Context, metadataWriteCommand, CredentialAAD) {
	t.Helper()
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	deviceID := insertCredentialSchemaDevice(t, ctx, tx, tenant)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, table := range []string{"metadata_audit_events", "metadata_idempotency", "device_credentials", "devices"} {
			if _, err := db.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant); err != nil {
				t.Errorf("clean receipt fixture %s: %v", table, err)
			}
		}
	})
	return ctx, metadataWriteCommand{TenantID: tenant, SubjectID: "credential-receipt-test", Operation: "createDeviceCredential", Path: "/api/v2/devices/" + deviceID + "/credentials", Key: "receipt-" + testTenantID(), Method: "POST", RequestID: testTenantID(), CanonicalBody: []byte(`{"kind":"token","secret":"synthetic-receipt-secret"}`)}, CredentialAAD{TenantID: tenant, OwnerKind: CredentialOwnerDevice, OwnerID: deviceID, Purpose: CredentialPurposeDeviceAccess}
}

func credentialReceiptApply(ctx context.Context, command metadataWriteCommand, protector *CredentialSecretProtector, calls *atomic.Int32, reject bool) func(*sqldb.Tx, CredentialAAD, time.Time) (metadataWriteResponse, metadataWriteAudit, error) {
	return func(tx *sqldb.Tx, aad CredentialAAD, expiresAt time.Time) (metadataWriteResponse, metadataWriteAudit, error) {
		calls.Add(1)
		secret := []byte("synthetic-receipt-secret")
		sealed, err := protector.Seal(aad, secret)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		fingerprint, err := protector.Fingerprint(aad, secret)
		if err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if err := execCredentialSchemaRow(ctx, tx, credentialSchemaRow{id: aad.CredentialID, tenantID: aad.TenantID, deviceID: aad.OwnerID, purpose: "device_access", kind: "token", mode: "encrypted", status: "pending", ciphertext: sealed.Ciphertext, keyID: sealed.KeyID, cipherSuite: sealed.CipherSuite, nonce: sealed.Nonce, fingerprintKeyID: fingerprint.KeyID, fingerprint: fingerprint.Fingerprint}); err != nil {
			return metadataWriteResponse{}, metadataWriteAudit{}, err
		}
		if reject {
			return metadataWriteResponse{}, metadataWriteAudit{}, &errs.Error{Code: errs.InvalidArgument, Message: "synthetic rollback"}
		}
		body, err := json.Marshal(map[string]any{"data": map[string]any{"id": aad.CredentialID, "secret": string(secret), "expiresAt": expiresAt}, "meta": map[string]string{"requestId": command.RequestID}})
		version := int64(1)
		return metadataWriteResponse{Status: 201, Body: body, Location: command.Path + "/" + aad.CredentialID}, metadataWriteAudit{ResourceKind: "device_credential", ResourceID: aad.CredentialID, AfterVersion: &version, RedactedChanges: json.RawMessage(`{"created":true,"kind":"token"}`)}, err
	}
}

func TestCredentialSecretReceiptConcurrentReplayRotationAndExpiry(t *testing.T) {
	ctx, command, owner := credentialReceiptFixture(t)
	protector := testCredentialProtector(t)
	var calls, authCalls atomic.Int32
	authorize := func(*sqldb.Tx) error { authCalls.Add(1); return nil }
	apply := credentialReceiptApply(ctx, command, protector, &calls, false)
	type outcome struct {
		response *metadataWriteResponse
		err      error
	}
	results := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		go func() {
			r, e := runCredentialSecretWriteTx(ctx, command, owner, protector, authorize, apply)
			results <- outcome{r, e}
		}()
	}
	a, b := <-results, <-results
	if a.err != nil || b.err != nil {
		t.Fatalf("concurrent secret receipt failed: %v / %v", a.err, b.err)
	}
	if calls.Load() != 1 || authCalls.Load() != 2 || a.response.Replayed == b.response.Replayed || !bytes.Equal(a.response.Body, b.response.Body) {
		t.Fatal("secret write was not deduplicated and reauthorized")
	}
	var plaintext, responseCipher []byte
	var requestKey string
	var hasPlain bool
	if err := db.QueryRow(ctx, `SELECT response::text,response_ciphertext,request_key_id,response IS NOT NULL OR response_hash IS NOT NULL FROM metadata_idempotency WHERE tenant_id=$1`, command.TenantID).Scan(&plaintext, &responseCipher, &requestKey, &hasPlain); err != nil {
		t.Fatal(err)
	}
	if hasPlain || len(plaintext) != 0 || bytes.Contains(responseCipher, []byte("synthetic-receipt-secret")) || requestKey != "key-v1" {
		t.Fatal("secret receipt persisted unsafe material")
	}
	var dump string
	if err := db.QueryRow(ctx, `SELECT row_to_json(c)::text FROM device_credentials c WHERE tenant_id=$1`, command.TenantID).Scan(&dump); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dump, "synthetic-receipt-secret") {
		t.Fatal("credential contains plaintext")
	}
	if err := db.QueryRow(ctx, `SELECT redacted_changes::text FROM metadata_audit_events WHERE tenant_id=$1`, command.TenantID).Scan(&dump); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dump, "synthetic-receipt-secret") {
		t.Fatal("audit contains plaintext")
	}
	rotated, err := NewCredentialSecretProtector("key-v2", map[string][]byte{"key-v1": bytes.Repeat([]byte{0x5a}, 32), "key-v2": bytes.Repeat([]byte{0x6b}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	r, err := runCredentialSecretWriteTx(ctx, command, owner, rotated, authorize, apply)
	if err != nil || !r.Replayed || !bytes.Equal(r.Body, a.response.Body) {
		t.Fatal("key rotation broke original response replay")
	}
	changed := command
	changed.CanonicalBody = []byte(`{"kind":"token","secret":"different-synthetic"}`)
	if _, err := runCredentialSecretWriteTx(ctx, changed, owner, rotated, authorize, apply); errs.Code(err) != errs.AlreadyExists {
		t.Fatal("changed secret request must conflict")
	}
	deny := func(*sqldb.Tx) error {
		return &errs.Error{Code: errs.PermissionDenied, Message: "synthetic authority revoked"}
	}
	if _, err := runCredentialSecretWriteTx(ctx, command, owner, rotated, deny, apply); errs.Code(err) != errs.PermissionDenied {
		t.Fatal("receipt replay bypassed current authorization")
	}
	if _, err := db.Exec(ctx, `UPDATE metadata_idempotency SET replay_until=NOW()-INTERVAL '1 second' WHERE tenant_id=$1`, command.TenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := runCredentialSecretWriteTx(ctx, command, owner, rotated, authorize, apply); errs.Code(err) != errs.AlreadyExists {
		t.Fatal("expired secret receipt must conflict")
	}
	var state string
	var cleared bool
	if err := db.QueryRow(ctx, `SELECT state,response_ciphertext IS NULL AND response_key_id IS NULL AND response_nonce IS NULL AND request_key_id IS NOT NULL AND credential_id IS NOT NULL FROM metadata_idempotency WHERE tenant_id=$1`, command.TenantID).Scan(&state, &cleared); err != nil {
		t.Fatal(err)
	}
	if state != "expired" || !cleared || calls.Load() != 1 {
		t.Fatal("expiry did not clear cipher and retain deduplication tombstone")
	}
}

func TestCredentialSecretReceiptRollbackAndRevokedReplay(t *testing.T) {
	ctx, command, owner := credentialReceiptFixture(t)
	p := testCredentialProtector(t)
	var calls atomic.Int32
	authorize := func(*sqldb.Tx) error { return nil }
	if _, err := runCredentialSecretWriteTx(ctx, command, owner, p, authorize, credentialReceiptApply(ctx, command, p, &calls, true)); errs.Code(err) != errs.InvalidArgument {
		t.Fatal("expected rejected mutation")
	}
	for _, table := range []string{"device_credentials", "metadata_idempotency", "metadata_audit_events"} {
		var count int
		if err := db.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", command.TenantID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("rollback left rows in %s", table)
		}
	}
	apply := credentialReceiptApply(ctx, command, p, &calls, false)
	if _, err := runCredentialSecretWriteTx(ctx, command, owner, p, authorize, apply); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE device_credentials SET status='revoked',revoked_at=clock_timestamp(),revoked_by='receipt-test',revocation_reason='synthetic revoke' WHERE tenant_id=$1`, command.TenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := runCredentialSecretWriteTx(ctx, command, owner, p, authorize, apply); errs.Code(err) != errs.AlreadyExists {
		t.Fatal("revoked credential response must not replay")
	}
	if calls.Load() != 2 {
		t.Fatal("revoked replay ran the mutation again")
	}
}
