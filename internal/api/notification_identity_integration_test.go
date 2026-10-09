package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"project/internal/middleware"
	"project/internal/query"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type notificationSessionHelperHandshake struct {
	Address    string `json:"address"`
	Bearer     string `json:"bearer"`
	JWTKey     string `json:"jwtKey"`
	ControlKey string `json:"controlKey"`
}

type notificationSessionHelperCommand struct {
	Action string `json:"action"`
}

// TestNotificationSessionHTTPSubprocessHelper is an opt-in, test-only server
// launched by the notification core integration test. It publishes fixture
// connection details through a 0600 handshake file; no JWT or key is logged.
func TestNotificationSessionHTTPSubprocessHelper(t *testing.T) {
	if os.Getenv("NOTIFICATION_SESSION_HELPER_MODE") != "serve" {
		t.Skip("subprocess helper mode is opt-in")
	}
	db := openNotificationIdentityPGFixture(t)
	redisClient := startNotificationIdentityRedis(t)
	previousRedis := global.REDIS
	global.REDIS = redisClient
	t.Cleanup(func() { global.REDIS = previousRedis })

	const jwtKey = "fixture-only-cross-process-session-jwt-key"
	previousJWTKey := viper.GetString("jwt.key")
	previousSessionTimeout := viper.GetInt("session.timeout")
	viper.Set("jwt.key", jwtKey)
	viper.Set("session.timeout", 30)
	t.Cleanup(func() {
		viper.Set("jwt.key", previousJWTKey)
		viper.Set("session.timeout", previousSessionTimeout)
	})
	if err := db.Exec(`INSERT INTO users (id, tenant_id, status, authority) VALUES (?, ?, ?, ?)`, "identity-cross-process", "tenant-cross-process", "N", "TENANT_ADMIN").Error; err != nil {
		t.Fatal("seed isolated cross-process identity user")
	}
	bearer := notificationSessionJWT(t, jwtKey, "identity-cross-process", "tenant-cross-process", "TENANT_ADMIN", time.Now().Add(time.Hour))
	if err := redisClient.Set(context.Background(), bearer, "1", time.Hour).Err(); err != nil {
		t.Fatal("seed isolated cross-process Redis session")
	}
	controlKeyBytes := make([]byte, 32)
	if _, err := rand.Read(controlKeyBytes); err != nil {
		t.Fatal("create private control key")
	}
	controlKey := hex.EncodeToString(controlKeyBytes)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("listen on an owned random loopback address")
	}
	defer listener.Close()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/api/v1/notification/session-context", middleware.JWTAuth(), (&NotificationIdentityApi{}).SessionContext)
	stop := make(chan struct{})
	var stopOnce sync.Once
	engine.POST("/__test/session-control", func(c *gin.Context) {
		if c.ClientIP() != "127.0.0.1" || c.GetHeader("Authorization") != "Bearer "+controlKey {
			c.Status(http.StatusUnauthorized)
			return
		}
		var command notificationSessionHelperCommand
		decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1024))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&command) != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		var updateErr error
		switch command.Action {
		case "revoke":
			updateErr = redisClient.Del(c.Request.Context(), bearer).Err()
		case "restore-session":
			updateErr = redisClient.Set(c.Request.Context(), bearer, "1", time.Hour).Err()
		case "change-role":
			updateErr = db.Exec(`UPDATE users SET authority='TENANT_USER' WHERE id='identity-cross-process'`).Error
		case "change-tenant":
			updateErr = db.Exec(`UPDATE users SET tenant_id='tenant-other' WHERE id='identity-cross-process'`).Error
		case "restore-identity":
			updateErr = db.Exec(`UPDATE users SET status='N', authority='TENANT_ADMIN', tenant_id='tenant-cross-process' WHERE id='identity-cross-process'`).Error
		case "stop":
			c.Status(http.StatusNoContent)
			stopOnce.Do(func() { close(stop) })
			return
		default:
			c.Status(http.StatusBadRequest)
			return
		}
		if updateErr != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	server := &http.Server{Handler: engine, ReadHeaderTimeout: 2 * time.Second}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})

	handshakePath := os.Getenv("NOTIFICATION_SESSION_HELPER_HANDSHAKE")
	if handshakePath == "" {
		t.Fatal("private session helper handshake path is required")
	}
	handshake := notificationSessionHelperHandshake{Address: "http://" + listener.Addr().String(), Bearer: bearer, JWTKey: jwtKey, ControlKey: controlKey}
	data, err := json.Marshal(handshake)
	if err != nil {
		t.Fatal("encode private session helper handshake")
	}
	temporary := handshakePath + ".tmp"
	if err := os.WriteFile(temporary, data, 0600); err != nil {
		t.Fatal("write private session helper handshake")
	}
	if err := os.Rename(temporary, handshakePath); err != nil {
		t.Fatal("publish private session helper handshake")
	}

	select {
	case <-stop:
	case err := <-serveDone:
		if err != nil && err != http.ErrServerClosed {
			t.Fatalf("session helper HTTP server stopped unexpectedly: %v", err)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("session helper exceeded its hard lifetime")
	}
}

