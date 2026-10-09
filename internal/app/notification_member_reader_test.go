package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNotificationMemberReaderEnvironmentFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("NOTIFICATION_MEMBER_READER_TOKEN", "")
	t.Setenv("NOTIFICATION_MEMBER_READER_DEPLOYMENTS", "deployment-a")
	t.Setenv("NOTIFICATION_MEMBER_READER_TENANT_IDS", "tenant-a")
	reader := NewNotificationMemberReaderFromEnv()
	engine := gin.New()
	engine.GET("/api/v1/notification/source-members/:id", reader.GetMemberContact)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/notification/source-members/member-a?tenantId=tenant-a&contactField=email", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing private reader token did not disable route: status=%d", response.Code)
	}
}

func TestNotificationMemberReaderEnvironmentParsesAllowLists(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("NOTIFICATION_MEMBER_READER_TOKEN", "0123456789abcdef0123456789abcdef")
	t.Setenv("NOTIFICATION_MEMBER_READER_DEPLOYMENTS", "deployment-a, deployment-b")
	t.Setenv("NOTIFICATION_MEMBER_READER_TENANT_IDS", "tenant-a,tenant-b")
	reader := NewNotificationMemberReaderFromEnv()
	engine := gin.New()
	engine.GET("/api/v1/notification/source-members/:id", reader.GetMemberContact)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/notification/source-members/member-a?tenantId=tenant-a&contactField=email", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("configured route did not reach authentication: status=%d", response.Code)
	}

	t.Setenv("NOTIFICATION_MEMBER_READER_DEPLOYMENTS", "deployment-a,,deployment-b")
	reader = NewNotificationMemberReaderFromEnv()
	engine = gin.New()
	engine.GET("/api/v1/notification/source-members/:id", reader.GetMemberContact)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/notification/source-members/member-a?tenantId=tenant-a&contactField=email", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("malformed allowlist did not disable route: status=%d", response.Code)
	}
}
