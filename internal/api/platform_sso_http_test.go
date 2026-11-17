package api

import (
	"context"
	"encoding/json"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"net/http/httptest"
	"project/internal/middleware"
	"project/internal/query"
	"project/pkg/global"
	"project/pkg/utils"
	"testing"
	"time"
)

func TestPlatformIdentityHTTPSession(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	if err := db.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT, name TEXT, tenant_id TEXT, authority TEXT, status TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO users VALUES ('u1','admin@example.com','Admin','t1','SYS_ADMIN','N')`).Error; err != nil {
		t.Fatal(err)
	}
	oldUser, oldRedis := query.User, global.REDIS
	oldKey, oldTimeout := viper.Get("jwt.key"), viper.Get("session.timeout")
	q := query.Use(db)
	query.User = &q.User
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	global.REDIS = client
	t.Cleanup(func() {
		query.User = oldUser
		global.REDIS = oldRedis
		client.Close()
		viper.Set("jwt.key", oldKey)
		viper.Set("session.timeout", oldTimeout)
	})
	const key = "sso-contract-test-A9s7d6F5g4H3j2K1l0ZxCvBnMqWe"
	viper.Set("jwt.key", key)
	viper.Set("session.timeout", 30)
	token, err := utils.NewJWT([]byte(key)).GenerateToken(utils.UserClaims{ID: "u1", TenantID: "t1", Authority: "SYS_ADMIN"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Set(context.Background(), token, "1", time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.GET("/api/v1/auth/sso/identity", middleware.JWTAuth(), PlatformSSOIdentity)
	call := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/v1/auth/sso/identity", nil)
		if token != "" {
			req.Header.Set("x-token", token)
		}
		req.Header.Set("X-User-Id", "victim")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	if w := call(""); w.Code != 401 {
		t.Fatalf("anonymous status=%d", w.Code)
	}
	w := call(token)
	var body struct {
		Code int              `json:"code"`
		Data platformIdentity `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || body.Code != 200 || body.Data.ID != "u1" || body.Data.TenantID != "thingspanel-sys-admin" {
		t.Fatalf("identity contract mismatch: %s", w.Body.String())
	}
	if err := db.Exec(`UPDATE users SET status='F'`).Error; err != nil {
		t.Fatal(err)
	}
	if w := call(token); w.Code != 401 {
		t.Fatalf("disabled user status=%d", w.Code)
	}
}
