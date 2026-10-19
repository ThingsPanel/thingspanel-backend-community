package service

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/brokercommands"
	"project/internal/model"
)

func commandFingerprintPG(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("TEST_BROKER_DATABASE_DSN"))
	if dsn == "" {
		data, err := os.ReadFile("/tmp/tp-mqtt-20261019/qa-dsn")
		if err != nil {
			t.Skip("isolated PostgreSQL QA DSN unavailable")
		}
		dsn = strings.TrimSpace(string(data))
	}
	if dsn == "" {
		t.Skip("isolated PostgreSQL QA DSN empty")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open isolated QA PostgreSQL")
	}
	adminPool, err := admin.DB()
	if err != nil {
		t.Fatal("access isolated QA pool")
	}
	schema := "command_fingerprint_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := `"` + schema + `"`
	if err := admin.Exec("CREATE SCHEMA " + quoted).Error; err != nil {
		t.Fatal("create private schema")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("parse isolated PostgreSQL DSN")
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		q := parsed.Query()
		q.Set("search_path", schema)
		parsed.RawQuery = q.Encode()
		dsn = parsed.String()
	} else {
		dsn += " search_path=" + schema
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open private schema")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("access private schema pool")
	}
	pool.SetMaxOpenConns(8)
	t.Cleanup(func() {
		_ = pool.Close()
		_ = admin.Exec("DROP SCHEMA " + quoted + " CASCADE").Error
		_ = adminPool.Close()
	})
	if err := db.AutoMigrate(&model.Device{}, &model.DeviceConfig{}, &model.DeviceModelCommand{}, &model.DataScript{}, &model.DeviceTopicMapping{}); err != nil {
		t.Fatal("migrate isolated command path schema")
	}
	now := time.Now().UTC()
	for _, config := range []model.DeviceConfig{
		{ID: "cfg-root", Name: "root", DeviceType: "2", TenantID: "tenant-a", CreatedAt: now, UpdatedAt: now},
		{ID: "cfg-middle", Name: "middle", DeviceType: "3", TenantID: "tenant-a", CreatedAt: now, UpdatedAt: now},
		{ID: "cfg-leaf", Name: "leaf", DeviceType: "3", TenantID: "tenant-a", CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(&config).Error; err != nil {
			t.Fatal("seed command-path device configuration")
		}
	}
	middleID, rootID := "middle", "root"
	cfgLeaf, cfgMiddle, cfgRoot := "cfg-leaf", "cfg-middle", "cfg-root"
	for _, device := range []model.Device{
		{ID: "root", TenantID: "tenant-a", IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "root-number", Voucher: `{}`, IsOnline: 0, DeviceConfigID: &cfgRoot},
		{ID: middleID, TenantID: "tenant-a", IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "middle-number", Voucher: `{}`, IsOnline: 0, ParentID: &rootID, DeviceConfigID: &cfgMiddle},
		{ID: "leaf", TenantID: "tenant-a", IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "leaf-number", Voucher: `{}`, IsOnline: 0, ParentID: &middleID, DeviceConfigID: &cfgLeaf},
	} {
		if err := db.Create(&device).Error; err != nil {
			t.Fatal("seed command-path device")
		}
	}
	return db
}

func readFingerprintForTest(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		_, _, _, err := readCommandFingerprint(context.Background(), tx, "tenant-a", "leaf", "root", "cfg-root")
		return err
	})
}

func TestReadCommandFingerprintRejectsDisabledOrInactiveRootAndAncestor(t *testing.T) {
	t.Run("active-path-accepted", func(t *testing.T) {
		db := commandFingerprintPG(t)
		if err := readFingerprintForTest(db); err != nil {
			t.Fatalf("active three-level path rejected: %v", err)
		}
	})
	for _, tc := range []struct{ name, device, column, value string }{
		{"root-disabled", "root", "is_enabled", "disabled"},
		{"root-inactive", "root", "activate_flag", "inactive"},
		{"middle-disabled", "middle", "is_enabled", "disabled"},
		{"middle-inactive", "middle", "activate_flag", "inactive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := commandFingerprintPG(t)
			if err := db.Exec("UPDATE devices SET "+tc.column+"=? WHERE id=? AND tenant_id=?", tc.value, tc.device, "tenant-a").Error; err != nil {
				t.Fatal(err)
			}
			err := readFingerprintForTest(db)
			if !errors.Is(err, brokercommands.ErrRejected) {
				t.Fatalf("fingerprint with %s %s returned %v, want ErrRejected", tc.column, tc.value, err)
			}
		})
	}
}
