package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/utils"
)

func TestServiceAccessTenantMutationsPostgres(t *testing.T) {
	db := brokerEvidenceTestDB(t)
	if err := db.AutoMigrate(&model.ServiceAccess{}, &model.ServicePlugin{}); err != nil {
		t.Fatal(err)
	}
	q := query.Use(db)
	oldAccess, oldDevice, oldPlugin := query.ServiceAccess, query.Device, query.ServicePlugin
	query.ServiceAccess, query.Device, query.ServicePlugin = &q.ServiceAccess, &q.Device, &q.ServicePlugin
	t.Cleanup(func() { query.ServiceAccess, query.Device, query.ServicePlugin = oldAccess, oldDevice, oldPlugin })
	var notifications atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { notifications.Add(1); w.WriteHeader(http.StatusOK) }))
	t.Cleanup(server.Close)
	config := fmt.Sprintf(`{"http_address":%q}`, strings.TrimPrefix(server.URL, "http://"))
	plugin := model.ServicePlugin{ID: "plugin-b", Name: "fixture", ServiceIdentifier: "fixture", ServiceType: 2, ServiceConfig: &config}
	if err := db.Create(&plugin).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rows := []model.ServiceAccess{
		{ID: "access-a", TenantID: "tenant-a", Name: "A", ServicePluginID: "plugin-a", Voucher: "", CreateAt: now, UpdateAt: now},
		{ID: "access-b", TenantID: "tenant-b", Name: "B", ServicePluginID: "plugin-b", Voucher: "fixture", CreateAt: now, UpdateAt: now},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	claimsA := &utils.UserClaims{ID: "user-a", TenantID: "tenant-a"}
	claimsB := &utils.UserClaims{ID: "user-b", TenantID: "tenant-b"}
	newName := "changed"
	assertCode := func(err error, code int) {
		t.Helper()
		var e *errcode.Error
		if !errors.As(err, &e) || e.Code != code {
			t.Fatalf("error=%v, want code=%d", err, code)
		}
	}
	assertCode(GroupApp.ServiceAccess.Update(&model.UpdateAccessReq{ID: "access-b", Name: &newName}, claimsA), errcode.CodeNotFound)
	assertCode(GroupApp.ServiceAccess.Update(&model.UpdateAccessReq{ID: "missing", Name: &newName}, claimsA), errcode.CodeNotFound)
	assertCode(GroupApp.ServiceAccess.Delete("access-b", claimsA), errcode.CodeNotFound)
	assertCode(GroupApp.ServiceAccess.Delete("missing", claimsA), errcode.CodeNotFound)
	if got := notifications.Load(); got != 0 {
		t.Fatalf("cross-tenant request triggered %d plugin notifications", got)
	}
	var b model.ServiceAccess
	if err := db.Where("id=?", "access-b").Take(&b).Error; err != nil || b.Name != "B" || b.Voucher != "fixture" {
		t.Fatalf("tenant B changed: %+v err=%v", b, err)
	}
	if err := GroupApp.ServiceAccess.Update(&model.UpdateAccessReq{ID: "access-b", Name: &newName}, claimsB); err != nil {
		t.Fatalf("own update with plugin: %v", err)
	}
	if got := notifications.Load(); got != 1 {
		t.Fatalf("authorized update notifications=%d", got)
	}
	if err := GroupApp.ServiceAccess.Update(&model.UpdateAccessReq{ID: "access-a", Name: &newName}, claimsA); err != nil {
		t.Fatalf("own update: %v", err)
	}
	var a model.ServiceAccess
	if err := db.Where("id=?", "access-a").Take(&a).Error; err != nil || a.Name != newName {
		t.Fatalf("own update missing: %+v err=%v", a, err)
	}
	if err := GroupApp.ServiceAccess.Delete("access-a", claimsA); err != nil {
		t.Fatalf("own delete: %v", err)
	}
	assertCode(GroupApp.ServiceAccess.Delete("access-a", claimsA), errcode.CodeNotFound)
	if err := db.Model(&model.Device{}).Where("id=?", "device-a").Update("service_access_id", "access-b").Error; err != nil {
		t.Fatal(err)
	}
	assertCode(GroupApp.ServiceAccess.Delete("access-b", claimsA), errcode.CodeNotFound)
	assertCode(GroupApp.ServiceAccess.Delete("access-b", claimsB), 200064)
	if err := db.Where("id=?", "access-b").Take(&b).Error; err != nil || b.Name != newName {
		t.Fatalf("bound B missing: %+v err=%v", b, err)
	}
}
