package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLegacyPluginRuntimeGuardRejectsBeforeOwnerSideEffects(t *testing.T) {
	for _, credential := range []string{"", "Bearer user-session", "Bearer stale-plugin-session"} {
		t.Run(credential, func(t *testing.T) {
			engine := gin.New()
			calls := 0
			engine.POST("/runtime", RejectUnboundPluginRuntime(), func(c *gin.Context) {
				calls++
				c.JSON(http.StatusOK, gin.H{"secret": "must-not-be-returned"})
			})
			request := httptest.NewRequest(http.MethodPost, "/runtime", strings.NewReader(`{"service_identifier":"known-service","instance_id":"invented-instance","tenant_id":"other-tenant"}`))
			request.Header.Set("Authorization", credential)
			request.Header.Set("X-Tenant-ID", "other-tenant")
			request.Header.Set("X-Instance-ID", "invented-instance")
			request.Header.Set("X-Instance-Active", "true")
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusServiceUnavailable || calls != 0 || recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("untrusted request reached owner or was acknowledged: status=%d calls=%d", recorder.Code, calls)
			}
			var result struct {
				Code int `json:"code"`
				Data struct {
					State          string `json:"state"`
					ErrorCode      string `json:"errorCode"`
					AutomaticRetry bool   `json:"automatic_retry"`
				} `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Code != http.StatusServiceUnavailable || result.Data.State != "blocked" || result.Data.ErrorCode != "PLUGIN_RUNTIME_IDENTITY_REQUIRED" || result.Data.AutomaticRetry {
				t.Fatalf("guard must report blocked without suggesting blind retry: %s", recorder.Body.String())
			}
			for _, secret := range []string{"user-session", "stale-plugin-session", "known-service", "invented-instance", "other-tenant", "must-not-be-returned"} {
				if strings.Contains(recorder.Body.String(), secret) {
					t.Fatal("guard echoed request credentials or business data")
				}
			}
		})
	}
}
