package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"project/internal/downlink"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/global"
)

const commandValidationModel = `{"name":"Validation fixture","fields":[{"kind":"COMMAND","identifier":"set","nameI18n":{"default":"Set"},"access":{"write":true},"parameters":{"schemaVersion":1,"items":[{"identifier":"enabled","nameI18n":{"default":"Enabled"},"valueType":{"kind":"BOOL","constraint":{"trueText":"On","falseText":"Off"}}},{"identifier":"mode","nameI18n":{"default":"Mode"},"valueType":{"kind":"ENUM","baseKind":"STRING","enumValues":[{"code":"auto","label":{"default":"Auto"}},{"code":"manual","label":{"default":"Manual"}}]}},{"identifier":"level","nameI18n":{"default":"Level"},"valueType":{"kind":"NUMBER","constraint":{"min":0,"max":10,"step":0.5}}}]}}]}`

func TestNativeCommandValidationBeforeLogOrEnqueue(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:native-command-validation?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.Device{}, &model.DeviceConfig{}, &model.CommandSetLog{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	oldDevice, oldConfig, oldLog, oldProfile := query.Device, query.DeviceConfig, query.CommandSetLog, viper.Get("mqtt.input_profile")
	q := query.Use(db)
	query.Device, query.DeviceConfig, query.CommandSetLog = &q.Device, &q.DeviceConfig, &q.CommandSetLog
	viper.Set("mqtt.input_profile", "legacy")
	t.Cleanup(func() {
		query.Device, query.DeviceConfig, query.CommandSetLog = oldDevice, oldConfig, oldLog
		viper.Set("mqtt.input_profile", oldProfile)
	})
	config := nativeCommandDeviceConfig("model", "snapshot", 7)
	config.ID, config.TenantID, config.DeviceType = "config", "tenant-a", "1"
	if err := db.Create(config).Error; err != nil {
		t.Fatal(err)
	}
	device := model.Device{ID: "device", TenantID: "tenant-a", DeviceConfigID: &config.ID, DeviceNumber: "device-number", IsEnabled: "enabled", ActivateFlag: "active"}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/v2/thing-models/model/versions/7" || r.Header.Get("Authorization") != "Bearer session" {
			t.Errorf("not pinned/delegated snapshot request")
		}
		writeNativeCommandSnapshot(t, w, "model", 7, "snapshot", json.RawMessage(commandValidationModel))
	}))
	defer server.Close()
	t.Setenv("TP_THINGMODEL_NATIVE_URL", server.URL)
	bus := downlink.NewBus(1)
	service := &CommandData{downlinkBus: bus}
	for _, tc := range []struct{ name, method, value string }{
		{"string bool", "set", `{"enabled":"true","mode":"auto","level":1}`},
		{"numeric bool", "set", `{"enabled":1,"mode":"auto","level":1}`},
		{"unknown enum", "set", `{"enabled":true,"mode":"unknown","level":1}`},
		{"string number", "set", `{"enabled":true,"mode":"auto","level":"1"}`},
		{"number above max", "set", `{"enabled":true,"mode":"auto","level":11}`},
		{"number below min", "set", `{"enabled":true,"mode":"auto","level":-1}`},
		{"number off step", "set", `{"enabled":true,"mode":"auto","level":1.1}`},
		{"unknown key", "set", `{"enabled":true,"mode":"auto","level":1,"extra":1}`},
		{"missing key", "set", `{"enabled":true,"mode":"auto"}`},
		{"duplicate key", "set", `{"enabled":true,"mode":"auto","level":1,"level":2}`},
		{"unknown command", "missing", `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, err := service.CommandPutMessageWithResult(context.Background(), "user", &model.PutMessageForCommand{DeviceID: device.ID, Identify: tc.method, Value: &tc.value}, "1", device.TenantID, "session")
			var businessErr *errcode.Error
			if !errors.As(err, &businessErr) || id != "" || !strings.Contains(businessErr.CustomMsg, "invalid native command") {
				t.Fatalf("not rejected before enqueue: id=%q err=%v", id, err)
			}
			select {
			case <-bus.SubscribeCommand():
				t.Fatal("invalid command entered bus")
			default:
			}
			var n int64
			if err := db.Model(&model.CommandSetLog{}).Count(&n).Error; err != nil || n != 0 {
				t.Fatalf("invalid command persisted: %d %v", n, err)
			}
		})
	}
	oldRedis := global.REDIS
	cache := redis.NewClient(&redis.Options{MaxRetries: -1, Dialer: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("test cache unavailable")
	}})
	global.REDIS = cache
	t.Cleanup(func() { global.REDIS = oldRedis; _ = cache.Close() })
	for _, value := range []string{`{"enabled":true,"mode":"auto","level":0}`, `{"enabled":false,"mode":"manual","level":10}`, `{"enabled":false,"mode":"auto","level":0.5}`} {
		id, err := service.CommandPutMessageWithResult(context.Background(), "user", &model.PutMessageForCommand{DeviceID: device.ID, Identify: "set", Value: &value}, "1", device.TenantID, "session")
		if err != nil || id == "" {
			t.Fatalf("valid parameters failed: %v", err)
		}
		select {
		case msg := <-bus.SubscribeCommand():
			if msg.MessageID != id || msg.DeviceID != device.ID {
				t.Fatal("wrong accepted command")
			}
			var wire struct {
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if json.Unmarshal(msg.Data, &wire) != nil || wire.Method != "set" {
				t.Fatalf("wire=%s", msg.Data)
			}
			var count int64
			if err := db.Model(&model.CommandSetLog{}).Where("message_id=?", id).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("accepted log count=%d err=%v", count, err)
			}
		default:
			t.Fatal("valid command was not queued")
		}
	}
	before := requests
	value := `{"enabled":true,"mode":"auto","level":1}`
	_, err = service.CommandPutMessageWithResult(context.Background(), "other", &model.PutMessageForCommand{DeviceID: device.ID, Identify: "set", Value: &value}, "1", "tenant-b", "session")
	if err == nil || requests != before {
		t.Fatal("foreign tenant reached snapshot service")
	}
	_, err = service.CommandPutMessageWithResult(context.Background(), "user", &model.PutMessageForCommand{DeviceID: device.ID, Identify: "set", Value: &value}, "1", device.TenantID)
	if err == nil || requests != before {
		t.Fatal("missing delegated session reached snapshot service")
	}
	other := "other-snapshot"
	config.NativeThingModelSnapshotID = &other
	if validateNativeDeviceCommand(context.Background(), "session", config, "set", &value) == nil {
		t.Fatal("wrong pinned snapshot accepted")
	}
}
