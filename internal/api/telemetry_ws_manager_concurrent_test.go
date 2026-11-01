package api

import (
	"context"
	"net"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
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
)

// Pause the real handler after the Redis heartbeat refresh succeeds but before
// RefreshSubscription returns, so writer teardown can close its send queue.
type telemetryPingBarrier struct {
	armed       atomic.Bool
	reached     chan bool
	release     chan struct{}
	releaseOnce sync.Once
}

func (b *telemetryPingBarrier) unblock() { b.releaseOnce.Do(func() { close(b.release) }) }
func (*telemetryPingBarrier) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) { return next(ctx, network, addr) }
}
func (b *telemetryPingBarrier) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if cmd.Name() == "expire" && b.armed.CompareAndSwap(true, false) {
			result, ok := cmd.(*redis.BoolCmd)
			b.reached <- (ok && err == nil && result.Val())
			<-b.release
		}
		return err
	}
}
func (*telemetryPingBarrier) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func TestTelemetryWSWriterFailureConcurrentPing(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	t.Setenv("TP_AUTH_OWNER", "")
	redisClient := startWSAuthTestRedis(t)
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
	if err := db.AutoMigrate(&model.Device{}, &model.TelemetryCurrentData{}, &middleware.OpenAPIKey{}); err != nil {
		t.Fatal(err)
	}
	oldQ, oldDevice, oldCurrent := *query.Q, query.Device, query.TelemetryCurrentData
	*query.Q = *query.Use(db)
	query.Device = &query.Q.Device
	query.TelemetryCurrentData = &query.Q.TelemetryCurrentData
	oldDB, oldRedis, oldManager := global.DB, global.REDIS, global.TPWSManager
	global.DB, global.REDIS = db, redisClient
	global.TPWSManager = global.NewWSManager()
	oldDBType := viper.Get("grpc.tptodb_type")
	viper.Set("grpc.tptodb_type", "")
	t.Cleanup(func() {
		*query.Q = oldQ
		query.Device, query.TelemetryCurrentData = oldDevice, oldCurrent
		global.DB, global.REDIS, global.TPWSManager = oldDB, oldRedis, oldManager
		viper.Set("grpc.tptodb_type", oldDBType)
	})
	if err := db.Create(&model.Device{ID: "telemetry-owned", TenantID: "telemetry-tenant", Voucher: "fixture-voucher", DeviceNumber: "fixture-number"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&middleware.OpenAPIKey{ID: "fixture-key-id", TenantID: "telemetry-tenant", APIKey: "fixture-key", Status: 1, CreatedID: "fixture-user"}).Error; err != nil {
		t.Fatal(err)
	}
	value := float64(1)
	tenant := "telemetry-tenant"
	if err := db.Create(&model.TelemetryCurrentData{DeviceID: "telemetry-owned", Key: "temp", T: time.Now(), NumberV: &value, TenantID: &tenant}).Error; err != nil {
		t.Fatal(err)
	}
	api := &TelemetryDataApi{}
	for _, tc := range []struct {
		name    string
		handler gin.HandlerFunc
		keys    bool
	}{{"current", api.ServeCurrentDataByWS, false}, {"keys", api.ServeCurrentDataByKey, true}} {
		t.Run(tc.name, func(t *testing.T) {
			options := *redisClient.Options()
			caseRedis := redis.NewClient(&options)
			barrier := &telemetryPingBarrier{reached: make(chan bool, 1), release: make(chan struct{})}
			caseRedis.AddHook(barrier)
			global.REDIS = caseRedis
			global.TPWSManager = global.NewWSManager()
			defer func() { barrier.unblock(); _ = caseRedis.Close(); global.REDIS = redisClient }()
			done := make(chan interface{}, 1)
			router := gin.New()
			router.GET("/ws", func(c *gin.Context) { defer func() { done <- recover() }(); tc.handler(c) })
			server := httptest.NewUnstartedServer(router)
			listener := &statusFaultListener{Listener: server.Listener, accepted: make(chan *statusFaultConn, 1)}
			server.Listener = listener
			server.Start()
			defer func() {
				barrier.unblock()
				server.Close()
			}()
			u, _ := url.Parse(server.URL)
			u.Scheme = "ws"
			u.Path = "/ws"
			conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			serverConn := <-listener.accepted
			initial := map[string]interface{}{"device_id": "telemetry-owned", "x-api-key": "fixture-key"}
			if tc.keys {
				initial["keys"] = []string{"temp"}
			}
			if err := conn.WriteJSON(initial); err != nil {
				t.Fatal(err)
			}
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			if _, _, err := conn.ReadMessage(); err != nil {
				t.Fatalf("initial frame: %v", err)
			}
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) && (!serverConn.reading.Load() || global.TPWSManager.GetStats()["total_clients"] != 1) {
				time.Sleep(10 * time.Millisecond)
			}
			if !serverConn.reading.Load() || global.TPWSManager.GetStats()["total_clients"] != 1 {
				t.Fatal("handler did not reach subscribed read")
			}
			barrier.armed.Store(true)
			if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
				t.Fatal("client ping was not sent")
			}
			select {
			case ok := <-barrier.reached:
				if !ok {
					t.Fatal("heartbeat refresh did not succeed before the barrier")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("handler did not reach heartbeat barrier")
			}
			serverConn.failWrite.Store(true)
			global.TPWSManager.PushToDevice("telemetry-owned", map[string]interface{}{"temp": 1})
			deadline = time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) && (serverConn.failedWrites.Load() == 0 || global.TPWSManager.GetStats()["total_clients"] != 0) {
				time.Sleep(10 * time.Millisecond)
			}
			if serverConn.failedWrites.Load() == 0 || global.TPWSManager.GetStats()["total_clients"] != 0 {
				t.Fatal("writer failure and unsubscribe did not complete before release")
			}
			// The handler now resumes the same ping after the managed queue is closed.
			barrier.unblock()
			select {
			case panicValue := <-done:
				if panicValue != nil {
					t.Fatalf("handler panicked: %v", panicValue)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("handler did not exit after write failure and ping barrier")
			}
			if serverConn.failedWrites.Load() == 0 {
				t.Fatal("server write failure was not observed")
			}
			if global.TPWSManager.GetStats()["total_clients"] != 0 {
				t.Fatal("manager still has client")
			}
			if n := redisClient.Exists(context.Background(), "ws:sub:telemetry-owned").Val(); n != 0 {
				t.Fatalf("subscription key remained: %d", n)
			}
		})
	}
}
