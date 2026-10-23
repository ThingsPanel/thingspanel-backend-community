package notification

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

func TestReceiptCredentialRotationReplaysEncryptedResponseAndSupportsEmergencyRevoke(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createFixtureInstance(t, store, "tenant-receipt-credential")
	instance, err := store.GetInstance(ctx, "tenant-receipt-credential", instanceID)
	if err != nil {
		t.Fatalf("get fixture instance: %v", err)
	}
	otherInstance, err := store.CreateInstance(ctx, "tenant-receipt-credential", "receipt-other-instance-001", strings.Repeat("f", 64), contract.InstanceCreate{PluginRegistrationID: instance.PluginRegistrationID, Name: "other fixture", Channel: contract.ChannelEmail, Config: json.RawMessage(`{"region":"other"}`), ProviderIdentity: map[string]string{"account": "other"}}, map[string]string{"token": "other-instance-secret"})
	if err != nil {
		t.Fatalf("create other instance fixture: %v", err)
	}
	enabled := true
	if _, err := store.UpdateInstance(ctx, "tenant-receipt-credential", otherInstance.ID, "receipt-other-enable-01", strings.Repeat("e", 64), contract.InstanceUpdate{ExpectedVersion: 1, Enabled: &enabled}); err != nil {
		t.Fatalf("enable other instance fixture: %v", err)
	}
	if _, err := db.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1),hashtext($2))`, "fixture-lock-tenant", "fixture-lock-key"); err != nil {
		t.Fatalf("verify PostgreSQL advisory lock support: %v", err)
	}
	expires := time.Now().UTC().Add(time.Hour)
	first := NotificationCredentialRecord{ID: "receipt-credential-1", Purpose: CredentialPurposeReceipt, TokenHash: strings.Repeat("1", 64), TenantID: "tenant-receipt-credential", InstanceID: instanceID, ExpiresAt: expires}
	otherExpiry := time.Now().UTC().Add(90 * 24 * time.Hour)
	other := NotificationCredentialRecord{ID: "receipt-credential-other", Purpose: CredentialPurposeReceipt, TokenHash: strings.Repeat("4", 64), TenantID: "tenant-receipt-credential", InstanceID: otherInstance.ID, ExpiresAt: otherExpiry}
	if err := validateNotificationCredential(other, store.now()); err != nil {
		t.Fatalf("validate other-instance receipt credential fixture: %+v (%v)", other, err)
	}
	if err := store.CreateNotificationCredential(ctx, other); err != nil {
		t.Fatalf("create other-instance receipt credential: %v", err)
	}
	response := []byte(`{"credential":"one-time-secret-token","expiresAt":"fixture"}`)
	stored, replayed, err := store.RotateReceiptCredential(ctx, first.TenantID, "receipt-create-001", strings.Repeat("a", 64), "", first, response)
	if err != nil || replayed || string(stored) != string(response) {
		t.Fatalf("create receipt credential=(%s,%v,%v)", stored, replayed, err)
	}
	stored, replayed, err = store.RotateReceiptCredential(ctx, first.TenantID, "receipt-create-001", strings.Repeat("a", 64), "", first, []byte(`{"credential":"ignored"}`))
	if err != nil || !replayed || string(stored) != string(response) {
		t.Fatalf("receipt credential replay=(%s,%v,%v)", stored, replayed, err)
	}
	if _, _, err := store.RotateReceiptCredential(ctx, first.TenantID, "receipt-create-001", strings.Repeat("b", 64), "", first, response); !isStoreError(err, StoreErrorConflict) {
		t.Fatalf("same idempotency key with different digest=%v", err)
	}
	var ciphertext []byte
	if err := db.QueryRowContext(ctx, `SELECT response_ciphertext FROM notification_credential_replays WHERE tenant_id=$1 AND idempotency_key=$2`, first.TenantID, "receipt-create-001").Scan(&ciphertext); err != nil || strings.Contains(string(ciphertext), "one-time-secret-token") {
		t.Fatalf("receipt response snapshot plaintext at rest: contains=%v err=%v", strings.Contains(string(ciphertext), "one-time-secret-token"), err)
	}
	second := NotificationCredentialRecord{ID: "receipt-credential-2", Purpose: CredentialPurposeReceipt, TokenHash: strings.Repeat("2", 64), TenantID: first.TenantID, InstanceID: instanceID, ExpiresAt: time.Now().UTC().Add(2 * time.Hour)}
	rotatedResponse := []byte(`{"credential":"second-one-time-token"}`)
	if _, replayed, err := store.RotateReceiptCredential(ctx, first.TenantID, "receipt-rotate-001", strings.Repeat("c", 64), "", second, rotatedResponse); err != nil || replayed {
		t.Fatalf("rotate receipt credential=(%v,%v)", replayed, err)
	}
	old, err := store.FindNotificationCredential(ctx, CredentialPurposeReceipt, first.TokenHash)
	if err != nil || !old.ExpiresAt.After(time.Now().UTC()) || time.Until(old.ExpiresAt) > 15*time.Minute {
		t.Fatalf("old token grace after rotation=(%+v,%v)", old, err)
	}
	firstGrace := old.ExpiresAt
	secondGrace, err := store.FindNotificationCredential(ctx, CredentialPurposeReceipt, second.TokenHash)
	if err != nil {
		t.Fatalf("lookup new active token: (%+v,%v)", secondGrace, err)
	}
	third := NotificationCredentialRecord{ID: "receipt-credential-3", Purpose: CredentialPurposeReceipt, TokenHash: strings.Repeat("3", 64), TenantID: first.TenantID, InstanceID: instanceID, ExpiresAt: time.Now().UTC().Add(3 * time.Hour)}
	if _, replayed, err := store.RotateReceiptCredential(ctx, first.TenantID, "receipt-rotate-002", strings.Repeat("d", 64), "", third, []byte(`{"credential":"third-one-time-token"}`)); err != nil || replayed {
		t.Fatalf("rotate receipt credential a second time=(%v,%v)", replayed, err)
	}
	old, err = store.FindNotificationCredential(ctx, CredentialPurposeReceipt, first.TokenHash)
	if err != nil || old.ExpiresAt.After(firstGrace) {
		t.Fatalf("second rotation extended first token grace=(%+v,%v)", old, err)
	}
	secondGrace, err = store.FindNotificationCredential(ctx, CredentialPurposeReceipt, second.TokenHash)
	if err != nil || !secondGrace.ExpiresAt.After(time.Now().UTC()) || time.Until(secondGrace.ExpiresAt) > 15*time.Minute {
		t.Fatalf("second token grace after next rotation=(%+v,%v)", secondGrace, err)
	}
	otherAfterRotation, err := store.FindNotificationCredential(ctx, CredentialPurposeReceipt, other.TokenHash)
	delta := otherAfterRotation.ExpiresAt.Sub(otherExpiry)
	if err != nil || delta > time.Millisecond || delta < -time.Millisecond {
		t.Fatalf("rotation modified another instance credential=(%+v,%v)", otherAfterRotation, err)
	}
	if err := store.RevokeReceiptCredential(ctx, first.TenantID, instanceID, third.ID); err != nil {
		t.Fatalf("emergency revoke: %v", err)
	}
	if _, err := store.FindNotificationCredential(ctx, CredentialPurposeReceipt, third.TokenHash); !isStoreError(err, StoreErrorNotFound) {
		t.Fatalf("revoked current receipt token lookup=%v", err)
	}
}
