package automation

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/runtime"
	"encore.app/automation/store"
	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
)

func TestQ09TrustedOwnerGatewayPathRetainsReceiptAndReplay(t *testing.T) {
	e := newEnv(t)
	token := strings.Repeat("q", 32)
	t.Setenv("TP_AUTOMATION_SERVICE_TOKEN", token)
	t.Setenv("TP_SOURCE_DEPLOYMENT_ID", "qa-q09-owner")
	body, err := json.Marshal(UplinkRequest{Class: "gateway_telemetry", DeviceID: e.dev1, GatewayID: e.dev2, MessageID: runtime.NewID(), Values: `{"temperature":35}`})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest("POST", "/internal/automation/uplink", strings.NewReader(string(body)))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Automation-Deployment", "qa-q09-owner")
		request.Header.Set("X-Automation-Tenant", e.tenant)
		response := httptest.NewRecorder()
		e.svc().OwnerUplink(response, request)
		if response.Code != 200 {
			t.Fatalf("trusted owner path failed: %d %s", response.Code, response.Body.String())
		}
		var received UplinkResponse
		if err := json.Unmarshal(response.Body.Bytes(), &received); err != nil {
			t.Fatal(err)
		}
		if received.Accepted != 1-attempt || received.Duplicates != attempt {
			t.Fatalf("owner receipt replay changed: %+v", received)
		}
		var gateway string
		if err := e.st.DB.QueryRowContext(e.ctx(), `SELECT gateway_id FROM auto_inbox WHERE event_id=$1 AND tenant_id=$2`, received.EventIDs[0], e.tenant).Scan(&gateway); err != nil || gateway != e.dev2 {
			t.Fatalf("validated owner gateway context lost: %s %v", gateway, err)
		}
	}
}

func q09Authenticate(tenant string) {
	et.OverrideAuthInfo(auth.UID("qa-q09-"+tenant), &authn.Data{UserID: "qa-q09-" + tenant, TenantID: tenant, Roles: []string{"TENANT_ADMIN"}})
}

func TestQ09RawIngressSameSourceIDDoesNotCrossTenantDeduplication(t *testing.T) {
	a, b := newEnv(t), newEnv(t)
	defer et.OverrideAuthInfo("", nil)
	sharedID := "qa-q09-shared-" + runtime.NewID()
	for _, e := range []*testEnv{a, b} {
		q09Authenticate(e.tenant)
		rule := e.createRule(e.baseDefinition())
		req := &IngestRequest{EventID: sharedID, DeviceID: e.dev1, Signal: "telemetry", Key: "temperature", Value: "35", Unit: "℃", ObservedAt: e.now}
		first, err := e.svc().IngestEvent(e.ctx(), req)
		if err != nil {
			t.Fatalf("first tenant ingress: %v", err)
		}
		if first.Duplicate {
			t.Errorf("independent tenant event was swallowed as another tenant's duplicate")
		}
		if got := len(e.runs(rule.ID)); got != 1 {
			t.Errorf("tenant should execute exactly once, got %d runs", got)
		}
		replay, err := e.svc().IngestEvent(e.ctx(), req)
		if err != nil || !replay.Duplicate || replay.Seq != first.Seq {
			t.Errorf("same tenant retry must keep original receipt: %v", err)
		}
		// Some clients retain the returned receipt ID instead of the original
		// source ID. It is still only a replay within its owning tenant/device.
		req.EventID = first.EventID
		returnedIDReplay, err := e.svc().IngestEvent(e.ctx(), req)
		if err != nil || !returnedIDReplay.Duplicate || returnedIDReplay.Seq != first.Seq {
			t.Errorf("returned receipt ID did not remain idempotent: %+v %v", returnedIDReplay, err)
		}
	}
}

