package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"project/internal/dal"
	"project/internal/model"
	"strings"
	"time"
)

var ErrNativeRegistrationConflict = errors.New("native runtime registration conflict")
var ErrNativeRegistrationInvalid = errors.New("invalid native runtime registration")

type NativeRuntimeRegistration struct {
	DeviceID                string `json:"deviceId"`
	CredentialID            string `json:"credentialId"`
	DeviceNumber            string `json:"deviceNumber"`
	Name                    string `json:"name"`
	Voucher                 string `json:"voucher"`
	EventID                 string `json:"eventId"`
	Change                  string `json:"change"`
	ReplacementCredentialID string `json:"replacementCredentialId"`
}
type nativeRuntimeRegistration struct {
	DeviceID       string `gorm:"primaryKey"`
	NativeTenantID string
	TenantID       string
	DeploymentID   string
	CredentialID   string
	VoucherSHA256  string `gorm:"column:voucher_sha256"`
	Active         bool
}

func (nativeRuntimeRegistration) TableName() string { return "native_runtime_registrations" }

// Same canonical namespace as Encore internal/tenantidentity; never truncate or
// guess an inverse UUID. Only an existing unique source tenant is accepted.
func nativeTenantIdentity(raw string) string {
	if id, err := uuid.Parse(raw); err == nil {
		return id.String()
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("thingspanel/community/tenant/"+raw)).String()
}
func RegisterNativeRuntime(ctx context.Context, db *gorm.DB, tenant, deployment string, input NativeRuntimeRegistration) error {
	validUUID := func(s string) bool { id, e := uuid.Parse(s); return e == nil && id != uuid.Nil && id.String() == s }
	var voucher map[string]string
	if !validUUID(tenant) || !validUUID(input.DeviceID) || !validUUID(input.CredentialID) || !validUUID(input.EventID) || (input.Change != "activation" && input.Change != "rotation" && input.Change != "revocation") || (input.Change == "rotation" && !validUUID(input.ReplacementCredentialID)) || deployment == "" || len(deployment) > 128 || input.DeviceNumber == "" || len(input.DeviceNumber) > 36 || strings.TrimSpace(input.DeviceNumber) != input.DeviceNumber || len(input.Name) > 255 || len(input.Voucher) > 500 || json.Unmarshal([]byte(input.Voucher), &voucher) != nil || voucher["username"] == "" || len(voucher) > 2 {
		return ErrNativeRegistrationInvalid
	}
	for k := range voucher {
		if k != "username" && k != "password" {
			return ErrNativeRegistrationInvalid
		}
	}
	sum := sha256.Sum256([]byte(input.Voucher))
	digest := hex.EncodeToString(sum[:])
	return db.Session(&gorm.Session{Logger: logger.Discard}).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sources []string
		if err := tx.Table("users").Distinct("tenant_id").Where("tenant_id IS NOT NULL AND tenant_id <> ''").Pluck("tenant_id", &sources).Error; err != nil {
			return err
		}
		source := ""
		for _, raw := range sources {
			if nativeTenantIdentity(raw) == tenant {
				if source != "" && source != raw {
					return ErrNativeRegistrationConflict
				}
				source = raw
			}
		}
		if source == "" {
			return ErrNativeRegistrationInvalid
		}
		// Serialize same-device registration across independent workers.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 72311031))", input.DeviceID).Error; err != nil {
			return err
		}
		var registered nativeRuntimeRegistration
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("device_id=?", input.DeviceID).Take(&registered).Error
		if err == nil {
			if registered.NativeTenantID != tenant || registered.TenantID != source || registered.DeploymentID != deployment {
				return ErrNativeRegistrationConflict
			}
			return applyRegisteredNativeCredential(tx, &registered, input, digest)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if input.Change != "activation" {
			return ErrNativeRegistrationConflict
		}
		var count int64
		if err := tx.Model(&model.Device{}).Where("id=? OR device_number=? OR voucher=?", input.DeviceID, input.DeviceNumber, input.Voucher).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return ErrNativeRegistrationConflict
		}
		now := time.Now().UTC()
		protocol, access := "MQTT", "A"
		device := model.Device{ID: input.DeviceID, TenantID: source, Name: &input.Name, DeviceNumber: input.DeviceNumber, Voucher: input.Voucher, IsEnabled: "enabled", ActivateFlag: "active", CreatedAt: &now, UpdateAt: &now, ActivateAt: &now, Protocol: &protocol, AccessWay: &access}
		// Reserve the UUID through the same permanent identity ledger as normal devices.
		if err := tx.Create(&dal.DeviceIDReservation{ID: input.DeviceID, TenantID: source, State: "active", CreatedAt: now}).Error; err != nil {
			return ErrNativeRegistrationConflict
		}
		if err := tx.Create(&device).Error; err != nil {
			return err
		}
		registered = nativeRuntimeRegistration{DeviceID: input.DeviceID, NativeTenantID: tenant, TenantID: source, DeploymentID: deployment, CredentialID: input.CredentialID, VoucherSHA256: digest, Active: true}
		if err := tx.Create(&registered).Error; err != nil {
			return err
		}
		if err := tx.Create(&nativeCredentialHistory{DeviceID: input.DeviceID, CredentialID: input.CredentialID}).Error; err != nil {
			return err
		}
		return storeNativeCredentialEvent(tx, input)
	})
}

