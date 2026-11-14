package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialOwnerProtocolReadinessFailsClosed(t *testing.T) {
	secret := strings.Repeat("s", 32)
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TP_BROKER_TOKEN_FILE", file)
	t.Setenv("TP_BROKER_CELL", "community")
	t.Setenv("TP_BROKER_INSTANCE", "broker-a")
	for _, tc := range []struct {
		name, body string
		code       int
		ok         bool
	}{
		{"current", `{"protocol":1,"ready":true}`, 200, true},
		{"old owner", `not found`, 404, false}, {"wrong version", `{"protocol":2,"ready":true}`, 200, false},
		{"not ready", `{"protocol":1,"ready":false}`, 200, false}, {"malformed", `{`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/credential-leases/ready" || r.Header.Get("Authorization") != "Bearer "+secret {
					t.Error("wrong readiness identity")
				}
				w.WriteHeader(tc.code)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			t.Setenv("TP_BROKER_OWNER_URL", srv.URL)
			if err := RequireNativeCredentialOwnerReady(context.Background()); (err == nil) != tc.ok {
				t.Fatalf("readiness outcome %v", err)
			}
		})
	}
}
