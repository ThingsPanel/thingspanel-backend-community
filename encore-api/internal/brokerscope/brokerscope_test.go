package brokerscope

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
)

const testToken = "broker-scope-token-broker-scope-0001"

var fixture = []string{
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
	// config of another tenant must not be joined: device keeps the MQTT default
	`INSERT INTO devices VALUES ('d-xtenant','t2','cfg-modbus','enabled','active',NULL,NULL,NULL)`,
}

// pgDB returns a transaction on a real PostgreSQL with the legacy tables created as TEMP tables (they shadow any
// real ones and vanish at rollback). It skips unless TEST_PG_SCHEMA_DSN is set (the variable pkg/relational uses).
func pgDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_PG_SCHEMA_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_SCHEMA_DSN not set: PostgreSQL run skipped")
	}
	db, err := relational.Open(context.Background(), relational.Target{Name: "brokerscope_test", Dialect: relational.Postgres, DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	t.Cleanup(func() { tx.Rollback(); relational.CloseDB(db) })
	for _, stmt := range fixture {
		if err := tx.Exec(strings.Replace(stmt, "CREATE TABLE ", "CREATE TEMP TABLE ", 1)).Error; err != nil {
			t.Fatal(err)
		}
	}
	return tx
}

func ids(list []Device) string {
	out := make([]string, len(list))
	for i, d := range list {
		out[i] = d.ID
	}
	return strings.Join(out, ",")
}

func checkRules(t *testing.T, db *gorm.DB) {
	t.Helper()
	scope, err := Query(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(scope.Targets); got != "d-mqtt,d-noconf,d-xtenant" {
		t.Fatalf("targets=%s", got)
	}
	if got := ids(scope.Plugins); got != "d-modbus,d-svc" {
		t.Fatalf("plugins=%s", got)
	}
}

func TestQueryMatchesTheInstallerRules(t *testing.T) { checkRules(t, pgDB(t)) }

func TestLimitIsApplied(t *testing.T) {
	db := pgDB(t)
	if err := db.Exec(`INSERT INTO devices SELECT 'x-' || lpad(g::text, 6, '0'), 't1', 'cfg-mqtt', 'enabled', 'active', NULL, NULL, NULL FROM generate_series(1, ?) g`, MaxDevices+5).Error; err != nil {
		t.Fatal(err)
	}
	scope, err := Query(context.Background(), db)
	if err != nil || len(scope.Targets) != MaxDevices {
		t.Fatalf("len=%d err=%v", len(scope.Targets), err)
	}
}

func call(h http.HandlerFunc, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/internal/broker-scope", nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func TestHandlerAuthRejections(t *testing.T) {
	opened := false
	open := func() (*gorm.DB, error) { opened = true; return nil, errors.New("must not be reached") }
	token := testToken
	h := Handler(func() string { return token }, open)

	token = ""
	if w := call(h, "Bearer "+testToken); w.Code != http.StatusNotFound {
		t.Fatalf("unset token must hide the route: %d", w.Code)
	}
	token = "too-short"
	if w := call(h, "Bearer too-short"); w.Code != http.StatusNotFound {
		t.Fatalf("short token must hide the route: %d", w.Code)
	}
	token = testToken
	for _, auth := range []string{"", "Bearer wrong", "Bearer ", testToken, "Basic " + testToken, "bearer " + testToken} {
		if w := call(h, auth); w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "targets") {
			t.Fatalf("auth %q got %d %s", auth, w.Code, w.Body)
		}
	}
	if opened {
		t.Fatal("database was opened before the caller was authenticated")
	}
}

func TestHandlerServesScope(t *testing.T) {
	db := pgDB(t)
	w := call(Handler(func() string { return testToken }, func() (*gorm.DB, error) { return db, nil }), "Bearer "+testToken)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(w.Header().Get("Content-Type"), "application/json") ||
		!strings.Contains(w.Body.String(), `"targets":[{"id":"d-mqtt","tenant_id":"t1"}`) ||
		!strings.Contains(w.Body.String(), `"plugins":[{"id":"d-modbus","tenant_id":"t1"}`) {
		t.Fatalf("%d %v %s", w.Code, w.Header(), w.Body)
	}
}

func TestHandlerEmptyListsAreArrays(t *testing.T) {
	db := pgDB(t)
	db.Exec(`DELETE FROM devices`)
	w := call(Handler(func() string { return testToken }, func() (*gorm.DB, error) { return db, nil }), "Bearer "+testToken)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"targets":[],"plugins":[]}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestHandlerDatabaseFailures(t *testing.T) {
	h := Handler(func() string { return testToken }, func() (*gorm.DB, error) { return nil, errors.New("down") })
	if w := call(h, "Bearer "+testToken); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unreachable db: %d", w.Code)
	}
	// a closed pool makes the query fail: 500 without leaking the error text
	dsn := os.Getenv("TEST_PG_SCHEMA_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_SCHEMA_DSN not set: query-failure case skipped")
	}
	db, err := relational.Open(context.Background(), relational.Target{Name: "brokerscope_closed", Dialect: relational.Postgres, DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	relational.CloseDB(db)
	h = Handler(func() string { return testToken }, func() (*gorm.DB, error) { return db, nil })
	if w := call(h, "Bearer "+testToken); w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "closed") {
		t.Fatalf("query failure: %d %s", w.Code, w.Body)
	}
}
