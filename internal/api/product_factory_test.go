package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

func TestFactoryProxyPreservesConflictStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"detail":"factory_batch_conflict"}`))
	}))
	defer upstream.Close()
	t.Setenv("YOMI_FACTORY_API_BASE_URL", upstream.URL)
	t.Setenv("YOMI_INTERNAL_TOKEN", "test-token")
	router := gin.New()
	router.POST("/batch", func(c *gin.Context) {
		c.Set("claims", &utils.UserClaims{Authority: "SYS_ADMIN"})
		proxyFactory(c, http.MethodPost, "", map[string]interface{}{"batchId": "batch-1"})
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/batch", nil))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestFactoryAdminAllowsProductUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		role string
		want bool
	}{
		{"SYS_ADMIN", true},
		{"TENANT_ADMIN", true},
		{"TENANT_USER", true},
		{"", true},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("claims", &utils.UserClaims{Authority: tc.role, TenantID: "tenant-1"})
		if got := factoryAdmin(c); got != tc.want {
			t.Errorf("role %s: allowed=%v, want %v", tc.role, got, tc.want)
		}
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("claims", &utils.UserClaims{Authority: "TENANT_USER"})
	if factoryAdmin(c) {
		t.Fatal("user without tenant must be denied")
	}
}
