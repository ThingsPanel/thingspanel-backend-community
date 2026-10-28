package brokercontrol

import (
	"context"
	"testing"

	"project/internal/brokercontract"
	"project/internal/brokermapper"
)

func TestServiceCustomPublishACLIsDerivedFromAuthorizedCanonicalFamily(t *testing.T) {
	snapshot, err := brokermapper.Compile([]brokermapper.Rule{{
		ID: 7, Direction: "down", SourceTopic: "custom/{device_number}/cmd/#",
		TargetTopic: "devices/command/{device_number}/{message_id}", DataIdentifier: "set_temp",
	}})
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepository{snapshots: map[string]*brokermapper.Snapshot{"cfg-1": snapshot}}
	policy := brokercontract.Policy{Publish: []string{"devices/command/num-1/+"}, MaxPayload: 1024, MaxQoS: 1, RetainProfile: brokercontract.RetainedLiveIngress}
	targets := []DeviceRecord{
		{ID: "dev-1", TenantID: "tenant-1", DeviceNumber: "num-1", DeviceNumberCount: 1, DeviceConfigID: ptrString("cfg-1")},
		{ID: "dev-2", TenantID: "tenant-1", DeviceNumber: "num-2", DeviceNumberCount: 1, DeviceConfigID: ptrString("cfg-1")},
	}
	got, protected, err := addServiceMappingPublishAliases(context.Background(), repo, policy, targets)
	if err != nil {
		t.Fatal(err)
	}
	if !containsExact(got.Publish, "custom/num-1/cmd/#") || containsExact(got.Publish, "custom/num-2/cmd/#") {
		t.Fatalf("custom filters exceed configured canonical target scope: %v", got.Publish)
	}
	if len(protected) != 1 || protected[0] != "custom/num-1/cmd/#" {
		t.Fatalf("protected command filters = %v", protected)
	}
}

func ptrString(v string) *string { return &v }
