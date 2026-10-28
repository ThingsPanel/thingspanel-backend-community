package service

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"project/internal/model"
	"project/pkg/global"

	"github.com/redis/go-redis/v9"
)

func TestPasswordResetCodeRemainsSynchronousLegacySMTP(t *testing.T) {
	openSourceCompatPGFixture(t)
	db := global.DB
	if err := db.Exec(`CREATE TABLE users (
		id text PRIMARY KEY, name text, phone_number text NOT NULL, email text NOT NULL,
		status text, authority text, password text NOT NULL, tenant_id text, remark text,
		additional_info text, created_at timestamptz, updated_at timestamptz,
		password_last_updated timestamptz, last_visit_time timestamptz, last_visit_ip text,
		last_visit_device text, organization text, timezone text, default_language text,
		password_fail_count integer, avatar_url text)`).Error; err != nil {
		t.Fatal("create isolated user fixture table")
	}
	const recipient = "reset-user@example.test"
	if err := db.Exec(`INSERT INTO users (id, phone_number, email, password, status, authority, tenant_id) VALUES (?, ?, ?, ?, 'N', 'TENANT_USER', ?)`, "legacy-reset-user", "0000000000", recipient, "fixture-hash-only", "tenant-a").Error; err != nil {
		t.Fatal("seed isolated user for reset-code path")
	}

	listener, port := startFixtureSMTP(t)
	config := fmt.Sprintf(`{"host":"127.0.0.1","port":%d,"from_password":"fixture-only-password","from_email":"sender@example.test","ssl":false}`, port)
	if err := db.Exec(`UPDATE notification_services_config SET config = ? WHERE id = ?`, config, "email-config-a").Error; err != nil {
		t.Fatal("configure isolated legacy SMTP fixture")
	}

	redisClient := startLegacyNotificationRedis(t)
	previousRedis := global.REDIS
	global.REDIS = redisClient
	t.Cleanup(func() {
		global.REDIS = previousRedis
		_ = redisClient.Close()
	})
	previousBridge := currentSourceBridge()
	SetActiveSourceBridge(nil)
	t.Cleanup(func() { SetActiveSourceBridge(previousBridge) })

	type result struct{ err error }
	completed := make(chan result, 1)
	go func() { completed <- result{err: GroupApp.User.GetVerificationCode(recipient, "")} }()

	var email []byte
	select {
	case email = <-listener.data:
	case <-time.After(5 * time.Second):
		t.Fatal("legacy reset-code request did not reach its synchronous SMTP send")
	}
	if len(email) == 0 || !strings.Contains(string(email), recipient) {
		t.Fatal("local SMTP fixture did not capture the reset-code message")
	}
	code, err := redisClient.Get(context.Background(), recipient+"_code").Result()
	if err != nil || len(code) != 6 || strings.Trim(code, "0123456789") != "" || !strings.Contains(string(email), code) {
		t.Fatal("verification code was not stored before the synchronous SMTP message")
	}
	select {
	case <-completed:
		t.Fatal("reset-code API returned before SMTP accepted the message")
	case <-time.After(100 * time.Millisecond):
	}
	listener.allow()
	select {
	case result := <-completed:
		if result.err != nil {
			t.Fatal("legacy reset-code send returned an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reset-code API did not return after SMTP accepted the message")
	}
	if listener.messages.Load() != 1 {
		t.Fatalf("legacy reset-code path sent %d fixture messages, want 1", listener.messages.Load())
	}
}

func TestUnprovenLegacyGroupTypesCannotSwitchToEncore(t *testing.T) {
	openSourceCompatPGFixture(t)
	checker := &countingCompatibilityChecker{}
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	bridge := testSourceBridge(t, server)
	bridge.check = checker
	defer bridge.Close()

	for _, legacyType := range []string{model.NoticeType_Member, "SMS", model.NoticeType_SME_CODE, model.NoticeType_Webhook, model.NoticeType_APP, "EMAIL,MEMBER"} {
		if err := global.DB.Exec(`UPDATE notification_groups SET notification_type = ? WHERE id = ? AND tenant_id = ?`, legacyType, "legacy-a", "tenant-a").Error; err != nil {
			t.Fatal("set isolated legacy notification type")
		}
		request := SourceGroupProjectionRequest{SourceDeploymentID: "deployment-test", TenantID: "tenant-a", LegacyGroupID: "legacy-a", NotificationGroupID: "native-target", GroupRevision: 1}
		if err := bridge.SwitchToEncore(context.Background(), request, "unproven-type-key-0001", 0); err == nil {
			t.Fatalf("legacy group type %q switched to Encore without a compatibility proof", legacyType)
		}
	}
	if checker.calls != 0 || requests != 0 {
		t.Fatalf("unproven types reached compatibility or projection networking: checks=%d requests=%d", checker.calls, requests)
	}
	var routes int64
	if err := global.DB.Raw(`SELECT count(*) FROM notification_source_group_routes WHERE source_deployment_id = ? AND tenant_id = ? AND legacy_group_id = ?`, "deployment-test", "tenant-a", "legacy-a").Scan(&routes).Error; err != nil || routes != 0 {
		t.Fatalf("unproven group created a source route: rows=%d", routes)
	}
}

type countingCompatibilityChecker struct{ calls int }

func (c *countingCompatibilityChecker) CheckEncoreCompatibility(context.Context, *model.NotificationGroup, SourceGroupProjectionRequest) error {
	c.calls++
	return nil
}

type fixtureSMTP struct {
	listener    net.Listener
	data        chan []byte
	release     chan struct{}
	releaseOnce sync.Once
	messages    atomic.Int32
}

func (fixture *fixtureSMTP) allow() { fixture.releaseOnce.Do(func() { close(fixture.release) }) }

func startFixtureSMTP(t *testing.T) (*fixtureSMTP, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("start isolated local SMTP listener")
	}
	fixture := &fixtureSMTP{listener: listener, data: make(chan []byte, 1), release: make(chan struct{})}
	t.Cleanup(fixture.allow)
	_, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		reader := bufio.NewReader(connection)
		write := func(value string) bool {
			_, writeErr := io.WriteString(connection, value)
			return writeErr == nil
		}
		if !write("220 fixture.local ESMTP\r\n") {
			return
		}
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				return
			}
			command := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(command, "EHLO ") || strings.HasPrefix(command, "HELO "):
				if !write("250-fixture.local\r\n250 AUTH PLAIN\r\n") {
					return
				}
			case command == "AUTH PLAIN":
				if !write("334 \r\n") {
					return
				}
				if _, readErr = reader.ReadString('\n'); readErr != nil || !write("235 2.7.0 authenticated\r\n") {
					return
				}
			case strings.HasPrefix(command, "AUTH PLAIN "):
				if !write("235 2.7.0 authenticated\r\n") {
					return
				}
			case strings.HasPrefix(command, "MAIL FROM:") || strings.HasPrefix(command, "RCPT TO:") || command == "RSET":
				if !write("250 2.1.0 ok\r\n") {
					return
				}
			case command == "DATA":
				if !write("354 end with dot\r\n") {
					return
				}
				var message strings.Builder
				for {
					dataLine, dataErr := reader.ReadString('\n')
					if dataErr != nil {
						return
					}
					if dataLine == ".\r\n" {
						break
					}
					message.WriteString(dataLine)
				}
				fixture.data <- []byte(message.String())
				<-fixture.release
				fixture.messages.Add(1)
				if !write("250 2.0.0 queued\r\n") {
					return
				}
			case command == "QUIT":
				_ = write("221 2.0.0 bye\r\n")
				return
			default:
				if !write("250 2.0.0 ok\r\n") {
					return
				}
			}
		}
	}()
	return fixture, port
}

func startLegacyNotificationRedis(t *testing.T) *redis.Client {
	t.Helper()
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server unavailable for isolated notification compatibility test")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve isolated Redis port")
	}
	address := listener.Addr().String()
	_, portText, _ := net.SplitHostPort(address)
	_ = listener.Close()
	command := exec.Command(binary, "--bind", "127.0.0.1", "--port", portText, "--protected-mode", "no", "--save", "", "--appendonly", "no")
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		t.Fatal("start isolated Redis fixture")
	}
	client := redis.NewClient(&redis.Options{Addr: address, MaxRetries: 0, DialTimeout: 100 * time.Millisecond})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if client.Ping(context.Background()).Err() == nil {
			t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
			return client
		}
		time.Sleep(25 * time.Millisecond)
	}
	_ = command.Process.Kill()
	_ = command.Wait()
	_ = client.Close()
	t.Fatal("isolated Redis fixture did not become ready")
	return nil
}
