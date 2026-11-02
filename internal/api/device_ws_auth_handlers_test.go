package api

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/middleware"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/global"
	"project/pkg/utils"
)

type wsAuthSQLRecorder struct {
	logger.Interface
	mu      sync.Mutex
	queries []string
}

func wsStringPointer(value string) *string { return &value }

func startWSAuthTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server unavailable for isolated websocket auth test")
	}
	dir, err := os.MkdirTemp("/tmp", "wsa-")
	if err != nil {
		t.Fatal("create short isolated websocket Redis directory")
	}
	socketPath := filepath.Join(dir, "r.sock")
	cmd := exec.Command(binary, "--port", "0", "--unixsocket", socketPath, "--unixsocketperm", "700", "--save", "", "--appendonly", "no")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		t.Fatal("start isolated websocket Redis")
	}
	client := redis.NewClient(&redis.Options{Network: "unix", Addr: socketPath, MaxRetries: 0, DialTimeout: 100 * time.Millisecond})
	waitDone := make(chan struct{})
	var processErr error
	go func() {
		processErr = cmd.Wait()
		close(waitDone)
	}()
	t.Cleanup(func() {
		_ = client.Close()
		select {
		case <-waitDone:
		default:
			_ = cmd.Process.Kill()
			<-waitDone
		}
		_ = os.RemoveAll(dir)
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := client.Ping(context.Background()).Err(); err == nil {
			return client
		}
		select {
		case <-waitDone:
			t.Fatalf("isolated websocket Redis exited during startup: %v", processErr)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("isolated websocket Redis Unix socket did not become ready")
	return nil
}

func (r *wsAuthSQLRecorder) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, _ := fc()
	r.mu.Lock()
	r.queries = append(r.queries, strings.ToLower(sql))
	r.mu.Unlock()
	r.Interface.Trace(ctx, begin, fc, err)
}

func (r *wsAuthSQLRecorder) reset() {
	r.mu.Lock()
	r.queries = nil
	r.mu.Unlock()
}

func (r *wsAuthSQLRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.queries...)
}

