package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"project/internal/dal"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

func TestCommunitySessionStatus(t *testing.T) {
	t.Setenv("TP_AUTH_OWNER", "")
	const key = "test-community-session-key"
	oldKey, oldTimeout := viper.Get("jwt.key"), viper.Get("session.timeout")
	viper.Set("jwt.key", key)
	viper.Set("session.timeout", 10)
	oldDB, oldRedis := global.DB, global.REDIS
	t.Cleanup(func() {
		global.DB, global.REDIS = oldDB, oldRedis
		viper.Set("jwt.key", oldKey)
		viper.Set("session.timeout", oldTimeout)
	})

	store := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: store.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	global.REDIS = client
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	global.DB = db
	for _, statement := range []string{
		"CREATE TABLE users (id text PRIMARY KEY, tenant_id text, authority text, status text, auth_version integer)",
		"INSERT INTO users VALUES ('user-1', 'tenant-1', 'TENANT_USER', 'N', 2)",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	token, err := utils.NewJWT([]byte(key)).GenerateToken(utils.UserClaims{
		ID: "user-1", TenantID: "tenant-1", Authority: "TENANT_USER", AuthVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(token, "1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(dal.AppPushSessionKey(token), "linked"); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.GET("/api/v1/auth/session-status", CommunitySessionStatus)
	check := func(token string, want int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session-status", nil)
		request.Header.Set("x-token", token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != want || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("status = %d, want %d, body = %s", response.Code, want, response.Body)
		}
	}
	check("invalid", http.StatusUnauthorized)
	check(token, http.StatusOK)
	for _, key := range []string{token, dal.AppPushSessionKey(token)} {
		if store.TTL(key) != 10*time.Minute {
			t.Fatal("active session did not renew both expiration times")
		}
	}
	t.Setenv("TP_AUTH_OWNER", "encore")
	check(token, http.StatusUnauthorized)
	t.Setenv("TP_AUTH_OWNER", "")
	if err := db.Exec("UPDATE users SET tenant_id = 'moved'").Error; err != nil {
		t.Fatal(err)
	}
	check(token, http.StatusUnauthorized)
	if err := db.Exec("UPDATE users SET tenant_id = 'tenant-1', status = 'F'").Error; err != nil {
		t.Fatal(err)
	}
	check(token, http.StatusUnauthorized)
	global.DB = nil
	check(token, http.StatusServiceUnavailable)
	store.Del(token)
	check(token, http.StatusUnauthorized)
	global.REDIS = nil
	check(token, http.StatusServiceUnavailable)
}
