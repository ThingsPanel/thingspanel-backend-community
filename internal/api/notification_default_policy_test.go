package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"project/internal/service"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

func TestTenantDefaultPolicyHTTPStrictTenantScopedCAS(t *testing.T) {
	openNotificationGroupSourceFixture(t)
	bridge, err := service.NewSourceBridge(service.SourceBridgeConfig{Enabled: true, BaseURL: "https://core-fixture.invalid",
		DeploymentID: "deployment-default-http", SourceBearerToken: "fixture-source-token", ProjectionBearerToken: "fixture-projection-token"}, groupGuardChecker{})
	if err != nil {
		t.Fatal("construct fixture bridge")
	}
	defer bridge.Close()
	service.SetActiveSourceBridge(bridge)
	t.Cleanup(func() { service.SetActiveSourceBridge(nil) })

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		if c.GetHeader("x-token") != "verified-fixture-session" {
			c.Next()
			return
		}
		c.Set("claims", &utils.UserClaims{ID: "fixture-user", TenantID: c.GetHeader("X-Fixture-Tenant"), Authority: c.GetHeader("X-Fixture-Authority")})
		c.Next()
	})
	engine.GET("/api/v1/notification-default-policy", (&NotificationGroupApi{}).GetTenantDefaultPolicy)
	engine.PUT("/api/v1/notification-default-policy", (&NotificationGroupApi{}).PutTenantDefaultPolicy)
	request := func(method, body, authority string, authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/notification-default-policy", strings.NewReader(body))
		req.Header.Set("X-Fixture-Tenant", "tenant-default-http")
		req.Header.Set("X-Fixture-Authority", authority)
		if authenticated {
			req.Header.Set("x-token", "verified-fixture-session")
		}
		if method == http.MethodPut {
			req.Header.Set("Content-Type", "application/json")
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, req)
		return recorder
	}
	if got := request(http.MethodGet, "", "TENANT_ADMIN", false); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET status=%d", got.Code)
	}
	if got := request(http.MethodGet, "", "TENANT_USER", true); got.Code != http.StatusForbidden {
		t.Fatalf("tenant user GET status=%d", got.Code)
	}
	if got := request(http.MethodGet, "", "UNKNOWN_ROLE", true); got.Code != http.StatusForbidden {
		t.Fatalf("unknown role GET status=%d", got.Code)
	}
	get := request(http.MethodGet, "", "TENANT_ADMIN", true)
	if get.Code != http.StatusOK {
		t.Fatalf("tenant admin GET status=%d body=%s", get.Code, get.Body.String())
	}
	var getEnvelope struct {
		Code int                             `json:"code"`
		Data service.TenantDefaultPolicyView `json:"data"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &getEnvelope); err != nil || getEnvelope.Code != 200 || getEnvelope.Data.Version != 0 || getEnvelope.Data.Selected != nil || len(getEnvelope.Data.AvailablePolicies) != 0 {
		t.Fatalf("unexpected initial default response: %+v err=%v", getEnvelope, err)
	}
	for _, body := range []string{
		`{"nativeGroupId":"","expectedVersion":0,"tenantId":"other"}`,
		`{"nativeGroupId":"","nativeGroupId":"other","expectedVersion":0}`,
		`{"nativeGroupId":null,"expectedVersion":0}`,
		`{"nativeGroupId":"","expectedVersion":null}`,
	} {
		if got := request(http.MethodPut, body, "TENANT_ADMIN", true); got.Code != http.StatusBadRequest {
			t.Fatalf("invalid body accepted: status=%d body=%s", got.Code, body)
		}
	}
	cleared := request(http.MethodPut, `{"nativeGroupId":"","expectedVersion":0}`, "TENANT_ADMIN", true)
	if cleared.Code != http.StatusOK {
		t.Fatalf("empty policy clear failed: status=%d body=%s", cleared.Code, cleared.Body.String())
	}
	var putEnvelope struct {
		Data service.TenantDefaultPolicyView `json:"data"`
	}
	if err := json.Unmarshal(cleared.Body.Bytes(), &putEnvelope); err != nil || putEnvelope.Data.Version != 1 || putEnvelope.Data.Selected != nil {
		t.Fatalf("clear response did not advance CAS version: %+v err=%v", putEnvelope, err)
	}
	if unavailable := request(http.MethodPut, `{"nativeGroupId":"missing-native-group","expectedVersion":1}`, "TENANT_ADMIN", true); unavailable.Code != http.StatusNotFound {
		t.Fatalf("unavailable target should be masked as 404: status=%d", unavailable.Code)
	}
	if stale := request(http.MethodPut, `{"nativeGroupId":"","expectedVersion":0}`, "TENANT_ADMIN", true); stale.Code != http.StatusConflict {
		t.Fatalf("stale version should conflict: status=%d body=%s", stale.Code, stale.Body.String())
	}
}
