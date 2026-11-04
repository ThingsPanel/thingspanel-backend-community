package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"project/internal/middleware"
	"project/internal/model"
	"project/internal/pluginidentity"
	"project/internal/query"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Real PostgreSQL (TEST_PG_SCHEMA_DSN: a database built by sql/1.sql..33.sql, see scripts/dev-databases.sh).
// Rows use unique ids/tenants and are removed afterwards.
type pluginIDEnv struct {
	t       *testing.T
	db      *gorm.DB
	engine  *gin.Engine
	tenantA string
	tenantB string
	service string
	access  map[string]string // tenant -> service_access id
	device  map[string]string // tenant -> device id
	voucher map[string]string
}

// Devices need an active identity reservation to be inserted and a tombstone to be deleted (sql/31.sql).
func createDevice(t *testing.T, db *gorm.DB, d model.Device) {
	t.Helper()
	if err := db.Exec("INSERT INTO device_id_reservations (id, tenant_id, state) VALUES (?, ?, 'active')", d.ID, d.TenantID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&d).Error; err != nil {
		t.Fatal(err)
	}
}

func deleteTenantDevices(db *gorm.DB, tenant string) {
	db.Exec("UPDATE device_id_reservations SET state='tombstoned', deleted_at=CURRENT_TIMESTAMP WHERE tenant_id = ?", tenant)
	db.Where("tenant_id = ?", tenant).Delete(&model.Device{})
	db.Exec("DELETE FROM device_id_reservations WHERE tenant_id = ?", tenant)
}

