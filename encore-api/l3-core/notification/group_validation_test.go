package notification

import (
	"context"
	"encoding/json"
	"testing"

	"encore.app/l3-core/notification/contract"
)

type groupValidationFixture struct {
	instance    contract.InstanceView
	plugin      contract.PluginView
	instanceErr error
	pluginErr   error
}

func (f groupValidationFixture) GetInstance(context.Context, string, string) (contract.InstanceView, error) {
	return f.instance, f.instanceErr
}

func (f groupValidationFixture) GetPlugin(context.Context, string, string) (contract.PluginView, error) {
	return f.plugin, f.pluginErr
}

func TestValidateGroupAcceptsTenantScopedCompatibleLiteralAndTextBinding(t *testing.T) {
	fixture := groupValidationFixture{
		instance: contract.InstanceView{ID: "instance-1", PluginRegistrationID: "plugin-1", Channel: contract.ChannelSMS, Enabled: true},
		plugin: contract.PluginView{
			ID: "plugin-1", Enabled: true,
			Manifest: contract.PluginManifestSummary{
				Channels: []contract.Channel{contract.ChannelSMS}, ContentModes: []string{"text"},
				RecipientSchema: json.RawMessage(`{"type":"object","required":["kind","address"],"additionalProperties":false,"properties":{"kind":{"const":"phone"},"address":{"type":"string","pattern":"^\\+86[0-9]{11}$"}}}`),
			},
		},
	}
	request := contract.GroupCreate{
		Name: "Cold room", Enabled: true,
		Bindings: []contract.GroupBinding{{
			BindingID: "binding-1", InstanceID: "instance-1",
			RecipientSource: json.RawMessage(`{"kind":"literal","recipient":{"kind":"phone","address":"+8613800138000"}}`),
			ContentBinding:  json.RawMessage(`{"kind":"text","title":"{{device.name}} alert","text":"{{payload.subject}}"}`),
		}},
	}
	if fields := validateGroupCreate(context.Background(), fixture, "tenant-1", request); len(fields) != 0 {
		t.Fatalf("valid group rejected: %+v", fields)
	}
}

func TestValidateGroupRejectsCrossTenantAndMismatchedBindingBeforeStore(t *testing.T) {
	fixture := groupValidationFixture{
		instanceErr: storeErr(StoreErrorNotFound),
	}
	request := contract.GroupCreate{
		Name: "Group", Enabled: true,
		Bindings: []contract.GroupBinding{{
			BindingID: "binding-1", InstanceID: "foreign-instance",
			RecipientSource: json.RawMessage(`{"kind":"literal","recipient":{"kind":"phone","address":"+8613800138000"}}`),
			ContentBinding:  json.RawMessage(`{"kind":"text","title":"alert","text":"hello"}`),
		}},
	}
	fields := validateGroupCreate(context.Background(), fixture, "tenant-1", request)
	if len(fields) != 1 || fields[0].Field != "bindings[0].instanceId" || fields[0].Reason != "instance_not_found" {
		t.Fatalf("cross-tenant instance result = %+v", fields)
	}
}

func TestValidateDisabledGroupMayRetainDisabledInstanceButRejectsInvalidLegacyMember(t *testing.T) {
	fixture := groupValidationFixture{
		instance: contract.InstanceView{ID: "instance-1", PluginRegistrationID: "plugin-1", Channel: contract.ChannelIM, Enabled: false},
		plugin: contract.PluginView{
			ID: "plugin-1", Enabled: false,
			Manifest: contract.PluginManifestSummary{
				Channels: []contract.Channel{contract.ChannelIM}, ContentModes: []string{"template"},
				RecipientSchema: json.RawMessage(`{"type":"object"}`),
			},
		},
	}
	request := contract.GroupCreate{
		Name: "Legacy APP", Enabled: false,
		Bindings: []contract.GroupBinding{{
			BindingID: "binding-1", InstanceID: "instance-1",
			RecipientSource: json.RawMessage(`{"kind":"member","userId":"member-1","contactField":"applicationUserId"}`),
			ContentBinding:  json.RawMessage(`{"kind":"template","templateId":"legacy","locale":"zh-CN","paramsMapping":{}}`),
		}},
	}
	fields := validateGroupCreate(context.Background(), fixture, "tenant-1", request)
	if len(fields) != 1 || fields[0].Field != "bindings[0].recipientSource" {
		t.Fatalf("unsupported APP recipient should be rejected without rejecting disabled instance: %+v", fields)
	}
	request.Bindings[0].RecipientSource = json.RawMessage(`{"kind":"literal","recipient":{"kind":"user_id","address":"provider-user-1"}}`)
	fields = validateGroupCreate(context.Background(), fixture, "tenant-1", request)
	if len(fields) != 0 {
		t.Fatalf("disabled group with disabled same-tenant instance should be retainable: %+v", fields)
	}
}

func TestValidateGroupRejectsUnknownBindingFieldsAndExecutableTemplateSyntax(t *testing.T) {
	if err := validateGroupRecipient(json.RawMessage(`{"kind":"literal","recipient":{"kind":"phone","address":"+8613800138000"},"sourceMapping":{}}`), contract.ChannelSMS, json.RawMessage(`{"type":"object"}`)); err == nil {
		t.Fatal("unknown recipient source field accepted")
	}
	if validNotificationTextTemplate("{{eval(device.name)}}") {
		t.Fatal("arbitrary expression accepted as template variable")
	}
	if validNotificationTextTemplate("alert {{device.name") {
		t.Fatal("unclosed placeholder accepted")
	}
}
