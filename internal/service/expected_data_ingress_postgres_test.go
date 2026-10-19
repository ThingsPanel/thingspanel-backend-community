package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/go-basic/uuid"
	"github.com/spf13/viper"
	"project/internal/brokercommands"
	"project/internal/downlink"
	"project/internal/model"
	"project/pkg/global"
	"project/pkg/utils"
)

func TestExpectedDataIngressPostgresAdmissionDispatchRetryAndScope(t *testing.T) {
	db := brokerEvidenceTestDB(t)
	if err := db.AutoMigrate(&model.User{}, &model.ExpectedData{}, &model.DataScript{}, &model.DeviceModelCommand{}, &model.DeviceTopicMapping{}, &model.CommandSetLog{}, &model.AttributeSetLog{}, &model.TelemetrySetLog{}); err != nil {
		t.Fatal("migrate isolated expected-data fixture")
	}
	for _, migration := range []string{"006_commands.sql", "015_expected_context.sql"} {
		contents, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", migration))
		if err != nil {
			t.Fatal("read expected-data migration")
		}
		if err := db.Exec(strings.ReplaceAll(string(contents), "public.", "")).Error; err != nil {
			t.Fatalf("apply expected-data migration %s: %v", migration, err)
		}
	}
	previousDB, previousEnforcer := global.DB, global.CasbinEnforcer
	previousProfile, hadProfile := viper.Get("mqtt.input_profile"), viper.IsSet("mqtt.input_profile")
	t.Cleanup(func() {
		global.DB, global.CasbinEnforcer = previousDB, previousEnforcer
		if hadProfile {
			viper.Set("mqtt.input_profile", previousProfile)
		} else {
			viper.Set("mqtt.input_profile", "")
		}
	})
	enforcer, err := casbin.NewEnforcer(filepath.Join("..", "..", "configs", "casbin.conf"))
	if err != nil {
		t.Fatal("load isolated Casbin model")
	}
	global.DB, global.CasbinEnforcer = db, enforcer
	viper.Set("mqtt.input_profile", "ingress_v1")

	const tenant, caller, deviceID = "tenant-a", "expected-user", "device-a"
	status, authority := "N", "TENANT_ADMIN"
	for _, u := range []model.User{
		{ID: caller, TenantID: stringPtr(tenant), Status: &status, Authority: &authority, PhoneNumber: "fixture", Email: "expected@example.invalid", Password: "unused"},
		{ID: "expected-user-b", TenantID: stringPtr("tenant-b"), Status: &status, Authority: &authority, PhoneNumber: "fixture-b", Email: "expected-b@example.invalid", Password: "unused"},
	} {
		if err := db.Create(&u).Error; err != nil {
			t.Fatal("seed expected-data caller")
		}
	}

	claims := &utils.UserClaims{ID: caller, TenantID: tenant}
	requests := []*model.CreateExpectedDataReq{
		{DeviceID: deviceID, SendType: "telemetry", Payload: stringPtr(`{"temperature":22}`)},
		{DeviceID: deviceID, SendType: "attribute", Payload: stringPtr(`{"desired":true}`)},
		{DeviceID: deviceID, SendType: "command", Identify: stringPtr("set_power"), Payload: stringPtr(`{"value":true}`)},
	}
	created := make([]*model.ExpectedData, 0, len(requests))
	for _, req := range requests {
		got, err := GroupApp.ExpectedData.Create(context.Background(), req, claims)
		if err != nil {
			t.Fatalf("create %s expected message: %v", req.SendType, err)
		}
		if got.Status != "pending" {
			t.Fatalf("offline %s admission status=%q, want pending", req.SendType, got.Status)
		}
		var owner expectedContext
		if err := db.Table("broker_expected_context").Where("expected_id=?", got.ID).Take(&owner).Error; err != nil {
			t.Fatalf("creator context for %s missing: %v", req.SendType, err)
		}
		if owner.CallerID != caller || owner.TenantID != tenant || owner.DeviceID != deviceID || owner.SendType != req.SendType || owner.PayloadSHA256 != expectedPayloadHash(got.Payload) {
			t.Fatalf("creator context mismatch for %s", req.SendType)
		}
		created = append(created, got)
	}
	if _, err := GroupApp.ExpectedData.Create(context.Background(), requests[0], &utils.UserClaims{ID: "expected-user-b", TenantID: "tenant-b"}); err == nil {
		t.Fatal("cross-tenant expected-data create was accepted")
	}
	var crossTenantRows int64
	if err := db.Model(&model.ExpectedData{}).Where("tenant_id=?", "tenant-b").Count(&crossTenantRows).Error; err != nil || crossTenantRows != 0 {
		t.Fatalf("cross-tenant rows=%d err=%v", crossTenantRows, err)
	}
	if err := GroupApp.ExpectedData.DeleteScoped(context.Background(), created[0].ID, &utils.UserClaims{ID: "expected-user-b", TenantID: "tenant-b"}); err == nil {
		t.Fatal("cross-tenant expected-data delete was accepted")
	}
	stillPending, err := getExpectedDataGlobal(context.Background(), created[0].ID)
	if err != nil || stillPending.Status != "pending" {
		t.Fatalf("cross-tenant delete changed expected message status=%v err=%v", stillPending, err)
	}

	// The current broker connection evidence is the only online gate; the
	// expected-data acceptance itself is tested independently of MQTT/device ACK.
	if err := db.Exec(`INSERT INTO broker_connection_observations(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id,route_epoch,event_seq,online,observed_at,received_at) VALUES(?,?,?,'instance-a','boot-a','connection-a',1,1,true,now(),now())`, tenant, deviceID, "cell-a").Error; err != nil {
		t.Fatal("seed fresh broker connection evidence")
	}
	for _, expected := range created {
		if err := db.Exec(`UPDATE broker_expected_context SET next_attempt_at=now() WHERE expected_id=?`, expected.ID).Error; err != nil {
			t.Fatal("make expected messages dispatchable")
		}
	}
	if err := DispatchPendingExpected(context.Background(), "cell-a"); err != nil {
		t.Fatalf("dispatch expected messages: %v", err)
	}
	var commandCount int64
	if err := db.Model(&brokercommands.Row{}).Count(&commandCount).Error; err != nil || commandCount != 3 {
		t.Fatalf("durable command rows=%d err=%v, want 3", commandCount, err)
	}
	for _, expected := range created {
		current, err := getExpectedDataGlobal(context.Background(), expected.ID)
		if err != nil || current.Status != "sent" || current.Message == nil || *current.Message == "" {
			t.Fatalf("%s expected status=%+v err=%v", expected.SendType, current, err)
		}
		var count int64
		if err := db.Model(&brokercommands.Row{}).Where("id=? AND tenant_id=? AND caller_id=? AND intent=? AND idempotency_key=?", *current.Message, tenant, caller, "expected."+expected.SendType, expected.ID).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("%s durable identity rows=%d err=%v", expected.SendType, count, err)
		}
		logTable := map[string]string{"command": "command_set_logs", "attribute": "attribute_set_logs", "telemetry": "telemetry_set_logs"}[expected.SendType]
		if err := db.Table(logTable).Where("id=?", *current.Message).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("%s expected-data audit rows=%d err=%v", expected.SendType, count, err)
		}
		commandID, err := submitExpectedDownlink(context.Background(), current, caller)
		if err != nil || commandID != *current.Message {
			t.Fatalf("%s retry command=%q err=%v", expected.SendType, commandID, err)
		}
	}
	if err := db.Model(&brokercommands.Row{}).Count(&commandCount).Error; err != nil || commandCount != 3 {
		t.Fatalf("idempotent retry created extra command rows=%d err=%v", commandCount, err)
	}

	t.Run("registered_plugin_socket_is_frozen", func(t *testing.T) {
		if err := db.AutoMigrate(&model.ServicePlugin{}, &model.ServiceAccess{}); err != nil {
			t.Fatal(err)
		}
		config := `{"sub_topic_prefix":"service/alarm/"}`
		plugin := model.ServicePlugin{ID: "plugin-expected", ServiceIdentifier: "expected-plugin", ServiceType: 2, ServiceConfig: &config}
		access := model.ServiceAccess{ID: "access-expected", ServicePluginID: plugin.ID, TenantID: tenant}
		if err := db.Create(&plugin).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&access).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.Device{}).Where("id=?", deviceID).Updates(map[string]any{"service_access_id": access.ID, "access_way": "B"}).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Model(&model.Device{}).Where("id=?", deviceID).Updates(map[string]any{"service_access_id": nil, "access_way": nil})
		for _, ed := range created {
			prepared, target, err := prepareExpectedDownlink(context.Background(), ed)
			if err != nil || target != deviceID || prepared.Message.TopicPrefix != "service/alarm/" || prepared.Message.DeviceType != "1" {
				t.Fatalf("prepare plugin %s: target=%s prefix=%s err=%v", ed.SendType, target, prepared.Message.TopicPrefix, err)
			}
			if prepared.Message.Type == downlink.MessageTypeCommand && !strings.Contains(string(prepared.Encoded), `"method":"set_power"`) {
				t.Fatalf("plugin command lost SDK method: %s", prepared.Encoded)
			}
			row := brokercommands.Row{TenantID: tenant, DeviceID: deviceID, TargetDeviceID: deviceID, Intent: "expected." + ed.SendType}
			if err := validateExpectedDownlinkSnapshot(context.Background(), db, row, prepared); err != nil {
				t.Fatalf("frozen plugin %s snapshot invalid: %v", ed.SendType, err)
			}
			if err := db.Model(&model.ServicePlugin{}).Where("id=?", plugin.ID).Update("service_config", `{"sub_topic_prefix":"service/changed/"}`).Error; err != nil {
				t.Fatal(err)
			}
			if err := validateExpectedDownlinkSnapshot(context.Background(), db, row, prepared); !errors.Is(err, brokercommands.ErrFenced) {
				t.Fatalf("plugin prefix change did not fence %s: %v", ed.SendType, err)
			}
			if err := db.Model(&model.ServicePlugin{}).Where("id=?", plugin.ID).Update("service_config", config).Error; err != nil {
				t.Fatal(err)
			}
		}
	})

	expiredAt := time.Now().UTC().Add(-time.Minute)
	expired := &model.ExpectedData{ID: uuid.New(), DeviceID: deviceID, SendType: "telemetry", Payload: `{"old":true}`, CreatedAt: time.Now().UTC(), Status: "pending", ExpiryTime: &expiredAt, TenantID: tenant}
	if err := db.Create(expired).Error; err != nil {
		t.Fatal("seed expired pending message")
	}
	owner := expectedContext{ExpectedID: expired.ID, TenantID: tenant, DeviceID: deviceID, CallerID: caller, SendType: expired.SendType, PayloadSHA256: expectedPayloadHash(expired.Payload)}
	if err := db.Table("broker_expected_context").Create(&owner).Error; err != nil {
		t.Fatal("seed expired message context")
	}
	legacy := &model.ExpectedData{ID: uuid.New(), DeviceID: deviceID, SendType: "telemetry", Payload: `{"legacy":true}`, CreatedAt: time.Now().UTC(), Status: "pending", TenantID: tenant}
	if err := db.Create(legacy).Error; err != nil {
		t.Fatal("seed historical message without creator context")
	}
	if err := db.Exec(`DELETE FROM broker_connection_observations`).Error; err != nil {
		t.Fatal("clear broker online evidence")
	}
	if err := DispatchPendingExpected(context.Background(), "cell-a"); !errors.Is(err, errExpectedContextUnavailable) {
		t.Fatalf("historical message without creator context error=%v", err)
	}
	var expiredState model.ExpectedData
	if err := db.Where("id=?", expired.ID).Take(&expiredState).Error; err != nil || expiredState.Status != "expired" {
		t.Fatalf("expired message status=%q err=%v", expiredState.Status, err)
	}
	var legacyState model.ExpectedData
	if err := db.Where("id=?", legacy.ID).Take(&legacyState).Error; err != nil || legacyState.Status != "pending" {
		t.Fatalf("historical unsupported message status=%q err=%v; expected it to remain pending", legacyState.Status, err)
	}
}

func idsOfExpected(rows []*model.ExpectedData) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}