const approvedNotificationIdentityTestDSN = "postgres://notification_test:fixture-only-password@127.0.0.1:25543/notification_test"

func TestNotificationSessionContextChecksLiveJWTRedisAndUserState(t *testing.T) {
	db := openNotificationIdentityPGFixture(t)
	redisClient := startNotificationIdentityRedis(t)
	previousRedis := global.REDIS
	global.REDIS = redisClient
	t.Cleanup(func() { global.REDIS = previousRedis })

	const jwtKey = "fixture-only-notification-session-jwt-key"
	previousJWTKey := viper.GetString("jwt.key")
	previousSessionTimeout := viper.GetInt("session.timeout")
	viper.Set("jwt.key", jwtKey)
	viper.Set("session.timeout", 30)
	t.Cleanup(func() {
		viper.Set("jwt.key", previousJWTKey)
		viper.Set("session.timeout", previousSessionTimeout)
	})

	if err := db.Exec(`INSERT INTO users (id, tenant_id, status, authority) VALUES (?, ?, ?, ?)`, "identity-user", "tenant-identity", "N", "TENANT_ADMIN").Error; err != nil {
		t.Fatal("seed isolated identity user")
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/api/v1/notification/session-context", middleware.JWTAuth(), (&NotificationIdentityApi{}).SessionContext)
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	request := func(tokenHeader, apiKey string) (int, http.Header, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/notification/session-context", nil)
		if err != nil {
			t.Fatal("build session-context request")
		}
		if tokenHeader != "" {
			req.Header.Set("x-token", tokenHeader)
		}
		if apiKey != "" {
			req.Header.Set("x-api-key", apiKey)
		}
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal("call isolated session-context HTTP server")
		}
		defer response.Body.Close()
		var payload map[string]any
		if response.Body != nil {
			_ = json.NewDecoder(io.LimitReader(response.Body, 1<<16)).Decode(&payload)
		}
		return response.StatusCode, response.Header, payload
	}
	validToken := notificationSessionJWT(t, jwtKey, "identity-user", "tenant-identity", "TENANT_ADMIN", time.Now().Add(time.Hour))
	if err := redisClient.Set(context.Background(), validToken, "1", time.Hour).Err(); err != nil {
		t.Fatal("seed active JWT session")
	}
	status, headers, payload := request(validToken, "")
	if status != http.StatusOK || payload["userId"] != "identity-user" || payload["tenantId"] != "tenant-identity" || payload["authority"] != "TENANT_ADMIN" || headers.Get("Cache-Control") != "no-store" || len(payload) != 3 {
		t.Fatalf("valid session context mismatch: status=%d payload=%v headers=%v", status, payload, headers)
	}

	// JWTs that still have a Redis marker but have expired must fail JWT parsing.
	expiredJWT := notificationSessionJWT(t, jwtKey, "identity-user", "tenant-identity", "TENANT_ADMIN", time.Now().Add(-time.Minute))
	if err := redisClient.Set(context.Background(), expiredJWT, "1", time.Hour).Err(); err != nil {
		t.Fatal("seed expired JWT session marker")
	}
	if status, _, _ := request(expiredJWT, ""); status != http.StatusUnauthorized {
		t.Fatalf("expired JWT was accepted: status=%d", status)
	}
	// A non-expired JWT without its live Redis marker is a revoked/expired session.
	revokedToken := notificationSessionJWT(t, jwtKey, "identity-user", "tenant-identity", "TENANT_ADMIN", time.Now().Add(time.Hour))
	if status, _, _ := request(revokedToken, ""); status != http.StatusUnauthorized {
		t.Fatalf("JWT without Redis session was accepted: status=%d", status)
	}

	// The middleware recognizes a cached API key, but this endpoint must still require x-token.
	if err := redisClient.Set(context.Background(), "apikey:identity-fixture-key", "tenant-identity", time.Hour).Err(); err != nil {
		t.Fatal("seed isolated API key tenant cache")
	}
	if err := redisClient.Set(context.Background(), "apikey:createdid:identity-fixture-key", "identity-user", time.Hour).Err(); err != nil {
		t.Fatal("seed isolated API key owner cache")
	}
	if status, _, payload := request("", "identity-fixture-key"); status != http.StatusUnauthorized || payload["error"] != "unauthorized" {
		t.Fatalf("API key was not rejected by the user-session endpoint after middleware auth: status=%d payload=%v", status, payload)
	}

	for _, testCase := range []struct {
		name string
		set  string
	}{
		{name: "database-disabled user", set: `UPDATE users SET status='F', authority='TENANT_ADMIN', tenant_id='tenant-identity' WHERE id='identity-user'`},
		{name: "database role changed", set: `UPDATE users SET status='N', authority='TENANT_USER', tenant_id='tenant-identity' WHERE id='identity-user'`},
		{name: "database tenant changed", set: `UPDATE users SET status='N', authority='TENANT_ADMIN', tenant_id='tenant-other' WHERE id='identity-user'`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := db.Exec(testCase.set).Error; err != nil {
				t.Fatal("change database identity state")
			}
			if err := redisClient.Set(context.Background(), validToken, "1", time.Hour).Err(); err != nil {
				t.Fatal("restore valid Redis session marker")
			}
			if status, _, _ := request(validToken, ""); status != http.StatusUnauthorized {
				t.Fatalf("stale token identity was accepted: status=%d", status)
			}
		})
		if err := db.Exec(`UPDATE users SET status='N', authority='TENANT_ADMIN', tenant_id='tenant-identity' WHERE id='identity-user'`).Error; err != nil {
			t.Fatal("restore isolated identity user")
		}
	}

	// Replace only this test's Redis client with an unavailable loopback endpoint.
	previousClient := global.REDIS
	global.REDIS = redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: 0, DialTimeout: 100 * time.Millisecond, ReadTimeout: 100 * time.Millisecond, WriteTimeout: 100 * time.Millisecond})
	if status, _, _ := request(validToken, ""); status != http.StatusUnauthorized {
		t.Fatalf("Redis outage did not fail closed: status=%d", status)
	}
	_ = global.REDIS.Close()
	global.REDIS = previousClient
}

