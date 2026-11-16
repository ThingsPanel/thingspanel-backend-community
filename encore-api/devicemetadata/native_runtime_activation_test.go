package devicemetadata

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRuntimeActivationRequiresBoundOwnerFacts(t *testing.T) {
	for _, mode := range []string{"ok", "device", "event", "inactive", "missing-time", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Runtime-Confirm-Only") != "true" || r.Header.Get("X-Automation-Tenant") != "tenant" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) {
					t.Error("unbound request")
				}
				var input map[string]string
				_ = json.NewDecoder(r.Body).Decode(&input)
				if input["credentialId"] != "credential" || input["eventId"] != "event" || input["deviceId"] != "device" || input["voucher"] != "" {
					t.Error("unexpected confirmation body")
				}
				if mode == "unavailable" {
					w.WriteHeader(503)
					return
				}
				data := map[string]any{"deviceId": "device", "eventId": "event", "activationState": "active", "activatedAt": "2026-11-01T00:00:00Z"}
				switch mode {
				case "device":
					data["deviceId"] = "other"
				case "event":
					data["eventId"] = "other"
				case "inactive":
					data["activationState"] = "inactive"
				case "missing-time":
					delete(data, "activatedAt")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": data})
			}))
			defer server.Close()
			t.Setenv("TP_COMMUNITY_BASE_URL", server.URL)
			t.Setenv("TP_AUTOMATION_SERVICE_TOKEN", strings.Repeat("x", 32))
			t.Setenv("TP_SOURCE_DEPLOYMENT_ID", "dep")
			_, err := requestRuntimeActivationConfirmation(context.Background(), "tenant", "device", "event", credentialChangeDesired{Change: "activation", CredentialID: "credential"})
			if (err == nil) != (mode == "ok") {
				t.Fatalf("mode %s: %v", mode, err)
			}
		})
	}
}

func TestInactiveOwnedRuntimeAdministrativeStateTransaction(t *testing.T) {
	for _, change := range []string{"activation", "rotation"} {
		t.Run(change, func(t *testing.T) { testInactiveOwnedRuntimeAdministrativeStateTransaction(t, change) })
	}
}
func testInactiveOwnedRuntimeAdministrativeStateTransaction(t *testing.T, changeKind string) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	identity := nativeWriteIdentity(tenant)
	created, err := createNativeDevice(ctx, identity, "create-"+uuid.NewString(), nativeCreateBody("restore-"+uuid.NewString(), "activation fixture"), "create", nil)
	if err != nil {
		t.Fatal(err)
	}
	dev := nativeWriteDevice(t, created)
	id := dev.ID
	t.Cleanup(func() {
		for _, table := range []string{"device_runtime_outbox", "metadata_operations", "device_credentials"} {
			db.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant)
		}
		cleanupNativeWriteTenants(t, ctx, tenant)
	})
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	provider := uuid.NewSHA1(uuid.NameSpaceOID, []byte("thingspanel/community/runtime/dep")).String()
	if _, err := tx.Exec(ctx, `UPDATE device_access_bindings SET configuration_generation=configuration_generation+1,direct_access=$3::jsonb,runtime_provider_id=$4,runtime_namespace='community',runtime_external_id=$5 WHERE tenant_id=$1 AND device_id=$2`, tenant, id, validNativeDirectAccess, provider, id); err != nil {
		t.Fatal(err)
	}
	cid := uuid.NewString()
	insertCredentialSchemaRow(t, ctx, tx, "active", credentialSchemaRow{id: cid, tenantID: tenant, deviceID: id, purpose: "device_access", kind: "token", mode: "verifier", status: "active", fingerprintKeyID: "k1", fingerprint: make([]byte, 32), activatedAt: timePointer(time.Now().UTC())})
	oid, err := createDeviceOperationTx(ctx, tx, tenant, identity.subjectID, "credential_change", id, "test", "req", strings.Repeat("a", 64), nil)
	if err != nil {
		t.Fatal(err)
	}
	change := credentialChangeDesired{Change: changeKind, CredentialID: cid}
	if changeKind == "rotation" {
		change.CredentialID = uuid.NewString()
		change.ReplacementCredentialID = cid
	}
	if err := enqueueDeviceRuntimeTx(ctx, tx, tenant, id, oid, "credential_change", 1, change); err != nil {
		t.Fatal(err)
	}
	var event string
	if err := tx.QueryRow(ctx, `UPDATE device_runtime_outbox SET state='applied',applied_at=now() WHERE operation_id=$1 RETURNING id::text`, oid).Scan(&event); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["eventId"] != event || body["credentialId"] != change.CredentialID || body["replacementCredentialId"] != change.ReplacementCredentialID || body["deviceId"] != id || body["change"] != changeKind || r.Header.Get("X-Runtime-Confirm-Only") != "true" {
			t.Error("confirmation not bound to applied event")
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"deviceId": id, "eventId": event, "activationState": "active", "activatedAt": "2026-11-01T00:00:00Z"}})
	}))
	defer server.Close()
	t.Setenv("TP_SOURCE_DEPLOYMENT_ID", "dep")
	t.Setenv("TP_COMMUNITY_BASE_URL", server.URL)
	t.Setenv("TP_AUTOMATION_SERVICE_TOKEN", strings.Repeat("x", 32))
	etag, err := nativeAdministrativeStateETag(NativeAdministrativeState{State: dev.AdministrativeState, ResourceVersion: dev.ResourceVersion})
	if err != nil {
		t.Fatal(err)
	}
	result, err := setNativeDeviceAdministrativeState(ctx, identity, id, "disable-"+uuid.NewString(), etag, []byte(`{"state":"disabled"}`), "restore-state")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != 202 || calls != 1 {
		t.Fatalf("status/calls=%d/%d", result.Status, calls)
	}
	var state, activation, source string
	var at time.Time
	var version int64
	if err := db.QueryRow(ctx, `SELECT administrative_state,activation_state,activation_time_source,activated_at,resource_version FROM devices WHERE id=$1`, id).Scan(&state, &activation, &source, &at, &version); err != nil {
		t.Fatal(err)
	}
	if state != "disabled" || activation != "active" || source != "runtime_confirmation" || version != dev.ResourceVersion+1 || at.UTC().Format(time.RFC3339) != "2026-11-01T00:00:00Z" {
		t.Fatalf("wrong projection %s %s %s %d %s", state, activation, source, version, at)
	}
	var pending int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM device_runtime_outbox WHERE tenant_id=$1 AND kind='administrative_state' AND state='pending'`, tenant).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("pending operation missing: %d %v", pending, err)
	}
}