func TestQ09ConcurrentTenantsMayReuseClientEventID(t *testing.T) {
	sharedID := "qa-q09-concurrent-" + runtime.NewID()
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	go func() { <-ready; <-ready; close(start) }()
	for _, name := range []string{"tenant_a", "tenant_b"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			// Encore gives each subtest its own request/auth context; never
			// override auth concurrently inside the same request.
			q09Authenticate(e.tenant)
			defer et.OverrideAuthInfo("", nil)
			rule := e.createRule(e.baseDefinition())
			ready <- struct{}{}
			select {
			case <-start:
			case <-time.After(10 * time.Second):
				t.Fatal("concurrent fixture did not become ready")
			}
			if got := authn.CurrentData().TenantID; got != e.tenant {
				t.Fatalf("test request lost its own auth context: %s", got)
			}
			result, err := e.svc().IngestEvent(e.ctx(), &IngestRequest{EventID: sharedID, DeviceID: e.dev1, Signal: "telemetry", Key: "temperature", Value: "35", Unit: "℃", ObservedAt: e.now})
			if err != nil || result.Duplicate {
				t.Fatalf("concurrent independent event was not accepted: %+v %v", result, err)
			}
			if got := len(e.runs(rule.ID)); got != 1 {
				t.Fatalf("expected one own run, got %d", got)
			}
		})
	}
}

func TestQ09RawIngressRejectsForeignDeviceBeforeStateMutation(t *testing.T) {
	a, b := newEnv(t), newEnv(t)
	defer et.OverrideAuthInfo("", nil)
	// The trusted directory knows B's device but does not return it for A.
	a.dir.devices[b.dev1] = b.dir.devices[b.dev1]
	q09Authenticate(a.tenant)
	_, err := a.svc().IngestEvent(a.ctx(), &IngestRequest{EventID: "qa-q09-foreign-" + runtime.NewID(), DeviceID: b.dev1, Signal: "telemetry", Key: "temperature", Value: "99", ObservedAt: a.now})
	if errs.Code(err) != errs.NotFound {
		t.Errorf("foreign device must be hidden before ingest, got %v", err)
	}
	_, exists, stateErr := a.st.LatestState(a.ctx(), b.dev1, domain.SignalTelemetry, "temperature")
	if stateErr != nil {
		t.Fatal(stateErr)
	}
	if exists {
		t.Error("foreign device state was mutated by tenant A")
	}
}

