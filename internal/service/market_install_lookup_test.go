package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"project/pkg/errcode"
	"project/pkg/utils"
)

func lookupClaims() *utils.UserClaims {
	return &utils.UserClaims{ID: "original-subject", TenantID: "original-tenant", Authority: "TENANT_ADMIN"}
}

func TestMarketInstallationLookupForwardsOnlyOneAuthenticatedGET(t *testing.T) {
	lookupPermissionFixture(t)
	key := "opaque/key + ? # original"
	token := strings.Repeat("t", 40)
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/market-installation-operations" || r.URL.Query().Get("operationKey") != key || len(r.URL.Query()) != 1 || r.Header.Get("X-Token") != token {
			t.Error("original-key query changed route, credential or method")
		}
		if r.ContentLength > 0 || strings.Contains(r.URL.String(), token) {
			t.Error("read forwarded a body or credential in its URL")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"state": "confirmed", "operationKey": key,
			"operation": map[string]interface{}{"id": "original-id", "bundleKey": "original-bundle", "version": "1", "state": "queued"},
		})
	}))
	defer server.Close()
	t.Setenv("TP_MARKETINSTALL_OWNER", "encore")
	t.Setenv("TP_MARKETINSTALL_COORDINATOR_BASE_URL", server.URL)
	observed, err := GetSelectedMarketInstallationByKey(context.Background(), key, lookupClaims(), token)
	if err != nil || calls.Load() != 1 || observed == nil || observed.State != "confirmed" || observed.OperationKey != key || observed.Result == nil || observed.Result.InstallationID != "original-id" || observed.Result.BundleKey != "original-bundle" || observed.Result.Version != "1" || observed.Result.Status != "QUEUED" {
		t.Fatalf("lookup result=%#v calls=%d error=%v", observed, calls.Load(), err)
	}
}

func TestMarketInstallationLookupMapsActualOwnerProgress(t *testing.T) {
	lookupPermissionFixture(t)
	for state, expected := range map[string]string{"queued": "QUEUED", "running": "RUNNING", "paused": "COMPENSATION_REQUIRED", "completed": "COMPLETED", "rejected": "FAILED"} {
		t.Run(state, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"state": "confirmed", "operationKey": "original-key",
					"operation": map[string]interface{}{"id": "original-id", "bundleKey": "original-bundle", "version": "1", "state": state, "errorCode": "OWNER_RESULT"},
				})
			}))
			defer server.Close()
			t.Setenv("TP_MARKETINSTALL_OWNER", "encore")
			t.Setenv("TP_MARKETINSTALL_COORDINATOR_BASE_URL", server.URL)
			result, err := GetSelectedMarketInstallationByKey(context.Background(), "original-key", lookupClaims(), strings.Repeat("t", 40))
			if err != nil || result == nil || result.Result == nil || result.Result.Status != expected || len(result.Result.Errors) != 1 || result.Result.Errors[0] != "OWNER_RESULT" || calls.Load() != 1 {
				t.Fatalf("actual owner state was replaced: %#v %v", result, err)
			}
		})
	}
}

