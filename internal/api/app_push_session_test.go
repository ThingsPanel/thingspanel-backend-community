package api

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt"
	"github.com/spf13/viper"
	"net/http"
	"net/http/httptest"
	"project/internal/dal"
	"project/internal/middleware"
	"project/internal/service"
	"project/pkg/global"
	"project/pkg/utils"
	"testing"
	"time"
)

func TestAppPushSessionRefreshAndRevocation(t *testing.T) {
	old := global.REDIS
	global.REDIS = startNotificationIdentityRedis(t)
	t.Cleanup(func() { global.REDIS = old })
	key, timeout := viper.Get("jwt.key"), viper.Get("session.timeout")
	viper.Set("jwt.key", "app-session-fixture")
	viper.Set("session.timeout", 60)
	t.Cleanup(func() { viper.Set("jwt.key", key); viper.Set("session.timeout", timeout) })
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, utils.UserClaims{ID: "member", TenantID: "tenant", StandardClaims: jwt.StandardClaims{ExpiresAt: time.Now().Add(5 * time.Minute).Unix()}}).SignedString([]byte("app-session-fixture"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	global.REDIS.Set(ctx, token, "1", time.Minute)
	marker := dal.AppPushSessionKey(token)
	global.REDIS.Set(ctx, marker, "1", time.Minute)
	engine := gin.New()
	engine.GET("/session", middleware.JWTAuth(), func(c *gin.Context) { c.Status(204) })
	request := func(value string) int {
		r := httptest.NewRequest("GET", "/session", nil)
		r.Header.Set("x-token", value)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		return w.Code
	}
	if request(token) != 204 {
		t.Fatal("valid session rejected")
	}
	ttl := global.REDIS.TTL(ctx, marker).Val()
	if ttl < 4*time.Minute || ttl > 5*time.Minute {
		t.Fatal("device TTL not capped at JWT expiry", ttl)
	}
	if err := (&service.User{}).Logout(token); err != nil {
		t.Fatal(err)
	}
	if request(token) != 401 || global.REDIS.Exists(ctx, marker).Val() != 0 {
		t.Fatal("logout marker was resurrected")
	}
	global.REDIS.Set(ctx, "invalid-token", "1", time.Minute)
	if request("invalid-token") != 401 || global.REDIS.TTL(ctx, "invalid-token").Val() > time.Minute {
		t.Fatal("invalid JWT refreshed session")
	}
}

func TestAppDirectoryRequiresScopedCredential(t *testing.T) {
	reader := NewNotificationMemberReader(NotificationMemberReaderConfig{Token: "fixture-token-at-least-32-characters", AllowedDeploymentIDs: []string{"source"}, AllowedTenantIDs: []string{"tenant"}})
	engine := gin.New()
	engine.GET("/devices/:id", reader.GetAppDevice)
	for _, tc := range []struct {
		token, deployment, query string
		status                   int
	}{
		{"", "source", "tenantId=tenant", 401},
		{"fixture-token-at-least-32-characters", "other", "tenantId=tenant", 403},
		{"fixture-token-at-least-32-characters", "source", "tenantId=other", 403},
		{"fixture-token-at-least-32-characters", "source", "tenantId=tenant&tenantId=other", 400},
	} {
		r := httptest.NewRequest(http.MethodGet, "/devices/device?"+tc.query, nil)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		r.Header.Set("X-Notification-Deployment", tc.deployment)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("private device route scope", w.Code)
		}
	}
}
