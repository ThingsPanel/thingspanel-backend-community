package api

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func q09LookupRequest(t *testing.T, router *gin.Engine, body, tenant, token string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest("POST", "/internal/automation/devices/lookup", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Automation-Tenant", tenant)
	req.Header.Set("X-Automation-Deployment", "qa-directory")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}
func TestAutomationDeviceLookupRejectsInvalidRequestsBeforeQuery(t *testing.T) {
	token := strings.Repeat("d", 32)
	t.Setenv("GOTP_AUTOMATION_TOKEN", token)
	t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "qa-directory")
	router := gin.New()
	RegisterAutomationOwnerRoutes(router, nil)
	ids := make([]string, 201)
	for i := range ids {
		ids[i] = fmt.Sprintf("d-%d", i)
	}
	over, _ := json.Marshal(map[string]any{"ids": ids})
	for _, body := range []string{`{}`, `{"ids":[]}`, `{"ids":[""]}`, `{"ids":[" d"]}`, `{"ids":["d\n"]}`, `{"ids":["d"],"tenant_id":"foreign"}`, `{"ids":["d"]} {}`, string(over), `{"ids":["` + strings.Repeat("x", 37) + `"]}`} {
		if status, _ := q09LookupRequest(t, router, body, "tenant-a", token); status != 400 {
			t.Fatalf("invalid body status=%d", status)
		}
	}
	for _, v := range []struct{ tenant, token string }{{"tenant-a", "browser-jwt"}, {"", token}} {
		if status, _ := q09LookupRequest(t, router, `{"ids":["a"]}`, v.tenant, v.token); status != 401 {
			t.Fatalf("auth status%d", status)
		}
	}
	if status, _ := q09LookupRequest(t, router, `{"ids":["a"]}`, "tenant-a", token); status != 503 {
		t.Fatalf("nil database must failclosed got%d", status)
	}
}
func TestAutomationDeviceLookupPostgresIsBoundedAndTenantScoped(t *testing.T) {
	dsn := os.Getenv("TEST_AUTOMATION_DIRECTORY_DSN")
	if dsn == "" {
		t.Skip("TEST_AUTOMATION_DIRECTORY_DSN required for real PostgreSQL")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	schema := "qa_directory_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DROP SCHEMA " + schema + " CASCADE"); pool.Close() })
	if err := db.Exec("SET search_path TO " + schema).Error; err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE devices(id text PRIMARY KEY,tenant_id text,is_enabled text,name text,label text,device_config_id text,is_online int)`,
		`CREATE TABLE device_configs(id text PRIMARY KEY,tenant_id text,device_type text)`,
		`INSERT INTO device_configs VALUES ('cfg-a','tenant-a','3'),('cfg-b','tenant-b','9')`,
		`INSERT INTO devices VALUES ('a','tenant-a','enabled','A','floor-1','cfg-a',1),('disabled','tenant-a','disabled','Disabled','','cfg-a',0),('foreign','tenant-b','enabled','Foreign','','cfg-b',1),('cross-config','tenant-a','enabled','Cross','','cfg-b',1)`,
		`INSERT INTO devices SELECT 'noise-'||n,'tenant-a','enabled','Noise','','cfg-a',0 FROM generate_series(1,1500) n`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	token := strings.Repeat("d", 32)
	t.Setenv("GOTP_AUTOMATION_TOKEN", token)
	t.Setenv("GOTP_AUTOMATION_DEPLOYMENT_ID", "qa-directory")
	router := gin.New()
	RegisterAutomationOwnerRoutes(router, db)
	status, raw := q09LookupRequest(t, router, `{"ids":["a","a","disabled","foreign","missing","cross-config"]}`, "tenant-a", token)
	if status != 200 {
		t.Fatalf("lookup status%d", status)
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Items []map[string]any `json:"list"`
			Total int              `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != 200 || body.Data.Total != 2 || len(body.Data.Items) != 2 {
		t.Fatalf("unbounded or missing result: %s", raw)
	}
	for _, d := range body.Data.Items {
		if d["tenant_id"] != "tenant-a" || (d["id"] != "a" && d["id"] != "cross-config") {
			t.Fatal("foreign/unrequested/disabled device returned")
		}
		if d["id"] == "cross-config" && d["device_type"] == "9" {
			t.Fatal("foreign template data leaked")
		}
	}
	status, raw = q09LookupRequest(t, router, `{"ids":["a","foreign"]}`, "tenant-b", token)
	if status != 200 || json.Unmarshal(raw, &body) != nil || body.Data.Total != 1 || body.Data.Items[0]["id"] != "foreign" {
		t.Fatal("reverse tenant isolation failed")
	}
	status, raw = q09LookupRequest(t, router, `{"ids":["missing"]}`, "tenant-a", token)
	if status != 200 || json.Unmarshal(raw, &body) != nil || body.Data.Total != 0 || len(body.Data.Items) != 0 {
		t.Fatal("missing ID broadened lookup")
	}
	ids := make([]string, 200)
	for i := range ids {
		ids[i] = fmt.Sprintf("noise-%d", i+1)
	}
	payload, _ := json.Marshal(map[string]any{"ids": ids})
	status, raw = q09LookupRequest(t, router, string(payload), "tenant-a", token)
	if status != 200 || json.Unmarshal(raw, &body) != nil || body.Data.Total != 200 {
		t.Fatal("legal exact batch failed")
	}
}
