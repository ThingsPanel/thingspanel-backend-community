package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/global"
	"project/pkg/utils"
)

// Only the session store is simulated; requests run the production registration,
// JWT signature verification, handler and tenant-scoped DAL query.
type diagnosticSessionHook struct{ sessions map[string]bool }

func (h diagnosticSessionHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h diagnosticSessionHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h diagnosticSessionHook) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		switch cmd.Name() {
		case "get":
			if h.sessions[fmt.Sprint(cmd.Args()[1])] {
				cmd.(*redis.StringCmd).SetVal("1")
				return nil
			}
			cmd.SetErr(redis.Nil)
			return redis.Nil
		case "eval":
			cmd.(*redis.Cmd).SetVal(int64(1))
			return nil
		default:
			return fmt.Errorf("unexpected Redis access: %s", cmd.Name())
		}
	}
}

func TestDeviceDiagnosticsAuthenticatedTenantBoundary(t *testing.T) {
	t.Setenv("TP_AUTH_OWNER", "")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&model.Device{}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []model.Device{{ID: "device-a", TenantID: "tenant-a"}, {ID: "device-b", TenantID: "tenant-b"}} {
		if err := db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	oldQuery, oldDevice, oldRedis := *query.Q, query.Device, global.REDIS
	oldKey := viper.Get("jwt.key")
	*query.Q = *query.Use(db)
	query.Device = &query.Q.Device
	global.REDIS = redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() {
		_ = global.REDIS.Close()
		global.REDIS = oldRedis
		*query.Q = oldQuery
		query.Device = oldDevice
		viper.Set("jwt.key", oldKey)
	})
	viper.Set("jwt.key", "diagnostics-test-signing-key")
	token, err := utils.NewJWT([]byte(viper.GetString("jwt.key"))).GenerateToken(utils.UserClaims{ID: "user-a", TenantID: "tenant-a", Authority: "TENANT_ADMIN"})
	if err != nil {
		t.Fatal(err)
	}
	wrongSignature, err := utils.NewJWT([]byte("different-key")).GenerateToken(utils.UserClaims{ID: "user-a", TenantID: "tenant-a", Authority: "TENANT_ADMIN"})
	if err != nil {
		t.Fatal(err)
	}
	global.REDIS.AddHook(diagnosticSessionHook{map[string]bool{token: true, wrongSignature: true}})
	r := gin.New()
	// Minimal response serializer keeps the exact data/error boundary observable.
	r.Use(func(c *gin.Context) {
		c.Next()
		if c.Writer.Written() {
			return
		}
		if len(c.Errors) > 0 {
			c.JSON(403, gin.H{"error": "device access denied"})
			return
		}
		if data, ok := c.Get("data"); ok {
			c.JSON(200, data)
		}
	})
	registerDeviceDiagnosticsRoute(r.Group("/api/v1"))
	for _, tc := range []struct {
		name, token, target string
		status              int
	}{
		{"anonymous", "", "device-a", 401},
		{"unknown session", "bad-token", "device-a", 401},
		{"invalid signature", wrongSignature, "device-a", 401},
		{"own tenant", token, "device-a", 200},
		{"other tenant", token, "device-b", 403},
		{"absent device", token, "absent", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+tc.target+"/diagnostics", nil)
			if tc.token != "" {
				req.Header.Set("x-token", tc.token)
			}
			r.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var body map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.status == 200 {
				if body["device_id"] != "device-a" {
					t.Fatalf("own diagnostic unavailable: %s", w.Body.String())
				}
			} else {
				for _, field := range []string{"device_id", "stats", "recent_failures", "data"} {
					if _, ok := body[field]; ok {
						t.Fatalf("denial leaked %s", field)
					}
				}
			}
		})
	}
}
