package mosquittodriver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	bc "project/internal/brokercontract"
)

func testPluginTarget() bc.PluginTarget {
	return bc.PluginTarget{TenantID: "tenant-a", DeviceID: "device-a", DeviceNumber: "1001", TopicPrefix: "service/alarm/", MappingVersion: "standard-1", Kind: bc.PrincipalDevice, RouteEpoch: 1, BindingPhase: "active"}
}

func testPluginServiceGrant() bc.Grant {
	target := testPluginTarget()
	return bc.Grant{
		PluginContextID: strings.Repeat("a", 64), PluginTargets: []bc.PluginTarget{target}, Allow: true,
		Principal: bc.Principal{TenantID: "tenant-a", DeviceID: "service-a", Kind: bc.PrincipalService},
		Policy: bc.Policy{
			Publish:    []string{"devices/telemetry", "devices/event/+", "devices/attributes/+", "devices/command/response/+", "devices/attributes/set/response/+", "devices/status/device-a", "service/alarm/devices/telemetry", "service/alarm/devices/event/+", "service/alarm/devices/attributes/+", "service/alarm/devices/command/response/+", "service/alarm/devices/attributes/set/response/+", "service/alarm/devices/status/device-a"},
			Subscribe:  []string{"service/alarm/devices/command/1001/+", "service/alarm/devices/attributes/set/1001/+", "service/alarm/devices/telemetry/control/1001", "service/alarm/devices/attributes/get/1001"},
			MaxPayload: bc.MaxPayloadBytes, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress,
		},
		PolicyVersion: "policy-1", MappingVersion: "standard-1", ConnectionID: "service-connection", RouteEpoch: 1,
		BindingPhase: "active", ExpiresAt: time.Now().Add(60 * time.Second),
	}
}

func TestPluginServicePublishKeepsSDKWireAndAddsTrustedEnvelope(t *testing.T) {
	grant := testPluginServiceGrant()
	d := commandDriver(t, grant)
	for _, topic := range []string{"service/alarm/devices/telemetry", "devices/event/event-1", "service/alarm/devices/attributes/attribute-1", "service/alarm/devices/command/response/cmd-1", "service/alarm/devices/attributes/set/response/attr-1", "service/alarm/devices/status/device-a"} {
		t.Run(topic, func(t *testing.T) {
			payload := []byte{0, 1, 2, 255, '{', 'x', '}'}
			internalTopic, body, retain, err := d.Rewrite(1, topic, payload, 1, false)
			if err != nil {
				t.Fatalf("plugin SDK publish denied: %v", err)
			}
			if retain || internalTopic == topic || !strings.HasPrefix(internalTopic, bc.IngressPrefix+"/") {
				t.Fatalf("publish did not use trusted ingress path: topic=%q retain=%v", internalTopic, retain)
			}
			env, err := bc.ParseIngress(internalTopic, body, bc.TrustedMetadata{BrokerCell: "community", BrokerInstance: "node-a", Principal: grant.Principal, ConnectionID: grant.ConnectionID, RouteEpoch: &grant.RouteEpoch})
			if err != nil {
				t.Fatalf("parse wrapped service ingress: %v", err)
			}
			original, err := base64.StdEncoding.DecodeString(env.Metadata.OriginalTopicB64)
			if err != nil || string(original) != topic || !bytes.Equal(env.Values, payload) {
				t.Fatalf("SDK topic/payload changed: topic=%q values=%x err=%v", original, env.Values, err)
			}
			if env.Metadata.PluginContextID != grant.PluginContextID || env.Metadata.ConnectionID != grant.ConnectionID || env.Metadata.IngestionID == "" || env.Metadata.ObservedAt.IsZero() || env.Metadata.PrincipalKind != bc.PrincipalService {
				t.Fatalf("trusted plugin metadata missing: %+v", env.Metadata)
			}
		})
	}
	for _, topic := range []string{"service/alarm/devices/command/1001/cmd-1", "service/alarm/devices/telemetry/control/1001", "service/other/devices/event/e1", "service/alarm/$tp-internal/ingress/v1/x"} {
		if _, _, _, err := d.Rewrite(1, topic, []byte(`{"device_id":"device-a"}`), 1, false); err == nil {
			t.Errorf("non-uplink or unregistered plugin topic accepted: %q", topic)
		}
	}
}

