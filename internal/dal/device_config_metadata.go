package dal

import (
	"errors"
	"gorm.io/gorm"
	"project/internal/model"
	"time"
)

var ErrDeviceConfigBindingConflict = errors.New("native and legacy templates cannot be bound together")

type DeviceConfigMutation struct {
	Condition *model.DeviceConfigCondition
	Before    *model.DeviceConfig
	After     *model.DeviceConfig
}

func UpdateDeviceConfigConditional(id, tenantID string, updates map[string]interface{}, revision *int, condition *model.DeviceConfigCondition) (*DeviceConfigMutation, error) {
	// Do not mutate a caller's map or bypass the owner transaction on mainline.
	values := make(map[string]interface{}, len(updates)+2)
	for k, v := range updates {
		values[k] = v
	}
	values["updated_at"] = time.Now().UTC()
	if revision != nil {
		values["native_binding_revision"] = gorm.Expr("native_binding_revision + 1")
	}
	result := &DeviceConfigMutation{Condition: condition}
	if err := updateDeviceConfigMetadataOwner(id, tenantID, values, revision, result); err != nil {
		return nil, err
	}
	return result, nil
}

// Called only after tenant-scoped row locking, before any persisted change.
func prepareDeviceConfigMetadata(config *model.DeviceConfig, values map[string]interface{}, result *DeviceConfigMutation) error {
	if err := result.Condition.Check(config); err != nil {
		return err
	}
	legacy := effectiveMetadataBinding(config.DeviceTemplateID, values, "device_template_id")
	native := effectiveMetadataBinding(config.NativeThingModelID, values, "native_thing_model_id")
	if legacy != "" && native != "" {
		return ErrDeviceConfigBindingConflict
	}
	if protocol, ok := metadataString(values["protocol_type"]); ok && config.ProtocolType != nil && protocol != *config.ProtocolType {
		if protocol == "MQTT" {
			values["voucher_type"] = "ACCESSTOKEN"
		} else {
			values["voucher_type"] = nil
		}
	}
	result.Before = config
	return nil
}

// Evaluate the final locked row symmetrically. Explicit nil/empty clears a
// binding; an omitted field retains it. The service's existing native-switch
// operation explicitly supplies legacy=nil after verifying the snapshot.
func effectiveMetadataBinding(current *string, values map[string]interface{}, key string) string {
	if value, provided := values[key]; provided {
		text, _ := metadataString(value)
		return text
	}
	if current != nil {
		return *current
	}
	return ""
}

// The service's DTO mapper deliberately retains pointers to distinguish omitted
// fields. Internal DAL callers also supply plain strings.
func metadataString(value interface{}) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case *string:
		if value != nil {
			return *value, true
		}
	}
	return "", false
}
