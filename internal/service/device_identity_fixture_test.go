package service

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"project/internal/dal"
	"project/internal/model"
	"reflect"
	"testing"
	"time"
)

// Existing tests seed device rows directly rather than through the public
// creation service. Seed their corresponding cutover identity in the same seed
// transaction before insertion; real creation tests still exercise the DAL's reservation CAS.
func installDeviceIdentityFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&dal.DeviceIDReservation{}); err != nil {
		t.Fatal(err)
	}
	name := "fixture-device-reservation"
	if err := db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table != "devices" || tx.Error != nil {
			return
		}
		var seed func(reflect.Value)
		seed = func(value reflect.Value) {
			for value.IsValid() && (value.Kind() == reflect.Ptr || value.Kind() == reflect.Interface) {
				if value.IsNil() {
					return
				}
				value = value.Elem()
			}
			if !value.IsValid() {
				return
			}
			if value.Kind() == reflect.Slice || value.Kind() == reflect.Array {
				for i := 0; i < value.Len(); i++ {
					seed(value.Index(i))
				}
				return
			}
			if !value.CanInterface() {
				return
			}
			d, ok := value.Interface().(model.Device)
			if !ok {
				return
			}
			result := tx.Session(&gorm.Session{NewDB: true}).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoNothing: true}).Create(&dal.DeviceIDReservation{ID: d.ID, TenantID: d.TenantID, State: "active", CreatedAt: time.Now().UTC()})
			if result.Error != nil {
				tx.AddError(result.Error)
			}
		}
		seed(tx.Statement.ReflectValue)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(name) })
}
