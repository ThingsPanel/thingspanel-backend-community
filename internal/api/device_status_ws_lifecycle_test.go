package api

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/middleware"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/global"
)

type statusFaultConn struct {
	net.Conn
	failWrite     atomic.Bool
	reading       atomic.Bool
	failedWrites  atomic.Int64
	blockWrite    atomic.Bool
	blocked       chan struct{}
	writeExited   chan struct{}
	closed        chan struct{}
	closeOnce     sync.Once
	blockedOnce   sync.Once
	writeExitOnce sync.Once
}

func (c *statusFaultConn) Read(p []byte) (int, error) {
	c.reading.Store(true)
	defer c.reading.Store(false)
	return c.Conn.Read(p)
}
func (c *statusFaultConn) Write(p []byte) (int, error) {
	if c.blockWrite.Load() {
		c.blockedOnce.Do(func() { close(c.blocked) })
		<-c.closed
		c.writeExitOnce.Do(func() { close(c.writeExited) })
		return 0, errors.New("synthetic blocked write released by close")
	}
	if c.failWrite.Load() {
		c.failedWrites.Add(1)
		return 0, errors.New("synthetic WebSocket write failure")
	}
	return c.Conn.Write(p)
}
func (c *statusFaultConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

type statusFaultListener struct {
	net.Listener
	accepted chan *statusFaultConn
}

func (l *statusFaultListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	wrapped := &statusFaultConn{Conn: c, blocked: make(chan struct{}), writeExited: make(chan struct{}), closed: make(chan struct{})}
	l.accepted <- wrapped
	return wrapped, nil
}

type statusTimeoutHook struct{ count atomic.Int64 }

func (*statusTimeoutHook) Levels() []logrus.Level { return logrus.AllLevels }
func (h *statusTimeoutHook) Fire(entry *logrus.Entry) error {
	if entry.Message == "status WebSocket shutdown timed out" {
		h.count.Add(1)
	}
	return nil
}

func TestStatusWSLifecycle(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	timeouts := &statusTimeoutHook{}
	oldHooks := logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))
	logrus.AddHook(timeouts)
	t.Cleanup(func() { logrus.StandardLogger().ReplaceHooks(oldHooks) })
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
	if err := db.AutoMigrate(&model.Device{}, &middleware.OpenAPIKey{}); err != nil {
		t.Fatal(err)
	}
	oldQ, oldDevice := *query.Q, query.Device
	*query.Q = *query.Use(db)
	query.Device = &query.Q.Device
	oldDB, oldRedis := global.DB, global.REDIS
	global.DB, global.REDIS = db, redisClient
	t.Cleanup(func() { *query.Q = oldQ; query.Device = oldDevice; global.DB, global.REDIS = oldDB, oldRedis })
	if err := db.Create(&model.Device{ID: "status-owned", TenantID: "status-tenant", Voucher: "status-voucher", DeviceNumber: "status-number"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&middleware.OpenAPIKey{ID: "status-key-id", TenantID: "status-tenant", APIKey: "status-test-key", Status: 1, CreatedID: "status-user"}).Error; err != nil {
		t.Fatal(err)
	}

	api := &TelemetryDataApi{}
	for _, tc := range []struct {
		name    string
		handler gin.HandlerFunc
		batch   bool
	}{{"single", api.ServeDeviceStatusByWS, false}, {"batch", api.ServeDeviceOnlineStatusWS, true}} {
		for _, kind := range []string{"client-close", "write-failure", "blocked-write-peer-close"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				done := make(chan struct{})
				router := gin.New()
				router.GET("/ws", func(c *gin.Context) { defer close(done); tc.handler(c) })
				server := httptest.NewUnstartedServer(router)
				listener := &statusFaultListener{Listener: server.Listener, accepted: make(chan *statusFaultConn, 1)}
				server.Listener = listener
				server.Start()
				defer server.Close()
				u, _ := url.Parse(server.URL)
				u.Scheme = "ws"
				u.Path = "/ws"
				conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				serverConn := <-listener.accepted
				initial := map[string]interface{}{"x-api-key": "status-test-key"}
				if tc.batch {
					initial["device_ids"] = []string{"status-owned"}
				} else {
					initial["device_id"] = "status-owned"
				}
				if err := conn.WriteJSON(initial); err != nil {
					t.Fatal(err)
				}
				_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
				_, initialFrame, err := conn.ReadMessage()
				if err != nil {
					t.Fatalf("initial frame: %v", err)
				}
				if tc.batch {
					var rows []map[string]interface{}
					if json.Unmarshal(initialFrame, &rows) != nil || len(rows) != 1 || rows[0]["device_id"] != "status-owned" {
						t.Fatalf("batch initial=%s", initialFrame)
					}
				} else {
					var row map[string]interface{}
					if json.Unmarshal(initialFrame, &row) != nil || row["is_online"] != float64(0) {
						t.Fatalf("single initial=%s", initialFrame)
					}
				}
				channel := "device:status-owned:status"
				wait := func(pred func() bool, label string) {
					t.Helper()
					deadline := time.Now().Add(3 * time.Second)
					for time.Now().Before(deadline) {
						if pred() {
							return
						}
						time.Sleep(10 * time.Millisecond)
					}
					t.Fatalf("timed out waiting for %s", label)
				}
				wait(func() bool { return redisClient.PubSubNumSub(context.Background(), channel).Val()[channel] == 1 }, "subscription")
				if kind == "client-close" {
					if err := redisClient.Publish(context.Background(), channel, `{"is_online":1}`).Err(); err != nil {
						t.Fatal(err)
					}
					_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
					_, update, err := conn.ReadMessage()
					if err != nil {
						t.Fatalf("status update: %v", err)
					}
					var row map[string]interface{}
					if json.Unmarshal(update, &row) != nil || row["is_online"] != float64(1) {
						t.Fatalf("status update=%s", update)
					}
					if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
						t.Fatal(err)
					}
					_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
					_, pong, err := conn.ReadMessage()
					if err != nil || string(pong) != "pong" {
						t.Fatalf("pong=%q err=%v", pong, err)
					}
					_ = conn.Close()
				} else if kind == "write-failure" {
					wait(func() bool { return serverConn.reading.Load() }, "server blocked read")
					serverConn.failWrite.Store(true)
					if err := redisClient.Publish(context.Background(), channel, `{"is_online":1}`).Err(); err != nil {
						t.Fatal(err)
					}
				} else {
					wait(func() bool { return serverConn.reading.Load() }, "server blocked read")
					serverConn.blockWrite.Store(true)
					if err := redisClient.Publish(context.Background(), channel, `{"is_online":1}`).Err(); err != nil {
						t.Fatal(err)
					}
					select {
					case <-serverConn.blocked:
					case <-time.After(3 * time.Second):
						t.Fatal("server write did not enter the barrier")
					}
					_ = conn.Close()
				}
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("handler or workers did not exit")
				}
				if kind == "write-failure" && serverConn.failedWrites.Load() == 0 {
					t.Fatal("write failure was not injected")
				}
				if kind == "blocked-write-peer-close" {
					select {
					case <-serverConn.writeExited:
					default:
						t.Fatal("blocked write did not exit")
					}
				}
				if timeouts.count.Load() != 0 {
					t.Fatal("worker join timed out")
				}
				wait(func() bool { return redisClient.PubSubNumSub(context.Background(), channel).Val()[channel] == 0 }, "PubSub exit")
			})
		}
	}
}
