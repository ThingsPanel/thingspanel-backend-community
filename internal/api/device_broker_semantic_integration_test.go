package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt"
	"github.com/google/uuid"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/global"
	"project/pkg/utils"
)

func TestBrokerSemanticEvidenceAuthenticatedEncoreLookup(t *testing.T) {
	db, cleanupDB := openSemanticEvidencePG(t)
	defer cleanupDB()
	redisClient := startBrokerAPITestRedis(t)
	previousDB, previousRedis := global.DB, global.REDIS
	global.DB, global.REDIS = db, redisClient
	t.Cleanup(func() { global.DB, global.REDIS = previousDB, previousRedis })

	previousProfile, previousBase, previousDev, previousJWT := viper.Get("mqtt.input_profile"), viper.Get("mqtt.semantic_base_url"), viper.Get("mqtt.semantic_development_http"), viper.Get("jwt.key")
	previousHasProfile, previousHasBase, previousHasDev, previousHasJWT := viper.IsSet("mqtt.input_profile"), viper.IsSet("mqtt.semantic_base_url"), viper.IsSet("mqtt.semantic_development_http"), viper.IsSet("jwt.key")
	t.Cleanup(func() {
		service.CloseBrokerSemanticIdleConnections()
		for _, item := range []struct {
			key   string
			value any
			set   bool
		}{
			{"mqtt.input_profile", previousProfile, previousHasProfile},
			{"mqtt.semantic_base_url", previousBase, previousHasBase},
			{"mqtt.semantic_development_http", previousDev, previousHasDev},
			{"jwt.key", previousJWT, previousHasJWT},
		} {
			if item.set {
				viper.Set(item.key, item.value)
			} else {
				viper.Set(item.key, "")
			}
		}
	})

	const (
		tenant   = "semantic-tenant-a"
		userID   = "semantic-user-a"
		deviceID = "semantic-device-a"
		jwtKey   = "semantic-integration-test-key"
	)
	if err := db.Exec(`INSERT INTO users(id,tenant_id,status,authority) VALUES (?,?,?,?)`, userID, tenant, "N", "TENANT_ADMIN").Error; err != nil {
		t.Fatal("seed user")
	}
	device := model.Device{ID: deviceID, TenantID: tenant, Name: stringPtr("fixture"), Voucher: `{}`, IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "semantic-number", IsOnline: 0}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal("seed device")
	}
	if err := db.Exec(`INSERT INTO broker_transport_bindings(tenant_id,device_id,broker_cell,route_epoch,phase) VALUES (?,?,?,1,'active')`, tenant, deviceID, "semantic-cell").Error; err != nil {
		t.Fatal("seed route")
	}
	token := semanticEvidenceJWT(t, jwtKey, userID, tenant, "TENANT_ADMIN")
	if err := redisClient.Set(context.Background(), token, "1", time.Hour).Err(); err != nil {
		t.Fatal("seed live user session")
	}

	encore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/devices/" + deviceID:
			fmt.Fprintf(w, `{"id":%q,"tenant_id":%q,"device_template_id":"semantic-template-a"}`, deviceID, tenant)
		case "/api/device-templates/semantic-template-a":
			fmt.Fprintf(w, `{"id":"semantic-template-a","tenant_id":%q,"thing_model_id":"semantic-model-a","thing_model_snapshot_id":"semantic-snapshot-a"}`, tenant)
		case "/api/v2/thing-models/semantic-model-a/versions":
			fmt.Fprint(w, `{"items":[{"publishedRevision":5,"snapshotID":"semantic-snapshot-a"}],"page":1,"pageSize":200,"total":1}`)
		case "/api/v2/thing-models/semantic-model-a/versions/5":
			fmt.Fprint(w, `{"modelID":"semantic-model-a","publishedRevision":5,"snapshotID":"semantic-snapshot-a","model":{"name":"Sample","fields":[]},"resources":{"modelID":"semantic-model-a","revision":0,"profile":{},"webChartConfig":{},"appChartConfig":{}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer encore.Close()
	viper.Set("mqtt.input_profile", "ingress_v1")
	viper.Set("mqtt.semantic_base_url", encore.URL)
	viper.Set("mqtt.semantic_development_http", true)
	viper.Set("jwt.key", jwtKey)

	gin.SetMode(gin.TestMode)
	call := func(claims *utils.UserClaims, headerToken string, id string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/device/semantic/evidence/"+id, nil)
		ctx.Params = gin.Params{{Key: "id", Value: id}}
		if headerToken != "" {
			ctx.Request.Header.Set("x-token", headerToken)
		}
		if claims != nil {
			ctx.Set("claims", claims)
		}
		(&DeviceApi{}).BrokerSemanticEvidence(ctx)
		return recorder
	}

	validClaims := &utils.UserClaims{ID: userID, TenantID: tenant, Authority: "TENANT_ADMIN"}
	valid := call(validClaims, token, deviceID)
	if valid.Code != http.StatusOK || !strings.Contains(valid.Body.String(), `"snapshot_id":"semantic-snapshot-a"`) || !strings.Contains(valid.Body.String(), `"resource_revision":0`) || !strings.Contains(valid.Body.String(), `"status":"unknown"`) {
		t.Fatalf("valid same-user semantic evidence status=%d body=%s", valid.Code, valid.Body.String())
	}
	if valid.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("semantic evidence response is cacheable")
	}

	otherTenantClaims := &utils.UserClaims{ID: userID, TenantID: "semantic-tenant-other", Authority: "TENANT_ADMIN"}
	if response := call(otherTenantClaims, token, deviceID); response.Code != http.StatusUnauthorized {
		t.Fatalf("JWT claim mismatch status=%d", response.Code)
	}
	if response := call(validClaims, "", deviceID); response.Code != http.StatusUnauthorized {
		t.Fatalf("API-key/no x-token identity status=%d", response.Code)
	}
	if response := call(validClaims, token, "unknown-device"); response.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant/nonexistent community device status=%d", response.Code)
	}
	for _, columnValue := range []struct{ column, value string }{{"is_enabled", "disabled"}, {"activate_flag", "inactive"}} {
		if err := db.Exec("UPDATE devices SET "+columnValue.column+"=? WHERE id=?", columnValue.value, deviceID).Error; err != nil {
			t.Fatalf("set inactive device fixture: %v", err)
		}
		if response := call(validClaims, token, deviceID); response.Code != http.StatusNotFound {
			t.Fatalf("device %s=%s status=%d, want not found", columnValue.column, columnValue.value, response.Code)
		}
		if err := db.Exec("UPDATE devices SET "+columnValue.column+"=? WHERE id=?", map[string]string{"is_enabled": "enabled", "activate_flag": "active"}[columnValue.column], deviceID).Error; err != nil {
			t.Fatalf("restore active device fixture: %v", err)
		}
	}

	if err := redisClient.Del(context.Background(), token).Err(); err != nil {
		t.Fatal("revoke session")
	}
	if response := call(validClaims, token, deviceID); response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status=%d", response.Code)
	}
	if err := db.Exec("UPDATE users SET status='Y' WHERE id=?", userID).Error; err != nil {
		t.Fatal("disable account")
	}
	newToken := semanticEvidenceJWT(t, jwtKey, userID, tenant, "TENANT_ADMIN")
	if err := redisClient.Set(context.Background(), newToken, "1", time.Hour).Err(); err != nil {
		t.Fatal("seed second session")
	}
	if response := call(validClaims, newToken, deviceID); response.Code != http.StatusUnauthorized {
		t.Fatalf("disabled user status=%d", response.Code)
	}
	if err := db.Exec("UPDATE users SET status='N' WHERE id=?", userID).Error; err != nil {
		t.Fatal("restore fixture account")
	}

	viper.Set("mqtt.semantic_base_url", "")
	if response := call(validClaims, newToken, deviceID); response.Code != http.StatusConflict {
		t.Fatalf("missing semantic config status=%d", response.Code)
	}
}

func openSemanticEvidencePG(t *testing.T) (*gorm.DB, func()) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("TEST_BROKER_DATABASE_DSN"))
	if dsn == "" {
		data, err := os.ReadFile("/tmp/tp-mqtt-20261019/qa-dsn")
		if err != nil {
			t.Skip("isolated QA PostgreSQL DSN unavailable")
		}
		dsn = strings.TrimSpace(string(data))
	}
	if dsn == "" {
		t.Skip("isolated QA PostgreSQL DSN empty")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open isolated QA PostgreSQL")
	}
	adminPool, err := admin.DB()
	if err != nil {
		t.Fatal("access isolated QA pool")
	}
	schema := "semantic_api_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal("create private schema")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("parse isolated DSN")
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		q := u.Query()
		q.Set("options", "-c search_path="+schema)
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		dsn += " search_path=" + schema
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open private schema")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("access private schema pool")
	}
	pool.SetMaxOpenConns(4)
	previousDB := global.DB
	global.DB = db
	if err := db.Exec(`CREATE TABLE users(id text PRIMARY KEY, tenant_id text, status text, authority text)`).Error; err != nil {
		t.Fatal("create minimal users")
	}
	if err := db.AutoMigrate(&model.Device{}); err != nil {
		t.Fatal("migrate devices")
	}
	if err := db.Exec(`
		CREATE TABLE broker_transport_bindings(tenant_id text,device_id text,broker_cell text,route_epoch bigint,phase text,PRIMARY KEY(tenant_id,device_id));
		CREATE TABLE broker_telemetry_activity(tenant_id text,device_id text,transport_device_id text,broker_cell text,route_epoch bigint,receipt_id text,observed_at timestamptz,next_check_at timestamptz,PRIMARY KEY(tenant_id,device_id));
		CREATE TABLE broker_connection_observations(tenant_id text,device_id text,broker_cell text,broker_instance text,node_boot_id text,connection_id text,route_epoch bigint,event_seq bigint,online boolean,observed_at timestamptz,received_at timestamptz,PRIMARY KEY(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id));
	`).Error; err != nil {
		t.Fatal("create brokerstate read fixture")
	}
	cleanup := func() {
		global.DB = previousDB
		_ = pool.Close()
		_ = admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error
		_ = adminPool.Close()
	}
	return db, cleanup
}

func semanticEvidenceJWT(t *testing.T, key, user, tenant, authority string) string {
	t.Helper()
	claims := utils.UserClaims{ID: user, TenantID: tenant, Authority: authority, StandardClaims: jwt.StandardClaims{ExpiresAt: time.Now().Add(time.Hour).Unix(), IssuedAt: time.Now().Add(-time.Minute).Unix()}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(key))
	if err != nil {
		t.Fatal("sign fixture session token")
	}
	return token
}

func stringPtr(value string) *string { return &value }