func TestPluginGatewayTargetAcceptsCanonicalDeviceFamilySDKTopics(t *testing.T) {
	grant := testPluginServiceGrant()
	grant.PluginTargets[0].Kind = bc.PrincipalGateway
	grant.Policy.Publish = append(grant.Policy.Publish, "devices/status/device-a", "gateway/telemetry", "gateway/status/device-a")
	d := commandDriver(t, grant)
	for _, topic := range []string{"devices/telemetry", "devices/status/device-a", "gateway/telemetry", "gateway/status/device-a"} {
		if _, body, _, err := d.Rewrite(1, topic, []byte("1"), 1, false); err != nil {
			t.Errorf("SDK topic %q denied for gateway target: %v", topic, err)
		} else if !bytes.Contains(body, []byte(grant.PluginContextID)) {
			t.Errorf("SDK topic %q did not carry plugin context", topic)
		}
	}
}

func TestPluginServiceWillStatusUsesTrustedEnvelope(t *testing.T) {
	grant := testPluginServiceGrant()
	d := commandDriver(t, grant)
	d.Closed(1)
	topic, body, retain, err := d.RewriteWill(1, "service/alarm/devices/status/device-a", []byte("0"), 1, false)
	if err != nil || retain {
		t.Fatalf("plugin service status Will denied: topic=%q retain=%v err=%v", topic, retain, err)
	}
	env, err := bc.ParseIngress(topic, body, bc.TrustedMetadata{BrokerCell: "community", BrokerInstance: "node-a", Principal: grant.Principal, ConnectionID: grant.ConnectionID, RouteEpoch: &grant.RouteEpoch})
	if err != nil || env.Metadata.PluginContextID != grant.PluginContextID || string(env.Values) != "0" {
		t.Fatalf("status Will was not enveloped with service context: env=%+v err=%v", env, err)
	}
}

func TestPluginStatusPreservesPhysicalValueAndClosedSessionEmitsOfflineOnly(t *testing.T) {
	grant := testPluginServiceGrant()
	d := commandDriver(t, grant)
	statusTopic := "devices/status/device-a" // canonical SDK topic remains valid alongside its registered prefix.
	for _, value := range []string{"1", "0"} {
		if _, _, _, err := d.Rewrite(1, statusTopic, []byte(value), 1, false); err != nil {
			t.Fatalf("canonical SDK status %q denied: %v", value, err)
		}
		d.mu.Lock()
		status := d.sessions[1].pluginStatuses["device-a"]
		status.lastEmitted = time.Now().Add(-31 * time.Second)
		d.sessions[1].pluginStatuses["device-a"] = status
		d.mu.Unlock()
		internalTopic, body, err := d.NextObservation()
		if err != nil {
			t.Fatalf("periodic status re-emission failed: %v", err)
		}
		env, err := bc.ParseIngress(internalTopic, body, bc.TrustedMetadata{BrokerCell: "community", BrokerInstance: "node-a", Principal: grant.Principal, ConnectionID: grant.ConnectionID, RouteEpoch: &grant.RouteEpoch})
		if err != nil || string(env.Values) != value || env.Metadata.PluginContextID != grant.PluginContextID {
			t.Fatalf("periodic status value changed: got=%q metadata=%+v err=%v want=%q", env.Values, env.Metadata, err, value)
		}
	}

	d.Closed(1)
	internalTopic, body, err := d.NextObservation()
	if err != nil {
		t.Fatalf("closed plugin status drain failed: %v", err)
	}
	env, err := bc.ParseIngress(internalTopic, body, bc.TrustedMetadata{BrokerCell: "community", BrokerInstance: "node-a", Principal: grant.Principal, ConnectionID: grant.ConnectionID, RouteEpoch: &grant.RouteEpoch})
	if err != nil || string(env.Values) != "0" {
		t.Fatalf("disconnect status did not become offline: got=%q err=%v", env.Values, err)
	}
	if topic, payload, err := d.NextObservation(); err != nil || topic != "" || payload != nil {
		t.Fatalf("disconnect status emitted repeatedly: topic=%q payload=%x err=%v", topic, payload, err)
	}

	dExpired := commandDriver(t, grant)
	if _, _, _, err := dExpired.Rewrite(1, statusTopic, []byte("1"), 1, false); err != nil {
		t.Fatal(err)
	}
	dExpired.mu.Lock()
	dExpired.sessions[1].grant.ExpiresAt = time.Now().Add(-time.Second)
	dExpired.mu.Unlock()
	if topic, payload, err := dExpired.NextObservation(); err != nil || topic != "" || payload != nil {
		t.Fatalf("expired plugin lease continued status refresh: topic=%q payload=%x err=%v", topic, payload, err)
	}
}

