package notification

import (
	"context"
	"encoding/json"
	"testing"

	"encore.app/l3-core/notification/contract"
)

type targetFixture struct {
	instance contract.InstanceView
	plugin   contract.PluginView
	tenants  []string
}

func (f *targetFixture) GetInstance(_ context.Context, tenant, _ string) (contract.InstanceView, error) {
	f.tenants = append(f.tenants, tenant)
	return f.instance, nil
}
func (f *targetFixture) GetPlugin(_ context.Context, tenant, _ string) (contract.PluginView, error) {
	f.tenants = append(f.tenants, tenant)
	return f.plugin, nil
}

func TestTargetValidatorUsesRegisteredSchemaWithoutProviderBranch(t *testing.T) {
	f := &targetFixture{instance: contract.InstanceView{ID: "instance-1", PluginRegistrationID: "plugin-1", Channel: "sms", Enabled: true}, plugin: contract.PluginView{ID: "plugin-1", Enabled: true, Manifest: contract.PluginManifestSummary{Channels: []contract.Channel{"sms"}, ContentModes: []string{"template"}, RecipientSchema: json.RawMessage(`{"type":"object","required":["kind","address"],"additionalProperties":false,"properties":{"kind":{"const":"phone"},"address":{"type":"string","pattern":"^\\+86[0-9]{11}$"}}}`)}}}
	v := &TargetValidator{Instances: f, Plugins: f}
	delivery := contract.SubmitDelivery{InstanceID: "instance-1", Recipient: contract.Recipient{Kind: "phone", Address: "+8613800000000"}, Content: contract.Content{Kind: "template", Template: &contract.TemplateContent{ID: "approved", Params: map[string]json.RawMessage{}}}}
	if err := v.Validate(context.Background(), "tenant-1", delivery); err != nil {
		t.Fatal(err)
	}
	for _, tenant := range f.tenants {
		if tenant != "tenant-1" {
			t.Fatal("lookup escaped tenant scope")
		}
	}
	delivery.Recipient.Address = "user-123"
	if err := v.Validate(context.Background(), "tenant-1", delivery); err == nil {
		t.Fatal("non-phone target accepted")
	}
	delivery.Recipient.Address = "+8613800000000"
	delivery.Content.Kind = "text"
	if err := v.Validate(context.Background(), "tenant-1", delivery); err == nil {
		t.Fatal("unsupported mode accepted")
	}
	f.instance.Enabled = false
	delivery.Content.Kind = "template"
	if err := v.Validate(context.Background(), "tenant-1", delivery); err == nil {
		t.Fatal("disabled instance accepted")
	}
}
