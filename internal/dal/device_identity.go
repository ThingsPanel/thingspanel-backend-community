package dal

import (
	"errors"
	"gorm.io/gorm"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/dbdialect"
	"sort"
	"strings"
	"time"
)

var ErrDeviceIDReserved = errors.New("device ID is permanently reserved; choose a new ID")
var ErrDeviceIdentityState = errors.New("device identity reservation is missing or inconsistent")

type DeviceIDReservation struct {
	ID        string     `gorm:"column:id;primaryKey"`
	TenantID  string     `gorm:"column:tenant_id;not null"`
	State     string     `gorm:"column:state;not null;size:16"`
	CreatedAt time.Time  `gorm:"column:created_at;not null"`
	DeletedAt *time.Time `gorm:"column:deleted_at"`
}

func (DeviceIDReservation) TableName() string { return "device_id_reservations" }

// reserveDeviceIDs is part of the device/group transaction. A reservation is
// never reused, including after deletion, and unknown commits are reconciled
// through the tenant-authorized device/reservation state rather than removed.
func reserveDeviceIDs(tx *query.Query, devices []*model.Device) error {
	sorted := append([]*model.Device(nil), devices...)
	for _, d := range sorted {
		if d == nil || strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.TenantID) == "" {
			return ErrDeviceIdentityState
		}
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	db := tx.Device.UnderlyingDB().Session(&gorm.Session{NewDB: true})
	for _, d := range sorted {
		reservation := DeviceIDReservation{ID: d.ID, TenantID: d.TenantID, State: "active", CreatedAt: time.Now().UTC()}
		inserted, err := dbdialect.InsertIfAbsent(db, &reservation, "id")
		if err != nil {
			return err
		}
		if !inserted {
			return ErrDeviceIDReserved
		}
	}
	return nil
}

func tombstoneDeviceID(tx *query.Query, id, tenantID string) error {
	db := tx.Device.UnderlyingDB().Session(&gorm.Session{NewDB: true})
	result := db.Model(&DeviceIDReservation{}).Where("id=? AND tenant_id=? AND state='active'", id, tenantID).Updates(map[string]interface{}{"state": "tombstoned", "deleted_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrDeviceIdentityState
	}
	return nil
}
