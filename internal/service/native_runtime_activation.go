package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"gorm.io/gorm"
	"project/internal/model"
	"time"
)

// ConfirmNativeRuntimeActivation reads an already owned and confirmed registration.
// It never creates an identity, changes credentials, or grants a tenant access.
func ConfirmNativeRuntimeActivation(ctx context.Context, db *gorm.DB, tenant, deployment string, input NativeRuntimeRegistration) (*time.Time, error) {
	deviceID, eventID := input.DeviceID, input.EventID
	credentialID := input.CredentialID
	if input.Change == "rotation" {
		credentialID = input.ReplacementCredentialID
	}
	if deviceID == "" || eventID == "" || credentialID == "" || (input.Change != "activation" && input.Change != "rotation") {
		return nil, ErrNativeRegistrationInvalid
	}
	var activated *time.Time
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 72311031))", deviceID).Error; err != nil {
			return err
		}
		var owner nativeRuntimeRegistration
		if err := tx.Where("device_id=? AND native_tenant_id=? AND deployment_id=? AND credential_id=? AND active=true AND deleting=false", deviceID, tenant, deployment, credentialID).Take(&owner).Error; err != nil {
			return ErrNativeRegistrationConflict
		}
		var event nativeCredentialEvent
		if err := tx.Where("event_id=? AND device_id=?", eventID, deviceID).Take(&event).Error; err != nil {
			return ErrNativeRegistrationConflict
		}
		var device model.Device
		if err := tx.Where("id=? AND tenant_id=? AND activate_flag='active'", deviceID, owner.TenantID).Take(&device).Error; err != nil {
			return ErrNativeRegistrationConflict
		}
		// Reconstruct the original immutable event from the current owner voucher.
		// Only a matching original event may repair a stale native projection.
		sum := sha256.Sum256([]byte(device.Voucher))
		if hex.EncodeToString(sum[:]) != owner.VoucherSHA256 {
			return ErrNativeRegistrationConflict
		}
		input.Voucher = device.Voucher
		if event.RequestSHA256 != nativeCredentialRequestDigest(input) {
			return ErrNativeRegistrationConflict
		}
		if device.ActivateAt == nil {
			return ErrNativeRegistrationConflict
		}
		activated = device.ActivateAt
		return nil
	})
	return activated, err
}
