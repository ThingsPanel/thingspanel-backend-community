package dal

import (
	"testing"
	"time"

	"project/internal/model"
	"project/internal/query"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestRawDeviceDataReadsUseBindingAwareMetadataPath(t *testing.T) {
	db, closeDB := setupRawDeviceDataDB(t, true)
	defer closeDB()
	setRawDeviceDataFixture(t, db, true)

	attrs, err := GetAttributeDataListWithDeviceName("native-device")
	if err != nil {
		t.Fatalf("read native attributes without legacy metadata tables: %v", err)
	}
	if len(attrs) != 1 || attrs[0]["key"] != "temperature" {
		t.Fatalf("native attribute read did not return the raw record: %#v", attrs)
	}
	if _, ok := attrs[0]["data_name"]; ok {
		t.Fatalf("native attribute read projected legacy metadata: %#v", attrs[0])
	}

	count, events, err := GetEventDatasListByPage(&model.GetEventDatasListByPageReq{DeviceId: "native-device"})
	if err != nil {
		t.Fatalf("read native events without legacy metadata tables: %v", err)
	}
	if count != 1 || len(events) != 1 || events[0]["identify"] != "alarm" {
		t.Fatalf("native event read did not return the raw record: count=%d rows=%#v", count, events)
	}
	if _, ok := events[0]["data_name"]; ok {
		t.Fatalf("native event read projected legacy metadata: %#v", events[0])
	}
}

func TestRawDeviceDataReadsKeepLegacyMetadataPath(t *testing.T) {
	db, closeDB := setupRawDeviceDataDB(t, false)
	defer closeDB()
	setRawDeviceDataFixture(t, db, false)

	attrs, err := GetAttributeDataListWithDeviceName("legacy-device")
	if err != nil {
		t.Fatalf("read legacy attributes: %v", err)
	}
	if len(attrs) != 1 || attrs[0]["data_name"] != "Temperature" {
		t.Fatalf("legacy attribute metadata join was not preserved: %#v", attrs)
	}

	count, events, err := GetEventDatasListByPage(&model.GetEventDatasListByPageReq{DeviceId: "legacy-device"})
	if err != nil {
		t.Fatalf("read legacy events: %v", err)
	}
	if count != 1 || len(events) != 1 || events[0]["data_name"] != "Alarm" {
		t.Fatalf("legacy event metadata join was not preserved: count=%d rows=%#v", count, events)
	}
}

func TestHasNativeDeviceConfigBindingTreatsAnyReferenceAsBinding(t *testing.T) {
	modelID, snapshotID := "model", "snapshot"
	revision := 1
	tests := []struct {
		name   string
		config *model.DeviceConfig
		want   bool
	}{
		{name: "nil"},
		{name: "legacy only", config: &model.DeviceConfig{DeviceTemplateID: stringPtr("template")}},
		{name: "model id only", config: &model.DeviceConfig{NativeThingModelID: &modelID}, want: true},
		{name: "snapshot id only", config: &model.DeviceConfig{NativeThingModelSnapshotID: &snapshotID}, want: true},
		{name: "revision only", config: &model.DeviceConfig{NativeThingModelRevision: &revision}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasNativeDeviceConfigBinding(test.config); got != test.want {
				t.Fatalf("hasNativeDeviceConfigBinding()=%t, want %t", got, test.want)
			}
		})
	}
}

func setupRawDeviceDataDB(t *testing.T, native bool) (*gorm.DB, func()) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:raw-device-data?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	tables := []any{&model.Device{}, &model.DeviceConfig{}, &model.AttributeData{}, &model.EventData{}}
	if !native {
		tables = append(tables, &model.DeviceTemplate{}, &model.DeviceModelAttribute{}, &model.DeviceModelEvent{})
	}
	if err := db.AutoMigrate(tables...); err != nil {
		t.Fatalf("migrate sqlite fixture: %v", err)
	}
	oldQuery := *query.Q
	query.SetDefault(db)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite connection: %v", err)
	}
	return db, func() {
		*query.Q = oldQuery
		_ = sqlDB.Close()
	}
}

func setRawDeviceDataFixture(t *testing.T, db *gorm.DB, native bool) {
	t.Helper()
	now := time.Now().UTC()
	templateID := "legacy-template"
	configID := "device-config"
	deviceID := "legacy-device"
	if native {
		deviceID = "native-device"
	}
	config := &model.DeviceConfig{
		ID: configID, Name: "config", DeviceTemplateID: &templateID, DeviceType: "1", TenantID: "tenant",
		CreatedAt: now, UpdatedAt: now,
	}
	if native {
		config.NativeThingModelID = stringPtr("native-model") // partial binding must still fail closed from legacy metadata.
	}
	if err := db.Create(config).Error; err != nil {
		t.Fatalf("create device config: %v", err)
	}
	device := &model.Device{
		ID: deviceID, Voucher: "voucher", TenantID: "tenant", IsEnabled: "enabled", ActivateFlag: "active",
		DeviceNumber: "device-number", DeviceConfigID: &configID,
	}
	if err := db.Create(device).Error; err != nil {
		t.Fatalf("create device: %v", err)
	}
	if !native {
		if err := db.Create(&model.DeviceTemplate{
			ID: templateID, Name: "legacy", TenantID: "tenant", CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatalf("create legacy template: %v", err)
		}
		if err := db.Create(&model.DeviceModelAttribute{
			ID: "attribute-metadata", DeviceTemplateID: templateID, DataIdentifier: "temperature",
			DataName: stringPtr("Temperature"), CreatedAt: now, UpdatedAt: now, TenantID: "tenant",
		}).Error; err != nil {
			t.Fatalf("create legacy attribute metadata: %v", err)
		}
		if err := db.Create(&model.DeviceModelEvent{
			ID: "event-metadata", DeviceTemplateID: templateID, DataIdentifier: "alarm",
			DataName: stringPtr("Alarm"), CreatedAt: now, UpdatedAt: now, TenantID: "tenant",
		}).Error; err != nil {
			t.Fatalf("create legacy event metadata: %v", err)
		}
	}
	if err := db.Create(&model.AttributeData{ID: "attribute", DeviceID: deviceID, Key: "temperature", T: now}).Error; err != nil {
		t.Fatalf("create attribute raw record: %v", err)
	}
	if err := db.Create(&model.EventData{ID: "event", DeviceID: deviceID, Identify: "alarm", T: now}).Error; err != nil {
		t.Fatalf("create event raw record: %v", err)
	}
}

func stringPtr(value string) *string { return &value }
