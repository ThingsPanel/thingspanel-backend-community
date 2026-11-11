package brokercontract

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDelegationSurvivesReceiptJSONWithoutLosingEpochs(t *testing.T) {
	d := Delegation{ContextID: strings.Repeat("a", 64), Actor: Principal{TenantID: "t", DeviceID: "svc", Kind: PrincipalService}, ConnectionID: "c", BrokerCell: "a", BrokerInstance: "i", TargetDeviceID: "child", TargetRouteEpoch: 3, TransportDeviceID: "root", TransportRouteEpoch: 9, MappingVersion: "standard-v1", ObservedAt: time.Now().UTC()}
	raw, _ := json.Marshal(d)
	var restored any
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	got, err := ParseDelegation(restored)
	if err != nil || got.TargetRouteEpoch != 3 || got.TransportRouteEpoch != 9 || got.Actor != d.Actor {
		t.Fatalf("proof roundtrip failed: %+v %v", got, err)
	}
	for _, mutate := range []func(*Delegation){func(d *Delegation) { d.Actor.Kind = PrincipalDevice }, func(d *Delegation) { d.TransportRouteEpoch = 0 }, func(d *Delegation) { d.ContextID = "untrusted" }, func(d *Delegation) { d.TransportDeviceID = d.TargetDeviceID }} {
		bad := d
		mutate(&bad)
		if _, err := ParseDelegation(bad); err == nil {
			t.Fatalf("invalid proof accepted: %+v", bad)
		}
	}
}

func TestPluginContextHashIncludesIndependentTransportSnapshot(t *testing.T) {
	target := PluginTarget{TenantID: "t", DeviceID: "child", DeviceNumber: "1", TopicPrefix: "plugin/a/", MappingVersion: "standard-v1", Kind: PrincipalDevice, RouteEpoch: 3, BindingPhase: "active", TransportDeviceID: "root", TransportRouteEpoch: 9}
	actor := Principal{TenantID: "t", DeviceID: "svc", Kind: PrincipalService}
	first, err := PluginContextID(actor, "c", "i", "connection", []PluginTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	target.TransportRouteEpoch++
	next, _ := PluginContextID(actor, "c", "i", "connection", []PluginTarget{target})
	if first == next {
		t.Fatal("root migration did not change immutable context identity")
	}
	legacy := target
	legacy.TransportDeviceID = ""
	legacy.TransportRouteEpoch = 0
	if legacy.Validate() != nil {
		t.Fatal("legacy self-transport snapshot lost compatibility")
	}
	legacy.TransportDeviceID = "root"
	if legacy.Validate() == nil {
		t.Fatal("partial root route accepted")
	}
}

func TestPluginTargetSetRequiresExplicitPhysicalRoot(t *testing.T) {
	child := PluginTarget{TenantID: "t", DeviceID: "child", DeviceNumber: "1", TopicPrefix: "plugin/a/", MappingVersion: "standard-v1", Kind: PrincipalDevice, RouteEpoch: 3, BindingPhase: "active", TransportDeviceID: "root", TransportRouteEpoch: 9}
	if ValidatePluginTargets([]PluginTarget{child}) == nil {
		t.Fatal("unlisted root granted implicitly")
	}
	root := child
	root.DeviceID = "root"
	root.DeviceNumber = "2"
	root.RouteEpoch = 9
	root.TransportDeviceID = "root"
	root.TransportRouteEpoch = 9
	if err := ValidatePluginTargets([]PluginTarget{child, root}); err != nil {
		t.Fatal(err)
	}
	root.TenantID = "other"
	if ValidatePluginTargets([]PluginTarget{child, root}) == nil {
		t.Fatal("cross-tenant root substituted")
	}
}
