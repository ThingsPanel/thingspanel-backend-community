package brokercontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testPrincipal() Principal {
	return Principal{TenantID: "租户/a", DeviceID: "dev/测", Kind: PrincipalDevice}
}
func testMetadata(p Principal, topic []byte) Metadata {
	return Metadata{ContractVersion: ContractVersion, TenantID: p.TenantID, PrincipalKind: p.Kind, BrokerCell: "community-1", BrokerInstance: "mosquitto-1", ConnectionID: "conn-1", RouteEpoch: 12, PolicyVersion: "policy-3", MappingVersion: "mapping-2", OriginalTopicB64: EncodeOriginalTopic(topic), OriginalQoS: 1, ObservedAt: time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)}
}
func testTrust(p Principal, m Metadata) TrustedMetadata {
	epoch := m.RouteEpoch
	return TrustedMetadata{Principal: p, BrokerCell: m.BrokerCell, BrokerInstance: m.BrokerInstance, ConnectionID: m.ConnectionID, RouteEpoch: &epoch, PolicyVersion: m.PolicyVersion, MappingVersion: m.MappingVersion}
}

func TestGoldenEnvelopeAndParseArbitraryBinary(t *testing.T) {
	p := testPrincipal()
	wireTopic := []byte("设备/遥测")
	m := testMetadata(p, wireTopic)
	body, err := EncodeEnvelope(p, m, []byte{0, 1, 2, 255})
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile(filepath.Join("..", "..", "docs", "broker-contract-v1", "golden-envelope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, bytes.TrimSpace(golden)) {
		t.Fatalf("golden mismatch\n got: %s\nwant: %s", body, bytes.TrimSpace(golden))
	}
	topic, err := IngressTopic(p.TenantID, p.DeviceID, wireTopic)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseIngress(topic, body, testTrust(p, m))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Values, []byte{0, 1, 2, 255}) || got.DeviceID != p.DeviceID {
		t.Fatalf("round trip mismatch: %#v", got)
	}
}

