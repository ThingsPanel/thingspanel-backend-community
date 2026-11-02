package brokercontrol

import (
	bc "project/internal/brokercontract"
	"testing"
)

func TestProtocolTelemetryBackendAliasKeepsCanonicalAndLegacyFamiliesSeparate(t *testing.T) {
	target := bc.PluginTarget{TenantID: "tenant", DeviceID: "device-id", DeviceNumber: "number-1", TopicPrefix: "plugin/modbus/", MappingVersion: "standard-v1", Kind: bc.PrincipalGateway, RouteEpoch: 3, BindingPhase: "active", LegacyTelemetryID: true}
	canonical := "gateway/telemetry/control/number-1"
	policy, err := addPluginPublishAliases(bc.Policy{Publish: []string{canonical}, MaxPayload: 1024, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress}, []bc.PluginTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range []string{canonical, target.TopicPrefix + canonical, "plugin/modbus/devices/telemetry/control/device-id"} {
		if !containsExact(policy.Publish, topic) {
			t.Fatalf("required finite topic missing: %s in %+v", topic, policy.Publish)
		}
	}
	if containsExact(policy.Publish, "plugin/modbus/gateway/telemetry/control/device-id") {
		t.Fatal("invented gateway-family device-ID wire alias")
	}
	if containsExact(policy.Publish, "plugin/modbus/devices/telemetry/control/+") {
		t.Fatal("legacy compatibility widened target authorization")
	}
	target.LegacyTelemetryID = false
	ordinary, err := addPluginPublishAliases(bc.Policy{Publish: []string{canonical}, MaxPayload: 1024, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress}, []bc.PluginTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if containsExact(ordinary.Publish, "plugin/modbus/devices/telemetry/control/device-id") {
		t.Fatal("non-protocol plugin gained legacy wire alias")
	}
}
