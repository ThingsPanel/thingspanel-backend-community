package service

import (
	"context"
	"encoding/json"
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
	"project/internal/brokermapper"
	"project/internal/downlink"
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

func durableCustomMappingRow(t *testing.T) (*gorm.DB, brokercommands.Row, string) {
	t.Helper()
	db := commandFingerprintPG(t)
	if err := db.Exec(`CREATE TABLE broker_mapping_snapshots (
		version varchar(80) PRIMARY KEY,
		rules jsonb NOT NULL,
		created_at timestamptz NOT NULL DEFAULT now()
	)`).Error; err != nil {
		t.Fatal("create immutable mapping snapshot table")
	}
	now := time.Now().UTC()
	configID := "cfg-direct-custom"
	if err := db.Create(&model.DeviceConfig{ID: configID, Name: "direct custom", DeviceType: "1", TenantID: "tenant-a", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal("seed direct custom config")
	}
	if err := db.Create(&model.Device{ID: "direct-custom", TenantID: "tenant-a", IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "direct-number", Voucher: `{}`, IsOnline: 0, DeviceConfigID: &configID}).Error; err != nil {
		t.Fatal("seed direct custom device")
	}
	if err := db.Create(&model.DeviceTopicMapping{DeviceConfigID: configID, Name: "custom command", Direction: "down", SourceTopic: "custom/{device_number}/command/{message_id}", TargetTopic: "devices/command/{device_number}/{message_id}", Priority: 10, Enabled: true, DataIdentifier: stringPtr("set")}).Error; err != nil {
		t.Fatal("seed custom command mapping")
	}
	script := "function encodeInp(input, topic) return input end"
	createdAt, updatedAt := now, now
	if err := db.Create(&model.DataScript{ID: "script-custom-e", Name: "command encoder", DeviceConfigID: configID, EnableFlag: "Y", Content: &script, ScriptType: "E", CreatedAt: &createdAt, UpdatedAt: &updatedAt}).Error; err != nil {
		t.Fatal("seed command encoder script")
	}
	fingerprint, active, snapshot, err := func() (string, *model.DataScript, commandFingerprintSnapshot, error) {
		var fp string
		var active *model.DataScript
		var snap commandFingerprintSnapshot
		err := db.Transaction(func(tx *gorm.DB) error {
			var readErr error
			fp, active, snap, readErr = readCommandFingerprint(context.Background(), tx, "tenant-a", "direct-custom", "direct-custom", configID)
			return readErr
		})
		return fp, active, snap, err
	}()
	if err != nil {
		t.Fatal("read accepted custom command fingerprint")
	}
	if snapshot.MappingVersion == "" || snapshot.MappingVersion == "standard-v1" || len(snapshot.Mappings) != 1 {
		t.Fatalf("custom mapping was not frozen into fingerprint: version=%q rules=%#v", snapshot.MappingVersion, snapshot.Mappings)
	}
	repository := brokermapper.NewRepository(db)
	loaded, err := repository.Load(context.Background(), snapshot.MappingVersion)
	if err != nil || loaded.Version != snapshot.MappingVersion || len(loaded.Rules()) != 1 {
		t.Fatalf("load accepted mapping snapshot version=%q: snapshot=%#v err=%v", snapshot.MappingVersion, loaded, err)
	}
	if active == nil {
		t.Fatal("command encoder script missing from accepted configuration")
	}
	prepared := brokercommands.PreparedCommand{
		Message: downlink.Message{DeviceID: "direct-custom", DeviceNumber: "direct-number", DeviceType: "1", DeviceConfigID: configID, Type: downlink.MessageTypeCommand},
		Encoded: []byte(`{"method":"set","params":{"value":1}}`), ConfigurationSHA256: fingerprint, MappingVersion: snapshot.MappingVersion,
	}
	payload, err := json.Marshal(prepared)
	if err != nil {
		t.Fatal("encode accepted command row")
	}
	row := brokercommands.Row{TenantID: "tenant-a", DeviceID: "direct-custom", TargetDeviceID: "direct-custom", Payload: payload}
	if err := db.Transaction(func(tx *gorm.DB) error { return ValidateDurableCommandSnapshot(context.Background(), tx, row) }); err != nil {
		t.Fatalf("unchanged accepted command did not validate: %v", err)
	}
	return db, row, configID
}

func TestDurableCustomCommandMappingAndScriptChangesFenceAcceptedRow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, *gorm.DB, string)
	}{
		{
			name: "mapping-edited",
			mutate: func(t *testing.T, db *gorm.DB, configID string) {
				if err := db.Model(&model.DeviceTopicMapping{}).Where("device_config_id = ?", configID).Update("source_topic", "custom-v2/{device_number}/command/{message_id}").Error; err != nil {
					t.Fatal("edit accepted mapping")
				}
			},
		},
		{
			name: "mapping-added",
			mutate: func(t *testing.T, db *gorm.DB, configID string) {
				rule := model.DeviceTopicMapping{DeviceConfigID: configID, Name: "second command", Direction: "down", SourceTopic: "custom/{device_number}/other/{message_id}", TargetTopic: "devices/command/{device_number}/{message_id}", Priority: 20, Enabled: true}
				if err := db.Create(&rule).Error; err != nil {
					t.Fatal("add mapping")
				}
			},
		},
		{
			name: "mapping-disabled",
			mutate: func(t *testing.T, db *gorm.DB, configID string) {
				if err := db.Model(&model.DeviceTopicMapping{}).Where("device_config_id = ?", configID).Update("enabled", false).Error; err != nil {
					t.Fatal("disable accepted mapping")
				}
			},
		},
		{
			name: "command-script-edited",
			mutate: func(t *testing.T, db *gorm.DB, _ string) {
				if err := db.Model(&model.DataScript{}).Where("id = ?", "script-custom-e").Update("content", "function encodeInp(input, topic) return {changed=true} end").Error; err != nil {
					t.Fatal("edit accepted command script")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, row, configID := durableCustomMappingRow(t)
			tc.mutate(t, db, configID)
			err := db.Transaction(func(tx *gorm.DB) error { return ValidateDurableCommandSnapshot(context.Background(), tx, row) })
			if !errors.Is(err, brokercommands.ErrFenced) {
				t.Fatalf("changed configuration returned %v, want ErrFenced", err)
			}
		})
	}
}
