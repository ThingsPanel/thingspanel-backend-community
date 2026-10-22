package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"project/pkg/global"
	"project/pkg/utils"

	"github.com/casbin/casbin/v2"
	"github.com/gin-gonic/gin"
)

func TestDefaultPolicyRouteUsesSignedAuthorityCasbinSubject(t *testing.T) {
	previous := global.CasbinEnforcer
	enforcer, err := casbin.NewEnforcer("../../configs/casbin.conf")
	if err != nil {
		t.Fatal("load Casbin model")
	}
	global.CasbinEnforcer = enforcer
	t.Cleanup(func() { global.CasbinEnforcer = previous })
	const path = "api/v1/notification-default-policy"
	if _, err := enforcer.AddNamedGroupingPolicy("g2", path, path); err != nil {
		t.Fatal("register policy resource")
	}
	for _, role := range []string{"TENANT_ADMIN", "SYS_ADMIN"} {
		if _, err := enforcer.AddPolicy(role, path, "allow"); err != nil {
			t.Fatal("register policy role")
		}
	}

	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		role string
		want int
	}{{"TENANT_ADMIN", http.StatusNoContent}, {"SYS_ADMIN", http.StatusNoContent}, {"TENANT_USER", http.StatusBadRequest}, {"UNKNOWN_ROLE", http.StatusBadRequest}} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("claims", &utils.UserClaims{ID: "user-with-custom-role-id", Authority: test.role})
			c.Next()
		}, CasbinRBAC())
		router.GET("/api/v1/notification-default-policy", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/notification-default-policy", nil)
		router.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("role %s got HTTP %d, wanted %d", test.role, response.Code, test.want)
		}
	}
}