func TestPluginServiceWildcardSubscriptionsRemainFiniteAndReadIsTargeted(t *testing.T) {
	grant := testPluginServiceGrant()
	grant.Policy.Subscribe = append(grant.Policy.Subscribe,
		"service/alarm/devices/command/+/+", "service/alarm/devices/attributes/set/+/+",
		"service/alarm/devices/telemetry/control/+", "service/alarm/devices/attributes/get/+",
		"service/alarm/devices/event/response/+/+", "service/alarm/devices/attributes/response/+/+",
	)
	d := commandDriver(t, grant)
	for _, filter := range []string{
		"service/alarm/devices/command/+/+", "service/alarm/devices/attributes/set/+/+",
		"service/alarm/devices/telemetry/control/+", "service/alarm/devices/attributes/get/+",
		"service/alarm/devices/event/response/+/+", "devices/command/+/+",
	} {
		if !d.Authorize(1, 4, filter, 0, 1, false) {
			t.Errorf("finite plugin SDK wildcard subscribe denied: %q", filter)
		}
	}
	for _, filter := range []string{"service/other/devices/command/+/+", "service/alarm/devices/#", "service/alarm/devices/telemetry", "service/alarm/devices/command/+"} {
		if d.Authorize(1, 4, filter, 0, 1, false) {
			t.Errorf("unregistered/broad subscription accepted: %q", filter)
		}
	}

	now := time.Now()
	frameFor := func(target bc.PluginTarget, topic string) []byte {
		t.Helper()
		body, err := bc.EncodeCommandFrame(bc.CommandFrame{CommandID: "cmd-1", TenantID: target.TenantID, TargetDeviceID: target.DeviceID, RouteEpoch: target.RouteEpoch, MappingVersion: target.MappingVersion, WireTopic: topic, ExpiresAt: now.Add(time.Minute).UnixMilli(), Payload: []byte(`{"go":true}`)}, now)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	topic := "service/alarm/devices/command/1001/cmd-1"
	frame := frameFor(grant.PluginTargets[0], topic)
	if !d.AuthorizePayload(1, 1, topic, frame, 1, false) {
		t.Fatal("owner-scoped plugin frame delivery denied")
	}
	wire, changed, err := d.Outgoing(1, topic, frame, 1, false)
	if err != nil || !changed || string(wire) != `{"go":true}` {
		t.Fatalf("plugin frame was not stripped for the SDK: %s %v %v", wire, changed, err)
	}
	for name, mutate := range map[string]func(*bc.CommandFrame){
		"wrong tenant":  func(f *bc.CommandFrame) { f.TenantID = "tenant-b" },
		"wrong device":  func(f *bc.CommandFrame) { f.TargetDeviceID = "device-b" },
		"wrong epoch":   func(f *bc.CommandFrame) { f.RouteEpoch++ },
		"wrong mapping": func(f *bc.CommandFrame) { f.MappingVersion = "other" },
		"wrong prefix":  func(f *bc.CommandFrame) { f.WireTopic = "service/other/devices/command/1001/cmd-1" },
		"wrong number":  func(f *bc.CommandFrame) { f.WireTopic = "service/alarm/devices/command/1002/cmd-1" },
		"expired":       func(f *bc.CommandFrame) { f.ExpiresAt = now.Add(-time.Second).UnixMilli() },
	} {
		t.Run(name, func(t *testing.T) {
			f, err := bc.ParseCommandFrame(frame, now)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&f)
			bad, err := json.Marshal(f)
			if err != nil {
				t.Fatal(err)
			}
			bad = append([]byte("\x00TP-COMMAND/1\n"), bad...)
			if d.AuthorizePayload(1, 1, topic, bad, 1, false) {
				t.Fatal("frame outside exact plugin target was admitted")
			}
			if _, _, err := d.Outgoing(1, topic, bad, 1, false); err == nil {
				t.Fatal("frame outside exact plugin target was sent")
			}
		})
	}
	connecting := grant
	connecting.PluginTargets = append([]bc.PluginTarget(nil), grant.PluginTargets...)
	connecting.PluginTargets[0].BindingPhase = "connecting"
	dConnecting := commandDriver(t, connecting)
	if dConnecting.AuthorizePayload(1, 1, topic, frame, 1, false) {
		t.Fatal("plugin target in connecting phase received controlled frame")
	}
	dFenced := commandDriver(t, grant)
	dFenced.cfg.RuntimeFencing = true
	dFenced.runtimeValidUntil = time.Now().Add(time.Minute)
	dFenced.runtimeFences = make(map[string]uint64)
	dFenced.runtimeFences[runtimeScope("tenant-a", "device-a")] = 1
	if dFenced.AuthorizePayload(1, 1, topic, frame, 1, false) {
		t.Fatal("runtime-fenced plugin target received controlled frame")
	}
}