func TestQ09StoreRejectsForeignReceiptCollision(t *testing.T) {
	a, b := newEnv(t), newEnv(t)
	id := "qa-q09-store-collision-" + runtime.NewID()
	first := store.IngestEvent{EventID: id, TenantID: a.tenant, DeviceID: a.dev1, Signal: domain.SignalTelemetry, Key: "temperature", Value: 35, ObservedAt: a.now}
	if _, err := a.rt.Ingest(a.ctx(), first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.TenantID = b.tenant
	second.DeviceID = b.dev1
	if receipt, err := b.rt.Ingest(b.ctx(), second); err == nil || receipt != nil {
		t.Fatalf("foreign global receipt must not be accepted or disclosed: %+v %v", receipt, err)
	}
	_, exists, err := b.st.LatestState(b.ctx(), b.dev1, domain.SignalTelemetry, "temperature")
	if err != nil || exists {
		t.Fatalf("collision must not mutate state: exists=%v err=%v", exists, err)
	}
}

func TestQ09RawIngressKeepsOwnedLegacyReceiptIdempotent(t *testing.T) {
	e := newEnv(t)
	defer et.OverrideAuthInfo("", nil)
	q09Authenticate(e.tenant)
	id := "qa-q09-legacy-" + runtime.NewID()
	prior, err := e.rt.Ingest(e.ctx(), store.IngestEvent{EventID: id, TenantID: e.tenant, DeviceID: e.dev1, Signal: domain.SignalTelemetry, Key: "temperature", Value: "35", Source: "api", ObservedAt: e.now})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := e.svc().IngestEvent(e.ctx(), &IngestRequest{EventID: id, DeviceID: e.dev1, Signal: "telemetry", Key: "temperature", Value: "35", ObservedAt: e.now})
	if err != nil || !replay.Duplicate || replay.Seq != prior.Seq || replay.EventID != id {
		t.Fatalf("legacy accepted retry must retain original identity: %+v %v", replay, err)
	}
	// The same client source ID on a second owned device is independent.
	other, err := e.svc().IngestEvent(e.ctx(), &IngestRequest{EventID: id, DeviceID: e.dev2, Signal: "telemetry", Key: "temperature", Value: "35", ObservedAt: e.now})
	if err != nil || other.Duplicate || other.Seq == prior.Seq {
		t.Fatalf("legacy receipt swallowed another device event: %+v %v", other, err)
	}
}

func TestQ09PublicUplinkChecksDeviceAndGatewayOwnership(t *testing.T) {
	a, b := newEnv(t), newEnv(t)
	defer et.OverrideAuthInfo("", nil)
	a.dir.devices[b.dev1] = b.dir.devices[b.dev1]
	q09Authenticate(a.tenant)
	for _, tc := range []struct {
		name, device, gateway, class string
		code                         errs.ErrCode
	}{
		{"foreign_device", b.dev1, "", "telemetry", errs.NotFound},
		{"foreign_gateway", a.dev1, b.dev1, "telemetry", errs.PermissionDenied},
		{"same_tenant_unverified_gateway", a.dev1, a.dev2, "gateway_telemetry", errs.PermissionDenied},
		{"gateway_class_without_route", a.dev1, "", "gateway_telemetry", errs.PermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := a.svc().IngestUplink(a.ctx(), &UplinkRequest{Class: tc.class, DeviceID: tc.device, GatewayID: tc.gateway, MessageID: runtime.NewID(), Values: `{"temperature":99}`})
			if errs.Code(err) != tc.code {
				t.Fatalf("unowned ingress must be rejected: %v", err)
			}
			_, exists, err := a.st.LatestState(a.ctx(), tc.device, domain.SignalTelemetry, "temperature")
			if err != nil || exists {
				t.Fatalf("rejected uplink changed state: exists=%v err=%v", exists, err)
			}
		})
	}
	response, err := a.svc().IngestUplink(a.ctx(), &UplinkRequest{Class: "telemetry", DeviceID: a.dev1, MessageID: runtime.NewID(), Values: `{"temperature":35}`})
	if err != nil || response.Accepted != 1 {
		t.Fatalf("owned uplink rejected: %+v %v", response, err)
	}
}

func TestQ09UserIngressFailsClosedWhenOwnershipCannotBeVerified(t *testing.T) {
	e := newEnv(t)
	defer et.OverrideAuthInfo("", nil)
	q09Authenticate(e.tenant)
	for _, mode := range []string{"unavailable", "missing_directory", "unattributed_device"} {
		t.Run(mode, func(t *testing.T) {
			rt := e.rt
			e.dir.err = nil
			expected := errs.Unavailable
			switch mode {
			case "unavailable":
				e.dir.err = errors.New("private directory response")
			case "missing_directory":
				rt = runtime.New(e.st, e.cmds, e.notif, nil, func() time.Time { return e.now })
			case "unattributed_device":
				device := e.dir.devices[e.dev1]
				device.TenantID = ""
				e.dir.devices[e.dev1] = device
				expected = errs.NotFound
			}
			_, err := (&Service{rt: rt}).IngestEvent(e.ctx(), &IngestRequest{EventID: runtime.NewID(), DeviceID: e.dev1, Signal: "telemetry", Key: "temperature", Value: "99"})
			if errs.Code(err) != expected {
				t.Fatalf("ownership must fail closed: %v", err)
			}
			_, exists, stateErr := e.st.LatestState(e.ctx(), e.dev1, domain.SignalTelemetry, "temperature")
			if stateErr != nil || exists {
				t.Fatalf("unverified ingress changed state: exists=%v err=%v", exists, stateErr)
			}
		})
	}
}
