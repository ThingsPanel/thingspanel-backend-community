package service

import (
	"context"
	"encoding/json"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"project/initialize"
	"project/internal/dal"
	"project/internal/model"
	protocolplugin "project/internal/service/protocol_plugin"
	"project/pkg/global"
	"project/pkg/utils"
)

// runtimeDeviceTx owns the device lock and lifecycle check in the exact same
// transaction as a mutation. It must never wrap a legacy independent writer.
func runtimeDeviceTx(ctx context.Context, tenant, id string, mutate func(*gorm.DB, *model.Device) error) error {
	if ctx == nil || global.DB == nil || tenant == "" || id == "" {
		return errors.New("runtime device scope unavailable")
	}
	return global.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() != "postgres" {
			return errors.New("runtime scope requires PostgreSQL row locking")
		}
		if err := tx.Exec("SET LOCAL lock_timeout='3s'").Error; err != nil {
			return err
		}
		var d model.Device
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=?", id, tenant).Take(&d).Error; err != nil {
			return err
		}
		var reservation dal.DeviceIDReservation
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=? AND state='active'", id, tenant).Take(&reservation).Error; err != nil {
			return err
		}
		return mutate(tx, &d)
	})
}

func (*Device) RuntimeUpdateVoucher(ctx context.Context, req *model.UpdateDeviceVoucherReq, claims *utils.UserClaims) (string, error) {
	if req == nil || claims == nil {
		return "", errors.New("runtime voucher scope unavailable")
	}
	voucher, ok := req.Voucher.(string)
	if !ok {
		b, err := json.Marshal(req.Voucher)
		if err != nil {
			return "", err
		}
		voucher = string(b)
	}
	var old string
	var disconnect bool
	err := runtimeDeviceTx(ctx, claims.TenantID, req.DeviceID, func(tx *gorm.DB, d *model.Device) error {
		old = d.Voucher
		disconnect = d.DeviceConfigID != nil && *d.DeviceConfigID != ""
		if voucher == "{}" {
			return errors.New("empty voucher is not a credential rotation")
		}
		// All runtime credential writers serialize the uniqueness check, including
		// distinct device IDs. The existing database constraints remain authoritative.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "runtime-voucher:"+voucher).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&model.Device{}).Where("voucher=? AND id<>?", voucher, req.DeviceID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return errors.New("voucher already exists")
		}
		r := tx.Model(&model.Device{}).Where("id=? AND tenant_id=?", req.DeviceID, claims.TenantID).Update("voucher", voucher)
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if global.REDIS != nil {
		initialize.DelDeviceCache(req.DeviceID)
	}
	if global.REDIS != nil && old != voucher {
		global.REDIS.Del(ctx, old)
	}
	// The device ID cannot be reused across tenants; disconnect uses the frozen
	// authorized ID and contains no caller-supplied parent or credential.
	if old != voucher && disconnect {
		_ = protocolplugin.DisconnectDeviceByDeviceID(req.DeviceID)
	}
	return voucher, nil
}

func (*Device) RuntimeRemoveSubDevice(ctx context.Context, id string, claims *utils.UserClaims) error {
	if claims == nil {
		return errors.New("runtime subdevice scope unavailable")
	}
	var parent string
	var disconnect bool
	err := runtimeDeviceTx(ctx, claims.TenantID, id, func(tx *gorm.DB, d *model.Device) error {
		if d.ParentID != nil && *d.ParentID != "" {
			parent = *d.ParentID
			var p model.Device
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=?", parent, claims.TenantID).Take(&p).Error; err != nil {
				return err
			}
			disconnect = p.DeviceConfigID != nil && *p.DeviceConfigID != ""
		}
		r := tx.Model(&model.Device{}).Where("id=? AND tenant_id=?", id, claims.TenantID).Updates(map[string]any{"parent_id": nil, "sub_device_addr": nil})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}
	if global.REDIS != nil {
		initialize.DelDeviceCache(id)
	}
	if parent != "" && disconnect {
		_ = protocolplugin.DisconnectDeviceByDeviceID(parent)
	}
	return nil
}

func (*ExpectedData) RuntimeDelete(ctx context.Context, id string, claims *utils.UserClaims) error {
	if claims == nil || global.DB == nil {
		return errors.New("runtime expected scope unavailable")
	}
	// Resolve association server-side, then lock the device before reloading the
	// record for update. A concurrent association change fails the second predicate.
	var row model.ExpectedData
	if err := global.DB.WithContext(ctx).Where("id=? AND tenant_id=?", id, claims.TenantID).Take(&row).Error; err != nil {
		return err
	}
	return runtimeDeviceTx(ctx, claims.TenantID, row.DeviceID, func(tx *gorm.DB, _ *model.Device) error {
		var locked model.ExpectedData
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=? AND device_id=? AND status='pending'", id, claims.TenantID, row.DeviceID).Take(&locked).Error; err != nil {
			return err
		}
		r := tx.Where("id=? AND tenant_id=? AND device_id=? AND status='pending'", id, claims.TenantID, row.DeviceID).Delete(&model.ExpectedData{})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		if tx.Migrator().HasTable("broker_expected_context") {
			return tx.Table("broker_expected_context").Where("expected_id=? AND tenant_id=? AND device_id=?", id, claims.TenantID, row.DeviceID).Delete(&expectedContext{}).Error
		}
		return nil
	})
}
