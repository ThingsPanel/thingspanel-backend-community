package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/gin-gonic/gin"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"
)

func TestServiceAccessOperationQueryUsesCurrentCreationPermission(t *testing.T) {
	previous := global.CasbinEnforcer
	t.Cleanup(func() { global.CasbinEnforcer = previous })
	gin.SetMode(gin.TestMode)
	const resource = "api/v1/service/access"
	const function = "fixture-service-access-function"
	const role = "fixture-service-access-role"
	const subject = "fixture-service-access-subject"
	newEnforcer := func() *casbin.Enforcer {
		t.Helper()
		e, err := casbin.NewEnforcer("../../configs/casbin.conf")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.AddNamedGroupingPolicy("g2", resource, function); err != nil {
			t.Fatal(err)
		}
		if _, err := e.AddPolicy(role, function, "allow"); err != nil {
			t.Fatal(err)
		}
		if _, err := e.AddNamedGroupingPolicy("g", subject, role); err != nil {
			t.Fatal(err)
		}
		global.CasbinEnforcer = e
		return e
	}
	request := func(claims interface{}, path string, want int) {
		t.Helper()
		called := false
		router := gin.New()
		router.Use(func(c *gin.Context) {
			if claims != nil {
				c.Set("claims", claims)
			}
			c.Next()
		}, CasbinRBAC())
		router.GET("/api/v1/service/access/operations/:key", func(c *gin.Context) { called = true; c.Status(http.StatusNoContent) })
		router.GET("/api/v1/unregistered-legacy-probe", func(c *gin.Context) { called = true; c.Status(http.StatusNoContent) })
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != want || called != (want == http.StatusNoContent) {
			t.Fatalf("status=%d handler=%v want=%d", response.Code, called, want)
		}
		if want == http.StatusBadRequest {
			var body struct {
				Code int `json:"code"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Code != errcode.CodeNoPermission {
				t.Fatalf("denial did not use existing permission code: code=%d err=%v", body.Code, err)
			}
		}
	}
	claims := &utils.UserClaims{ID: subject, TenantID: "tenant"}
	t.Run("all_original_keys_share_the_registered_creation_permission", func(t *testing.T) {
		newEnforcer()
		for _, key := range []string{"original-key", "second-key", "step:abc.123"} {
			request(claims, "/api/v1/service/access/operations/"+key, http.StatusNoContent)
		}
		request(&utils.UserClaims{ID: "unprivileged", TenantID: "tenant"}, "/api/v1/service/access/operations/original-key", http.StatusBadRequest)
	})
	t.Run("removed_role_and_permission_take_effect_on_next_query", func(t *testing.T) {
		e := newEnforcer()
		request(claims, "/api/v1/service/access/operations/original-key", http.StatusNoContent)
		if _, err := e.RemoveNamedGroupingPolicy("g", subject, role); err != nil {
			t.Fatal(err)
		}
		request(claims, "/api/v1/service/access/operations/original-key", http.StatusBadRequest)
		if _, err := e.AddNamedGroupingPolicy("g", subject, role); err != nil {
			t.Fatal(err)
		}
		if _, err := e.RemovePolicy(role, function, "allow"); err != nil {
			t.Fatal(err)
		}
		request(claims, "/api/v1/service/access/operations/another-key", http.StatusBadRequest)
	})
	t.Run("missing_canonical_resource_does_not_use_the_key_url_or_superuser_bypass", func(t *testing.T) {
		e := newEnforcer()
		if _, err := e.RemoveNamedGroupingPolicy("g2", resource, function); err != nil {
			t.Fatal(err)
		}
		if _, err := e.AddNamedGroupingPolicy("g2", "api/v1/service/access/operations/original-key", function); err != nil {
			t.Fatal(err)
		}
		request(claims, "/api/v1/service/access/operations/original-key", http.StatusBadRequest)
		request(&utils.UserClaims{ID: "admin@thingspanel.cn", TenantID: "tenant"}, "/api/v1/service/access/operations/original-key", http.StatusBadRequest)
	})
	t.Run("missing_or_invalid_identity_and_unavailable_enforcer_fail_closed", func(t *testing.T) {
		newEnforcer()
		for _, invalid := range []interface{}{nil, "wrong-type", (*utils.UserClaims)(nil), &utils.UserClaims{ID: subject}, &utils.UserClaims{ID: " ", TenantID: "tenant"}} {
			request(invalid, "/api/v1/service/access/operations/original-key", http.StatusBadRequest)
		}
		global.CasbinEnforcer = nil
		request(claims, "/api/v1/service/access/operations/original-key", http.StatusBadRequest)
	})
	t.Run("unrelated_legacy_unregistered_route_is_unchanged", func(t *testing.T) {
		newEnforcer()
		request(claims, "/api/v1/unregistered-legacy-probe", http.StatusNoContent)
	})
}
