package service

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/middleware/response"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/global"
)

func setupConnectMessages(t *testing.T) {
	t.Helper()
	previous := global.ResponseHandler
	handler, err := response.NewHandler("../../configs/messages.yaml", "../../configs/messages_str.yaml")
	if err != nil {
		t.Fatal(err)
	}
	global.ResponseHandler = handler
	t.Cleanup(func() { global.ResponseHandler = previous })
}

func TestProtocolConnectionInfoDoesNotInventHost(t *testing.T) {
	setupConnectMessages(t)
	for _, address := range []string{":502", "0.0.0.0:502", "[::]:502"} {
		info := protocolConnectionInfo(address, "zh_CN")
		if info["接入端口"] != "502" || info["接入地址"] != "未配置" || info["操作提示"] == "" {
			t.Fatalf("listener %q must not be advertised as a complete endpoint: %#v", address, info)
		}
	}
	for _, address := range []string{"gateway.example.com:502", "[2001:db8::1]:502"} {
		if info := protocolConnectionInfo(address, "en_US"); !reflect.DeepEqual(info, map[string]string{"Access Address": address}) {
			t.Fatalf("complete endpoint changed: %#v", info)
		}
	}
	if info := protocolConnectionInfo("", "en_US"); info["Access Address"] != "Not configured" {
		t.Fatal(info)
	}
}

func TestChildDeviceConnectInfoDoesNotUseGatewayPluginAddress(t *testing.T) {
	setupConnectMessages(t)
	previousRedis := global.REDIS
	cache := redis.NewClient(&redis.Options{MaxRetries: -1, Dialer: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("test cache unavailable")
	}})
	global.REDIS = cache
	t.Cleanup(func() { global.REDIS = previousRedis; _ = cache.Close() })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&model.Device{}, &model.DeviceConfig{}); err != nil {
		t.Fatal(err)
	}
	previousDevice, previousConfig := query.Device, query.DeviceConfig
	q := query.Use(db)
	query.Device, query.DeviceConfig = &q.Device, &q.DeviceConfig
	t.Cleanup(func() { query.Device, query.DeviceConfig = previousDevice, previousConfig })
	protocol := "MODBUS_RTU"
	config := model.DeviceConfig{ID: "child-template", Name: "RTU child", TenantID: "tenant", DeviceType: "3", ProtocolType: &protocol}
	if err := db.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	device := model.Device{ID: "child-device", DeviceConfigID: &config.ID, TenantID: "tenant"}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	// No plugin table: child information must not depend on a gateway listener.
	for _, lang := range []string{"zh_CN", "en_US"} {
		result, err := GroupApp.Device.DeviceConnect(context.Background(), &model.DeviceConnectFormReq{DeviceID: device.ID}, lang)
		if err != nil {
			t.Fatal(err)
		}
		info := result.(map[string]string)
		message := global.ResponseHandler.ErrManager.GetMessage
		if info[message(500008, lang)] != message(500009, lang) || info[message(500010, lang)] != message(500011, lang) || len(info) != 2 {
			t.Fatalf("unexpected child connection information: %#v", info)
		}
	}
}
