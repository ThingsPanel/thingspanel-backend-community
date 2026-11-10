// Package pluginidentity stores managed plugin runtime identities (service + tenant + generation, revocable).
// Relational access uses GORM only; time is supplied by the caller so no SQL dialect function is needed.
package pluginidentity

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrDenied              = errors.New("plugin runtime identity denied")
	ErrGenerationNotHigher = errors.New("generation is not higher than an existing one")

	idPattern     = regexp.MustCompile(`^pri_[0-9a-f]{16}$`)
	secretPattern = regexp.MustCompile(`^[0-9a-f]{48}$`)
)

// Record is the plugin_runtime_identities row. Only the SHA-256 of the secret is stored.
type Record struct {
	InstanceID        string     `gorm:"column:instance_id;primaryKey;size:64"`
	PluginInstance    string     `gorm:"column:plugin_instance;size:128;not null"`
	ServiceIdentifier string     `gorm:"column:service_identifier;size:100;not null"`
	TenantID          string     `gorm:"column:tenant_id;size:36;not null"`
	Generation        int64      `gorm:"column:generation;not null"`
	SecretSHA256      string     `gorm:"column:secret_sha256;size:64;not null"`
	CreatedAt         time.Time  `gorm:"column:created_at;not null"`
	ExpiresAt         time.Time  `gorm:"column:expires_at;not null"`
	RevokedAt         *time.Time `gorm:"column:revoked_at"`
}

func (Record) TableName() string { return "plugin_runtime_identities" }

// Identity is what an authenticated plugin request carries.
type Identity struct {
	InstanceID        string
	PluginInstance    string
	ServiceIdentifier string
	TenantID          string
	Generation        int64
}

type Store struct{ db func() *gorm.DB }

func NewStore(db func() *gorm.DB) *Store { return &Store{db: db} }

func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) string {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
}

// Issue creates generation `generation` for the plugin instance in the tenant and revokes every lower generation
// in one transaction. The latest row is locked so concurrent issuers serialize; the unique
// (tenant_id, plugin_instance, generation) key stops a concurrent first insert. The token is "<instance_id>.<secret>".
func (s *Store) Issue(tenantID, pluginInstance, serviceIdentifier string, generation int64, ttl time.Duration, now time.Time) (token string, rec Record, err error) {
	id, secret := "pri_"+randomHex(8), randomHex(24)
	err = s.db().Transaction(func(tx *gorm.DB) error {
		var latest []Record
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND plugin_instance = ?", tenantID, pluginInstance).
			Order("generation DESC").Limit(1).Find(&latest).Error; e != nil {
			return e
		}
		if len(latest) > 0 && generation <= latest[0].Generation {
			return ErrGenerationNotHigher
		}
		if e := tx.Model(&Record{}).
			Where("tenant_id = ? AND plugin_instance = ? AND revoked_at IS NULL", tenantID, pluginInstance).
			Update("revoked_at", now).Error; e != nil {
			return e
		}
		rec = Record{InstanceID: id, PluginInstance: pluginInstance, ServiceIdentifier: serviceIdentifier, TenantID: tenantID,
			Generation: generation, SecretSHA256: HashSecret(secret), CreatedAt: now, ExpiresAt: now.Add(ttl)}
		return tx.Create(&rec).Error
	})
	if err != nil {
		return "", Record{}, err
	}
	return id + "." + secret, rec, nil
}

// Revoke revokes every live generation of the plugin instance in the tenant and returns how many were revoked.
func (s *Store) Revoke(tenantID, pluginInstance string, now time.Time) (int64, error) {
	res := s.db().Model(&Record{}).
		Where("tenant_id = ? AND plugin_instance = ? AND revoked_at IS NULL", tenantID, pluginInstance).
		Update("revoked_at", now)
	return res.RowsAffected, res.Error
}

// Authenticate resolves a bearer token. Malformed, unknown, revoked, expired and wrong-secret tokens are all
// ErrDenied so callers cannot tell them apart. A row that is not the highest generation of its instance is
// denied even if its own revoked_at was never set.
func (s *Store) Authenticate(token string, now time.Time) (*Identity, error) {
	id, secret, found := strings.Cut(token, ".")
	if !found || !idPattern.MatchString(id) || !secretPattern.MatchString(secret) {
		return nil, ErrDenied
	}
	var rec Record
	res := s.db().Where("instance_id = ? AND revoked_at IS NULL AND expires_at > ?", id, now).Limit(1).Find(&rec)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 || subtle.ConstantTimeCompare([]byte(rec.SecretSHA256), []byte(HashSecret(secret))) != 1 {
		return nil, ErrDenied
	}
	var newer int64
	if e := s.db().Model(&Record{}).
		Where("tenant_id = ? AND plugin_instance = ? AND generation > ?", rec.TenantID, rec.PluginInstance, rec.Generation).
		Count(&newer).Error; e != nil {
		return nil, e
	}
	if newer > 0 {
		return nil, ErrDenied
	}
	return &Identity{InstanceID: rec.InstanceID, PluginInstance: rec.PluginInstance, ServiceIdentifier: rec.ServiceIdentifier,
		TenantID: rec.TenantID, Generation: rec.Generation}, nil
}

// Renew extends the lease of every live generation of the plugin instance (in practice only the highest one is
// live) without changing the token. Revoked identities are never revived. Returns how many were extended.
func (s *Store) Renew(tenantID, pluginInstance string, ttl time.Duration, now time.Time) (int64, error) {
	res := s.db().Model(&Record{}).
		Where("tenant_id = ? AND plugin_instance = ? AND revoked_at IS NULL AND expires_at > ?", tenantID, pluginInstance, now).
		Update("expires_at", now.Add(ttl))
	return res.RowsAffected, res.Error
}
