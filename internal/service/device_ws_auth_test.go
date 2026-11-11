package service

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/utils"
)

func TestAuthorizeWebSocketDeviceReadScopeAndBatch(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	if err := db.AutoMigrate(&model.Device{}); err != nil {
		t.Fatal(err)
	}
	previousQuery := *query.Q
	previousDevice := query.Device
	*query.Q = *query.Use(db)
	query.Device = &query.Q.Device
	t.Cleanup(func() {
		*query.Q = previousQuery
		query.Device = previousDevice
	})
	for _, d := range []model.Device{{ID: "device-a", TenantID: "tenant-a"}, {ID: "device-b", TenantID: "tenant-b"}} {
		if err := db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	auth := GroupApp.Device
	assertDecision := func(name string, claims *utils.UserClaims, ids []string, wantErr bool) {
		t.Helper()
		err := auth.AuthorizeWebSocketDeviceRead(claims, ids)
		if (err != nil) != wantErr {
			t.Errorf("%s: err=%v, wantErr=%v", name, err, wantErr)
		}
	}
	assertDecision("tenant owner", &utils.UserClaims{ID: "user-a", TenantID: "tenant-a", Authority: "TENANT_ADMIN"}, []string{"device-a"}, false)
	assertDecision("cross tenant", &utils.UserClaims{ID: "user-a", TenantID: "tenant-a", Authority: "TENANT_USER"}, []string{"device-b"}, true)
	assertDecision("missing device", &utils.UserClaims{ID: "user-a", TenantID: "tenant-a"}, []string{"absent"}, true)
	assertDecision("mixed batch rejects all", &utils.UserClaims{ID: "user-a", TenantID: "tenant-a"}, []string{"device-a", "device-b"}, true)
	assertDecision("mixed valid and empty ID rejects all", &utils.UserClaims{ID: "user-a", TenantID: "tenant-a"}, []string{"device-a", ""}, true)
	assertDecision("missing claims", nil, []string{"device-a"}, true)
	assertDecision("missing subject", &utils.UserClaims{TenantID: "tenant-a"}, []string{"device-a"}, true)
	assertDecision("missing tenant", &utils.UserClaims{ID: "user-a"}, []string{"device-a"}, true)
	assertDecision("SYS_ADMIN empty tenant keeps cross-tenant semantics", &utils.UserClaims{ID: "root", Authority: "SYS_ADMIN"}, []string{"device-b"}, false)
	assertDecision("SYS_ADMIN still requires subject", &utils.UserClaims{Authority: "SYS_ADMIN"}, []string{"device-b"}, true)
	assertDecision("API key tenant-admin synthetic claims scoped", &utils.UserClaims{ID: "key-owner", TenantID: "tenant-a", Authority: "TENANT_ADMIN"}, []string{"device-a"}, false)
	assertDecision("API key empty tenant fails closed", &utils.UserClaims{ID: "key-owner", Authority: "TENANT_ADMIN"}, []string{"device-a"}, true)
}
