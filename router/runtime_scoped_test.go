package router

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"project/pkg/utils"
)

func TestRuntimeDeviceIdentityRejectsConfusion(t *testing.T) {
	cases := []struct {
		name, source, url, body string
		valid                   bool
	}{
		{"path", "param:id", "/?", "", true},
		{"path query override", "param:id", "/?device_id=foreign", "", false},
		{"query", "query:device_id", "/?device_id=local", "", true},
		{"duplicate query", "query:device_id", "/?device_id=local&device_id=foreign", "", false},
		{"malformed query", "query:device_id", "/?device_id=%zz", "", false},
		{"body", "body:device_id", "/", `{"device_id":"local","value":{}}`, true},
		{"duplicate JSON", "body:device_id", "/", `{"device_id":"local","device_id":"foreign"}`, false},
		{"case alias", "body:device_id", "/", `{"device_id":"local","DEVICE_ID":"foreign"}`, false},
		{"wrong field", "body:device_id", "/", `{"id":"local"}`, false},
		{"multiple IDs", "body:device_id", "/", `{"device_id":"local","id":"foreign"}`, false},
		{"number ID", "body:device_id", "/", `{"device_id":1}`, false},
		{"null ID", "body:device_id", "/", `{"device_id":null}`, false},
		{"parent update", "body:id", "/", `{"id":"local","parent_id":"parent"}`, true},
		{"parent case alias", "body:id", "/", `{"id":"local","PARENT_ID":"parent"}`, false},
		{"trailing JSON", "body:device_id", "/", `{"device_id":"local"} {}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", tc.url, strings.NewReader(tc.body))
			c.Params = gin.Params{{Key: "id", Value: "local"}}
			_, err := runtimeDeviceID(c, tc.source)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if tc.valid && strings.HasPrefix(tc.source, "body:") {
				body, _ := io.ReadAll(c.Request.Body)
				if string(body) != tc.body {
					t.Fatal("body was not restored for handler")
				}
			}
		})
	}
}

func TestRuntimeScopeNeverAllowsSysAdminTenantBypass(t *testing.T) {
	for _, claims := range []*utils.UserClaims{nil, {TenantID: "other", Authority: "SYS_ADMIN"}, {TenantID: "", Authority: "SYS_ADMIN"}, {TenantID: "other", Authority: "TENANT_ADMIN"}} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/v1/runtime-scoped/local/device/detail/d", nil)
		c.Params = gin.Params{{Key: "tenantID", Value: "local"}, {Key: "id", Value: "d"}}
		c.Set("claims", claims)
		called := false
		runtimeScope("param:id", false, func(*gin.Context) { called = true })(c)
		if w.Code != 403 || called {
			t.Fatalf("scope bypass: status=%d called=%v", w.Code, called)
		}
	}
}

func TestRuntimeSimulationFailsClosedBeforeExternalEffects(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/runtime-scoped/local/telemetry/datas/simulation/send", strings.NewReader(`{"device_id":"d","server":"untrusted"}`))
	c.Params = gin.Params{{Key: "tenantID", Value: "local"}}
	c.Set("claims", &utils.UserClaims{TenantID: "local", Authority: "TENANT_ADMIN"})
	runtimeScope("unsupported", false, nil)(c)
	if w.Code != 503 {
		t.Fatalf("unsafe simulation writer accepted: %d", w.Code)
	}
}

func TestRuntimeScopeRejectsSameTenantDeviceUsersForReadsAndWrites(t *testing.T) {
	for _, authority := range []string{"TENANT_USER", "SYS_ADMIN", "", "tenant_admin"} {
		for _, source := range []string{"param:id", "owner:body:id", "body:device_id"} {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/api/v1/runtime-scoped/local/device", strings.NewReader(`{"id":"d","device_id":"d"}`))
			c.Params = gin.Params{{Key: "tenantID", Value: "local"}, {Key: "id", Value: "d"}}
			c.Set("claims", &utils.UserClaims{ID: "same-tenant-user", TenantID: "local", Authority: authority})
			called := false
			runtimeScope(source, false, func(*gin.Context) { called = true })(c)
			if w.Code != 403 || called {
				t.Fatalf("ordinary role accepted: authority=%q source=%s code=%d", authority, source, w.Code)
			}
		}
	}
}
