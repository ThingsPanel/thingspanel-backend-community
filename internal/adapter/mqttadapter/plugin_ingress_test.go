package mqttadapter

import (
	"encoding/base64"
	"reflect"
	"testing"

	bc "project/internal/brokercontract"
)

func pluginTarget(id, number, prefix string, kind bc.PrincipalKind) bc.PluginTarget {
	return bc.PluginTarget{TenantID: "tenant-a", DeviceID: id, DeviceNumber: number, TopicPrefix: prefix,
		MappingVersion: "standard-v1", Kind: kind, RouteEpoch: 3, BindingPhase: "active"}
}

func TestPluginWireEnvelopeDecodesValuesExactlyOnce(t *testing.T) {
	raw := []byte{0x00, 0xff, 0x00, '{', 'x', '}'}
	body := []byte(`{"device_id":"dev-1","values":"` + base64.StdEncoding.EncodeToString(raw) + `"}`)
	decoded, err := decodePluginWirePayload(body)
	if err != nil || !decoded.Wrapped || decoded.DeviceID != "dev-1" || !reflect.DeepEqual(decoded.Values, raw) {
		t.Fatalf("decode wrapper: %#v, %v", decoded, err)
	}
	if _, err := decodePluginWirePayload([]byte(`{"device_id":"dev-1","values":"AP8"}`)); err == nil {
		t.Fatal("accepted malformed/non-padded legacy base64")
	}
}

func TestPluginPrincipalPreservesDirectDevicePayload(t *testing.T) {
	body := []byte(`{"device_id":"logical-id","values":{"temperature":21}}`)
	decoded, err := decodePluginPrincipalPayload(body, false)
	if err != nil || decoded.Wrapped || decoded.DeviceID != "" || !reflect.DeepEqual(decoded.Values, body) {
		t.Fatalf("direct-device payload was reinterpreted: %#v, %v", decoded, err)
	}

	legacy := []byte(`{"device_id":"logical-id","values":"e30="}`)
	decoded, err = decodePluginPrincipalPayload(legacy, true)
	if err != nil || !decoded.Wrapped || decoded.DeviceID != "logical-id" || string(decoded.Values) != "{}" {
		t.Fatalf("service plugin compatibility wrapper: %#v, %v", decoded, err)
	}
}

func TestPluginTopicScopeAndCanonicalSelection(t *testing.T) {
	if got, ok := stripPluginTopicPrefix("plugin/alarm/devices/telemetry", "plugin/alarm/"); !ok || got != "devices/telemetry" {
		t.Fatalf("prefix strip: %q %v", got, ok)
	}
	if got, ok := stripPluginTopicPrefix("devices/telemetry", "plugin/alarm/"); !ok || got != "devices/telemetry" {
		t.Fatalf("canonical service/device topic: %q %v", got, ok)
	}

	candidates := []pluginTopicCandidate{
		{target: pluginTarget("dev-1", "101", "plugin/alarm/", bc.PrincipalDevice), canonical: "devices/telemetry"},
		{target: pluginTarget("dev-2", "102", "plugin/alarm/", bc.PrincipalDevice), canonical: "devices/telemetry"},
	}
	if _, _, err := selectPluginBusinessTarget(candidates, pluginWirePayload{}); err == nil {
		t.Fatal("ambiguous multi-target payload selected a device")
	}
	got, _, err := selectPluginBusinessTarget(candidates, pluginWirePayload{DeviceID: "102", Wrapped: true})
	if err != nil || got.DeviceID != "dev-2" {
		t.Fatalf("explicit legacy target selection: %#v, %v", got, err)
	}
}

func TestPluginCanonicalBareCommandAckHasExistingTopicShape(t *testing.T) {
	kind, messageID, err := classifyIngressTopic("devices/command/response/receipt-123")
	if err != nil || kind != "command_response" || messageID != "receipt-123" {
		t.Fatalf("canonical service ACK topic: %q %q %v", kind, messageID, err)
	}
}

func TestPluginStatusSupportsDeviceAndGatewayScopes(t *testing.T) {
	for _, tt := range []struct {
		kind  bc.PrincipalKind
		topic string
	}{
		{bc.PrincipalDevice, "devices/status/dev-1"},
		{bc.PrincipalGateway, "gateway/status/gw-1"},
	} {
		target := pluginTarget("dev-1", "101", "plugin/alarm/", tt.kind)
		if tt.kind == bc.PrincipalGateway {
			target = pluginTarget("gw-1", "201", "plugin/alarm/", tt.kind)
		}
		selected, topic, err := selectPluginStatusTarget([]pluginTopicCandidate{{target: target, canonical: tt.topic}}, pluginWirePayload{Values: []byte("1")})
		if err != nil || selected.DeviceID != target.DeviceID || topic != tt.topic {
			t.Fatalf("status target %s: %#v %q %v", tt.topic, selected, topic, err)
		}
	}
	if _, _, err := selectPluginStatusTarget([]pluginTopicCandidate{{target: pluginTarget("dev-1", "101", "plugin/alarm/", bc.PrincipalDevice), canonical: "gateway/status/dev-1"}}, pluginWirePayload{Values: []byte("1")}); err != nil {
		t.Fatal("trusted device target did not accept legacy gateway status family")
	}
}

func TestPluginIngestionIDParsesBootAndSequence(t *testing.T) {
	boot, seq, err := parsePluginIngestionID("mosq-boot-id-a1")
	if err != nil || boot != "boot-id" || seq != 0xa1 {
		t.Fatalf("parse ingestion id: %q %x %v", boot, seq, err)
	}
	for _, input := range []string{"", "mosq-boot-0", "mosq--1", "mosq-boot-xyz", "mosq-a/evil-1"} {
		if _, _, err := parsePluginIngestionID(input); err == nil {
			t.Fatalf("accepted malformed ingestion id %q", input)
		}
	}
}