func TestMarketInstallationLookupNeverTurnsUncertainResponseIntoAbsence(t *testing.T) {
	lookupPermissionFixture(t)
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"missing-body", 200, ""}, {"malformed", 200, "{"},
		{"unknown-state", 200, `{"state":"unknown","operationKey":"original-key"}`},
		{"wrong-key", 200, `{"state":"not_observed","operationKey":"another-key"}`},
		{"missing-record", 200, `{"state":"confirmed","operationKey":"original-key"}`},
		{"absent-with-record", 200, `{"state":"not_observed","operationKey":"original-key","operation":{"id":"original-id"}}`},
		{"incomplete-record", 200, `{"state":"confirmed","operationKey":"original-key","operation":{"id":"original-id","state":"completed"}}`},
		{"unknown-progress", 200, `{"state":"confirmed","operationKey":"original-key","operation":{"id":"original-id","bundleKey":"bundle","version":"1","state":"future"}}`},
		{"missing-route", 404, `{"code":"not_found"}`},
		{"owner-unavailable", 503, `{"code":"unavailable"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			t.Setenv("TP_MARKETINSTALL_OWNER", "encore")
			t.Setenv("TP_MARKETINSTALL_COORDINATOR_BASE_URL", server.URL)
			result, err := GetSelectedMarketInstallationByKey(context.Background(), "original-key", lookupClaims(), strings.Repeat("t", 40))
			if err == nil || result != nil || calls.Load() != 1 {
				t.Fatalf("uncertain reply became absence or retried: %#v calls=%d error=%v", result, calls.Load(), err)
			}
		})
	}
}

func TestMarketInstallationLookupAbsenceDoesNotRetryOrSubmit(t *testing.T) {
	lookupPermissionFixture(t)
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet {
			t.Error("absence caused an installation mutation")
		}
		fmt.Fprint(w, `{"state":"not_observed","operationKey":"original-key"}`)
	}))
	defer server.Close()
	t.Setenv("TP_MARKETINSTALL_OWNER", "encore")
	t.Setenv("TP_MARKETINSTALL_COORDINATOR_BASE_URL", server.URL)
	result, err := GetSelectedMarketInstallationByKey(context.Background(), "original-key", lookupClaims(), strings.Repeat("t", 40))
	if err != nil || result == nil || result.State != "not_observed" || result.Result != nil || calls.Load() != 1 {
		t.Fatalf("absent result=%#v calls=%d error=%v", result, calls.Load(), err)
	}
}

func TestMarketInstallationLookupRejectsUnauthorizedAndUnsupportedOwner(t *testing.T) {
	lookupPermissionFixture(t)
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	t.Setenv("TP_MARKETINSTALL_COORDINATOR_BASE_URL", server.URL)
	t.Setenv("TP_MARKETINSTALL_OWNER", "encore")
	for _, claims := range []*utils.UserClaims{nil, {}, {ID: "subject", TenantID: "tenant", Authority: "TENANT_USER"}} {
		if result, err := GetSelectedMarketInstallationByKey(context.Background(), "original-key", claims, strings.Repeat("t", 40)); result != nil || err == nil {
			t.Fatal("missing current management identity accepted")
		}
	}
	if _, err := GetSelectedMarketInstallationByKey(context.Background(), "original-key", lookupClaims(), ""); err == nil {
		t.Fatal("API-key-only caller supplied no live user JWT")
	}
	for _, key := range []string{"", "short", strings.Repeat("k", 129), "original\x00key"} {
		if _, err := GetSelectedMarketInstallationByKey(context.Background(), key, lookupClaims(), strings.Repeat("t", 40)); err == nil {
			t.Fatal("invalid original key accepted")
		}
	}
	for _, owner := range []string{"", "legacy", "unknown"} {
		t.Setenv("TP_MARKETINSTALL_OWNER", owner)
		if result, err := GetSelectedMarketInstallationByKey(context.Background(), "original-key", lookupClaims(), strings.Repeat("t", 40)); result != nil || err == nil {
			t.Fatal("owner fallback enabled tenant-only legacy read")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("denied lookup reached a downstream service")
	}
}

func TestMarketInstallationLookupDoesNotFollowRedirectOrIgnoreRevocation(t *testing.T) {
	lookupPermissionFixture(t)
	var targetCalls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusUnauthorized, http.StatusForbidden} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", target.URL)
			w.WriteHeader(status)
			fmt.Fprint(w, `{"code":"denied"}`)
		}))
		t.Setenv("TP_MARKETINSTALL_OWNER", "encore")
		t.Setenv("TP_MARKETINSTALL_COORDINATOR_BASE_URL", server.URL)
		result, err := GetSelectedMarketInstallationByKey(context.Background(), "original-key", lookupClaims(), strings.Repeat("t", 40))
		server.Close()
		if err == nil || result != nil {
			t.Fatal("redirect or revoked authorization became an observation")
		}
		if status != http.StatusTemporaryRedirect {
			code, ok := err.(*errcode.Error)
			if !ok || code.Code != errcode.CodeNoPermission {
				t.Fatalf("revoked read did not deny permission: %v", err)
			}
		}
	}
	if targetCalls.Load() != 0 {
		t.Fatal("lookup leaked the user credential to a redirect target")
	}
}
