package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"project/internal/middleware"
	"project/internal/middleware/response"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/global"
	"project/pkg/utils"
	"strings"
	"testing"
	"time"
)

type metadataCacheProbe struct{ calls int }

func (*metadataCacheProbe) DialHook(next redis.DialHook) redis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) {
		return nil, fmt.Errorf("unexpected Redis dial")
	}
}
func (p *metadataCacheProbe) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		p.calls++
		if v, ok := cmd.(*redis.IntCmd); ok {
			v.SetVal(1)
		}
		return nil
	}
}
func (*metadataCacheProbe) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func TestTemplateMetadataAPI(t *testing.T) {
	dsn := os.Getenv("Q02_METADATA_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL fixture required")
	}
	u, e := url.Parse(dsn)
	if e != nil || u.Host != "127.0.0.1:25543" || u.Path != "/notification_test" {
		t.Fatal("refusing non-local fixture")
	}
	admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("q02_metadata_api_%d", time.Now().UnixNano())
	if e = admin.Exec("CREATE SCHEMA " + schema).Error; e != nil {
		t.Fatal(e)
	}
	params := u.Query()
	params.Set("search_path", schema)
	u.RawQuery = params.Encode()
	db, e := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Discard})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(&model.DeviceConfig{}, &model.Device{}); e != nil {
		t.Fatal(e)
	}
	oldDB, oldRedis := global.DB, global.REDIS
	global.DB = db
	query.SetDefault(db)
	cache := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	probe := &metadataCacheProbe{}
	cache.AddHook(probe)
	global.REDIS = cache
	t.Cleanup(func() {
		global.DB = oldDB
		global.REDIS = oldRedis
		if oldDB != nil {
			query.SetDefault(oldDB)
		}
		cache.Close()
		sql, _ := db.DB()
		sql.Close()
		admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		a, _ := admin.DB()
		a.Close()
	})
	protocol := "MQTT"
	other := `{"heartbeat":19,"custom":true}`
	row := model.DeviceConfig{ID: "q02-api", TenantID: "a", Name: "before", DeviceType: "1", ProtocolType: &protocol, OtherConfig: &other, AutoRegister: 1}
	if e = db.Create(&row).Error; e != nil {
		t.Fatal(e)
	}
	handler, e := response.NewHandler("../../configs/messages.yaml", "../../configs/messages_str.yaml")
	if e != nil {
		t.Fatal(e)
	}
	r := gin.New()
	r.Use(middleware.Cors(), handler.Middleware(), func(c *gin.Context) { c.Set("claims", &utils.UserClaims{TenantID: c.GetHeader("QA-Tenant")}); c.Next() })
	controller := &DeviceConfigApi{}
	r.GET("/device_config/:id", controller.HandleDeviceConfigById)
	r.PUT("/device_config", controller.UpdateDeviceConfig)
	call := func(method, path, tenant, match string, payload any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(payload)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("QA-Tenant", tenant)
		if match != "" {
			req.Header.Set("If-Match", match)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	first := call("GET", "/device_config/q02-api", "a", "", nil)
	tag := first.Header().Get("ETag")
	if first.Code != 200 || tag == "" || !strings.Contains(first.Header().Get("Access-Control-Expose-Headers"), "ETag") {
		t.Fatal("GET did not expose entity version")
	}
	saved := call("PUT", "/device_config", "a", tag, map[string]any{"id": "q02-api", "name": "saved"})
	if saved.Code != 200 || saved.Header().Get("ETag") == tag || probe.calls != 1 {
		t.Fatalf("successful save/status/cache: %d/%d", saved.Code, probe.calls)
	}
	after := call("GET", "/device_config/q02-api", "a", "", nil)
	if after.Header().Get("ETag") != saved.Header().Get("ETag") {
		t.Fatal("response tag did not match stored entity")
	}
	stale := call("PUT", "/device_config", "a", tag, map[string]any{"id": "q02-api", "name": "stale"})
	if stale.Code != 412 || !strings.Contains(stale.Body.String(), "template_revision_conflict") || probe.calls != 1 {
		t.Fatal("conflict response/cache side effect incorrect")
	}
	foreign := call("PUT", "/device_config", "b", tag, map[string]any{"id": "q02-api", "name": "foreign"})
	if foreign.Header().Get("ETag") != "" || strings.Contains(foreign.Body.String(), "template_revision_conflict") || probe.calls != 1 {
		t.Fatal("foreign tenant learned metadata version or changed cache")
	}
	invalid := call("PUT", "/device_config", "a", "bad", map[string]any{"id": "q02-api", "name": "invalid"})
	if invalid.Code != 400 {
		t.Fatal("invalid condition accepted")
	}
	legacy := call("PUT", "/device_config", "a", "", map[string]any{"id": "q02-api", "description": "old caller"})
	if legacy.Code != 200 || legacy.Header().Get("ETag") == "" {
		t.Fatal("old caller incompatible")
	}
	db.First(&row, "id = ?", "q02-api")
	if row.Name != "saved" || *row.OtherConfig != other || row.AutoRegister != 1 {
		t.Fatal("partial DTO reset configuration")
	}
	// Exercise the real pointer-valued DTO mapping, not just DAL string maps.
	httpResult := call("PUT", "/device_config", "a", legacy.Header().Get("ETag"), map[string]any{"id": "q02-api", "protocol_type": "HTTP", "voucher_type": "obsolete"})
	if httpResult.Code != 200 || httpResult.Header().Get("ETag") == "" {
		t.Fatal("protocol transition failed")
	}
	var final model.DeviceConfig
	db.First(&final, "id = ?", "q02-api")
	if final.VoucherType != nil {
		t.Fatal("HTTP transition did not clear derived credential")
	}
	mqttResult := call("PUT", "/device_config", "a", httpResult.Header().Get("ETag"), map[string]any{"id": "q02-api", "protocol_type": "MQTT"})
	db.First(&final, "id = ?", "q02-api")
	if final.VoucherType == nil || *final.VoucherType != "ACCESSTOKEN" {
		t.Fatal("MQTT transition did not derive credential")
	}
	currentTag, _ := model.DeviceConfigETag(&final)
	if currentTag != mqttResult.Header().Get("ETag") {
		t.Fatal("protocol response version differs from persisted final row")
	}
}
