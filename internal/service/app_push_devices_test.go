package service

import (
	"context"
	"project/initialize"
	"project/internal/dal"
	"project/internal/model"
	"project/pkg/global"
	"sync"
	"testing"
	"time"
)

func TestAppDeviceLifecycle(t *testing.T) {
	openSourceCompatPGFixture(t)
	previous := global.REDIS
	global.REDIS = startLegacyNotificationRedis(t)
	t.Cleanup(func() { global.REDIS.Close(); global.REDIS = previous })
	if err := initialize.ApplyAppPushSchema(global.DB); err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Exec(`CREATE TABLE users(id text PRIMARY KEY,tenant_id text,status text,auth_version bigint NOT NULL DEFAULT 0); INSERT INTO users(id,tenant_id,status) VALUES ('u1','t1','N'),('u2','t1','N'),('u3','t2','N')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Exec(`ALTER TABLE app_push_devices ADD COLUMN auth_version bigint NOT NULL DEFAULT 0`).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	bind := func(user, tenant, cid, install, token string) dal.AppPushDevice {
		t.Helper()
		global.REDIS.Set(ctx, token, "1", time.Hour)
		d, err := dal.BindAppPushDevice(ctx, dal.AppPushDevice{UserID: user, TenantID: tenant, CID: cid, InstallationID: install, AppID: "app", Platform: "android", ExpiresAt: time.Now().Add(time.Hour)}, token)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	read := func(tenant, user, id string) []dal.AppPushDevice {
		t.Helper()
		rows, err := dal.ReadAppPushDevices(ctx, tenant, user, id)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	a := bind("u1", "t1", "cid1", "phone1", "s1")
	if b := bind("u1", "t1", "cid1", "phone1", "s1"); a.ID != b.ID {
		t.Fatal("repeat bind changed identity")
	}
	b := bind("u1", "t1", "cid2", "phone2", "s2")
	if len(read("t1", "u1", "")) != 2 {
		t.Fatal("same account lost one phone")
	}
	if len(read("t2", "", a.ID)) != 0 {
		t.Fatal("cross tenant read")
	}
	c := bind("u2", "t1", "cid1", "phone1", "s3")
	if len(read("t1", "", a.ID)) != 0 || len(read("t1", "u1", "")) != 1 || len(read("t1", "u2", "")) != 1 {
		t.Fatal("account switch kept old recipient")
	}
	if err := dal.UnbindAppPushDevice(ctx, "u1", "cid1", "s1"); err != nil {
		t.Fatal(err)
	}
	if len(read("t1", "", c.ID)) != 1 {
		t.Fatal("old account logout revoked new account")
	}
	if err := (&User{}).Logout("s2"); err != nil {
		t.Fatal(err)
	}
	if len(read("t1", "", b.ID)) != 0 {
		t.Fatal("logged out session can receive")
	}
	if _, err := dal.BindAppPushDevice(ctx, b, "s2"); err == nil {
		t.Fatal("dead session revived")
	}
	global.REDIS.Del(ctx, dal.AppPushSessionKey("s3"))
	if len(read("t1", "", c.ID)) != 0 {
		t.Fatal("expired session can receive")
	}
	d := bind("u2", "t1", "cid-new", "phone1", "s4")
	global.DB.Exec("UPDATE users SET status = 'D' WHERE id = 'u2'")
	if len(read("t1", "", d.ID)) != 0 {
		t.Fatal("disabled member can receive")
	}
	global.DB.Exec("UPDATE users SET status = 'N' WHERE id = 'u2'")
	global.DB.Exec("UPDATE users SET auth_version=1 WHERE id='u2'")
	if len(read("t1", "", d.ID)) != 0 {
		t.Fatal("revoked auth version can receive notifications")
	}
	global.DB.Exec("UPDATE users SET auth_version=0 WHERE id='u2'")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := dal.BindAppPushDevice(ctx, d, "s4")
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(read("t1", "u2", "")) != 1 {
		t.Fatal("concurrent bind duplicated device")
	}
}

func TestAppAlarmDetailTenantScope(t *testing.T) {
	openSourceCompatPGFixture(t)
	if err := global.DB.AutoMigrate(&model.AlarmInfo{}, &model.AlarmHistory{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	level := "M"
	if err := global.DB.Create(&model.AlarmInfo{ID: "info-id", AlarmConfigID: "rule", Name: "fixture", AlarmTime: now, TenantID: "tenant-a", ProcessingResult: "UND", AlarmLevel: &level}).Error; err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Create(&model.AlarmHistory{ID: "history-id", AlarmConfigID: "rule", Name: "fixture", CreateAt: now, TenantID: "tenant-a", AlarmStatus: "M", AlarmDeviceList: "[]"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"info", "history"} {
		read := dal.GetAlarmInfoHistoryByID
		if kind == "info" {
			read = dal.GetAlarmNotificationInfoByID
		}
		row, err := read(kind+"-id", "tenant-a")
		if err != nil || row["id"] != kind+"-id" {
			t.Fatal(row, err)
		}
		row, err = read(kind+"-id", "tenant-b")
		if err != nil || len(row) != 0 {
			t.Fatal("cross tenant alarm detail", row, err)
		}
	}
}
