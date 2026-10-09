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

func TestNativePublishStatusIsTenantClaimScopedAndStrict(t *testing.T) {
	openNotificationGroupSourceFixture(t)
	bridge, err := service.NewSourceBridge(service.SourceBridgeConfig{Enabled: true, BaseURL: "https://core-fixture.invalid",
		DeploymentID: "deployment-native-publish-http", SourceBearerToken: "fixture-source-token",
		ProjectionBearerToken: "fixture-projection-token"}, groupGuardChecker{})
	if err != nil {
		t.Fatal("construct enabled fixture bridge")
	}
	defer bridge.Close()
	service.SetActiveSourceBridge(bridge)
	t.Cleanup(func() { service.SetActiveSourceBridge(nil) })

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("claims", &utils.UserClaims{ID: "fixture-admin", TenantID: c.GetHeader("X-Fixture-Tenant"), Authority: c.GetHeader("X-Fixture-Authority")})
		c.Next()
	})
	engine.GET("/api/v1/notification_group/native-publish", (&NotificationGroupApi{}).GetNativePublishStatus)
	engine.POST("/api/v1/notification_group/native-publish", (&NotificationGroupApi{}).ApplyNativePublish)

	request := func(method, target, body, authority string, withToken bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("X-Fixture-Tenant", "tenant-native-http")
		req.Header.Set("X-Fixture-Authority", authority)
		if withToken {
			req.Header.Set("x-token", "verified-test-session")
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "native-http-key-0001")
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, req)
		return recorder
	}

	unauthenticated := request(http.MethodGet, "/api/v1/notification_group/native-publish?nativeGroupId=native-http-1", "", "TENANT_ADMIN", false)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("missing user JWT was accepted: status=%d", unauthenticated.Code)
	}
	user := request(http.MethodGet, "/api/v1/notification_group/native-publish?nativeGroupId=native-http-1", "", "TENANT_USER", true)
	if user.Code != http.StatusForbidden {
		t.Fatalf("non-admin tenant user was accepted: status=%d", user.Code)
	}
	unknownTenantQuery := request(http.MethodGet, "/api/v1/notification_group/native-publish?nativeGroupId=native-http-1&tenantId=other", "", "TENANT_ADMIN", true)
	if unknownTenantQuery.Code != http.StatusBadRequest {
		t.Fatalf("caller-selected tenant query was accepted: status=%d", unknownTenantQuery.Code)
	}

	response := request(http.MethodGet, "/api/v1/notification_group/native-publish?nativeGroupId=native-http-1", "", "TENANT_ADMIN", true)
	if response.Code != http.StatusOK {
		t.Fatalf("tenant status GET failed: status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Code int                         `json:"code"`
		Data service.NativePublishStatus `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal("decode native publish status")
	}
	if envelope.Code != 200 || envelope.Data.Published || envelope.Data.Status != "unpublished" || envelope.Data.RouteVersion != 0 || envelope.Data.EffectiveGroupRevision != 0 || envelope.Data.Engine != "legacy" || envelope.Data.Enabled {
		t.Fatalf("unexpected unpublished state: %+v", envelope)
	}

	unknownBody := request(http.MethodPost, "/api/v1/notification_group/native-publish",
		`{"operation":"unpublish","tenantId":"other","nativeGroupId":"native-http-1","expectedRouteVersion":0}`,
		"TENANT_ADMIN", true)
	if unknownBody.Code != http.StatusBadRequest {
		t.Fatalf("body-supplied tenant was accepted: status=%d body=%s", unknownBody.Code, unknownBody.Body.String())
	}

	for _, invalid := range []string{
		`{"operation":"unpublish","operation":"publish","nativeGroupId":"native-http-1","expectedRouteVersion":0}`,
		`{"operation":null,"nativeGroupId":"native-http-1","expectedRouteVersion":0,"groupRevision":1,"name":"Alarm"}`,
		`{"operation":"unpublish","nativeGroupId":"native-http-1","expectedRouteVersion":0,"name":null}`,
		`{"operation":"unpublish","nativeGroupId":"native-http-1","expectedRouteVersion":null}`,
	} {
		response := request(http.MethodPost, "/api/v1/notification_group/native-publish", invalid, "TENANT_ADMIN", true)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("explicit null field was accepted: body=%s status=%d", invalid, response.Code)
		}
	}
}
