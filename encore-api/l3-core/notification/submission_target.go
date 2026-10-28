package notification

import (
	"context"
	"encoding/json"

	"encore.app/l3-core/notification/contract"
)

type targetPluginReader interface {
	GetPlugin(context.Context, string, string) (contract.PluginView, error)
}

// TargetValidator uses the registered schema and tenant-scoped instance, never
// a provider name or a network call. Validate/save therefore cannot send.
type TargetValidator struct {
	Instances InstanceRepository
	Plugins   targetPluginReader
}

func (v *TargetValidator) Validate(ctx context.Context, tenant string, delivery contract.SubmitDelivery) error {
	if v == nil || v.Instances == nil || v.Plugins == nil || tenant == "" {
		return coreFailure(503, "notification_unavailable")
	}
	instance, err := v.Instances.GetInstance(ctx, tenant, delivery.InstanceID)
	if err != nil {
		return err
	}
	if instance.ID != delivery.InstanceID || !instance.Enabled {
		return coreFailure(422, "instance_unavailable")
	}
	plugin, err := v.Plugins.GetPlugin(ctx, tenant, instance.PluginRegistrationID)
	if err != nil {
		return err
	}
	if !plugin.Enabled || plugin.ID != instance.PluginRegistrationID {
		return coreFailure(422, "plugin_unavailable")
	}
	channelOK := false
	for _, channel := range plugin.Manifest.Channels {
		if channel == instance.Channel {
			channelOK = true
			break
		}
	}
	if !channelOK {
		return coreFailure(422, "channel_unsupported")
	}
	modeOK := false
	for _, mode := range plugin.Manifest.ContentModes {
		if mode == delivery.Content.Kind {
			modeOK = true
			break
		}
	}
	if !modeOK {
		return coreFailure(422, "content_mode_unsupported")
	}
	schema, err := CompileNotificationSchema(plugin.Manifest.RecipientSchema)
	if err != nil {
		return coreFailure(422, "recipient_schema_unsupported")
	}
	raw, err := json.Marshal(delivery.Recipient)
	if err != nil || ValidateNotificationSchema(schema, raw) != nil {
		return coreFailure(422, "recipient_invalid")
	}
	return nil
}
