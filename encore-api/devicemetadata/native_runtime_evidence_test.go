package devicemetadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"encore.app/internal/communityclient"
	"github.com/google/uuid"
)

func TestNativeRuntimeStateAndDiagnosticsUseSameEvidencePostgres(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant, device := testTenantID(), uuid.NewString()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := insertNativeReadDevice(ctx, tx, tenant, device, "STATE-"+device, "runtime evidence fixture", "{}", true); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{`DELETE FROM device_access_bindings WHERE tenant_id=$1 AND device_id=$2`, `DELETE FROM devices WHERE tenant_id=$1 AND id=$2`} {
			if _, err := db.Exec(ctx, sql, tenant, device); err != nil {
				t.Error(err)
			}
		}
	})
	// Explicit fixture-only identity mode; production still requires confirmed runtimeRef.
	t.Setenv(runtimeIdentityModeEnv, "identity")
	identity := nativeDeviceReadIdentity{tenantID: tenant}
	for _, tc := range []struct {
		name, status, want, check string
		fresh                     bool
	}{
		{"fresh online", "online", "online", "pass", true},
		{"fresh offline", "offline", "offline", "fail", true},
		{"stale projected online", "online", "unknown", "unknown", false},
		{"no evidence", "unknown", "unknown", "unknown", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/device/status/evidence/"+device || r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Error("wrong evidence request")
					w.WriteHeader(403)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"device_id": device, "status": tc.status, "fresh": tc.fresh, "connections": 0, "source": "ingress_v1", "as_of": time.Now().UTC()}})
			}))
			defer server.Close()
			t.Setenv("TP_COMMUNITY_BASE_URL", server.URL)
			client := communityclient.NewFromEnv()
			r := httptest.NewRequest("GET", "http://native.test", nil)
			r.Header.Set("Authorization", "Bearer fixture-token")
			readctx := communityclient.WithAuthenticatedReadRequest(ctx, r)
			state, err := readNativeRuntimeStateWithClient(readctx, identity, device, "state", client)
			if err != nil {
				t.Fatal(err)
			}
			if state.Data.Connectivity != tc.want || state.Data.ObservedAt != nil || state.Data.AppliedConfigurationGeneration != nil {
				t.Fatalf("invented runtime evidence: %+v", state.Data)
			}
			now := time.Now().UTC()
			diagnostic, err := readNativeDiagnostics(readctx, identity, nativeDiagnosticsQuery{from: now.Add(-time.Hour), to: now, limit: 5}, device, "diag", now, func(ctx context.Context) (string, error) {
				return readNativeConnectivity(ctx, identity, device, client)
			})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, check := range diagnostic.Data.Checks {
				if check.Stage == "connection" {
					found = true
					if check.State != tc.check {
						t.Fatalf("diagnostic differs: %+v", check)
					}
				}
			}
			if !found || calls != 2 {
				t.Fatalf("connection check missing/calls=%d", calls)
			}
			if _, err := readNativeRuntimeStateWithClient(readctx, nativeDeviceReadIdentity{tenantID: testTenantID()}, device, "foreign", client); err == nil || calls != 2 {
				t.Fatal("foreign tenant reached runtime")
			}
		})
	}
}
