package devicemetadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"encore.app/internal/communityclient"
	"encore.app/thingmodel/core"
	"github.com/google/uuid"
)

const commandTestSnapshot = "11111111-1111-1111-1111-111111111111"

func TestNativeCommandDecode(t *testing.T) {
	ok, err := decodeNativeCommandSubmission([]byte(`{"identifier":"reboot","snapshotId":"` + commandTestSnapshot + `","parameters":{"a":1},"timeoutSeconds":5,"label":"x"}`))
	if err != nil || ok.Identifier != "reboot" || ok.TimeoutSeconds != 5 || ok.Label == nil {
		t.Fatalf("%v %+v", err, ok)
	}
	d, _ := decodeNativeCommandSubmission([]byte(`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{}}`))
	if d.TimeoutSeconds != 30 {
		t.Fatal("default timeout")
	}
	bad := []string{
		`[]`, `{}`, `{"identifier":"r","snapshotId":"` + commandTestSnapshot + `"}`,
		`{"identifier":"r","snapshotId":"x","parameters":{}}`,
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":[]}`,
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"timeoutSeconds":301}`,
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"timeoutSeconds":0}`,
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"expiresAt":"2026-01-01T00:00:00Z"}`,
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"extra":1}`,
	}
	for _, b := range bad {
		if _, err := decodeNativeCommandSubmission([]byte(b)); err == nil {
			t.Errorf("accepted %s", b)
		}
	}
}

func TestNativeCommandParams(t *testing.T) {
	var f core.Field
	if err := json.Unmarshal([]byte(`{"kind":"COMMAND","identifier":"set","parameters":{"schema_version":1,"items":[{"identifier":"n","value_type":{"kind":"INT"}}]}}`), &f); err != nil {
		t.Fatal(err)
	}
	if err := validateNativeCommandParameters(f, []byte(`{"n":3}`)); err != nil {
		t.Fatalf("valid rejected: %v", err)
	}
	for _, b := range []string{`{}`, `{"n":"x"}`, `{"n":3,"z":1}`, `[]`} {
		if err := validateNativeCommandParameters(f, []byte(b)); err == nil {
			t.Errorf("accepted %s", b)
		}
	}
}

func TestNativeCommandMapAndText(t *testing.T) {
	cases := map[string]string{"accepted": "accepted", "published": "delivered", "publish_failed": "failed",
		"device_failed": "failed", "device_succeeded": "acknowledged", "timeout": "timed_out", "weird": "unknown",
		"dispatching": "accepted", "broker_acknowledged": "delivered", "device_reported_success": "acknowledged",
		"device_reported_failure": "failed", "expired": "expired", "failed": "failed", "unknown": "unknown", "fenced": "failed"}
	for in, want := range cases {
		if got := mapRuntimeCommandStatus(in, ""); got != want {
			t.Errorf("%s=%s", in, got)
		}
	}
	if runtimeParametersText([]byte(`{}`)) != "" || runtimeParametersText([]byte(`{"n":1}`)) != `{"n":1}` {
		t.Fatal("params text")
	}
	if !(commandStateRank("pending") < commandStateRank("accepted") && commandStateRank("delivered") < commandStateRank("failed")) {
		t.Fatal("rank")
	}
}

func TestNativeCommandTimeoutRequiresExplicitEvidence(t *testing.T) {
	for _, reason := range []string{"", "timeout", "HTTP timeout", "device acknowledgement timed out later"} {
		if got := mapRuntimeCommandStatus("failed", reason); got != "failed" {
			t.Errorf("%q inferred timeout: %s", reason, got)
		}
	}
	if got := mapRuntimeCommandStatus("failed", "device acknowledgement timed out"); got != "timed_out" {
		t.Fatalf("explicit broker timeout: %s", got)
	}
	if got := mapRuntimeCommandStatus("unknown", "device acknowledgement timed out"); got != "unknown" {
		t.Fatalf("unknown status guessed timeout: %s", got)
	}
	for _, terminal := range []string{"acknowledged", "failed", "timed_out", "expired", "cancelled"} {
		for next := range nativeCommandStates {
			if canRefreshNativeCommandState(terminal, next) {
				t.Errorf("terminal %s can become %s", terminal, next)
			}
		}
	}
	if canRefreshNativeCommandState("delivered", "accepted") || !canRefreshNativeCommandState("unknown", "accepted") {
		t.Fatal("progress and unknown recovery have different contracts")
	}
}

