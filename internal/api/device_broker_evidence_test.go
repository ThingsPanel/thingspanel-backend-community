package api

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
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

func TestBrokerStatusEvidenceHandlerAuthAndProfileErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousProfile, hadProfile := viper.Get("mqtt.input_profile"), viper.IsSet("mqtt.input_profile")
	t.Cleanup(func() {
		if hadProfile {
			viper.Set("mqtt.input_profile", previousProfile)
		} else {
			viper.Set("mqtt.input_profile", "")
		}
	})
	for _, test := range []struct {
		name    string
		profile string
		claims  *utils.UserClaims
		want    int
	}{
		{name: "missing identity", profile: "ingress_v1", want: 401},
		{name: "unsupported profile", profile: "legacy", claims: &utils.UserClaims{ID: "user-a", TenantID: "tenant-a"}, want: 409},
	} {
		t.Run(test.name, func(t *testing.T) {
			viper.Set("mqtt.input_profile", test.profile)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest("GET", "/api/v1/device/status/evidence/device-a", nil)
			ctx.Params = gin.Params{{Key: "id", Value: "device-a"}}
			if test.claims != nil {
				ctx.Set("claims", test.claims)
			}
			(&DeviceApi{}).BrokerStatusEvidence(ctx)
			if recorder.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, test.want, recorder.Body.String())
			}
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("sensitive state evidence response is cacheable")
			}
		})
	}
}

func TestBrokerStatusEvidenceHandlerUnknownAndTenantScopedNotFound(t *testing.T) {
	dsn := os.Getenv("TEST_BROKER_DATABASE_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL DSN not configured")
	}
	root, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open isolated PostgreSQL")
	}
	schema := "broker_evidence_api_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := root.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal("create isolated schema")
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open isolated schema connection")
	}
	previousDB := global.DB
	previousProfile, hadProfile := viper.Get("mqtt.input_profile"), viper.IsSet("mqtt.input_profile")
	global.DB = db
	viper.Set("mqtt.input_profile", "ingress_v1")
	t.Cleanup(func() {
		global.DB = previousDB
		if hadProfile {
			viper.Set("mqtt.input_profile", previousProfile)
		} else {
			viper.Set("mqtt.input_profile", "")
		}
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
		_ = root.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		if sqlDB, e := root.DB(); e == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.Device{}); err != nil {
		t.Fatal("migrate isolated device fixture")
	}
	if err := db.Exec(`
		CREATE TABLE broker_transport_bindings(tenant_id text,device_id text,broker_cell text,route_epoch bigint,phase text,PRIMARY KEY(tenant_id,device_id));
		CREATE TABLE broker_telemetry_activity(tenant_id text,device_id text,transport_device_id text,broker_cell text,route_epoch bigint,receipt_id text,observed_at timestamptz,next_check_at timestamptz,PRIMARY KEY(tenant_id,device_id));
		CREATE TABLE broker_connection_observations(tenant_id text,device_id text,broker_cell text,broker_instance text,node_boot_id text,connection_id text,route_epoch bigint,event_seq bigint,online boolean,observed_at timestamptz,received_at timestamptz,PRIMARY KEY(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id));
		INSERT INTO broker_transport_bindings VALUES('tenant-a','device-a','cell-a',1,'active');
	`).Error; err != nil {
		t.Fatal("create isolated state fixture")
	}
	device := model.Device{ID: "device-a", TenantID: "tenant-a", DeviceNumber: "device-number-a", IsEnabled: "enabled", ActivateFlag: "active", Voucher: `{}`}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal("insert isolated device")
	}

	for _, tc := range []struct {
		name       string
		tenant     string
		wantStatus int
		wantData   bool
	}{
		{name: "owner tenant sees unknown as explicit result", tenant: "tenant-a", wantStatus: 200, wantData: true},
		{name: "other tenant gets not found", tenant: "tenant-b", wantStatus: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest("GET", "/api/v1/device/status/evidence/device-a", nil).WithContext(context.Background())
			ctx.Params = gin.Params{{Key: "id", Value: "device-a"}}
			ctx.Set("claims", &utils.UserClaims{ID: "user-a", TenantID: tc.tenant})
			(&DeviceApi{}).BrokerStatusEvidence(ctx)
			if recorder.Code != tc.wantStatus || recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("HTTP status=%d cache=%q body=%s", recorder.Code, recorder.Header().Get("Cache-Control"), recorder.Body.String())
			}
			data, exists := ctx.Get("data")
			if exists != tc.wantData {
				t.Fatalf("data presence=%v want=%v", exists, tc.wantData)
			}
			if tc.wantData {
				evidence, ok := data.(brokerstate.Evidence)
				if !ok || evidence.Status != "unknown" || evidence.DeviceID != "device-a" || evidence.AsOf.IsZero() || evidence.AsOf.After(time.Now().UTC().Add(time.Second)) {
					t.Fatalf("unexpected evidence %#v", data)
				}
			}
		})
	}
}
