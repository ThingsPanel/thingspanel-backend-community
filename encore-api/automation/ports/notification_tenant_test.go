package ports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const fixtureIdentity = "fixture-machine-identity-not-a-real-secret-0001"

func serveLeaseFixture(t *testing.T, w http.ResponseWriter, r *http.Request, deployment string) bool {
	t.Helper()
	if r.URL.Path != "/internal/notification/source-credentials" {
		return false
	}
	if r.Header.Get("Authorization") != "Bearer "+fixtureIdentity || r.Header.Get("X-Notification-Deployment") != deployment {
		t.Error("machine identity mismatch")
		w.WriteHeader(401)
		return true
	}
	var body struct {
		TenantID string `json:"tenantId"`
		Purpose  string `json:"purpose"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		t.Error("invalid lease request")
	}
	if body.TenantID == "unknown" {
		w.WriteHeader(403)
		return true
	}
	json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": sourceCredentialLease{Credential: "fixture-scoped-credential-00000000-" + body.TenantID + "-" + body.Purpose, ExpiresAt: time.Now().Add(15 * time.Minute), TenantID: body.TenantID, DeploymentID: deployment, Purpose: body.Purpose}})
	return true
}
func TestNotificationManagedCredentialsStayWithinTenant(t *testing.T) {
	sends, leases := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveLeaseFixture(t, w, r, "deployment-a") {
			leases++
			return
		}
		sends++
		var body struct {
			Tenant string `json:"tenantId"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if r.Header.Get("Authorization") != "Bearer fixture-scoped-credential-00000000-"+body.Tenant+"-source" {
			t.Error("cross tenant credential")
		}
		w.WriteHeader(202)
		fmt.Fprint(w, `{"code":200,"data":{"accepted":true,"notificationId":"n","intakeStatus":"ready"}}`)
	}))
	defer srv.Close()
	owner := NewHTTPNotificationOwner(Config{NotificationURL: srv.URL, DeploymentID: "deployment-a", NotificationServiceIdentity: fixtureIdentity})
	for _, tenant := range []string{"a", "b", "a"} {
		result, err := owner.Submit(context.Background(), NotificationRequest{TenantID: tenant})
		if err != nil || result.State != NotificationAccepted {
			t.Fatalf("submit %s: %v %+v", tenant, err, result)
		}
	}
	if _, err := owner.Submit(context.Background(), NotificationRequest{TenantID: "unknown"}); err == nil {
		t.Fatal("unauthorized tenant accepted")
	}
	if sends != 3 || leases != 3 {
		t.Fatalf("sends=%d leases=%d", sends, leases)
	}
}
func TestNotificationManagedRefreshPreservesEventIdentity(t *testing.T) {
	leases, sends := 0, 0
	var firstBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveLeaseFixture(t, w, r, "deployment-a") {
			leases++
			return
		}
		sends++
		var body json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		if sends == 1 {
			firstBody = string(body)
			w.WriteHeader(401)
			return
		}
		if firstBody != string(body) || r.Header.Get("Idempotency-Key") != "same-key-0001" {
			t.Error("refresh changed event identity")
		}
		w.WriteHeader(202)
		fmt.Fprint(w, `{"code":200,"data":{"accepted":true,"notificationId":"n"}}`)
	}))
	defer srv.Close()
	owner := NewHTTPNotificationOwner(Config{NotificationURL: srv.URL, DeploymentID: "deployment-a", NotificationServiceIdentity: fixtureIdentity})
	result, err := owner.Submit(context.Background(), NotificationRequest{TenantID: "a", IdempotencyKey: "same-key-0001"})
	if err != nil || result.State != NotificationAccepted || sends != 2 || leases != 2 {
		t.Fatalf("refresh failed: %v %+v sends=%d leases=%d", err, result, sends, leases)
	}
}
func TestManagedLeaseSingleflightExpiryAndRestart(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); serveLeaseFixture(t, w, r, "deployment-a") }))
	defer srv.Close()
	client, err := newSourceCredentialClient(srv.URL, "deployment-a", fixtureIdentity, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.Credential(context.Background(), "a", "source"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("concurrent lease requests were not coalesced")
	}
	client.mu.Lock()
	lease := client.leases["a\x00source"]
	lease.ExpiresAt = time.Now().Add(30 * time.Second)
	client.leases["a\x00source"] = lease
	client.mu.Unlock()
	if _, err := client.Credential(context.Background(), "a", "source"); err != nil {
		t.Fatal(err)
	}
	restarted, _ := newSourceCredentialClient(srv.URL, "deployment-a", fixtureIdentity, srv.Client())
	if _, err := restarted.Credential(context.Background(), "a", "projection"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatal("expiry or restart failed to acquire scoped lease")
	}
}
func TestManagedLeaseRejectsScopeMismatchAndRedirects(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
	defer target.Close()
	for _, mode := range []string{"tenant", "deployment", "purpose", "expiry", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "redirect" {
					http.Redirect(w, r, target.URL, 307)
					return
				}
				lease := sourceCredentialLease{Credential: strings.Repeat("a", 40), TenantID: "a", DeploymentID: "deployment-a", Purpose: "source", ExpiresAt: time.Now().Add(15 * time.Minute)}
				switch mode {
				case "tenant":
					lease.TenantID = "b"
				case "deployment":
					lease.DeploymentID = "b"
				case "purpose":
					lease.Purpose = "projection"
				case "expiry":
					lease.ExpiresAt = time.Now().Add(time.Hour)
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": lease})
			}))
			defer srv.Close()
			client, _ := newSourceCredentialClient(srv.URL, "deployment-a", fixtureIdentity, srv.Client())
			if _, err := client.Credential(context.Background(), "a", "source"); err == nil {
				t.Fatal("untrusted lease accepted")
			}
		})
	}
	if leaked.Load() {
		t.Fatal("machine identity followed redirect")
	}
}
func TestRetiredManualCredentialsCannotEnableSender(t *testing.T) {
	t.Setenv("NOTIFICATION_SERVICE_TOKEN", "")
	t.Setenv("TP_NOTIFICATION_TOKEN", "retired-static-token")
	t.Setenv("TP_NOTIFICATION_TOKENS_JSON", `{"a":"retired-tenant-token"}`)
	if _, err := NewHTTPNotificationOwner(FromEnv()).Submit(context.Background(), NotificationRequest{TenantID: "a"}); err == nil {
		t.Fatal("retired configuration enabled sender")
	}
}