func TestNativeCommandRuntimeRefreshPostgres(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant, device := testTenantID(), uuid.NewString()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = insertNativeReadDevice(ctx, tx, tenant, device, "CMD-"+device, "command refresh fixture", "{}", true); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, statement := range []string{
			`DELETE FROM device_command_executions WHERE tenant_id=$1 AND device_id=$2`,
			`DELETE FROM device_access_bindings WHERE tenant_id=$1 AND device_id=$2`,
			`DELETE FROM devices WHERE tenant_id=$1 AND id=$2`,
		} {
			if _, err := db.Exec(ctx, statement, tenant, device); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})
	for _, tc := range []struct {
		name, initial, runtime, runtimeReason, want              string
		noID, delivered, denied, foreign, raceState, raceMessage bool
	}{
		{name: "broker acknowledgement is delivery only", initial: "accepted", runtime: "broker_acknowledged", want: "delivered"},
		{name: "dispatch is not delivery", initial: "accepted", runtime: "dispatching", want: "accepted"},
		{name: "unknown success recovers", initial: "unknown", runtime: "device_reported_success", want: "acknowledged"},
		{name: "unknown accepted recovers", initial: "unknown", runtime: "accepted", want: "accepted"},
		{name: "unknown failure recovers", initial: "unknown", runtime: "device_reported_failure", runtimeReason: "device refused", want: "failed"},
		{name: "unknown timeout recovers", initial: "unknown", runtime: "failed", runtimeReason: "device acknowledgement timed out", want: "timed_out"},
		{name: "generic failure is not timeout", initial: "accepted", runtime: "failed", runtimeReason: "HTTP timeout", want: "failed"},
		{name: "expired", initial: "unknown", runtime: "expired", want: "expired"},
		{name: "fenced is failure", initial: "accepted", runtime: "fenced", runtimeReason: "binding changed", want: "failed"},
		{name: "unknown stays unknown", initial: "unknown", runtime: "unknown", want: "unknown"},
		{name: "previous delivery preserved", initial: "unknown", runtime: "accepted", delivered: true, want: "delivered"},
		{name: "delivery cannot regress", initial: "delivered", runtime: "accepted", delivered: true, want: "delivered"},
		{name: "no runtime id cannot recover", initial: "unknown", noID: true, runtime: "device_reported_success", want: "unknown"},
		{name: "runtime denied retains state", initial: "unknown", runtime: "device_reported_success", denied: true, want: "unknown"},
		{name: "foreign tenant cannot query", initial: "unknown", runtime: "device_reported_success", foreign: true, want: "unknown"},
		{name: "success final", initial: "acknowledged", runtime: "failed", want: "acknowledged"},
		{name: "failure final", initial: "failed", runtime: "device_reported_success", want: "failed"},
		{name: "timeout final", initial: "timed_out", runtime: "device_reported_success", want: "timed_out"},
		{name: "expiry final", initial: "expired", runtime: "device_reported_success", want: "expired"},
		{name: "cancellation final", initial: "cancelled", runtime: "device_reported_success", want: "cancelled"},
		{name: "concurrent terminal wins", initial: "accepted", runtime: "device_reported_success", raceState: true, want: "cancelled"},
		{name: "changed runtime identity wins", initial: "accepted", runtime: "device_reported_success", raceMessage: true, want: "accepted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, msg := uuid.NewString(), uuid.NewString()
			var message *string
			if !tc.noID {
				message = &msg
			}
			_, err := db.Exec(ctx, `INSERT INTO device_command_executions(tenant_id,id,device_id,identifier,snapshot_id,parameters,state,reason,created_by,runtime_message_id,delivery_at)
				VALUES($1,$2,$3,'test',$4,'{}',$5,'previous uncertainty','tester',$6,CASE WHEN $7 THEN NOW() ELSE NULL END)`, tenant, id, device, uuid.NewString(), tc.initial, message, tc.delivered)
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "GET" || r.URL.Path != "/api/v1/command/datas/status/"+msg || r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Error("unexpected runtime request")
					w.WriteHeader(400)
					return
				}
				if tc.denied {
					w.WriteHeader(403)
					return
				}
				if tc.raceState {
					if _, err := db.Exec(ctx, `UPDATE device_command_executions SET state='cancelled' WHERE tenant_id=$1 AND id=$2`, tenant, id); err != nil {
						t.Error(err)
					}
				}
				if tc.raceMessage {
					if _, err := db.Exec(ctx, `UPDATE device_command_executions SET runtime_message_id=$3 WHERE tenant_id=$1 AND id=$2`, tenant, id, uuid.NewString()); err != nil {
						t.Error(err)
					}
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"message_id": msg, "status": tc.runtime, "error_message": tc.runtimeReason}})
			}))
			defer server.Close()
			t.Setenv("TP_COMMUNITY_BASE_URL", server.URL)
			client := communityclient.NewFromEnv()
			r := httptest.NewRequest(http.MethodGet, "http://native.test", nil)
			r.Header.Set("Authorization", "Bearer fixture-token")
			readctx := communityclient.WithAuthenticatedReadRequest(ctx, r)
			identity := nativeDeviceReadIdentity{tenantID: tenant}
			if tc.foreign {
				identity.tenantID = testTenantID()
			}
			refreshNativeDeviceCommandsWithClient(readctx, identity, device, id, client)
			var state string
			var reason *string
			var delivered, acknowledged bool
			if err := db.QueryRow(ctx, `SELECT state,reason,delivery_at IS NOT NULL,acknowledged_at IS NOT NULL FROM device_command_executions WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&state, &reason, &delivered, &acknowledged); err != nil {
				t.Fatal(err)
			}
			if state != tc.want {
				t.Fatalf("got %s want %s", state, tc.want)
			}
			wantCalls := int32(1)
			if tc.noID || tc.foreign || strings.Contains("|acknowledged|failed|timed_out|expired|cancelled|", "|"+tc.initial+"|") {
				wantCalls = 0
			}
			if calls.Load() != wantCalls {
				t.Fatalf("runtime calls %d want %d", calls.Load(), wantCalls)
			}
			changed := state != tc.initial && !tc.raceState
			if changed && tc.runtimeReason == "" && reason != nil {
				t.Errorf("obsolete uncertainty kept: %q", *reason)
			}
			if changed && tc.runtimeReason != "" && (reason == nil || *reason != tc.runtimeReason) {
				t.Error("runtime reason lost")
			}
			if acknowledged != (changed && state == "acknowledged") {
				t.Error("device acknowledgement timestamp invented or missing")
			}
			if delivered != (tc.delivered || changed && state == "delivered") {
				t.Error("delivery timestamp invented or missing")
			}
		})
	}
}

func TestNativeCommandWhenOnline(t *testing.T) {
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	ok, err := decodeNativeCommandSubmission([]byte(`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"deliveryMode":"when_online","expiresAt":"` + future + `"}`))
	if err != nil || ok.DeliveryMode != "when_online" || ok.ExpiresAt == nil {
		t.Fatalf("%+v %v", ok, err)
	}
	if d, err := decodeNativeCommandSubmission([]byte(`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"deliveryMode":"when_online"}`)); err != nil || d.ExpiresAt != nil {
		t.Fatalf("%+v %v", d, err)
	}
	for _, bad := range []string{
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"deliveryMode":"when_online","expiresAt":"2020-01-01T00:00:00Z"}`,
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"deliveryMode":"when_online","expiresAt":"soon"}`,
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"expiresAt":"` + future + `"}`,
	} {
		if _, err := decodeNativeCommandSubmission([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	for in, want := range map[string]string{"pending": "accepted", "sent": "delivered", "expired": "expired", "x": "unknown"} {
		if mapExpectedStatus(in) != want {
			t.Errorf("%s", in)
		}
	}
}

func TestNativeCancelReason(t *testing.T) {
	if r, err := decodeNativeCancelReason([]byte(`{"reason":"no longer needed"}`)); err != nil || r != "no longer needed" {
		t.Fatalf("%q %v", r, err)
	}
	for _, bad := range []string{`[]`, `{}`, `{"reason":""}`, `{"reason":"  "}`, `{"reason":1}`, `{"reason":"x","y":1}`} {
		if _, err := decodeNativeCancelReason([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestNativeCommandListDeliveryModeFilter(t *testing.T) {
	q, err := decodeNativeCommandListQuery(url.Values{"deliveryMode": {"when_online"}, "state": {"accepted"}})
	if err != nil || q.deliveryMode != "when_online" || q.state != "accepted" {
		t.Fatalf("%v %+v", err, q)
	}
	for _, bad := range []string{"", "queued", "WHEN_ONLINE"} {
		if _, err := decodeNativeCommandListQuery(url.Values{"deliveryMode": {bad}}); err == nil {
			t.Errorf("accepted deliveryMode %q", bad)
		}
	}
}
