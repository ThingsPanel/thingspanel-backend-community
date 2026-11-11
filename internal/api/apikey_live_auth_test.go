package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/middleware"
	"project/internal/model"
	"project/pkg/global"
	"project/pkg/utils"
)

func startLiveAPIKeyRedis(t *testing.T) *redis.Client {
	t.Helper()
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server not available")
	}
	dir, err := os.MkdirTemp("/tmp", "livekey-")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "r.sock")
	cmd := exec.Command(binary, "--port", "0", "--unixsocket", socket, "--unixsocketperm", "700", "--save", "", "--appendonly", "no")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		t.Fatal("start isolated Redis")
	}
	client := redis.NewClient(&redis.Options{Network: "unix", Addr: socket, MaxRetries: 0, DialTimeout: 100 * time.Millisecond})
	t.Cleanup(func() { _ = client.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait(); _ = os.RemoveAll(dir) })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if client.Ping(context.Background()).Err() == nil {
			return client
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("isolated Redis did not become ready")
	return nil
}

func TestAPIKeyLiveDatabaseAuthentication(t *testing.T) {
	var trace, appLogs bytes.Buffer
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.New(log.New(&trace, "", 0), logger.Config{LogLevel: logger.Info})})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&model.OpenAPIKey{}); err != nil {
		t.Fatal(err)
	}
	redisClient := startLiveAPIKeyRedis(t)
	oldDB, oldRedis, oldGin := global.DB, global.REDIS, gin.Mode()
	global.DB, global.REDIS = db, redisClient
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { global.DB, global.REDIS = oldDB, oldRedis; gin.SetMode(oldGin) })
	previousOutput := logrus.StandardLogger().Out
	logrus.SetOutput(&appLogs)
	t.Cleanup(func() { logrus.SetOutput(previousOutput) })
	key := "synthetic-key-do-not-log"
	creator := "creator-a"
	active := int16(1)
	row := model.OpenAPIKey{ID: "key-id", TenantID: "tenant-a", APIKey: key, Status: &active, Name: "fixture", CreatedID: &creator}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal("seed key")
	}
	for k, v := range map[string]string{"apikey:" + key: "tenant-a", "apikey:createdid:" + key: "creator-a", "apikey:info:" + key: `{"id":"key-id","tenant_id":"tenant-a","status":1,"name":"fixture","created_id":"creator-a"}`} {
		if err := redisClient.Set(context.Background(), k, v, time.Hour).Err(); err != nil {
			t.Fatal("seed legacy cache")
		}
	}
	trace.Reset()
	appLogs.Reset()
	engine := gin.New()
	engine.Use(middleware.JWTAuth())
	handlerCalls := 0
	engine.GET("/", func(c *gin.Context) {
		handlerCalls++
		claims := c.MustGet("claims").(*utils.UserClaims)
		c.JSON(http.StatusOK, gin.H{"id": claims.ID, "tenant": claims.TenantID, "authority": claims.Authority})
	})
	httpCall := func(token string) (int, map[string]interface{}) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("x-api-key", key)
		if token != "" {
			req.Header.Set("x-token", token)
		}
		out := httptest.NewRecorder()
		engine.ServeHTTP(out, req)
		var body map[string]interface{}
		if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
			t.Fatal("invalid HTTP response")
		}
		return out.Code, body
	}
	assertAllowed := func(wantCreator string) {
		t.Helper()
		status, body := httpCall("")
		if status != 200 || body["id"] != wantCreator || body["tenant"] != "tenant-a" || body["authority"] != "TENANT_ADMIN" {
			t.Fatalf("HTTP valid key status=%d body=%v", status, body)
		}
		claims, err := validateAuth(map[string]interface{}{"x-api-key": key})
		if err != nil || claims.ID != wantCreator || claims.TenantID != "tenant-a" || claims.Authority != "TENANT_ADMIN" {
			t.Fatalf("WS valid key claims=%v err=%v", claims, err)
		}
	}
	assertRejected := func() {
		t.Helper()
		before := handlerCalls
		status, body := httpCall("")
		if status != 401 || body["code"] != float64(middleware.ErrCodeInvalidAPIKey) || handlerCalls != before {
			t.Fatalf("HTTP rejection status=%d body=%v", status, body)
		}
		if claims, err := validateAuth(map[string]interface{}{"x-api-key": key}); err == nil || claims != nil {
			t.Fatal("WS accepted revoked or invalid key")
		}
	}
	assertAllowed("creator-a")
	global.REDIS = nil
	assertAllowed("creator-a")
	global.REDIS = redisClient
	if status, _ := httpCall("invalid-jwt"); status != 401 {
		t.Fatalf("JWT priority changed: %d", status)
	}
	if claims, err := validateAuth(map[string]interface{}{"token": "invalid-jwt", "x-api-key": key}); err != nil || claims.ID != "creator-a" {
		t.Fatal("WS token fallback changed")
	}
	for _, values := range []map[string]interface{}{{"status": 0}, {"status": nil}, {"status": 1, "created_id": nil}, {"status": 1, "created_id": "   "}} {
		if err := db.Session(&gorm.Session{Logger: logger.Discard}).Model(&model.OpenAPIKey{}).Where("id=?", row.ID).Updates(values).Error; err != nil {
			t.Fatal("change key state")
		}
		assertRejected()
	}
	for _, tenant := range []string{"", "   "} {
		if err := db.Session(&gorm.Session{Logger: logger.Discard}).Model(&model.OpenAPIKey{}).Where("id=?", row.ID).Updates(map[string]interface{}{"status": 1, "created_id": "creator-a", "tenant_id": tenant}).Error; err != nil {
			t.Fatal("change key tenant")
		}
		assertRejected()
	}
	if err := db.Session(&gorm.Session{Logger: logger.Discard}).Model(&model.OpenAPIKey{}).Where("id=?", row.ID).Update("tenant_id", "tenant-a").Error; err != nil {
		t.Fatal("restore key tenant")
	}
	if err := db.Session(&gorm.Session{Logger: logger.Discard}).Model(&model.OpenAPIKey{}).Where("id=?", row.ID).Updates(map[string]interface{}{"status": 1, "created_id": "creator-b"}).Error; err != nil {
		t.Fatal("change creator")
	}
	assertAllowed("creator-b")
	if err := db.Session(&gorm.Session{Logger: logger.Discard}).Delete(&model.OpenAPIKey{}, "id=?", row.ID).Error; err != nil {
		t.Fatal("delete key")
	}
	assertRejected()
	if err := db.Migrator().DropTable(&model.OpenAPIKey{}); err != nil {
		t.Fatal("remove isolated table")
	}
	assertRejected()
	global.DB = nil
	assertRejected()
	global.DB = db
	if strings.Contains(trace.String(), key) || strings.Contains(appLogs.String(), key) {
		t.Fatal("API key appeared in logs")
	}
}
