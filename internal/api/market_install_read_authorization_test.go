package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/errcode"
	"project/pkg/utils"
)

func TestMarketInstallationReadAuthorizationTrustedProviderBoundary(t *testing.T) {
	db, enforcer := lookupHTTPPermissionFixture(t)
	t.Setenv("TP_MARKETINSTALL_OWNER", "encore")
	serviceToken := strings.Repeat("s", 40)
	t.Setenv("TP_MARKETINSTALL_PROVIDER_TOKEN", serviceToken)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterMarketInstallProviderRoutes(engine)
	read := func(token, body string, expected int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/internal/market-install/read-authorize", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-TP-Service-Token", token)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != expected || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("provider status=%d want=%d body=%s", response.Code, expected, response.Body.String())
		}
		if expected == 200 {
			var decision model.MarketInstallationReadAuthorization
			if err := json.Unmarshal(response.Body.Bytes(), &decision); err != nil {
				t.Fatal(err)
			}
			if !decision.Authorized || decision.ProtocolVersion != 1 || decision.TenantID != "tenant" || decision.SubjectID != "subject" || decision.Resource != service.MarketInstallationReadResource {
				t.Fatalf("permission echo changed scope: %#v", decision)
			}
		}
	}
	body := `{"protocolVersion":1,"tenantId":"tenant","subjectId":"subject"}`
	read("", body, 403)
	read(strings.Repeat("x", 40), body, 403)
	read(serviceToken, body, 200)
	read(serviceToken, `{"protocolVersion":2,"tenantId":"tenant","subjectId":"subject"}`, 403)
	read(serviceToken, `{"protocolVersion":1,"tenantId":"foreign","subjectId":"subject"}`, 403)
	if _, err := enforcer.RemoveNamedGroupingPolicy("g2", service.MarketInstallationReadResource, "lookup-http-function"); err != nil {
		t.Fatal(err)
	}
	if _, err := enforcer.AddNamedGroupingPolicy("g2", "arbitrary-client-resource", "lookup-http-function"); err != nil {
		t.Fatal(err)
	}
	read(serviceToken, `{"protocolVersion":1,"tenantId":"tenant","subjectId":"subject","resource":"arbitrary-client-resource"}`, 403)
	if _, err := enforcer.AddNamedGroupingPolicy("g2", service.MarketInstallationReadResource, "lookup-http-function"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE users SET status='F' WHERE id='subject'").Error; err != nil {
		t.Fatal(err)
	}
	read(serviceToken, body, 403)
	t.Setenv("TP_MARKETINSTALL_OWNER", "legacy")
	read(serviceToken, body, 503)
}

func TestMarketInstallationLookupHTTPRequiresCurrentInstallResource(t *testing.T) {
	_, enforcer := lookupHTTPPermissionFixture(t)
	t.Setenv("TP_MARKETINSTALL_OWNER", "encore")
	var called atomic.Bool
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called.Store(true); w.WriteHeader(500) }))
	defer owner.Close()
	t.Setenv("TP_MARKETINSTALL_COORDINATOR_BASE_URL", owner.URL)
	engine := lookupHTTPTestEngine(&utils.UserClaims{ID: "subject", TenantID: "tenant", Authority: "TENANT_ADMIN"})
	if _, err := enforcer.RemovePolicy("lookup-http-role", "lookup-http-function", "allow"); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/device/market/bundles/installation-operations?operationKey=original-key", nil)
	request.Header.Set("X-Token", strings.Repeat("t", 40))
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if called.Load() || envelope.Code != errcode.CodeNoPermission {
		t.Fatalf("revoked resource still read owner: called=%v code=%d", called.Load(), envelope.Code)
	}
}
