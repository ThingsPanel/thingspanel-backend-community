package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"project/pkg/global"
)

func TestDictionaryAuthorizationServiceCredentialBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB, oldEnforcer := global.DB, global.CasbinEnforcer
	global.DB, global.CasbinEnforcer = nil, nil
	t.Cleanup(func() { global.DB, global.CasbinEnforcer = oldDB, oldEnforcer })
	t.Setenv("TP_PLATFORMCONFIG_OWNER", "encore")
	token := strings.Repeat("dictionary-service-token-", 2)
	t.Setenv("TP_PLATFORMCONFIG_AUTHORIZATION_TOKEN", token)
	router := gin.New()
	RegisterDictionaryAuthorizationRoutes(router)
	call := func(token, body string, want int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/internal/platformconfig/authorize", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-TP-Service-Token", token)
		r.Header.Set("Authorization", "Bearer user-token-cannot-substitute-for-service-credential")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("status=%d expected=%d body=%s", w.Code, want, w.Body.String())
		}
	}
	body := `{"protocolVersion":1,"tenantId":"tenant","subjectId":"subject","operation":"read-enum"}`
	call("", body, 403)
	call(strings.Repeat("x", len(token)), body, 403)
	call(token, body, 503) // trusted request still cannot bypass unavailable owners
	call(token, `{"protocolVersion":2,"tenantId":"tenant","subjectId":"subject","operation":"read-enum"}`, 403)
	call(token, `{"protocolVersion":1,"tenantId":"tenant","subjectId":"subject","operation":"read-enum","resource":"arbitrary"}`, 400)
	call(token, body+`{}`, 400)
	call(token, strings.Repeat(" ", 8193)+body, 400)
	t.Setenv("TP_PLATFORMCONFIG_OWNER", "")
	call(token, body, 503)
	t.Setenv("TP_PLATFORMCONFIG_OWNER", "encore")
	t.Setenv("TP_PLATFORMCONFIG_AUTHORIZATION_TOKEN", "short")
	call("short", body, 503)
}
