package systemagent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const token = "0123456789abcdef0123456789abcdef0123456789ab"

// agent starts a fake Agent on a Unix socket; handler gets (path, auth header, body).
func agent(t *testing.T, handler func(path, auth string, body map[string]any) (int, any)) *Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sa")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		code, out := handler(r.URL.Path, r.Header.Get("Authorization"), body)
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(out)
	})}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	tf := filepath.Join(dir, "token")
	if err := os.WriteFile(tf, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Client{Socket: sock, TokenFile: tf}
}

func TestCallSendsBearerAndFixedPath(t *testing.T) {
	c := agent(t, func(path, auth string, body map[string]any) (int, any) {
		if path != "/v1/system/restart" || auth != "Bearer "+token || body["scope"] != "control" {
			t.Errorf("unexpected request %s %q %v", path, auth, body)
		}
		return 200, map[string]any{"ok": true, "operation": map[string]any{"id": "x", "kind": "restart", "state": "running"}}
	})
	r, err := c.Call(context.Background(), "restart", map[string]any{"scope": "control"})
	if err != nil || r.Operation == nil || r.Operation.Kind != "restart" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestCallMapsBusyAndAgentErrors(t *testing.T) {
	c := agent(t, func(path, _ string, _ map[string]any) (int, any) {
		if path == "/v1/system/restart" {
			return 200, map[string]any{"ok": false, "error": "another system operation is running"}
		}
		return 200, map[string]any{"ok": false, "error": "update check failed", "code": "updates_fetch_failed"}
	})
	if _, err := c.Call(context.Background(), "restart", nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy: %v", err)
	}
	_, err := c.Call(context.Background(), "updates", nil)
	var ae *Error
	if !errors.As(err, &ae) || ae.Code != "updates_fetch_failed" {
		t.Fatalf("%v", err)
	}
}

func TestUnavailableWhenNotConfiguredOrRefused(t *testing.T) {
	if _, err := (*Client)(nil).Call(context.Background(), "status", nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("nil client")
	}
	if _, err := (&Client{}).Call(context.Background(), "status", nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("empty client")
	}
	c := agent(t, func(string, string, map[string]any) (int, any) {
		return 401, map[string]any{"ok": false, "error": "unauthorized"}
	})
	if _, err := c.Call(context.Background(), "status", nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a rejected token must look like no access, not leak the reason: %v", err)
	}
	// short token file, missing socket
	short := filepath.Join(t.TempDir(), "t")
	os.WriteFile(short, []byte("short"), 0o600)
	if _, err := (&Client{Socket: "/nonexistent.sock", TokenFile: short}).Call(context.Background(), "status", nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("short token")
	}
	if _, err := (&Client{Socket: "/nonexistent.sock", TokenFile: c.TokenFile}).Call(context.Background(), "status", nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing socket")
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("TP_SYSTEM_SOCKET", "")
	t.Setenv("TP_SYSTEM_TOKEN_FILE", "")
	if _, err := FromEnv().Call(context.Background(), "status", nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("no env must mean unavailable")
	}
	t.Setenv("TP_SYSTEM_SOCKET", "/run/tp-system/agent-system.sock")
	if c := FromEnv(); !strings.HasSuffix(c.Socket, "agent-system.sock") {
		t.Fatal(c.Socket)
	}
}