func TestPluginDirectDeviceCarriesContextAndDoesNotEmitSyntheticState(t *testing.T) {
	grant := testGrant()
	grant.PluginContextID = strings.Repeat("b", 64)
	grant.PluginTargets = []bc.PluginTarget{{TenantID: grant.Principal.TenantID, DeviceID: grant.Principal.DeviceID, DeviceNumber: "1001", TopicPrefix: "sdk/", MappingVersion: grant.MappingVersion, Kind: grant.Principal.Kind, RouteEpoch: grant.RouteEpoch, BindingPhase: "active"}}
	d := commandDriver(t, grant)
	internal, body, _, err := d.Rewrite(1, "devices/telemetry", []byte("{}"), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	env, err := bc.ParseIngress(internal, body, bc.TrustedMetadata{BrokerCell: "community", BrokerInstance: "node-a", Principal: grant.Principal})
	if err != nil || env.Metadata.PluginContextID != grant.PluginContextID {
		t.Fatalf("direct plugin device envelope lost context: %+v %v", env.Metadata, err)
	}
	if _, _, err := d.Observation(1, true); err == nil {
		t.Fatal("plugin context emitted a synthetic connection observation")
	}
	d.mu.Lock()
	s := d.sessions[1]
	s.lastObservation = time.Now().Add(-time.Hour)
	d.mu.Unlock()
	if topic, _, err := d.NextObservation(); err != nil || topic != "" {
		t.Fatalf("periodic socket status not suppressed for SDK plugin: %q %v", topic, err)
	}
}

func TestPluginGrantDecoderAcceptsOwnerBound256Targets(t *testing.T) {
	g := testPluginServiceGrant()
	g.PluginTargets = make([]bc.PluginTarget, 256)
	long := strings.Repeat("x", 230)
	prefix := strings.Repeat("p", 450) + "/"
	for i := range g.PluginTargets {
		g.PluginTargets[i] = bc.PluginTarget{TenantID: "tenant-a", DeviceID: "device-" + long + string(rune('a'+i%26)) + string(rune('A'+i/26)), DeviceNumber: "number-" + long, TopicPrefix: prefix, MappingVersion: "standard-1", Kind: bc.PrincipalDevice, RouteEpoch: 1, BindingPhase: "active"}
	}
	g.Policy.Publish = []string{"devices/telemetry"}
	g.Policy.Subscribe = []string{"devices/command/+/+"}
	encoded, err := json.Marshal(g)
	if err != nil || len(encoded) <= 65536 || len(encoded) > 1<<20 {
		t.Fatalf("fixture must exercise the supported >64 KiB grant bound: size=%d err=%v", len(encoded), err)
	}
	d := newTestDriver(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(encoded) }, 2)
	if !d.Begin(1, "client-a", "user", "password", "127.0.0.1", 4) || !poll(t, d).Allow {
		t.Fatalf("bounded 256-target owner grant rejected at %d bytes", len(encoded))
	}
}
