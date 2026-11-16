package brokercontrol

import (
	"project/internal/brokercontract"
	"strings"
	"testing"
)

func TestConnectionTopicsMatchOwnerPolicy(t *testing.T) {
	d := DeviceRecord{DeviceNumber: "qa-direct"}
	out := standardConnectionTopics(d)
	policy := buildDevicePolicy(d, nil, brokercontract.PrincipalDevice)
	if !out.Available || len(out.Items) != len(policy.Publish)+len(policy.Subscribe) {
		t.Fatal(out)
	}
	pub, sub := map[string]bool{}, map[string]bool{}
	for _, v := range policy.Publish {
		pub[v] = true
	}
	for _, v := range policy.Subscribe {
		sub[v] = true
	}
	for _, item := range out.Items {
		if item.Direction == "publish" {
			if strings.Contains(item.Topic, "+") || !pub[strings.ReplaceAll(item.Topic, "{message_id}", "+")] {
				t.Fatalf("not owner publish: %+v", item)
			}
		} else if !sub[item.Topic] {
			t.Fatalf("not owner subscribe: %+v", item)
		}
	}
}
func TestConnectionTopicsRejectUnsafeNumber(t *testing.T) {
	for _, n := range []string{"", "a/b", "a+", "a#", "a\x00"} {
		if standardConnectionTopics(DeviceRecord{DeviceNumber: n}).Available {
			t.Fatalf("accepted %q", n)
		}
	}
}