func nativeCredentialRequestDigest(input NativeRuntimeRegistration) string {
	voucher := input.Voucher
	if input.Change == "revocation" {
		voucher = ""
	} // The owner creates its own one-time disabled voucher.
	raw, _ := json.Marshal([]string{input.DeviceID, input.EventID, input.Change, input.CredentialID, input.ReplacementCredentialID, voucher})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func storeNativeCredentialEvent(tx *gorm.DB, input NativeRuntimeRegistration) error {
	return tx.Create(&nativeCredentialEvent{EventID: input.EventID, DeviceID: input.DeviceID, RequestSHA256: nativeCredentialRequestDigest(input)}).Error
}
func applyRegisteredNativeCredential(tx *gorm.DB, current *nativeRuntimeRegistration, input NativeRuntimeRegistration, digest string) error {
	var receipt struct {
		DeviceID      string
		RequestSHA256 string `gorm:"column:request_sha256"`
	}
	err := tx.Table("native_runtime_credential_events").Where("event_id=?", input.EventID).Take(&receipt).Error
	if err == nil {
		if receipt.DeviceID != input.DeviceID || receipt.RequestSHA256 != nativeCredentialRequestDigest(input) {
			return ErrNativeRegistrationConflict
		}
		return nil // A replay acknowledges its historic result, never rewrites today's credential.
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var existing model.Device
	if err := tx.Where("id=? AND tenant_id=?", input.DeviceID, current.TenantID).Take(&existing).Error; err != nil {
		return ErrNativeRegistrationConflict
	}
	sum := sha256.Sum256([]byte(existing.Voucher))
	if hex.EncodeToString(sum[:]) != current.VoucherSHA256 {
		return ErrNativeRegistrationConflict
	}
	nextID, nextVoucher, nextActive := current.CredentialID, existing.Voucher, current.Active
	fresh := false
	switch input.Change {
	case "activation":
		if current.Active {
			if current.CredentialID != input.CredentialID || current.VoucherSHA256 != digest {
				return ErrNativeRegistrationConflict
			}
		} else {
			nextID, nextVoucher, nextActive, fresh = input.CredentialID, input.Voucher, true, true
		}
	case "rotation":
		if !current.Active || current.CredentialID != input.CredentialID {
			return ErrNativeRegistrationConflict
		}
		nextID, nextVoucher, nextActive, fresh = input.ReplacementCredentialID, input.Voucher, true, true
	case "revocation":
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&nativeCredentialHistory{DeviceID: input.DeviceID, CredentialID: input.CredentialID}).Error; err != nil {
			return err
		}
		if current.CredentialID == input.CredentialID && current.Active {
			raw, _ := json.Marshal(map[string]string{"username": "revoked-" + uuid.NewString()})
			nextVoucher = string(raw)
			nextActive = false
		} // Revoking an unused/past credential never disables the current one.
	default:
		return ErrNativeRegistrationInvalid
	}
	if fresh {
		var used int64
		if err := tx.Table("native_runtime_credential_history").Where("device_id=? AND credential_id=?", input.DeviceID, nextID).Count(&used).Error; err != nil {
			return err
		}
		if used != 0 {
			return ErrNativeRegistrationConflict
		}
		if err := tx.Create(&nativeCredentialHistory{DeviceID: input.DeviceID, CredentialID: nextID}).Error; err != nil {
			return err
		}
	}
	sum = sha256.Sum256([]byte(nextVoucher))
	if err := tx.Model(&model.Device{}).Where("id=? AND tenant_id=?", input.DeviceID, current.TenantID).Update("voucher", nextVoucher).Error; err != nil {
		return err
	}
	if err := tx.Model(current).Updates(map[string]any{"credential_id": nextID, "voucher_sha256": hex.EncodeToString(sum[:]), "active": nextActive}).Error; err != nil {
		return err
	}
	return storeNativeCredentialEvent(tx, input)
}

type nativeCredentialHistory struct {
	DeviceID     string `gorm:"primaryKey"`
	CredentialID string `gorm:"primaryKey"`
}

func (nativeCredentialHistory) TableName() string { return "native_runtime_credential_history" }

type nativeCredentialEvent struct {
	EventID       string `gorm:"primaryKey"`
	DeviceID      string
	RequestSHA256 string `gorm:"column:request_sha256"`
}

func (nativeCredentialEvent) TableName() string { return "native_runtime_credential_events" }

// The installed broker owner authenticates from PostgreSQL, not Redis. Clear
// the community device projection cache before acknowledging the owner call.
// A failed cache invalidation is retried using the same durable event receipt.
func ClearNativeRuntimeDeviceCache(ctx context.Context, client *redis.Client, deviceID string) error {
	if client == nil {
		return errors.New("native runtime cache unavailable")
	}
	return client.Del(ctx, deviceID).Err()
}