func TestWSHandlersRejectUnauthorizedBeforeSensitiveReadOrSubscription(t *testing.T) {
	previousGinMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousGinMode) })
	t.Setenv("TP_AUTH_OWNER", "")
	redisClient := startWSAuthTestRedis(t)
	recorder := &wsAuthSQLRecorder{Interface: logger.New(log.New(io.Discard, "", 0), logger.Config{LogLevel: logger.Info})}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: recorder})
	if err != nil {
		t.Fatal("open isolated websocket auth sqlite")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("open isolated sqlite pool")
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&model.Device{}, &model.TelemetryCurrentData{}, &middleware.OpenAPIKey{}); err != nil {
		t.Fatal("migrate isolated websocket auth fixture")
	}
	previousQuery := *query.Q
	previousDevice := query.Device
	previousTelemetryCurrentData := query.TelemetryCurrentData
	*query.Q = *query.Use(db)
	query.Device = &query.Q.Device
	query.TelemetryCurrentData = &query.Q.TelemetryCurrentData
	t.Cleanup(func() {
		*query.Q = previousQuery
		query.Device = previousDevice
		query.TelemetryCurrentData = previousTelemetryCurrentData
	})
	previousDB, previousRedis, previousManager := global.DB, global.REDIS, global.TPWSManager
	global.DB, global.REDIS = db, redisClient
	global.TPWSManager = global.NewWSManager()
	t.Cleanup(func() {
		global.DB, global.REDIS, global.TPWSManager = previousDB, previousRedis, previousManager
	})
	previousJWT, previousTimeout, previousDBType := viper.Get("jwt.key"), viper.Get("session.timeout"), viper.Get("grpc.tptodb_type")
	viper.Set("jwt.key", "isolated-ws-auth-jwt-key")
	viper.Set("session.timeout", 60)
	viper.Set("grpc.tptodb_type", "")
	t.Cleanup(func() {
		viper.Set("jwt.key", previousJWT)
		viper.Set("session.timeout", previousTimeout)
		viper.Set("grpc.tptodb_type", previousDBType)
	})
	devices := []model.Device{
		{ID: "ws-owned", TenantID: "ws-tenant-a", Voucher: `{"secret":"must-not-load"}`, DeviceNumber: "owned-number"},
		{ID: "ws-foreign", TenantID: "ws-tenant-b", Voucher: `{"secret":"must-not-load"}`, DeviceNumber: "foreign-number"},
	}
	if err := db.Create(&devices).Error; err != nil {
		t.Fatal("seed isolated devices")
	}
	if err := db.Create(&middleware.OpenAPIKey{ID: "key-a", TenantID: "ws-tenant-a", APIKey: "ws-api-key-a", Status: 1, CreatedID: "user-a"}).Error; err != nil {
		t.Fatal("seed isolated API key")
	}
	if err := db.Create(&model.TelemetryCurrentData{DeviceID: "ws-foreign", Key: "temperature", T: time.Now(), TenantID: wsStringPointer("ws-tenant-b")}).Error; err != nil {
		t.Fatal("seed isolated telemetry")
	}
	adminToken, err := utils.NewJWT([]byte("isolated-ws-auth-jwt-key")).GenerateToken(utils.UserClaims{ID: "root-ws", Authority: "SYS_ADMIN"})
	if err != nil {
		t.Fatal("sign isolated SYS_ADMIN token")
	}
	if err := redisClient.Set(context.Background(), adminToken, "1", time.Hour).Err(); err != nil {
		t.Fatal("seed isolated token session")
	}

	api := &TelemetryDataApi{}
	cases := []struct {
		name    string
		handler gin.HandlerFunc
		kind    string
		batch   bool
	}{
		{name: "telemetry current", handler: api.ServeCurrentDataByWS, kind: "telemetry"},
		{name: "device status", handler: api.ServeDeviceStatusByWS, kind: "status"},
		{name: "telemetry keys", handler: api.ServeCurrentDataByKey, kind: "telemetry"},
		{name: "batch status", handler: api.ServeDeviceOnlineStatusWS, kind: "status", batch: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := func(payload map[string]interface{}) (*websocket.Conn, <-chan error) {
				router := gin.New()
				done := make(chan error, 1)
				router.GET("/ws", func(c *gin.Context) { tc.handler(c); done <- nil })
				server := httptest.NewServer(router)
				t.Cleanup(server.Close)
				u, _ := url.Parse(server.URL)
				u.Scheme = "ws"
				u.Path = "/ws"
				conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
				if err != nil {
					t.Fatalf("dial websocket: %v", err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				if err := conn.WriteJSON(payload); err != nil {
					t.Fatalf("write initial payload: %v", err)
				}
				return conn, done
			}
			payload := func(deviceID, credentialField, credential string) map[string]interface{} {
				m := map[string]interface{}{}
				if tc.batch {
					if strings.Contains(deviceID, ",") {
						m["device_ids"] = deviceID
					} else {
						m["device_ids"] = []string{deviceID}
					}
				} else {
					m["device_id"] = deviceID
				}
				if tc.name == "telemetry keys" {
					m["keys"] = []string{"temperature"}
				}
				m[credentialField] = credential
				return m
			}
			assertRejected := func(label string, msg map[string]interface{}, expectDenial bool) {
				t.Helper()
				recorder.reset()
				conn, done := request(msg)
				_, response, readErr := conn.ReadMessage()
				if readErr != nil || len(response) == 0 || (expectDenial && !strings.Contains(string(response), "device access denied")) {
					t.Fatalf("%s was not rejected as expected: msg=%q err=%v", label, response, readErr)
				}
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Fatalf("%s handler did not return", label)
				}
				for _, sql := range recorder.snapshot() {
					if strings.Contains(sql, "telemetry_current_datas") || (strings.Contains(sql, "select") && strings.Contains(sql, "devices") && !strings.Contains(sql, "count(")) {
						t.Errorf("%s reached sensitive data read: %s", label, sql)
					}
				}
				for _, deviceID := range []string{"ws-owned", "ws-foreign", "ws-missing"} {
					if got := redisClient.Get(context.Background(), "ws:sub:"+deviceID).Val(); got != "" {
						t.Errorf("%s created manager subscription for %s with count %q", label, deviceID, got)
					}
					if tc.kind == "status" {
						channel := "device:" + deviceID + ":status"
						if got := redisClient.PubSubNumSub(context.Background(), channel).Val()[channel]; got != 0 {
							t.Errorf("%s created Redis status subscription for %s with count %d", label, deviceID, got)
						}
					}
				}
			}

			assertRejected("cross tenant", payload("ws-foreign", "x-api-key", "ws-api-key-a"), true)
			assertRejected("missing device", payload("ws-missing", "x-api-key", "ws-api-key-a"), true)
			if tc.batch {
				assertRejected("mixed batch", map[string]interface{}{"device_ids": []interface{}{"ws-owned", "ws-foreign"}, "x-api-key": "ws-api-key-a"}, true)
				assertRejected("non-string item", map[string]interface{}{"device_ids": []interface{}{"ws-owned", 42}, "x-api-key": "ws-api-key-a"}, false)
				assertRejected("empty item", map[string]interface{}{"device_ids": []interface{}{"ws-owned", ""}, "x-api-key": "ws-api-key-a"}, false)
				assertRejected("empty comma segment", map[string]interface{}{"device_ids": "ws-owned,,ws-foreign", "x-api-key": "ws-api-key-a"}, false)
				assertRejected("invalid list does not fall back", map[string]interface{}{"device_ids": []interface{}{42}, "device_id": "ws-owned", "x-api-key": "ws-api-key-a"}, false)
			}

			identities := []struct{ name, field, value, device, observed string }{
				{name: "tenant owner", field: "x-api-key", value: "ws-api-key-a", device: "ws-owned"},
				{name: "SYS_ADMIN cross tenant", field: "token", value: adminToken, device: "ws-foreign"},
			}
			if tc.batch {
				identities = append(identities, struct{ name, field, value, device, observed string }{name: "tenant owner CSV deduplicates", field: "x-api-key", value: "ws-api-key-a", device: "ws-owned, ws-owned", observed: "ws-owned"})
			}
			for _, identity := range identities {
				t.Run(identity.name, func(t *testing.T) {
					recorder.reset()
					conn, done := request(payload(identity.device, identity.field, identity.value))
					deviceID := identity.observed
					if deviceID == "" {
						deviceID = identity.device
					}
					if identity.observed != "" {
						_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
						_, data, err := conn.ReadMessage()
						var initial []struct {
							DeviceID string `json:"device_id"`
						}
						if err != nil || json.Unmarshal(data, &initial) != nil || len(initial) != 1 || initial[0].DeviceID != deviceID {
							t.Fatalf("CSV initial response not deduplicated: %s, err=%v", data, err)
						}
					}
					waitFor := func() bool {
						if tc.kind == "telemetry" {
							if redisClient.Get(context.Background(), "ws:sub:"+deviceID).Val() != "1" {
								return false
							}
							for _, sql := range recorder.snapshot() {
								if strings.Contains(sql, "telemetry_current_datas") {
									return true
								}
							}
							return false
						}
						return redisClient.PubSubNumSub(context.Background(), "device:"+deviceID+":status").Val()["device:"+deviceID+":status"] == 1
					}
					isSubscribed := func() bool {
						if tc.kind == "telemetry" {
							return redisClient.Get(context.Background(), "ws:sub:"+deviceID).Val() != ""
						}
						return redisClient.PubSubNumSub(context.Background(), "device:"+deviceID+":status").Val()["device:"+deviceID+":status"] > 0
					}
					deadline := time.Now().Add(2 * time.Second)
					for time.Now().Before(deadline) && !waitFor() {
						time.Sleep(10 * time.Millisecond)
					}
					if !waitFor() {
						t.Fatalf("authorized %s request did not reach subscription", identity.name)
					}
					if tc.kind == "telemetry" {
						found := false
						for _, sql := range recorder.snapshot() {
							found = found || strings.Contains(sql, "telemetry_current_datas")
						}
						if !found {
							t.Errorf("authorized %s request did not perform initial telemetry read", identity.name)
						}
					}
					select {
					case <-done:
						t.Errorf("authorized %s handler returned before client close", identity.name)
					default:
					}
					_ = conn.Close()
					select {
					case <-done:
					case <-time.After(2 * time.Second):
						t.Errorf("authorized %s handler did not return after client close", identity.name)
					}
					deadline = time.Now().Add(2 * time.Second)
					for time.Now().Before(deadline) && isSubscribed() {
						time.Sleep(10 * time.Millisecond)
					}
					if isSubscribed() {
						t.Errorf("authorized %s subscription remained after handler exit", identity.name)
					}
					// ServeDeviceStatusByWS currently leaves its local writer goroutine
					// blocked on Send after the handler returns; this test verifies the
					// handler and Redis subscription cleanup, not writer goroutine exit.
				})
			}
		})
	}
}
