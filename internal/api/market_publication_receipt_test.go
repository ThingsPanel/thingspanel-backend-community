package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/casbin/casbin/v2"
	"github.com/gin-gonic/gin"
)

func TestPublicationReceiptQueryUsesCurrentRegisteredPublishPermission(t *testing.T) {
	previous := global.CasbinEnforcer
	t.Cleanup(func() { global.CasbinEnforcer = previous })
	const resource = "api/v1/device/market/dashboard-bundles"
	const function = "fixture-publication-function"
	const role = "fixture-publication-role"
	const subject = "fixture-publication-subject"
	enforcer, err := casbin.NewEnforcer("../../configs/casbin.conf")
	if err != nil {
		t.Fatal(err)
	}
	global.CasbinEnforcer = enforcer
	if _, err = enforcer.AddNamedGroupingPolicy("g2", resource, function); err != nil {
		t.Fatal(err)
	}
	if _, err = enforcer.AddPolicy(role, function, "allow"); err != nil {
		t.Fatal(err)
	}
	if _, err = enforcer.AddNamedGroupingPolicy("g", subject, role); err != nil {
		t.Fatal(err)
	}
	request := func(claims interface{}, want int) {
		t.Helper()
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/device/market/dashboard-bundles/publication-receipts", nil)
		if claims != nil {
			c.Set("claims", claims)
		}
		(&DeviceApi{}).QueryDashboardPublicationReceipt(c)
		if c.Errors.Last() == nil {
			t.Fatal("unvalidated query reached the provider")
		}
		var typed *errcode.Error
		if !errors.As(c.Errors.Last().Err, &typed) || typed.Code != want {
			t.Fatalf("error=%v want code=%d", c.Errors.Last(), want)
		}
		if recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("personal receipt may be cached")
		}
		if _, exists := c.Get("data"); exists {
			t.Fatal("denied query returned data")
		}
	}
	claims := &utils.UserClaims{TenantID: "tenant-a", ID: subject}
	// Allowed identity reaches only input validation; missing key makes no HTTP request.
	request(claims, errcode.CodeParamError)
	for _, invalid := range []interface{}{nil, "wrong-type", (*utils.UserClaims)(nil), &utils.UserClaims{TenantID: "tenant-a"}, &utils.UserClaims{ID: subject}, &utils.UserClaims{TenantID: "tenant-a", ID: "unprivileged"}} {
		request(invalid, errcode.CodeNoPermission)
	}
	if _, err = enforcer.RemoveNamedGroupingPolicy("g", subject, role); err != nil {
		t.Fatal(err)
	}
	request(claims, errcode.CodeNoPermission)
	if _, err = enforcer.AddNamedGroupingPolicy("g", subject, role); err != nil {
		t.Fatal(err)
	}
	if _, err = enforcer.RemoveNamedGroupingPolicy("g2", resource, function); err != nil {
		t.Fatal(err)
	}
	request(claims, errcode.CodeNoPermission)
	request(&utils.UserClaims{TenantID: "tenant-a", ID: "admin@thingspanel.cn"}, errcode.CodeNoPermission)
	global.CasbinEnforcer = nil
	request(claims, errcode.CodeNoPermission)
}
