package notification

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLegacyDirectoryPinsReadIdentityAndTenant(t *testing.T) {
	mode := "valid"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture-directory-token-at-least-32" || r.Header.Get("X-Notification-Deployment") != "deployment-test" || r.Method != "GET" {
			t.Error("wrong read credential or method")
		}
		w.Header().Set("Content-Type", "application/json")
		if mode == "redirect" {
			w.Header().Set("Location", "/leak")
			w.WriteHeader(302)
			return
		}
		if mode == "missing" {
			w.WriteHeader(404)
			return
		}
		if mode == "forbidden" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if mode == "unauthorized" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if mode == "unavailable" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/api/v1/notification/source-tenants/tenant-test" {
			fmt.Fprint(w, `{"tenantId":"tenant-test","exists":true}`)
			return
		}
		if r.URL.Path != "/api/v1/notification/source-members/user-test" || r.URL.Query().Get("tenantId") != "tenant-test" || r.URL.Query().Get("contactField") != "email" {
			t.Error("wrong bounded member target")
		}
		tenant := "tenant-test"
		if mode == "other" {
			tenant = "other"
		}
		fmt.Fprintf(w, `{"userId":"user-test","tenantId":%q,"contactField":"email","address":"fixture@example.test"}`, tenant)
	}))
	defer server.Close()
	options := LegacySessionOptions{TestMode: true, AllowHTTPTestEndpoint: true}
	reader, err := NewLegacyDirectoryReader(server.URL, "fixture-directory-token-at-least-32", "deployment-test", options)
	if err != nil {
		t.Fatal(err)
	}
	if found, err := reader.Exists(context.Background(), "tenant-test"); !found || err != nil {
		t.Fatal("tenant not resolved", err)
	}
	if values, err := reader.ResolveMember(context.Background(), "tenant-test", "user-test", "email"); err != nil || len(values) != 1 || values[0].Address != "fixture@example.test" {
		t.Fatal("member not resolved", err)
	}
	for _, value := range []string{"other", "redirect", "missing"} {
		mode = value
		if _, err := reader.ResolveMember(context.Background(), "tenant-test", "user-test", "email"); err == nil {
			t.Fatal("invalid result admitted", value)
		}
	}
	before := calls
	if _, err := reader.ResolveMember(context.Background(), "tenant-test", "user-test", "applicationUserId"); err == nil || calls != before {
		t.Fatal("unsupported APP field made request")
	}
	if _, err := reader.Exists(context.Background(), "../other"); err == nil || calls != before {
		t.Fatal("unsafe path made request")
	}
	mode = "forbidden"
	if found, err := reader.Exists(context.Background(), "tenant-outside-reader-scope"); found || err != nil {
		t.Fatalf("scope denial should resolve as not found: found=%t err=%v", found, err)
	}
	if _, err := reader.ResolveMember(context.Background(), "tenant-test", "user-test", "email"); err == nil {
		t.Fatal("member scope denial must remain unavailable")
	}
	for _, value := range []string{"unauthorized", "unavailable"} {
		mode = value
		if found, err := reader.Exists(context.Background(), "tenant-test"); found || err == nil {
			t.Fatalf("directory %s must fail closed: found=%t err=%v", value, found, err)
		}
	}
	if _, err := NewLegacyDirectoryReader(server.URL, "fixture-directory-token-at-least-32", "deployment-test", LegacySessionOptions{}); err == nil {
		t.Fatal("production allowed HTTP")
	}
}
