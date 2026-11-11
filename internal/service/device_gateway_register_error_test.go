package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
)

func TestGatewayDeviceRegisterParentLookupFailures(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open isolated fixture")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("open isolated pool")
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&model.Device{}); err != nil {
		t.Fatal("create isolated device table")
	}
	oldQ, oldDevice := *query.Q, query.Device
	*query.Q = *query.Use(db)
	query.Device = &query.Q.Device
	t.Cleanup(func() { *query.Q = oldQ; query.Device = oldDevice })
	const sensitiveID = "fixture-sensitive-device-id"
	req := model.DeviceRegisterReq{DeviceId: sensitiveID}
	got, err := (&Device{}).GatewayDeviceRegister(req)
	if err != nil || got.Type != "sub-register-response" || got.Status != "fail" || got.Message != "未查询到网关设备信息" {
		t.Fatalf("missing parent result=%+v err=%v", got, err)
	}
	if err := db.Migrator().DropTable(&model.Device{}); err != nil {
		t.Fatal("drop isolated device table")
	}
	got, err = (&Device{}).GatewayDeviceRegister(req)
	var code *errcode.Error
	if !errors.As(err, &code) || code.Code != errcode.CodeDBError || code.Data != nil || got.Status == "success" {
		t.Fatalf("database failure result=%+v err=%v", got, err)
	}
	body, marshalErr := json.Marshal(code)
	if marshalErr != nil || strings.Contains(string(body), sensitiveID) || strings.Contains(strings.ToLower(string(body)), "sql") || strings.Contains(err.Error(), sensitiveID) {
		t.Fatal("database failure exposed internal details")
	}
}
