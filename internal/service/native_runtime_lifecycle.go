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
	"project/internal/dal"
	"project/internal/model"
	"strings"
	"time"
)

type NativeRuntimeLifecycle struct {
	EventID      string `json:"eventId"`
	DeviceID     string `json:"deviceId"`
	Generation   int64  `json:"generation"`
	Kind         string `json:"kind"`
	DeviceNumber string `json:"deviceNumber"`
}
type nativeLifecycleEvent struct {
	EventID       string `gorm:"primaryKey"`
	DeviceID      string
	RequestSHA256 string `gorm:"column:request_sha256"`
	Generation    int64
	Applied       bool
}

func (nativeLifecycleEvent) TableName() string { return "native_runtime_lifecycle_events" }

// ApplyNativeRuntimeLifecycle changes only a projection owned by this deployment.
// Deletion commits credential invalidation first; retries finish only after all
// old broker leases are acknowledged or expire. The identity tombstone remains.
func ApplyNativeRuntimeLifecycle(ctx context.Context, db *gorm.DB, tenant, deployment string, in NativeRuntimeLifecycle) error {
	validID := func(s string) bool { u, e := uuid.Parse(s); return e == nil && u != uuid.Nil && u.String() == s }
	if !validID(in.EventID) || !validID(in.DeviceID) || !validID(tenant) || deployment == "" || in.Generation < 1 || (in.Kind != "device_renumber" && in.Kind != "device_deletion") {
		return ErrNativeRegistrationInvalid
	}
	if in.Kind == "device_renumber" && (in.DeviceNumber == "" || len(in.DeviceNumber) > 36 || strings.TrimSpace(in.DeviceNumber) != in.DeviceNumber) {
		return ErrNativeRegistrationInvalid
	}
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 72311031))", in.DeviceID).Error; err != nil {
			return err
		}
		var owner nativeRuntimeRegistration
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("device_id=?", in.DeviceID).Take(&owner).Error; err != nil {
			return ErrNativeRegistrationConflict
		}
		if owner.NativeTenantID != tenant || owner.DeploymentID != deployment {
			return ErrNativeRegistrationConflict
		}
		var event nativeLifecycleEvent
		err := tx.Where("event_id=?", in.EventID).Take(&event).Error
		if err == nil {
			if event.DeviceID != in.DeviceID || event.RequestSHA256 != digest {
				return ErrNativeRegistrationConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// Renumber advances resource_version; deletion intentionally uses the
		// current version without incrementing it. Equal-version deletion must
		// therefore follow renumber, but an equal-version new renumber cannot.
		if owner.Deleting || in.Generation < owner.LifecycleGeneration || (in.Generation == owner.LifecycleGeneration && in.Kind != "device_deletion") {
			return ErrNativeRegistrationConflict
		}
		if in.Kind == "device_renumber" {
			r := tx.Model(&model.Device{}).Where("id=? AND tenant_id=?", in.DeviceID, owner.TenantID).Update("device_number", in.DeviceNumber)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return ErrNativeRegistrationConflict
			}
		} else {
			voucher, _ := json.Marshal(map[string]string{"username": "deleted-" + uuid.NewString()})
			hash := sha256.Sum256(voucher)
			r := tx.Model(&model.Device{}).Where("id=? AND tenant_id=?", in.DeviceID, owner.TenantID).Updates(map[string]any{"voucher": string(voucher), "is_enabled": "disabled"})
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return ErrNativeRegistrationConflict
			}
			if err := tx.Model(&owner).Updates(map[string]any{"deleting": true, "active": false, "voucher_sha256": hex.EncodeToString(hash[:])}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&owner).Update("lifecycle_generation", in.Generation).Error; err != nil {
			return err
		}
		return tx.Create(&nativeLifecycleEvent{EventID: in.EventID, DeviceID: in.DeviceID, RequestSHA256: digest, Generation: in.Generation, Applied: in.Kind == "device_renumber"}).Error
	})
	if err != nil || in.Kind != "device_deletion" {
		return err
	}
	if err := ConfirmNativeCredentialRevocation(ctx, db, in.DeviceID); err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 72311031))", in.DeviceID).Error; err != nil {
			return err
		}
		var event nativeLifecycleEvent
		if err := tx.Where("event_id=? AND request_sha256=?", in.EventID, digest).Take(&event).Error; err != nil {
			return err
		}
		if event.Applied {
			return nil
		}
		var owner nativeRuntimeRegistration
		if err := tx.Where("device_id=? AND native_tenant_id=? AND deployment_id=? AND deleting=true", in.DeviceID, tenant, deployment).Take(&owner).Error; err != nil {
			return ErrNativeRegistrationConflict
		}
		// Keep the disabled runtime tombstone: physical deletion can cascade into
		// telemetry/command history or fail on event/OTA foreign keys. The native
		// owner hides the device only after this receipt, while broker access is
		// already revoked and registration cannot reactivate a deleting identity.
		if err := tx.Model(&dal.DeviceIDReservation{}).Where("id=? AND tenant_id=?", in.DeviceID, owner.TenantID).Updates(map[string]any{"state": "tombstoned", "deleted_at": time.Now().UTC()}).Error; err != nil {
			return err
		}
		return tx.Model(&event).Update("applied", true).Error
	})
}
