package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"project/internal/middleware"
	"project/internal/middleware/response"
	"project/internal/query"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Needs a real PostgreSQL: P2_TEST_PG_DSN must point at a throwaway database
// whose name starts with "p2". The test creates/drops its own tables.
func TestVoucherNeverLeaksIntoLogsURLsOrOperationLogs(t *testing.T) {
	dsn := os.Getenv("P2_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("P2_TEST_PG_DSN required (real PostgreSQL)")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	var dbName string
	if err := db.Raw("SELECT current_database()").Scan(&dbName).Error; err != nil || !strings.HasPrefix(dbName, "p2") {
		t.Fatalf("refusing database %q", dbName)
	}
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS service_access, service_plugins, operation_logs, devices CASCADE`,
		`CREATE TABLE devices (id varchar(36) PRIMARY KEY, name varchar(255), voucher varchar(500) NOT NULL DEFAULT '', tenant_id varchar(36) NOT NULL DEFAULT '', is_enabled varchar(36) NOT NULL DEFAULT '', activate_flag varchar(36) NOT NULL DEFAULT '', created_at timestamptz, update_at timestamptz, device_number varchar(36) NOT NULL DEFAULT '', product_id varchar(36), parent_id varchar(36), protocol varchar(36), label varchar(255), location varchar(100), sub_device_addr varchar(36), current_version varchar(36), additional_info json, protocol_config json, remark1 varchar(255), remark2 varchar(255), remark3 varchar(255), device_config_id varchar(36), batch_number varchar(500), activate_at timestamptz, is_online int2 NOT NULL DEFAULT 0, access_way varchar(10), description varchar(500), service_access_id varchar(36))`,
		`CREATE TABLE service_plugins (id varchar(36) PRIMARY KEY, name varchar(255) NOT NULL, service_identifier varchar(100) NOT NULL, service_type int NOT NULL, last_active_time timestamptz, version varchar(100), create_at timestamptz NOT NULL, update_at timestamptz NOT NULL, description varchar(255), service_config json, remark varchar(255))`,
		`CREATE TABLE service_access (id varchar(36) PRIMARY KEY, name varchar(100) NOT NULL, service_plugin_id varchar(36) NOT NULL REFERENCES service_plugins(id), voucher varchar(999) NOT NULL, description varchar(255), service_access_config json, remark varchar(255), create_at timestamptz NOT NULL, update_at timestamptz NOT NULL, tenant_id varchar(36) NOT NULL)`,
		`CREATE TABLE operation_logs (id varchar(36) PRIMARY KEY, ip varchar(36) NOT NULL, path varchar(2000), user_id varchar(36) NOT NULL, name varchar(255), created_at timestamptz NOT NULL, latency int8, request_message text, response_message text, tenant_id varchar(36) NOT NULL, remark varchar(255))`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	query.SetDefault(db)

	const token = "FAKE-HA-LLAT-9f8e7d6c5b4a"
	// Pretty-printed on purpose: before the fix this had to be compact JSON.
	voucher := fmt.Sprintf("{\n  \"url\": \"http://ha.local:8123\",\n  \"token\": \"%s\"\n}", token)

	// Healthy plugin: asserts it receives the decoded voucher intact.
	var gotVoucher string
	plugin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVoucher = r.URL.Query().Get("voucher")
		_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":{"list":[{"device_number":"d1","device_name":"light"}]}}`))
	}))
	defer plugin.Close()
	// Dead plugin: connection refused, net/http embeds the full URL in the error.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	deadAddr := l.Addr().String()
	_ = l.Close()

	now := "NOW()"
	for id, addr := range map[string]string{"plug-ok": strings.TrimPrefix(plugin.URL, "http://"), "plug-dead": deadAddr} {
		cfg, _ := json.Marshal(map[string]string{"http_address": addr})
		if err := db.Exec("INSERT INTO service_plugins(id,name,service_identifier,service_type,create_at,update_at,service_config) VALUES(?,?,?,1,"+now+","+now+",?)", id, id, id, string(cfg)).Error; err != nil {
			t.Fatal(err)
		}
	}

	var logs bytes.Buffer
	prevOut := logrus.StandardLogger().Out
	prevLevel := logrus.GetLevel()
	logrus.SetOutput(&logs)
	logrus.SetLevel(logrus.DebugLevel)
	prevGin := gin.DefaultWriter
	gin.DefaultWriter = &logs
	defer func() { logrus.SetOutput(prevOut); logrus.SetLevel(prevLevel); gin.DefaultWriter = prevGin }()

	handler, err := response.NewHandler("../../configs/messages.yaml", "../../configs/messages_str.yaml")
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.Use(middleware.AccessLogger(), gin.Recovery())
	engine.Use(func(c *gin.Context) {
		c.Set("claims", &utils.UserClaims{ID: "user-1", TenantID: "tenant-1", Authority: "TENANT_ADMIN"})
	})
	engine.Use(middleware.OperationLogs())
	engine.Use(handler.Middleware())
	api := &ServiceAccessApi{}
	engine.POST("/api/v1/service/access", api.Create)
	engine.GET("/api/v1/service/access/device/list", api.HandleDeviceList)

	do := func(method, target string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w
	}
	bodies := []string{}

	// 1. create access point (token inside voucher)
	for _, plug := range []string{"plug-ok", "plug-dead"} {
		b, _ := json.Marshal(map[string]string{"name": plug, "service_plugin_id": plug, "voucher": voucher + plug[len(plug)-2:]})
		w := do(http.MethodPost, "/api/v1/service/access", b)
		bodies = append(bodies, w.Body.String())
		if w.Code != 200 {
			t.Fatalf("create %s: %d %s", plug, w.Code, w.Body.String())
		}
	}
	var stored int
	_ = db.Raw("SELECT count(*) FROM service_access WHERE voucher LIKE ?", "%"+token+"%").Scan(&stored).Error
	if stored != 2 {
		t.Fatalf("voucher must still be stored (encryption is P2b), got %d rows", stored)
	}

	// 2. device list, healthy plugin: voucher passes through correctly encoded.
	okV := voucher + "ok"
	w := do(http.MethodGet, "/api/v1/service/access/device/list?page=1&page_size=10&voucher="+url.QueryEscape(okV), nil)
	bodies = append(bodies, w.Body.String())
	if w.Code != 200 || !strings.Contains(w.Body.String(), "light") {
		t.Fatalf("device list: %d %s", w.Code, w.Body.String())
	}
	if gotVoucher != okV {
		t.Fatalf("plugin received voucher %q, want %q", gotVoucher, okV)
	}

	// 3. device list, error path: plugin unreachable.
	deadV := voucher + "ad"
	w = do(http.MethodGet, "/api/v1/service/access/device/list?page=1&page_size=10&voucher="+url.QueryEscape(deadV), nil)
	bodies = append(bodies, w.Body.String())
	if !strings.Contains(w.Body.String(), "105001") && !strings.Contains(w.Body.String(), "refused") {
		t.Fatalf("expected plugin failure, got %d %s", w.Code, w.Body.String())
	}

	var opRows []string
	_ = db.Raw("SELECT coalesce(path,'')||coalesce(request_message,'')||coalesce(response_message,'') FROM operation_logs").Scan(&opRows).Error
	if len(opRows) == 0 {
		t.Fatal("operation log rows expected for the create calls")
	}
	for _, needle := range []string{token, url.QueryEscape(token)} {
		if n := strings.Count(logs.String(), needle); n != 0 {
			t.Errorf("token appears %d times in log output:\n%s", n, logs.String())
		}
		if n := strings.Count(strings.Join(opRows, "\n"), needle); n != 0 {
			t.Errorf("token appears %d times in operation_logs: %v", n, opRows)
		}
		if n := strings.Count(strings.Join(bodies, "\n"), needle); n != 0 {
			t.Errorf("token appears %d times in HTTP responses: %v", n, bodies)
		}
	}
	t.Logf("log bytes=%d operation_log rows=%d", logs.Len(), len(opRows))
}
