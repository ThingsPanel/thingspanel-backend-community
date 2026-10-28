package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type communitySessionHandshake struct {
	Address    string `json:"address"`
	Bearer     string `json:"bearer"`
	JWTKey     string `json:"jwtKey"`
	ControlKey string `json:"controlKey"`
}

// This opt-in test crosses the real process and HTTP boundary between Encore's
// LegacySessionChecker and the community API's middleware/session endpoint.
// It starts only the package's test helper, never the product server.
func TestLegacySessionCheckerAgainstCommunityProcess(t *testing.T) {
	if os.Getenv("NOTIFICATION_SESSION_CROSS_PROCESS") != "1" {
		t.Skip("set NOTIFICATION_SESSION_CROSS_PROCESS=1 to run the private cross-process identity test")
	}
	helperBinary := strings.TrimSpace(os.Getenv("NOTIFICATION_SESSION_HELPER_BINARY"))
	if helperBinary == "" {
		t.Fatal("NOTIFICATION_SESSION_HELPER_BINARY must point to the separately built community API test binary")
	}
	if _, err := os.Stat(helperBinary); err != nil {
		t.Fatalf("community session helper binary is unavailable: %v", err)
	}
	const fixtureDSN = "postgres://notification_test:fixture-only-password@127.0.0.1:25543/notification_test"
	if os.Getenv("NOTIFICATION_TEST_DSN") != fixtureDSN {
		t.Fatal("refusing to start the community helper without the approved isolated fixture DSN")
	}

	privateDir := t.TempDir()
	if err := os.Chmod(privateDir, 0700); err != nil {
		t.Fatal("secure private helper directory")
	}
	handshakePath := filepath.Join(privateDir, "handshake.json")
	logPath := filepath.Join(privateDir, "helper.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("create private helper log")
	}
	defer logFile.Close()
	helperCtx, cancelHelper := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancelHelper()
	command := exec.CommandContext(helperCtx, helperBinary, "-test.run=^TestNotificationSessionHTTPSubprocessHelper$", "-test.timeout=100s")
	command.Env = append(os.Environ(), "NOTIFICATION_SESSION_HELPER_MODE=serve", "NOTIFICATION_SESSION_HELPER_HANDSHAKE="+handshakePath)
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		t.Fatal("start test-only community HTTP helper")
	}
	processDone := make(chan error, 1)
	go func() { processDone <- command.Wait() }()
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			select {
			case <-processDone:
			case <-time.After(2 * time.Second):
			}
		}
	}()

	var handshake communitySessionHandshake
	deadline := time.Now().Add(8 * time.Second)
	for {
		data, readErr := os.ReadFile(handshakePath)
		if readErr == nil {
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&handshake) != nil || handshake.Address == "" || handshake.Bearer == "" || handshake.JWTKey == "" || handshake.ControlKey == "" {
				t.Fatal("community helper published an invalid private handshake")
			}
			break
		}
		select {
		case childErr := <-processDone:
			logFile.Sync()
			logBytes, _ := os.ReadFile(logPath)
			t.Fatalf("community helper exited before publishing its endpoint: %v (%s)", childErr, strings.TrimSpace(string(logBytes)))
		default:
		}
		if time.Now().After(deadline) {
			logFile.Sync()
			logBytes, _ := os.ReadFile(logPath)
			t.Fatalf("community helper did not become ready: %s", strings.TrimSpace(string(logBytes)))
		}
		time.Sleep(20 * time.Millisecond)
	}

	endpoint, err := url.Parse(handshake.Address)
	if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() == "" || !legacyLoopback(endpoint.Hostname()) || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		t.Fatal("community helper endpoint is not an exact loopback HTTP origin")
	}
	checker, err := NewLegacySessionChecker(handshake.Address, LegacySessionOptions{TestMode: true, AllowHTTPTestEndpoint: true, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal("construct explicit loopback-only legacy session checker")
	}
	privacy := crossProcessPrivacyFixture(t)
	t.Setenv("TP_AUTH_MODE", "legacy-jwt")
	t.Setenv("TP_JWT_KEY", handshake.JWTKey)
	service := &ManagementService{sessions: checker, privacy: privacy, clock: time.Now}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	principal, err := service.VerifyUser(ctx, handshake.Bearer)
	if err != nil || principal.UserID != "identity-cross-process" || principal.TenantID != "tenant-cross-process" || len(principal.Roles) != 1 || principal.Roles[0] != "TENANT_ADMIN" {
		t.Fatalf("live community session did not produce the expected principal: principal=%+v err=%v", principal, err)
	}
	if err := requireNotificationScope(principal, "notification:send"); err != nil {
		t.Fatalf("verified tenant admin did not receive send permission: %v", err)
	}
	err = requireNotificationScope(principal, "notification:plugin:manage")
	var forbidden *CoreFailure
	if !errors.As(err, &forbidden) || forbidden.Status != http.StatusForbidden || forbidden.Reason != "permission_denied" {
		t.Fatalf("tenant admin did not receive an explicit 403 for platform plugin management: %v", err)
	}

	control := func(action string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"action": action})
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, handshake.Address+"/__test/session-control", bytes.NewReader(body))
		if err != nil {
			t.Fatal("build private helper control request")
		}
		request.Header.Set("Authorization", "Bearer "+handshake.ControlKey)
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("control private community helper (%s): %v", action, err)
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
		if response.StatusCode != http.StatusNoContent {
			t.Fatalf("private community helper rejected control action %q: status=%d", action, response.StatusCode)
		}
	}
	assertRejected := func(label string) {
		t.Helper()
		if _, err := service.VerifyUser(ctx, handshake.Bearer); !errors.Is(err, ErrIdentityUnauthorized) {
			t.Fatalf("%s was accepted by Core's live identity verifier: %v", label, err)
		}
	}

	control("revoke")
	assertRejected("revoked Redis session")
	control("restore-session")
	control("change-role")
	assertRejected("database role change")
	control("restore-identity")
	control("change-tenant")
	assertRejected("database tenant change")
	control("restore-identity")

	control("stop")
	select {
	case childErr := <-processDone:
		if childErr != nil {
			logFile.Sync()
			logBytes, _ := os.ReadFile(logPath)
			t.Fatalf("community helper did not stop cleanly: %v (%s)", childErr, strings.TrimSpace(string(logBytes)))
		}
	case <-time.After(4 * time.Second):
		t.Fatal("community helper did not stop after the private stop request")
	}
	if err := logFile.Sync(); err != nil {
		t.Fatal("sync private helper log")
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal("read private helper log")
	}
	for _, secret := range []string{handshake.Bearer, handshake.JWTKey, handshake.ControlKey} {
		if bytes.Contains(logBytes, []byte(secret)) {
			t.Fatal("community helper wrote handshake credentials to stdout/stderr")
		}
	}
	if info, err := os.Stat(handshakePath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("handshake permissions are not private: info=%v err=%v", info, err)
	}
}

func crossProcessPrivacyFixture(t *testing.T) *PrivacyGuard {
	t.Helper()
	path := filepath.Join(t.TempDir(), "infra.json")
	if err := os.WriteFile(path, []byte(`{"log_config":"info"}`), 0600); err != nil {
		t.Fatal("write isolated privacy fixture")
	}
	env := []byte("ENCORE_INFRA_CONFIG_PATH=" + path + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + path + "\x00")
	guard, err := newPrivacyGuard(func() ([]byte, error) { return env, nil }, "linux")
	if err != nil {
		t.Fatal("construct isolated privacy guard")
	}
	return guard
}
