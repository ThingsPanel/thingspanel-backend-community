package ports

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotificationSourceCredentialsStayWithinTenant(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Tenant string `json:"tenantId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.Header.Get("Authorization") != "Bearer credential-"+body.Tenant {
			t.Error("wrong tenant source credential")
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"code":200,"data":{"accepted":true,"notificationId":"n","intakeStatus":"ready"}}`))
	}))
	defer srv.Close()
	owner := NewHTTPNotificationOwner(Config{NotificationURL: srv.URL, NotificationToken: "must-not-fallback", NotificationTokens: map[string]string{"a": "credential-a", "b": "credential-b"}})
	for _, tenant := range []string{"a", "b"} {
		result, err := owner.Submit(context.Background(), NotificationRequest{TenantID: tenant})
		if err != nil || result.State != NotificationAccepted {
			t.Fatalf("tenant %s: %+v %v", tenant, result, err)
		}
	}
	if _, err := owner.Submit(context.Background(), NotificationRequest{TenantID: "unknown"}); err == nil {
		t.Fatal("unknown tenant used a default credential")
	}
	if calls != 2 {
		t.Fatalf("unexpected network requests: %d", calls)
	}
}

func TestInvalidNotificationTokenMapFailsClosed(t *testing.T) {
	t.Setenv("TP_NOTIFICATION_TOKEN", "legacy-default")
	for _, raw := range []string{`invalid`, `null`, `{}`, `{"a":""}`, `{" a":"secret"}`, `{"a":123}`} {
		t.Setenv("TP_NOTIFICATION_TOKENS_JSON", raw)
		cfg := FromEnv()
		if cfg.NotificationTokens == nil || len(cfg.NotificationTokens) != 0 {
			t.Fatalf("invalid map enabled credentials")
		}
		if _, err := NewHTTPNotificationOwner(cfg).Submit(context.Background(), NotificationRequest{TenantID: "a"}); err == nil {
			t.Fatal("invalid map used legacy default")
		}
	}
	t.Setenv("TP_NOTIFICATION_TOKENS_JSON", `{"a":"credential-a"}`)
	if FromEnv().NotificationTokens["a"] != "credential-a" {
		t.Fatal("valid map not loaded")
	}
	t.Setenv("TP_NOTIFICATION_TOKENS_JSON", "")
	if cfg := FromEnv(); cfg.NotificationTokens != nil || cfg.NotificationToken != "legacy-default" {
		t.Fatal("single tenant configuration changed")
	}
}