func newPluginIDEnv(t *testing.T) *pluginIDEnv {
	dsn := os.Getenv("TEST_PG_SCHEMA_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_SCHEMA_DSN is not configured (scripts/dev-databases.sh up; eval \"$(scripts/dev-databases.sh env)\")")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := os.ReadFile(filepath.Join("..", "..", "sql", "33.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(ddl)).Error; err != nil {
		t.Fatal(err)
	}
	redisAddr := os.Getenv("TEST_REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("TEST_REDIS_ADDR is not configured (the device-config lookup caches in Redis)")
	}
	oldDB, oldRedis := global.DB, global.REDIS
	global.DB = db
	global.REDIS = redis.NewClient(&redis.Options{Addr: redisAddr})
	global.REDIS.FlushDB(context.Background())
	query.SetDefault(db)
	gin.SetMode(gin.TestMode)

	suffix := uuid.NewString()[:8]
	e := &pluginIDEnv{t: t, db: db, tenantA: "tnA" + suffix, tenantB: "tnB" + suffix, service: "svc_" + suffix,
		access: map[string]string{}, device: map[string]string{}, voucher: map[string]string{}}
	now := time.Now().UTC()
	plugin := model.ServicePlugin{ID: "sp" + suffix, Name: "t4 " + suffix, ServiceIdentifier: e.service, ServiceType: 1, CreateAt: now, UpdateAt: now}
	if err := db.Create(&plugin).Error; err != nil {
		t.Fatal(err)
	}
	protocol := e.service
	other := "other_" + suffix
	for _, tenant := range []string{e.tenantA, e.tenantB} {
		cfgID := "cfg" + tenant
		if err := db.Create(&model.DeviceConfig{ID: cfgID, Name: cfgID, DeviceType: "1", ProtocolType: &protocol, TenantID: tenant, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
		e.voucher[tenant] = "v-" + tenant
		e.device[tenant] = "dev" + tenant
		name := "n" + tenant
		createDevice(t, db, model.Device{ID: e.device[tenant], Name: &name, Voucher: e.voucher[tenant], TenantID: tenant, IsEnabled: "enabled",
			ActivateFlag: "active", DeviceNumber: "no-" + tenant, DeviceConfigID: &cfgID, CreatedAt: &now})
		e.access[tenant] = "sa" + tenant
		if err := db.Create(&model.ServiceAccess{ID: e.access[tenant], Name: "a" + tenant, ServicePluginID: plugin.ID, Voucher: "{}",
			CreateAt: now, UpdateAt: now, TenantID: tenant}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// A device of tenant A that uses a different protocol.
	otherCfg := "cfgother" + suffix
	if err := db.Create(&model.DeviceConfig{ID: otherCfg, Name: otherCfg, DeviceType: "1", ProtocolType: &other, TenantID: e.tenantA, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	createDevice(t, db, model.Device{ID: "devother" + suffix, Voucher: "vother" + suffix, TenantID: e.tenantA, IsEnabled: "enabled", ActivateFlag: "active",
		DeviceNumber: "noother" + suffix, DeviceConfigID: &otherCfg, CreatedAt: &now})
	e.voucher["other"] = "vother" + suffix

	r := gin.New()
	// The user-authenticated routes read tenant-admin claims; the test supplies them from a header (the real router
	// uses the JWT middleware).
	asAdmin := func(c *gin.Context) {
		if tenant := c.GetHeader("X-Test-Admin-Tenant"); tenant != "" {
			c.Set("claims", &utils.UserClaims{TenantID: tenant, Authority: "TENANT_ADMIN"})
		} else if tenant := c.GetHeader("X-Test-User-Tenant"); tenant != "" {
			c.Set("claims", &utils.UserClaims{TenantID: tenant, Authority: "TENANT_USER"})
		}
	}
	respond := func(c *gin.Context) {
		if c.IsAborted() {
			return
		}
		data, _ := c.Get("data")
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": data})
	}
	guard := middleware.RequireManagedPluginRuntime()
	pp, sp, sa := &ProtocolPluginApi{}, &ServicePluginApi{}, &ServiceAccessApi{}
	r.POST("/identity", asAdmin, IssuePluginRuntimeIdentity, respond)
	r.POST("/identity/revoke", asAdmin, RevokePluginRuntimeIdentity, respond)
	r.POST("/plugin/heartbeat", guard, sp.Heartbeat, respond)
	r.POST("/plugin/device/config", guard, pp.HandleDeviceConfigForProtocolPlugin, respond)
	r.POST("/plugin/devices", guard, pp.HandleDeviceConfigForProtocolPluginByProtocolType, respond)
	r.POST("/plugin/service/access/list", guard, sa.HandlePluginServiceAccessList, respond)
	r.POST("/plugin/service/access", guard, sa.HandlePluginServiceAccess, respond)
	e.engine = r

	t.Cleanup(func() {
		for _, tenant := range []string{e.tenantA, e.tenantB} {
			db.Where("tenant_id = ?", tenant).Delete(&pluginidentity.Record{})
			deleteTenantDevices(db, tenant)
			db.Where("tenant_id = ?", tenant).Delete(&model.ServiceAccess{})
			db.Where("tenant_id = ?", tenant).Delete(&model.DeviceConfig{})
		}
		db.Where("id = ?", plugin.ID).Delete(&model.ServicePlugin{})
		global.DB, global.REDIS = oldDB, oldRedis
	})
	return e
}

func (e *pluginIDEnv) do(path, auth string, body any, headers map[string]string) (int, map[string]any) {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func (e *pluginIDEnv) issue(tenant, instance string, generation int, service string) (int, string) {
	code, out := e.do("/identity", "", map[string]any{"plugin_instance": instance, "service_identifier": service, "generation": generation, "ttl_hours": 1},
		map[string]string{"X-Test-Admin-Tenant": tenant})
	if code != http.StatusOK {
		return code, ""
	}
	return code, out["data"].(map[string]any)["token"].(string)
}

func errorCodeOf(out map[string]any) string {
	data, _ := out["data"].(map[string]any)
	s, _ := data["errorCode"].(string)
	return s
}

// The five entrypoints and a request body each is valid for under the identity's own service/tenant.
func (e *pluginIDEnv) entrypoints() map[string]map[string]any {
	return map[string]map[string]any{
		"/plugin/heartbeat":           {"service_identifier": e.service},
		"/plugin/device/config":       {"voucher": e.voucher[e.tenantA]},
		"/plugin/devices":             {"service_identifier": e.service, "device_type": "1", "page": 1, "page_size": 10},
		"/plugin/service/access/list": {"service_identifier": e.service},
		"/plugin/service/access":      {"service_access_id": e.access[e.tenantA]},
	}
}

func TestManagedIdentityAllowsAllFiveEntrypointsWithinScope(t *testing.T) {
	e := newPluginIDEnv(t)
	code, token := e.issue(e.tenantA, "inst-1", 1, e.service)
	if code != http.StatusOK || token == "" {
		t.Fatalf("issue failed: %d", code)
	}
	for path, body := range e.entrypoints() {
		if status, out := e.do(path, "Bearer "+token, body, nil); status != http.StatusOK {
			t.Fatalf("%s: want 200, got %d %v", path, status, out)
		}
	}
	// Tenant filtering inside the allowed calls: only tenant A's device and access point are visible.
	_, devices := e.do("/plugin/devices", "Bearer "+token, e.entrypoints()["/plugin/devices"], nil)
	if total := devices["data"].(map[string]any)["total"].(float64); total != 1 {
		t.Fatalf("devices list must contain only the tenant's own device of this protocol, total=%v", total)
	}
	_, list := e.do("/plugin/service/access/list", "Bearer "+token, e.entrypoints()["/plugin/service/access/list"], nil)
	if items := list["data"].([]any); len(items) != 1 {
		t.Fatalf("access list must contain only the tenant's own access point, got %d", len(items))
	}
}

func TestIssueRequiresTenantAdminAndRevokeFencesTheIdentity(t *testing.T) {
	e := newPluginIDEnv(t)
	if code, _ := e.do("/identity", "", map[string]any{"plugin_instance": "i", "service_identifier": e.service, "generation": 1, "ttl_hours": 1}, nil); code != http.StatusForbidden {
		t.Fatalf("anonymous issue must be refused, got %d", code)
	}
	if code, _ := e.do("/identity", "", map[string]any{"plugin_instance": "i", "service_identifier": e.service, "generation": 1, "ttl_hours": 1},
		map[string]string{"X-Test-User-Tenant": e.tenantA}); code != http.StatusForbidden {
		t.Fatalf("non-admin issue must be refused, got %d", code)
	}
	_, token := e.issue(e.tenantA, "inst-1", 1, e.service)
	if status, _ := e.do("/plugin/heartbeat", "Bearer "+token, e.entrypoints()["/plugin/heartbeat"], nil); status != http.StatusOK {
		t.Fatalf("fresh identity must work, got %d", status)
	}
	// Another tenant's administrator cannot revoke it.
	e.do("/identity/revoke", "", map[string]any{"plugin_instance": "inst-1"}, map[string]string{"X-Test-Admin-Tenant": e.tenantB})
	if status, _ := e.do("/plugin/heartbeat", "Bearer "+token, e.entrypoints()["/plugin/heartbeat"], nil); status != http.StatusOK {
		t.Fatalf("a foreign tenant's revoke must not affect the identity, got %d", status)
	}
	code, out := e.do("/identity/revoke", "", map[string]any{"plugin_instance": "inst-1"}, map[string]string{"X-Test-Admin-Tenant": e.tenantA})
	if code != http.StatusOK || out["data"].(map[string]any)["revoked"].(float64) != 1 {
		t.Fatalf("revoke: %d %v", code, out)
	}
	for path, body := range e.entrypoints() {
		status, out := e.do(path, "Bearer "+token, body, nil)
		if status != http.StatusServiceUnavailable || errorCodeOf(out) != "PLUGIN_RUNTIME_IDENTITY_REQUIRED" {
			t.Fatalf("%s after revoke: want 503 PLUGIN_RUNTIME_IDENTITY_REQUIRED, got %d %v", path, status, out)
		}
	}
}

func TestHigherGenerationRevokesLowerAndLowerCannotBeIssued(t *testing.T) {
	e := newPluginIDEnv(t)
	_, gen1 := e.issue(e.tenantA, "inst-1", 1, e.service)
	_, gen2 := e.issue(e.tenantA, "inst-1", 2, e.service)
	if gen1 == "" || gen2 == "" {
		t.Fatal("both generations must be issued")
	}
	for path, body := range e.entrypoints() {
		if status, _ := e.do(path, "Bearer "+gen1, body, nil); status != http.StatusServiceUnavailable {
			t.Fatalf("%s: generation 1 must be fenced after generation 2, got %d", path, status)
		}
		if status, out := e.do(path, "Bearer "+gen2, body, nil); status != http.StatusOK {
			t.Fatalf("%s: generation 2 must work, got %d %v", path, status, out)
		}
	}
	if code, _ := e.issue(e.tenantA, "inst-1", 2, e.service); code != http.StatusConflict {
		t.Fatalf("re-issuing the same generation must conflict, got %d", code)
	}
	if code, _ := e.issue(e.tenantA, "inst-1", 1, e.service); code != http.StatusConflict {
		t.Fatalf("issuing a lower generation must conflict, got %d", code)
	}
	// Same instance name in another tenant is independent.
	if code, _ := e.issue(e.tenantB, "inst-1", 1, e.service); code != http.StatusOK {
		t.Fatalf("another tenant's instance is independent, got %d", code)
	}
	// A row whose own revoked_at was lost (e.g. a manual restore) is still fenced by the newer generation.
	if err := e.db.Model(&pluginidentity.Record{}).Where("tenant_id = ? AND generation = 1", e.tenantA).Update("revoked_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	if status, _ := e.do("/plugin/heartbeat", "Bearer "+gen1, e.entrypoints()["/plugin/heartbeat"], nil); status != http.StatusServiceUnavailable {
		t.Fatalf("a lower generation must stay fenced even without revoked_at, got %d", status)
	}
}

func TestExpiredLeaseIsRejectedOnAllFiveEntrypoints(t *testing.T) {
	e := newPluginIDEnv(t)
	_, token := e.issue(e.tenantA, "inst-1", 1, e.service)
	past := time.Now().UTC().Add(-time.Minute)
	if err := e.db.Model(&pluginidentity.Record{}).Where("tenant_id = ?", e.tenantA).Update("expires_at", past).Error; err != nil {
		t.Fatal(err)
	}
	for path, body := range e.entrypoints() {
		status, out := e.do(path, "Bearer "+token, body, nil)
		if status != http.StatusServiceUnavailable || errorCodeOf(out) != "PLUGIN_RUNTIME_IDENTITY_REQUIRED" {
			t.Fatalf("%s with an expired lease: want 503, got %d %v", path, status, out)
		}
	}
}

func TestCrossTenantAndProtocolMismatchAreRejected(t *testing.T) {
	e := newPluginIDEnv(t)
	_, token := e.issue(e.tenantA, "inst-1", 1, e.service)
	denied := func(name, path string, body map[string]any) {
		t.Helper()
		status, out := e.do(path, "Bearer "+token, body, nil)
		if status != http.StatusForbidden || errorCodeOf(out) != "PLUGIN_RUNTIME_SCOPE_DENIED" {
			t.Fatalf("%s: want 403 PLUGIN_RUNTIME_SCOPE_DENIED, got %d %v", name, status, out)
		}
	}
	// Cross tenant.
	denied("device of another tenant (voucher)", "/plugin/device/config", map[string]any{"voucher": e.voucher[e.tenantB]})
	denied("device of another tenant (id)", "/plugin/device/config", map[string]any{"device_id": e.device[e.tenantB]})
	denied("access point of another tenant", "/plugin/service/access", map[string]any{"service_access_id": e.access[e.tenantB]})
	// Protocol / service mismatch.
	denied("device using another protocol", "/plugin/device/config", map[string]any{"voucher": e.voucher["other"]})
	denied("heartbeat for another service", "/plugin/heartbeat", map[string]any{"service_identifier": "someone_else"})
	denied("device list for another service", "/plugin/devices", map[string]any{"service_identifier": "someone_else", "device_type": "1", "page": 1, "page_size": 10})
	denied("access list for another service", "/plugin/service/access/list", map[string]any{"service_identifier": "someone_else"})
	// Identity issued for a different service than the access point's plugin.
	_, otherSvc := e.issue(e.tenantA, "inst-2", 1, "other_service")
	if status, _ := e.do("/plugin/service/access", "Bearer "+otherSvc, map[string]any{"service_access_id": e.access[e.tenantA]}, nil); status != http.StatusForbidden {
		t.Fatalf("an identity of another service must not read this service's access point, got %d", status)
	}
}

func TestUserTokensAndForgedHeadersCannotImpersonateAPlugin(t *testing.T) {
	e := newPluginIDEnv(t)
	_, token := e.issue(e.tenantA, "inst-1", 1, e.service)
	forged := map[string]string{"X-Plugin-Active": "true", "X-Plugin-Instance": "inst-1", "X-Tenant-Id": e.tenantA, "X-Instance-Id": "inst-1",
		"X-Test-Admin-Tenant": e.tenantA, "x-token": "anything"}
	jwtLike := "Bearer eyJhbGciOiJIUzI1NiJ9.eyJhdXRob3JpdHkiOiJURU5BTlRfQURNSU4ifQ.c2ln"
	cases := map[string]struct {
		auth    string
		headers map[string]string
	}{
		"no credentials":                  {"", nil},
		"forged plugin headers only":      {"", forged},
		"user JWT as bearer":              {jwtLike, forged},
		"non-bearer scheme with token":    {"Basic " + token, nil},
		"raw token without scheme":        {token, nil},
		"right id, wrong secret":          {"Bearer " + strings.Split(token, ".")[0] + "." + strings.Repeat("0", 48), nil},
		"unknown id":                      {"Bearer pri_0000000000000000." + strings.Repeat("0", 48), nil},
		"valid token, id/secret reversed": {"Bearer " + strings.Split(token, ".")[1] + "." + strings.Split(token, ".")[0], nil},
	}
	for name, tc := range cases {
		for path, body := range e.entrypoints() {
			status, out := e.do(path, tc.auth, body, tc.headers)
			if status != http.StatusServiceUnavailable || errorCodeOf(out) != "PLUGIN_RUNTIME_IDENTITY_REQUIRED" {
				t.Fatalf("%s on %s: want 503 PLUGIN_RUNTIME_IDENTITY_REQUIRED, got %d %v", name, path, status, out)
			}
		}
	}
	// The issue endpoints are not reachable by a plugin identity: a plugin token is not a tenant-admin session.
	if code, _ := e.do("/identity", "Bearer "+token, map[string]any{"plugin_instance": "x", "service_identifier": e.service, "generation": 9, "ttl_hours": 1}, nil); code != http.StatusForbidden {
		t.Fatalf("a plugin identity must not be able to issue identities, got %d", code)
	}
}
