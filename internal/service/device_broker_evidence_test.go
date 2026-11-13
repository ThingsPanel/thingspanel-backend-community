package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/brokerstate"
	"project/internal/model"
	"project/pkg/global"
	"project/pkg/utils"
)

func brokerEvidenceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_BROKER_DATABASE_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not configured")
	}
	root, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open isolated PostgreSQL")
	}
	schema := "broker_evidence_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := root.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal("create isolated schema")
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open isolated schema connection")
	}
	t.Cleanup(func() {
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
		_ = root.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		if sqlDB, e := root.DB(); e == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.Device{}, &model.DeviceConfig{}); err != nil {
		t.Fatal("migrate isolated device fixture")
	}
	if err := db.Exec(`
		CREATE TABLE broker_transport_bindings(tenant_id text,device_id text,broker_cell text,route_epoch bigint,phase text,PRIMARY KEY(tenant_id,device_id));
		CREATE TABLE broker_telemetry_activity(tenant_id text,device_id text,transport_device_id text,broker_cell text,route_epoch bigint,receipt_id text,observed_at timestamptz,next_check_at timestamptz,PRIMARY KEY(tenant_id,device_id));
		CREATE TABLE broker_connection_observations(tenant_id text,device_id text,broker_cell text,broker_instance text,node_boot_id text,connection_id text,route_epoch bigint,event_seq bigint,online boolean,observed_at timestamptz,received_at timestamptz,PRIMARY KEY(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id));
		INSERT INTO broker_transport_bindings VALUES('tenant-a','device-a','cell-a',1,'active');
	`).Error; err != nil {
		t.Fatal("create isolated broker state fixture")
	}
	device := model.Device{ID: "device-a", TenantID: "tenant-a", DeviceNumber: "device-number-a", IsEnabled: "enabled", ActivateFlag: "active", Voucher: `{}`}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal("insert isolated device")
	}
	return db
}

func TestBrokerStatusEvidenceServiceAuthProfileAndTenantScope(t *testing.T) {
	previousDB := global.DB
	previousProfile, hadProfile := viper.Get("mqtt.input_profile"), viper.IsSet("mqtt.input_profile")
	t.Cleanup(func() {
		global.DB = previousDB
		if hadProfile {
			viper.Set("mqtt.input_profile", previousProfile)
		} else {
			viper.Set("mqtt.input_profile", "")
		}
	})

	viper.Set("mqtt.input_profile", "ingress_v1")
	if _, err := GroupApp.Device.BrokerStatusEvidence(context.Background(), nil, "device-a"); err != ErrBrokerEvidenceUnauthorized {
		t.Fatalf("nil claims error=%v", err)
	}
	if _, err := GroupApp.Device.BrokerStatusEvidence(context.Background(), &utils.UserClaims{ID: "user-a"}, "device-a"); err != ErrBrokerEvidenceUnauthorized {
		t.Fatalf("missing tenant error=%v", err)
	}
	viper.Set("mqtt.input_profile", "legacy")
	if _, err := GroupApp.Device.BrokerStatusEvidence(context.Background(), &utils.UserClaims{ID: "user-a", TenantID: "tenant-a"}, "device-a"); err != ErrBrokerEvidenceUnsupported {
		t.Fatalf("legacy profile error=%v", err)
	}

	db := brokerEvidenceTestDB(t)
	global.DB = db
	viper.Set("mqtt.input_profile", "ingress_v1")
	claims := &utils.UserClaims{ID: "user-a", TenantID: "tenant-a"}
	got, err := GroupApp.Device.BrokerStatusEvidence(context.Background(), claims, "device-a")
	if err != nil || got.Status != "unknown" || got.DeviceID != "device-a" || got.BrokerCell != "cell-a" || got.RouteEpoch != 1 {
		t.Fatalf("initial evidence=%+v err=%v", got, err)
	}
	if _, err := GroupApp.Device.BrokerStatusEvidence(context.Background(), claims, "missing-device"); err != brokerstate.ErrDeviceUnavailable {
		t.Fatalf("missing device error=%v", err)
	}
	if _, err := GroupApp.Device.BrokerStatusEvidence(context.Background(), &utils.UserClaims{ID: "user-b", TenantID: "tenant-b"}, "device-a"); err != brokerstate.ErrDeviceUnavailable {
		t.Fatalf("cross-tenant error=%v", err)
	}
}
