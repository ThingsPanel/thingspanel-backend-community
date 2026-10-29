package api

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAutomationOwnerAuthRunsBeforeDeviceOrCommandProcessing(t *testing.T) {
	t.Setenv("GOTP_AUTOMATION_TOKEN", strings.Repeat("x", 32))
	t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "deployment-a")
	router := gin.New()
	RegisterAutomationOwnerRoutes(router, nil)
	for _, path := range []string{"/internal/automation/devices", "/internal/automation/api/v1/telemetry/datas/pub"} {
		method := "GET"
		if strings.Contains(path, "/pub") {
			method = "POST"
		}
		r := httptest.NewRequest(method, path, strings.NewReader(`{"device_id":"forged"}`))
		r.Header.Set("Authorization", "Bearer browser-jwt")
		r.Header.Set("X-Automation-Deployment", "deployment-a")
		r.Header.Set("X-Automation-Tenant", "tenant-a")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("unauthenticated owner reached processing: %d", w.Code)
		}
	}
}
