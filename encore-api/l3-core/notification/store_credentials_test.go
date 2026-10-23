package notification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

func TestNotificationCredentialHashScopeAndAtomicRotation(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	hash := func(token string) string { sum := sha256.Sum256([]byte(token)); return hex.EncodeToString(sum[:]) }
	first := NotificationCredentialRecord{ID: "source-cred-1", Purpose: CredentialPurposeSource, TokenHash: hash("source-fixture-token-one"), DeploymentID: "deployment-1", AllowedTenantIDs: []string{"tenant-b", "tenant-a"}, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateNotificationCredential(ctx, first); err != nil {
		t.Fatalf("create source credential: %v", err)
	}
	loaded, err := store.FindNotificationCredential(ctx, CredentialPurposeSource, first.TokenHash)
	if err != nil || loaded.ID != first.ID || len(loaded.AllowedTenantIDs) != 2 || loaded.AllowedTenantIDs[0] != "tenant-a" {
		t.Fatalf("find source credential: (%+v,%v)", loaded, err)
	}
	if _, err := store.FindNotificationCredential(ctx, CredentialPurposeProjection, first.TokenHash); !isStoreError(err, StoreErrorNotFound) {
		t.Fatalf("purpose confusion lookup: %v", err)
	}
	badReplacement := NotificationCredentialRecord{ID: "source-cred-invalid", Purpose: first.Purpose, TokenHash: first.TokenHash, DeploymentID: first.DeploymentID, AllowedTenantIDs: []string{"tenant-a", "tenant-b"}, ExpiresAt: now.Add(2 * time.Hour), RotatedFrom: first.ID}
	if err := store.RotateNotificationCredential(ctx, CredentialPurposeSource, first.ID, badReplacement); !isStoreError(err, StoreErrorConflict) {
		t.Fatalf("duplicate hash rotation error=%v, want conflict", err)
	}
	if _, err := store.FindNotificationCredential(ctx, CredentialPurposeSource, first.TokenHash); err != nil {
		t.Fatalf("failed rotation revoked old credential: %v", err)
	}
	next := NotificationCredentialRecord{ID: "source-cred-2", Purpose: first.Purpose, TokenHash: hash("source-fixture-token-two"), DeploymentID: first.DeploymentID, AllowedTenantIDs: []string{"tenant-a", "tenant-b"}, ExpiresAt: now.Add(2 * time.Hour), RotatedFrom: first.ID}
	if err := store.RotateNotificationCredential(ctx, CredentialPurposeSource, first.ID, next); err != nil {
		t.Fatalf("atomic source credential rotation: %v", err)
	}
	if _, err := store.FindNotificationCredential(ctx, CredentialPurposeSource, first.TokenHash); !isStoreError(err, StoreErrorNotFound) {
		t.Fatalf("old rotated credential still resolves: %v", err)
	}
	if got, err := store.FindNotificationCredential(ctx, CredentialPurposeSource, next.TokenHash); err != nil || got.ID != next.ID {
		t.Fatalf("new rotated credential: (%+v,%v)", got, err)
	}
	var rawTokenCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notification_credentials WHERE token_hash LIKE '%'||$1||'%'`, "source-fixture-token").Scan(&rawTokenCount); err != nil || rawTokenCount != 0 {
		t.Fatalf("credential raw token persisted=(%d,%v)", rawTokenCount, err)
	}
	if err := store.RevokeNotificationCredential(ctx, CredentialPurposeSource, next.ID); err != nil {
		t.Fatalf("revoke source credential: %v", err)
	}
	if _, err := store.FindNotificationCredential(ctx, CredentialPurposeSource, next.TokenHash); !isStoreError(err, StoreErrorNotFound) {
		t.Fatalf("revoked credential still resolves: %v", err)
	}
}
