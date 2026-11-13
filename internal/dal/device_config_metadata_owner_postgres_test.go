package dal

import (
	"errors"
	"project/internal/model"
	"testing"
)

// Mainline-only: the stage intentionally retains its old device owner rather
// than importing main's topology rewrite alongside this metadata correction.
func TestDeviceConfigMetadataKeepsMainOwnerPostgres(t *testing.T) {
	db := metadataPG(t)
	cfg := model.DeviceConfig{ID: "q02-owner", TenantID: "tenant-a", Name: "gateway", DeviceType: "2"}
	if err := db.Create(&cfg).Error; err != nil {
		t.Fatal(err)
	}
	parent := "q02-parent"
	for _, d := range []model.Device{{ID: parent, TenantID: cfg.TenantID, DeviceNumber: parent, DeviceConfigID: &cfg.ID}, {ID: "q02-child", TenantID: cfg.TenantID, DeviceNumber: "q02-child", ParentID: &parent}} {
		if err := db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	db.First(&cfg, "id = ?", cfg.ID)
	tag, _ := model.DeviceConfigETag(&cfg)
	condition, _ := model.ParseDeviceConfigCondition(tag)
	if _, err := UpdateDeviceConfigConditional(cfg.ID, cfg.TenantID, map[string]interface{}{"device_type": "1"}, nil, condition); !errors.Is(err, ErrDeviceBindingInvalid) {
		t.Fatalf("metadata updater bypassed owner guard: %v", err)
	}
	var got model.DeviceConfig
	db.First(&got, "id = ?", cfg.ID)
	if got.DeviceType != "2" {
		t.Fatal("rejected role change persisted")
	}
}
