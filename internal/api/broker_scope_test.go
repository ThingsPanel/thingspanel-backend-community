package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const scopeTestToken = "broker-scope-token-broker-scope-0001"

func scopeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE device_configs (id TEXT, tenant_id TEXT, protocol_type TEXT)`,
		`CREATE TABLE devices (id TEXT, tenant_id TEXT, device_config_id TEXT, is_enabled TEXT, activate_flag TEXT, parent_id TEXT, service_access_id TEXT, access_way TEXT)`,
		`INSERT INTO device_configs VALUES ('cfg-mqtt','t1','MQTT'),('cfg-modbus','t1','MODBUS')`,
		// a plain MQTT device: target
		`INSERT INTO devices VALUES ('d-mqtt','t1','cfg-mqtt','enabled','active',NULL,NULL,NULL)`,
		// no config at all defaults to MQTT: target
		`INSERT INTO devices VALUES ('d-noconf','t1',NULL,'enabled','active','',NULL,'A')`,
		// protocol-plugin device: plugin, never a target
		`INSERT INTO devices VALUES ('d-modbus','t1','cfg-modbus','enabled','active',NULL,NULL,'A')`,
		// service-access device: plugin
		`INSERT INTO devices VALUES ('d-svc','t2','cfg-mqtt','enabled','active',NULL,'svc-1','A')`,
		// excluded: sub-device, access way B, disabled, inactive
		`INSERT INTO devices VALUES ('d-child','t1','cfg-mqtt','enabled','active','d-mqtt',NULL,NULL)`,
		`INSERT INTO devices VALUES ('d-b','t1','cfg-mqtt','enabled','active',NULL,NULL,'B')`,
		`INSERT INTO devices VALUES ('d-off','t1','cfg-mqtt','disabled','active',NULL,NULL,NULL)`,
		`INSERT INTO devices VALUES ('d-new','t1','cfg-mqtt','enabled','inactive',NULL,NULL,NULL)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func ids(list []BrokerScopeDevice) string {
	out := make([]string, len(list))
	for i, d := range list {
		out[i] = d.ID
	}
	return strings.Join(out, ",")
}

func TestQueryBrokerScopeMatchesTheInstallerRules(t *testing.T) {
	scope, err := QueryBrokerScope(context.Background(), scopeTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(scope.Targets); got != "d-mqtt,d-noconf" {
		t.Fatalf("targets=%s", got)
	}
	if got := ids(scope.Plugins); got != "d-modbus,d-svc" {
		t.Fatalf("plugins=%s", got)
	}
}

func scopeRouter(t *testing.T) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterBrokerScopeRoutes(r, scopeTestDB(t))
	return r
}

func scopeGet(r *gin.Engine, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/internal/broker-scope", nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestBrokerScopeRouteAuth(t *testing.T) {
	r := scopeRouter(t)
	t.Setenv(BrokerScopeTokenEnv, "")
	if w := scopeGet(r, "Bearer "+scopeTestToken); w.Code != http.StatusNotFound {
		t.Fatalf("unconfigured route must not exist: %d", w.Code)
	}
	t.Setenv(BrokerScopeTokenEnv, scopeTestToken)
	for _, auth := range []string{"", "Bearer wrong", scopeTestToken, "Basic " + scopeTestToken} {
		if w := scopeGet(r, auth); w.Code != http.StatusUnauthorized {
			t.Fatalf("auth %q got %d", auth, w.Code)
		}
	}
	w := scopeGet(r, "Bearer "+scopeTestToken)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"d-mqtt"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
