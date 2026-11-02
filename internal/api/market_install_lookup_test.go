package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/middleware/response"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

func lookupHTTPPermissionFixture(t *testing.T) (*gorm.DB, *casbin.Enforcer) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	if err := db.Exec(`CREATE TABLE users(id text PRIMARY KEY,tenant_id text,status text,authority text)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO users VALUES('subject','tenant','N','TENANT_ADMIN')`).Error; err != nil {
		t.Fatal(err)
	}
	enforcer, err := casbin.NewEnforcer("../../configs/casbin.conf")
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range [][]string{{"g2", service.MarketInstallationReadResource, "lookup-http-function"}, {"g", "subject", "lookup-http-role"}} {
		if _, err := enforcer.AddNamedGroupingPolicy(rule[0], rule[1], rule[2]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := enforcer.AddPolicy("lookup-http-role", "lookup-http-function", "allow"); err != nil {
		t.Fatal(err)
	}
	previousDB, previousEnforcer := global.DB, global.CasbinEnforcer
	global.DB, global.CasbinEnforcer = db, enforcer
	t.Cleanup(func() { global.DB, global.CasbinEnforcer = previousDB, previousEnforcer; pool.Close() })
	return db, enforcer
}

func lookupHTTPTestEngine(claims interface{}) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use((&response.Handler{ErrManager: errcode.NewErrorManager("", "")}).Middleware())
	engine.Use(func(c *gin.Context) {
		if claims != nil {
			c.Set("claims", claims)
		}
		c.Next()
	})
	engine.GET("/api/v1/device/market/bundles/installation-operations", (&DeviceApi{}).QueryBundleInstallationOperation)
	return engine
}

func TestMarketInstallationLookupHTTPUsesOriginalQueryKey(t *testing.T) {
	lookupHTTPPermissionFixture(t)
	key := "original/key + ? #"
	var calls atomic.Int64
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Query().Get("operationKey") != key {
			t.Error("controller did not preserve opaque original key")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"state": "confirmed", "operationKey": key,
			"operation": map[string]string{"id": "original-id", "bundleKey": "original-bundle", "version": "1", "state": "running"}})
	}))
	defer owner.Close()
	t.Setenv("TP_MARKETINSTALL_OWNER", "encore")
	t.Setenv("TP_MARKETINSTALL_COORDINATOR_BASE_URL", owner.URL)
	engine := lookupHTTPTestEngine(&utils.UserClaims{ID: "subject", TenantID: "tenant", Authority: "TENANT_ADMIN"})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/device/market/bundles/installation-operations?"+url.Values{"operationKey": {key}}.Encode(), nil)
	request.Header.Set("X-Token", strings.Repeat("t", 40))
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	var envelope struct {
		Code int                            `json:"code"`
		Data model.MarketInstallationLookup `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != errcode.CodeSuccess || envelope.Data.OperationKey != key || envelope.Data.Result == nil || envelope.Data.Result.InstallationID != "original-id" || envelope.Data.Result.Status != "RUNNING" || calls.Load() != 1 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("original-key HTTP receipt: status=%d code=%d calls=%d body=%s", response.Code, envelope.Code, calls.Load(), response.Body.String())
	}
}

func TestMarketInstallationLookupHTTPRejectsMissingIdentityAndAmbiguousKeys(t *testing.T) {
	lookupHTTPPermissionFixture(t)
	var calls atomic.Int64
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{}`) }))
	defer owner.Close()
	t.Setenv("TP_MARKETINSTALL_OWNER", "encore")
	t.Setenv("TP_MARKETINSTALL_COORDINATOR_BASE_URL", owner.URL)
	for _, test := range []struct {
		claims interface{}
		query  string
		code   int
	}{
		{nil, "operationKey=original-key", errcode.CodeNoPermission},
		{"wrong-claims-type", "operationKey=original-key", errcode.CodeNoPermission},
		{&utils.UserClaims{ID: "subject", TenantID: "tenant", Authority: "TENANT_USER"}, "operationKey=original-key", errcode.CodeNoPermission},
		{&utils.UserClaims{ID: "subject", TenantID: "tenant", Authority: "TENANT_ADMIN"}, "", errcode.CodeParamError},
		{&utils.UserClaims{ID: "subject", TenantID: "tenant", Authority: "TENANT_ADMIN"}, "operationKey=original-key&operationKey=second-key", errcode.CodeParamError},
	} {
		engine := lookupHTTPTestEngine(test.claims)
		request := httptest.NewRequest(http.MethodGet, "/api/v1/device/market/bundles/installation-operations?"+test.query, nil)
		request.Header.Set("X-Token", strings.Repeat("t", 40))
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		var envelope struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Code != test.code {
			t.Fatalf("query denial: got=%d want=%d body=%s", envelope.Code, test.code, response.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("rejected query reached owner")
	}
}
