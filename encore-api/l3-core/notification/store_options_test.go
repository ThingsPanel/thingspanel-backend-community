package notification

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

func TestSnapshotAADRequiresExplicitScopeAndSlot(t *testing.T) {
	tests := []struct {
		name string
		aad  SnapshotAAD
		want bool
	}{
		{name: "tenant", aad: SnapshotAAD{ScopeKind: "tenant", TenantID: "tenant-a", ResourceID: "instance-a", Version: 2, Purpose: "instance-secret", Slot: "smtp.password"}, want: true},
		{name: "platform registration", aad: SnapshotAAD{ScopeKind: "platform", ResourceID: "plugin-version-a", Version: 1, Purpose: "plugin-auth", Slot: "authSecret"}, want: true},
		{name: "tenant missing", aad: SnapshotAAD{ScopeKind: "tenant", ResourceID: "instance-a", Version: 1, Purpose: "secret", Slot: "password"}},
		{name: "platform tenant set", aad: SnapshotAAD{ScopeKind: "platform", TenantID: "tenant-a", ResourceID: "plugin-a", Version: 1, Purpose: "secret", Slot: "token"}},
		{name: "missing slot", aad: SnapshotAAD{ScopeKind: "tenant", TenantID: "tenant-a", ResourceID: "instance-a", Version: 1, Purpose: "secret"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.aad.validate()
			if (err == nil) != tt.want {
				t.Fatalf("validate() error = %v, want valid=%v", err, tt.want)
			}
		})
	}
}

func TestStoreProtectionFailsClosedWithoutProtector(t *testing.T) {
	s, err := NewStore(StoreOptions{DB: new(sql.DB)})
	if err != nil {
		t.Fatal("store constructor should accept a DB dependency")
	}
	aad := SnapshotAAD{ScopeKind: "tenant", TenantID: "tenant-a", ResourceID: "delivery-a", Version: 1, Purpose: "delivery-content", Slot: "content"}
	if _, err := s.seal(context.Background(), aad, []byte("sensitive")); !errors.Is(err, ErrSnapshotProtectorUnavailable) {
		t.Fatalf("seal() error = %v, want protector unavailable", err)
	}
	if _, err := s.open(context.Background(), aad, ProtectedSnapshot{KeyRef: "key", Ciphertext: []byte("cipher"), AADVersion: 1}); !errors.Is(err, ErrSnapshotProtectorUnavailable) {
		t.Fatalf("open() error = %v, want protector unavailable", err)
	}
}

func TestDefaultRetryPolicyUsesOnlyExplicitRetryableRejection(t *testing.T) {
	policy := defaultRetryPolicy{}
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)

	for _, result := range []contract.PluginSendResultV1{
		{Outcome: "accepted"},
		{Outcome: "unknown"},
		{Outcome: "rejected"},
		{Outcome: "rejected", Error: &contract.PluginDeliveryErrorV1{Retryable: false}},
	} {
		if _, retry := policy.NextRetry(result, 1, now, expires); retry {
			t.Fatalf("unexpected retry for outcome=%q retryable=%v", result.Outcome, result.Error != nil && result.Error.Retryable)
		}
	}

	result := contract.PluginSendResultV1{Outcome: "rejected", Error: &contract.PluginDeliveryErrorV1{Retryable: true}}
	if next, retry := policy.NextRetry(result, 1, now, expires); !retry || !next.Equal(now.Add(time.Second)) {
		t.Fatalf("first retry = %v, retry=%v; want one second", next, retry)
	}
	after := 17
	result.Error.RetryAfterSeconds = &after
	if next, retry := policy.NextRetry(result, 1, now, expires); !retry || !next.Equal(now.Add(17*time.Second)) {
		t.Fatalf("provider retry_after result = %v, retry=%v; want 17 seconds", next, retry)
	}
	if _, retry := policy.NextRetry(result, maxDeliveryAttempts, now, expires); retry {
		t.Fatal("retry policy exceeded maximum attempt count")
	}
	if _, retry := policy.NextRetry(result, 1, now, now.Add(10*time.Second)); retry {
		t.Fatal("retry policy scheduled a send after expiry")
	}
}
