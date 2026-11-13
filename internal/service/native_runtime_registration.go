package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
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
	DeviceID     string `json:"deviceId"`
	CredentialID string `json:"credentialId"`
	DeviceNumber string `json:"deviceNumber"`
	Name         string `json:"name"`
	Voucher      string `json:"voucher"`
}
type nativeRuntimeRegistration struct {
	DeviceID       string `gorm:"primaryKey"`
	NativeTenantID string
	TenantID       string
	DeploymentID   string
	CredentialID   string
	VoucherSHA256  string `gorm:"column:voucher_sha256"`
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
	if !validUUID(tenant) || !validUUID(input.DeviceID) || !validUUID(input.CredentialID) || deployment == "" || len(deployment) > 128 || input.DeviceNumber == "" || len(input.DeviceNumber) > 36 || strings.TrimSpace(input.DeviceNumber) != input.DeviceNumber || len(input.Name) > 255 || len(input.Voucher) > 500 || json.Unmarshal([]byte(input.Voucher), &voucher) != nil || voucher["username"] == "" || len(voucher) > 2 {
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
			if registered.NativeTenantID != tenant || registered.TenantID != source || registered.DeploymentID != deployment || registered.CredentialID != input.CredentialID || registered.VoucherSHA256 != digest {
				return ErrNativeRegistrationConflict
			}
			var existing model.Device
			if tx.Where("id=? AND tenant_id=?", input.DeviceID, source).Take(&existing).Error != nil || existing.Voucher != input.Voucher || existing.DeviceNumber != input.DeviceNumber {
				return ErrNativeRegistrationConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
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
		return tx.Create(&nativeRuntimeRegistration{DeviceID: input.DeviceID, NativeTenantID: tenant, TenantID: source, DeploymentID: deployment, CredentialID: input.CredentialID, VoucherSHA256: digest}).Error
	})
}
