package mosquittodriver

import (
	"testing"

	bc "project/internal/brokercontract"
)

func TestConnectingDeviceGrantAllowsPublishButDeniesCommandDelivery(t *testing.T) {
	grant := testGrant()
	grant.BindingPhase = "connecting"
	grant.Policy.Subscribe = []string{"devices/command/device-a/+"}
	d := commandDriver(t, grant)
	if !d.Authorize(1, 4, "devices/command/device-a/+", 0, 1, false) {
		t.Fatal("connecting grant could not retain command subscription")
	}
	if d.Authorize(1, 1, "devices/command/device-a/cmd-1", 1, 1, false) {
		t.Fatal("connecting grant delivered a command")
	}
	raw := []byte(`{"temperature":23}`)
	topic, payload, retain, err := d.Rewrite(1, "devices/telemetry", raw, 1, false)
	if err != nil || retain {
		t.Fatalf("connecting device could not publish trusted ingress: topic=%s err=%v", topic, err)
	}
	envelope, err := bc.ParseIngress(topic, payload, bc.TrustedMetadata{BrokerCell: "community", BrokerInstance: "node-a"})
	if err != nil || string(envelope.Values) != string(raw) {
		t.Fatalf("connecting telemetry rewrite invalid: envelope=%+v err=%v", envelope, err)
	}
}

func TestServiceCanReceiveInternalIngressAtAnyBindingPhase(t *testing.T) {
	grant := testGrant()
	grant.Principal = bc.Principal{TenantID: "tenant-a", DeviceID: "service-a", Kind: bc.PrincipalService}
	grant.BindingPhase = "connecting"
	grant.Policy = bc.Policy{Publish: []string{"devices/event/device-a/+"}, Subscribe: []string{bc.IngressPrefix + "/#"}, MaxPayload: 1024, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress}
	d := commandDriver(t, grant)
	if !d.Authorize(1, 1, bc.IngressPrefix+"/tenant-a/device-a/t-wire", 16, 1, false) {
		t.Fatal("service subscriber denied internal ingress")
	}
}

func TestActiveDeviceGrantCanReceiveOwnCommand(t *testing.T) {
	grant := testGrant()
	grant.BindingPhase = "active"
	d := commandDriver(t, grant)
	if !d.Authorize(1, 1, "devices/command/device-a/cmd-1", 1, 1, false) {
		t.Fatal("active grant denied command delivery")
	}
}
