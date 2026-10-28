package mosquittodriver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	bc "project/internal/brokercontract"
)

func payloadBoundaryDriver(t *testing.T, serviceLimit int) *Driver {
	t.Helper()
	d := newTestDriver(t, func(w http.ResponseWriter, r *http.Request) {
		var request bc.AuthRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		grant := testGrant()
		switch request.Username {
		case "device-boundary":
			grant.Principal = bc.Principal{TenantID: "tenant-a", DeviceID: "device-a", Kind: bc.PrincipalDevice}
			grant.Policy = bc.Policy{Publish: []string{"devices/telemetry", "devices/event/+"}, MaxPayload: bc.MaxPayloadBytes, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress}
		case "service-boundary":
			grant.Principal = bc.Principal{TenantID: "tenant-a", DeviceID: "service-a", Kind: bc.PrincipalService}
			grant.Policy = bc.Policy{Subscribe: []string{bc.IngressPrefix + "/#"}, MaxPayload: serviceLimit, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress}
		default:
			t.Errorf("unexpected boundary principal %q", request.Username)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(grant)
	}, 8)
	t.Cleanup(d.Close)
	return d
}

func authorizeBoundaryLease(t *testing.T, d *Driver, ptr uintptr, user string) {
	t.Helper()
	if !d.Begin(ptr, "boundary-"+user, user, "secret", "127.0.0.1", 4) {
		t.Fatalf("begin %s lease failed", user)
	}
	if result := poll(t, d); !result.Allow {
		t.Fatalf("%s lease denied: %+v", user, result)
	}
	d.Connected(ptr)
}

func TestServiceIngressEnvelopeLimitUsesDecodedDevicePayload(t *testing.T) {
	d := payloadBoundaryDriver(t, bc.MaxPayloadBytes)
	authorizeBoundaryLease(t, d, 1, "device-boundary")
	authorizeBoundaryLease(t, d, 2, "service-boundary")

	// A legal 1 MiB device payload expands to roughly 1.4 MiB on the private
	// transport because Values is encoded once as base64 plus trusted metadata.
	raw := bytes.Repeat([]byte{0xa5}, bc.MaxPayloadBytes)
	topic, envelope, retain, err := d.Rewrite(1, "devices/telemetry", raw, 1, false)
	if err != nil || retain {
		t.Fatalf("rewrite maximum raw payload: topic=%q retain=%v err=%v", topic, retain, err)
	}
	if len(envelope) <= bc.MaxPayloadBytes || len(envelope) > bc.MaxEnvelopeBytes {
		t.Fatalf("expected base64-expanded bounded envelope; size=%d", len(envelope))
	}
	if !d.AuthorizePayload(2, 1, topic, envelope, 1, false) {
		t.Fatalf("service rejected %d-byte envelope whose decoded raw payload is exactly the 1 MiB lease limit", len(envelope))
	}
}

func TestServiceIngressLowerDecodedLimitAndMaximumOriginalTopic(t *testing.T) {
	d := payloadBoundaryDriver(t, bc.MaxPayloadBytes-1)
	authorizeBoundaryLease(t, d, 1, "device-boundary")
	authorizeBoundaryLease(t, d, 2, "service-boundary")
	raw := bytes.Repeat([]byte{0x5a}, bc.MaxPayloadBytes)
	topic, envelope, _, err := d.Rewrite(1, "devices/telemetry", raw, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if d.AuthorizePayload(2, 1, topic, envelope, 1, false) {
		t.Fatal("service accepted decoded payload one byte over its lease MaxPayload")
	}

	// The original wire topic may be at the protocol's full 65,535-byte limit.
	// Its base64 metadata remains inside the 2 MiB envelope ceiling and must not
	// be confused with the service's decoded business-payload limit.
	longTopic := "devices/event/" + strings.Repeat("x", bc.MaxWireTopicBytes-len("devices/event/"))
	shortRaw := []byte{0, 0xff, 1}
	longIngress, longEnvelope, _, err := d.Rewrite(1, longTopic, shortRaw, 1, false)
	if err != nil {
		t.Fatalf("rewrite maximum original topic: %v", err)
	}
	if len(longIngress) >= bc.MaxIngressTopicBytes || len(longEnvelope) > bc.MaxEnvelopeBytes {
		t.Fatalf("long-topic hash did not bound transport: topic=%d envelope=%d", len(longIngress), len(longEnvelope))
	}
	if !d.AuthorizePayload(2, 1, longIngress, longEnvelope, 1, false) {
		t.Fatalf("service rejected short business payload due to large original-topic metadata (envelope=%d)", len(longEnvelope))
	}
}

func TestServiceRejectsIngressEnvelopeAboveTwoMiB(t *testing.T) {
	d := payloadBoundaryDriver(t, bc.MaxPayloadBytes)
	authorizeBoundaryLease(t, d, 1, "service-boundary")
	topic := bc.IngressPrefix + "/dA/ZGV2aWNlLWE/t-wire"
	oversized := make([]byte, bc.MaxEnvelopeBytes+1)
	if d.AuthorizePayload(1, 1, topic, oversized, 1, false) {
		t.Fatalf("service accepted private ingress envelope of %d bytes, ceiling=%d", len(oversized), bc.MaxEnvelopeBytes)
	}
	if d.Authorize(1, 1, topic, len(oversized), 1, false) {
		t.Fatal("basic service READ ACL admitted over-ceiling envelope")
	}
}