func TestEmptyLivePayloadIsEmptyBase64AndRetainedClearIsZeroBytes(t *testing.T) {
	p := testPrincipal()
	wire := []byte("dev/state")
	m := testMetadata(p, wire)
	body, err := EncodeRetained(p, m, RetainedLiveIngress, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]json.RawMessage
	if err = json.Unmarshal(body, &obj); err != nil {
		t.Fatal(err)
	}
	if string(obj["values"]) != "\"\"" {
		t.Fatalf("empty payload must encode as empty base64 string, got %s", obj["values"])
	}
	clear, err := EncodeRetained(p, m, RetainedPlatform, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(clear) != 0 {
		t.Fatalf("platform clear must be true zero-byte payload: %q", clear)
	}
	if _, err = EncodeRetained(p, m, RetainedLiveIngress, true, []byte("x")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("retained live ingress should reject: %v", err)
	}
	if _, err = EncodeRetained(p, m, RetainedWire, true, []byte("x")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("wire retained transformation must be explicit unsupported: %v", err)
	}
}

func TestParseIngressRejectsTenantIdentityVersionAndMetadataForgery(t *testing.T) {
	p := testPrincipal()
	wire := []byte("x/y")
	m := testMetadata(p, wire)
	body, err := EncodeEnvelope(p, m, []byte("ok"))
	if err != nil {
		t.Fatal(err)
	}
	topic, _ := IngressTopic(p.TenantID, p.DeviceID, wire)
	cases := map[string]func([]byte) []byte{
		"forged tenant": func(b []byte) []byte { return bytes.Replace(b, []byte(p.TenantID), []byte("tenant-other"), 1) },
		"unknown metadata": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"contract_version":"1"`), []byte(`"unknown":"x","contract_version":"1"`), 1)
		},
		"unsupported version": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"contract_version":"1"`), []byte(`"contract_version":"2"`), 1)
		},
		"forged connection": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"connection_id":"conn-1"`), []byte(`"connection_id":"other"`), 1)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if _, e := ParseIngress(topic, mutate(body), testTrust(p, m)); e == nil {
				t.Fatal("accepted forged ingress")
			}
		})
	}
	other, _ := IngressTopic("tenant-other", p.DeviceID, wire)
	if _, err = ParseIngress(other, body, testTrust(p, m)); err == nil {
		t.Fatal("accepted cross-tenant ingress topic")
	}
}

func TestIngressTopicTypedEncodingHashAndInputValidation(t *testing.T) {
	p := testPrincipal()
	wire := []byte("a/b/+/literal")
	if _, err := IngressTopic(p.TenantID, p.DeviceID, wire); err == nil {
		t.Fatal("wire topic wildcard accepted")
	}
	if _, err := IngressTopic(p.TenantID, p.DeviceID, nil); err == nil {
		t.Fatal("empty wire topic accepted")
	}
	long := []byte(strings.Repeat("a", 50000))
	topic, err := IngressTopic(p.TenantID, p.DeviceID, long)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(topic, "/h-") || len(topic) > MaxIngressTopicBytes {
		t.Fatalf("expected bounded hash topic, got %d bytes: %s", len(topic), topic)
	}
	if err := ValidateIngressTopic(topic, p.TenantID, p.DeviceID, long); err != nil {
		t.Fatal(err)
	}
	if err := ValidateIngressTopic(topic, p.TenantID, p.DeviceID, append(long, 'b')); err == nil {
		t.Fatal("hash key accepted a different original topic")
	}
	if _, err := IngressTopic("tenant\n", p.DeviceID, []byte("x")); err == nil {
		t.Fatal("control character identity accepted")
	}
}

func TestACLSeparatesPublishAndSubscribeAndHandlesMixedFilters(t *testing.T) {
	p := testPrincipal()
	policy := Policy{Publish: []string{"devices/a/telemetry"}, Subscribe: []string{"devices/+/state", "devices/a/#"}, MaxPayload: 1024, MaxQoS: 1, RetainProfile: RetainedLiveIngress}
	if err := ValidatePolicy(policy); err != nil {
		t.Fatal(err)
	}
	for _, topic := range []string{"devices/a/state", "devices/a/state/led"} {
		if !FilterMatches("devices/a/#", topic) {
			t.Errorf("filter should match %q", topic)
		}
	}
	if FilterMatches("devices/+/state", "devices/a/state/led") {
		t.Fatal("+ matched multiple levels")
	}
	if FilterMatches("#", "$tp/ingress/v1/a/b/c") {
		t.Fatal("leading wildcard matched system topic")
	}
	if err := AuthorizeTopic(p, policy, ActionPublish, "devices/a/telemetry"); err != nil {
		t.Fatal(err)
	}
	wildcardPublish := policy
	wildcardPublish.Publish = []string{"devices/event/+", "devices/command/response/+"}
	if err := AuthorizeTopic(p, wildcardPublish, ActionPublish, "devices/event/42"); err != nil {
		t.Fatalf("publish filter did not authorize concrete event topic: %v", err)
	}
	if err := AuthorizeTopic(p, wildcardPublish, ActionPublish, "$tp/ingress/v1/a/b/c"); err == nil {
		t.Fatal("publish filter authorized internal ingress")
	}
	if err := AuthorizeTopic(p, Policy{Publish: []string{"devices/#"}}, ActionReceive, "devices/a/state"); err == nil {
		t.Fatal("receive unexpectedly used publish policy")
	}
	if err := AuthorizeTopic(p, policy, ActionReceive, "devices/a/state/led"); err != nil {
		t.Fatal("receive did not use a matching subscribe filter:", err)
	}
	if err := AuthorizeTopic(p, policy, ActionSubscribe, "devices/+/state"); err != nil {
		t.Fatal(err)
	}
	if err := AuthorizeTopic(p, policy, ActionPublish, "devices/+/state"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("publish wildcard should reject: %v", err)
	}
	if err := AuthorizeTopic(p, policy, ActionSubscribe, "devices/#"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("ungranted mixed filter should reject: %v", err)
	}
	if err := AuthorizeTopic(p, policy, ActionSubscribe, "$tp/#"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("device internal subscription should reject: %v", err)
	}
	if err := AuthorizeTopic(p, policy, ActionReceive, "$tp/ingress/v1/a/b/c"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("device internal delivery should reject: %v", err)
	}
	service := p
	service.Kind = PrincipalService
	servicePolicy := Policy{Subscribe: []string{"$tp/ingress/v1/#"}}
	if err := AuthorizeTopic(service, servicePolicy, ActionReceive, "$tp/ingress/v1/a/b/c"); err != nil {
		t.Fatalf("explicit service internal receive grant should be usable: %v", err)
	}
}

func TestAuthTypesValidateBeforeUse(t *testing.T) {
	req := AuthRequest{RequestID: "r1", Provider: "mosquitto", BrokerCell: "community-1", BrokerInstance: "node-1", WireClientID: "wire-1", Protocol: 4}
	if err := ValidateAuthRequest(req); err != nil {
		t.Fatal(err)
	}
	req.Protocol = 2
	if err := ValidateAuthRequest(req); !errors.Is(err, ErrInvalidMetadata) {
		t.Fatalf("invalid protocol accepted: %v", err)
	}
	p := testPrincipal()
	g := Grant{Allow: true, Principal: p, Policy: Policy{MaxPayload: 1024, MaxQoS: 1}, PolicyVersion: "p1", MappingVersion: "m1", ConnectionID: "c1", RouteEpoch: 1, ExpiresAt: time.Now().Add(time.Minute)}
	if err := ValidateGrant(g); err != nil {
		t.Fatal(err)
	}
	g.Policy.MaxQoS = 3
	if err := ValidateGrant(g); !errors.Is(err, ErrInvalidMetadata) {
		t.Fatalf("invalid grant accepted: %v", err)
	}
}

func TestUnsupportedCapabilityIsExplicit(t *testing.T) {
	c := CommunityMosquittoCapabilities()
	if c.Scope != "community-only" || c.PersistentQueueReplication != CapabilityUnsupported || c.ExactConnectionEvents != CapabilityNotTested {
		t.Fatalf("capability status must not overclaim: %#v", c)
	}
}

func TestFilterSubsetDoesNotBroadenSubscriptions(t *testing.T) {
	for _, tt := range []struct {
		a, r string
		ok   bool
	}{{"devices/command/a/+", "devices/command/a/c1", true}, {"devices/command/a/+", "devices/command/+/+", false}, {"devices/command/a/+", "devices/command/a/#", false}, {"foo/#", "foo", true}, {"foo/+", "foo", false}, {"#", "$tp/ingress/#", false}, {"$tp/ingress/#", "$tp/ingress/t/d/topic", true}, {"$tp/ingress/#", "$share/prod/$tp/ingress/#", false}, {"$share/prod/$tp/ingress/#", "$share/shadow/$tp/ingress/#", false}} {
		if got := FilterContains(tt.a, tt.r); got != tt.ok {
			t.Fatalf("contains %q %q=%v", tt.a, tt.r, got)
		}
	}
}
func TestSharedDeliveryUsesUnderlyingFilter(t *testing.T) {
	p := Principal{TenantID: "ta", DeviceID: "service", Kind: PrincipalService}
	policy := Policy{Subscribe: []string{"$share/worker/$tp/ingress/v1/#"}, MaxPayload: 1024, MaxQoS: 1, RetainProfile: RetainedLiveIngress}
	if e := AuthorizeTopic(p, policy, ActionSubscribe, "$share/worker/$tp/ingress/v1/#"); e != nil {
		t.Fatal(e)
	}
	if e := AuthorizeTopic(p, policy, ActionReceive, "$tp/ingress/v1/ta/da/t-wire"); e != nil {
		t.Fatal(e)
	}
	if e := AuthorizeTopic(p, policy, ActionSubscribe, "$share/other/$tp/ingress/v1/#"); e == nil {
		t.Fatal("unauthorized shared group")
	}
}

func TestProtectedNormalNamespaceStillRequiresACL(t *testing.T) {
	p := Principal{TenantID: "tenant", DeviceID: "device", Kind: PrincipalDevice}
	policy := Policy{Publish: []string{"#"}, Subscribe: []string{"#"}, MaxQoS: 1}
	// Ordinary MQTT wildcards can match the prefix; explicit authorization must not.
	if !FilterMatches("#", IngressPrefix+"/tenant/device/topic") {
		t.Fatal("not an ordinary topic")
	}
	for _, action := range []Action{ActionPublish, ActionSubscribe, ActionReceive} {
		if e := AuthorizeTopic(p, policy, action, IngressPrefix+"/tenant/device/topic"); e == nil {
			t.Fatalf("protected namespace allowed %s", action)
		}
	}
}

func TestFilterMatchesInternalCoversWildcardAndLegacyForms(t *testing.T) {
	for _, filter := range []string{"#", "+/#", "+/reply", "tp-internal/#", "tp-internal/+", "$tp", "$tp/#", "$tp-old/+", "$share/group/tp-internal/#", ".+"} {
		if !FilterMatchesInternal(filter) {
			t.Errorf("internal filter escaped detection: %q", filter)
		}
	}
	for _, filter := range []string{"devices/event/response/+/+", "gateway/attributes/response/+/+", "devices/command/device-a/+"} {
		if FilterMatchesInternal(filter) {
			t.Errorf("ordinary filter marked internal: %q", filter)
		}
	}
}
