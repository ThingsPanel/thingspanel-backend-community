package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"encore.app/l3-core/notification/contract"
)

var (
	ErrSnapshotProtectorUnavailable = errors.New("notification snapshot protection unavailable")
	ErrSnapshotProtectionFailed     = errors.New("notification snapshot protection failed")
)

// SnapshotAAD binds ciphertext to its exact tenant/resource/version/purpose
// and field slot. ScopeKind is "tenant" or "platform"; platform records must
// leave TenantID empty rather than impersonating a tenant.
type SnapshotAAD struct {
	ScopeKind  string
	TenantID   string
	ResourceID string
	Version    int64
	Purpose    string
	Slot       string
}

// ProtectedSnapshot is the only encrypted value shape persisted by Store.
type ProtectedSnapshot struct {
	KeyRef     string
	Ciphertext []byte
	AADVersion int
}

// SnapshotProtector is implemented by the identity/security owner. It must
// authenticate every SnapshotAAD field and must never log plaintext or keys.
type SnapshotProtector interface {
	Seal(ctx context.Context, aad SnapshotAAD, plaintext []byte) (ProtectedSnapshot, error)
	Open(ctx context.Context, aad SnapshotAAD, protected ProtectedSnapshot) ([]byte, error)
}

// RetryPolicy only schedules a retry when the plugin proves that it rejected
// the send and marks the rejection retryable. Ambiguous outcomes are terminal
// for automatic dispatch.
type RetryPolicy interface {
	NextRetry(result contract.PluginSendResultV1, attempts int, now, expiresAt time.Time) (time.Time, bool)
}

type StoreOptions struct {
	DB            *sql.DB
	Protector     SnapshotProtector
	Clock         func() time.Time
	RetryPolicy   RetryPolicy
	SourcePlanner SourceEventPlanner
}

type Store struct {
	db            *sql.DB
	protector     SnapshotProtector
	now           func() time.Time
	retry         RetryPolicy
	sourcePlanner SourceEventPlanner
}

// NewStore adapts an Encore database's Stdlib connection or a test-owned
// PostgreSQL pool. It does not ping the database or run migrations.
func NewStore(options StoreOptions) (*Store, error) {
	if options.DB == nil {
		return nil, errors.New("notification store requires a database")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.RetryPolicy == nil {
		options.RetryPolicy = defaultRetryPolicy{}
	}
	return &Store{
		db:            options.DB,
		protector:     options.Protector,
		now:           options.Clock,
		retry:         options.RetryPolicy,
		sourcePlanner: options.SourcePlanner,
	}, nil
}

func (s *Store) seal(ctx context.Context, aad SnapshotAAD, plaintext []byte) (ProtectedSnapshot, error) {
	if err := aad.validate(); err != nil {
		return ProtectedSnapshot{}, err
	}
	if s == nil || s.protector == nil {
		return ProtectedSnapshot{}, ErrSnapshotProtectorUnavailable
	}
	protected, err := s.protector.Seal(ctx, aad, plaintext)
	if err != nil || protected.KeyRef == "" || len(protected.Ciphertext) == 0 || protected.AADVersion < 1 {
		return ProtectedSnapshot{}, ErrSnapshotProtectionFailed
	}
	return protected, nil
}

func (s *Store) open(ctx context.Context, aad SnapshotAAD, protected ProtectedSnapshot) ([]byte, error) {
	if err := aad.validate(); err != nil {
		return nil, err
	}
	if s == nil || s.protector == nil {
		return nil, ErrSnapshotProtectorUnavailable
	}
	if protected.KeyRef == "" || len(protected.Ciphertext) == 0 || protected.AADVersion < 1 {
		return nil, ErrSnapshotProtectionFailed
	}
	plaintext, err := s.protector.Open(ctx, aad, protected)
	if err != nil {
		return nil, ErrSnapshotProtectionFailed
	}
	return plaintext, nil
}

func (aad SnapshotAAD) validate() error {
	if aad.ScopeKind != "tenant" && aad.ScopeKind != "platform" {
		return errors.New("invalid snapshot protection scope")
	}
	if (aad.ScopeKind == "tenant" && aad.TenantID == "") || (aad.ScopeKind == "platform" && aad.TenantID != "") {
		return errors.New("invalid snapshot protection tenant scope")
	}
	if aad.ResourceID == "" || aad.Version < 1 || aad.Purpose == "" || aad.Slot == "" {
		return errors.New("incomplete snapshot protection context")
	}
	return nil
}

func (p defaultRetryPolicy) NextRetry(result contract.PluginSendResultV1, attempts int, now, expiresAt time.Time) (time.Time, bool) {
	if attempts < 1 || attempts >= maxDeliveryAttempts || result.Outcome != "rejected" || result.Error == nil || !result.Error.Retryable {
		return time.Time{}, false
	}
	delay := time.Duration(1<<(attempts-1)) * time.Second
	if retryAfter := result.Error.RetryAfterSeconds; retryAfter != nil {
		providerDelay := time.Duration(*retryAfter) * time.Second
		if providerDelay > delay {
			delay = providerDelay
		}
	}
	next := now.Add(delay)
	if !next.Before(expiresAt) {
		return time.Time{}, false
	}
	return next, true
}

const maxDeliveryAttempts = 3

type defaultRetryPolicy struct{}

func (s *Store) database() (*sql.DB, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("notification store is unavailable")
	}
	return s.db, nil
}
