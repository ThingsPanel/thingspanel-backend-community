package notification

import (
	"context"
	"encoding/json"
	"encore.app/l3-core/notification/contract"
	"testing"
)

type appMembers struct{}

func (appMembers) ResolveMember(_ context.Context, tenant, user, field string) ([]contract.Recipient, error) {
	if user == "empty" {
		return nil, nil
	}
	return []contract.Recipient{{Kind: "user_id", Address: "phone1"}, {Kind: "user_id", Address: "phone2"}, {Kind: "user_id", Address: "phone1"}}, nil
}
func TestAppMemberFanout(t *testing.T) {
	r, event := sourceResolverFixture()
	f := r.Groups.(resolverFixture)
	f.group.Bindings[0].RecipientSource = json.RawMessage(`{"kind":"member","userId":"member","contactField":"appDevice"}`)
	empty := f.group.Bindings[0]
	empty.BindingID = "empty"
	empty.RecipientSource = json.RawMessage(`{"kind":"member","userId":"empty","contactField":"appDevice"}`)
	f.group.Bindings = append(f.group.Bindings, empty)
	r.Groups = f
	r.Members = appMembers{}
	plan, err := r.PlanSourceEvent(context.Background(), event.TenantID, event)
	if err != nil || plan.BlockedReason != "" || len(plan.Deliveries) != 2 {
		t.Fatal("multi-device/dedup/empty member", plan, err)
	}
	f.group.Bindings = []contract.GroupBinding{empty}
	r.Groups = f
	plan, err = r.PlanSourceEvent(context.Background(), event.TenantID, event)
	if err != nil || plan.BlockedReason != "app_no_active_devices" {
		t.Fatal(plan, err)
	}
	schema := json.RawMessage(`{"type":"object","properties":{"kind":{"const":"user_id"},"address":{"type":"string"}},"required":["kind","address"]}`)
	if validateGroupRecipient(empty.RecipientSource, contract.ChannelIM, schema, "thingspanel.unipush") != nil {
		t.Fatal("App group rejected")
	}
	if validateGroupRecipient(empty.RecipientSource, contract.ChannelIM, schema, "other.plugin") == nil {
		t.Fatal("member binding accepted by unrelated IM plugin")
	}
}