func notificationSessionJWT(t *testing.T, key, userID, tenantID, authority string, expires time.Time) string {
	t.Helper()
	claims := utils.UserClaims{ID: userID, TenantID: tenantID, Authority: authority, StandardClaims: jwt.StandardClaims{ExpiresAt: expires.Unix(), IssuedAt: time.Now().Add(-time.Minute).Unix(), Id: uuid.NewString()}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(key))
	if err != nil {
		t.Fatal("sign isolated JWT")
	}
	return token
}

func openNotificationIdentityPGFixture(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("NOTIFICATION_TEST_DSN")
	if dsn == "" {
		t.Skip("NOTIFICATION_TEST_DSN fixture not supplied")
	}
	if dsn != approvedNotificationIdentityTestDSN {
		t.Fatal("refusing non-approved notification identity fixture DSN")
	}
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open approved notification identity fixture")
	}
	adminPool, err := adminDB.DB()
	if err != nil {
		t.Fatal("access approved notification identity fixture pool")
	}
	var database, user string
	if err := adminDB.Raw(`SELECT current_database(), current_user`).Row().Scan(&database, &user); err != nil || database != "notification_test" || user != "notification_test" {
		t.Fatal("fixture connection did not match approved database identity")
	}
	schema := "identity_session_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := adminDB.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal("create isolated identity schema")
	}
	previousDB := global.DB
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("parse approved identity fixture DSN")
	}
	queryValues := parsed.Query()
	queryValues.Set("options", "-c search_path="+schema)
	parsed.RawQuery = queryValues.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal("open isolated identity schema")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("access isolated identity schema pool")
	}
	pool.SetMaxOpenConns(4)
	global.DB = db
	query.SetDefault(db)
	t.Cleanup(func() {
		global.DB = previousDB
		if previousDB != nil {
			query.SetDefault(previousDB)
		}
		_ = db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error
		_ = pool.Close()
		_ = adminPool.Close()
	})
	if err := db.Exec(`CREATE TABLE users (
		id text PRIMARY KEY, tenant_id text, status text, authority text)`).Error; err != nil {
		t.Fatal("create isolated identity users table")
	}
	return db
}

func startNotificationIdentityRedis(t *testing.T) *redis.Client {
	t.Helper()
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server is not installed for the isolated session integration test")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve isolated Redis loopback port")
	}
	address := listener.Addr().String()
	_, portText, err := net.SplitHostPort(address)
	if err != nil {
		_ = listener.Close()
		t.Fatal("read isolated Redis port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		_ = listener.Close()
		t.Fatal("parse isolated Redis port")
	}
	_ = listener.Close()
	command := exec.Command(binary, "--bind", "127.0.0.1", "--port", strconv.Itoa(port), "--protected-mode", "no", "--save", "", "--appendonly", "no")
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		t.Fatal("start isolated Redis process")
	}
	client := redis.NewClient(&redis.Options{Addr: address, MaxRetries: 0, DialTimeout: 100 * time.Millisecond})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := client.Ping(context.Background()).Err(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = command.Process.Kill()
			_ = command.Wait()
			_ = client.Close()
			t.Fatal("isolated Redis did not become ready")
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	return client
}
