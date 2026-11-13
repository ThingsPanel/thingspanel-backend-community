package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"project/internal/dal"
	"project/internal/model"
	"project/pkg/errcode"
	"project/pkg/utils"
)

type nativeMetricOption struct {
	Key      string             `json:"key"`
	Label    string             `json:"label"`
	DataType string             `json:"data_type"`
	Unit     string             `json:"unit"`
	Enum     []nativeMetricEnum `json:"enum,omitempty"`
	Params   string             `json:"params,omitempty"`
}

type nativeMetricEnum struct {
	ValueType   string          `json:"value_type"`
	Value       json.RawMessage `json:"value"`
	Description string          `json:"description"`
}

type nativeMetricGroup struct {
	DataSourceType string               `json:"data_source_type"`
	Options        []nativeMetricOption `json:"options"`
}

type nativeMetricField struct {
	Kind       string                  `json:"kind"`
	Identifier string                  `json:"identifier"`
	Name       nativeCommandI18n       `json:"nameI18n"`
	Unit       string                  `json:"unit"`
	ValueType  *nativeCommandValueType `json:"valueType"`
	Parameters *nativeCommandParams    `json:"parameters"`
}

// All consumers read the exact published binding. No draft or legacy lookup.
func readNativeBindingSnapshot(ctx context.Context, authorization string, config *model.DeviceConfig) (*nativeMarketSnapshotResponse, error) {
	if config == nil || config.DeviceTemplateID != nil || config.NativeThingModelID == nil ||
		config.NativeThingModelSnapshotID == nil || config.NativeThingModelRevision == nil || config.NativeBindingRevision < 1 {
		return nil, fmt.Errorf("native device-config binding is incomplete; select a published model version")
	}
	modelID, snapshotID := strings.TrimSpace(*config.NativeThingModelID), strings.TrimSpace(*config.NativeThingModelSnapshotID)
	revision := *config.NativeThingModelRevision
	if modelID == "" || snapshotID == "" || revision < 1 {
		return nil, fmt.Errorf("native device-config binding is incomplete; select a published model version")
	}
	if strings.TrimSpace(authorization) == "" {
		return nil, fmt.Errorf("authenticated Community session is required to read native metadata")
	}
	client, err := newNativeMarketImportClient()
	if err != nil {
		return nil, err
	}
	snapshot, err := client.readSnapshot(ctx, authorization, modelID, revision)
	if err != nil {
		return nil, fmt.Errorf("read bound native model: %w", err)
	}
	if snapshot.SnapshotID != snapshotID {
		return nil, fmt.Errorf("native snapshot does not match the device-config binding")
	}
	return snapshot, nil
}

func nativeConfigMetricMenu(ctx context.Context, configID string, claims *utils.UserClaims, authorization string, kinds ...string) ([]nativeMetricGroup, error) {
	if claims == nil || claims.TenantID == "" {
		return nil, errcode.NewWithMessage(errcode.CodeNoPermission, "tenant session is required")
	}
	config, err := dal.GetDeviceConfigByID(configID)
	if err != nil {
		return nil, err
	}
	if config.TenantID != claims.TenantID {
		return nil, errcode.NewWithMessage(errcode.CodeNoPermission, "device template is not owned by the current tenant")
	}
	snapshot, err := readNativeBindingSnapshot(ctx, authorization, config)
	if err != nil {
		return nil, errcode.NewWithMessage(errcode.CodeParamError, err.Error())
	}
	groups, err := nativeMetricGroups(snapshot.Model, kinds...)
	if err != nil {
		return nil, errcode.NewWithMessage(errcode.CodeParamError, err.Error())
	}
	return groups, nil
}

func nativeDeviceMetricMenu(ctx context.Context, deviceID string, claims *utils.UserClaims, authorization string, kinds ...string) ([]nativeMetricGroup, error) {
	if claims == nil || claims.TenantID == "" {
		return nil, errcode.NewWithMessage(errcode.CodeNoPermission, "tenant session is required")
	}
	device, err := dal.GetDeviceByID(deviceID)
	if err != nil {
		return nil, err
	}
	if device.TenantID != claims.TenantID {
		return nil, errcode.NewWithMessage(errcode.CodeNoPermission, "device is not owned by the current tenant")
	}
	if device.DeviceConfigID == nil || *device.DeviceConfigID == "" {
		return nil, errcode.NewWithMessage(errcode.CodeParamError, "device has no template binding")
	}
	return nativeConfigMetricMenu(ctx, *device.DeviceConfigID, claims, authorization, kinds...)
}

func nativeMetricGroups(raw json.RawMessage, kinds ...string) ([]nativeMetricGroup, error) {
	var definition struct {
		Fields []nativeMetricField `json:"fields"`
	}
	if err := json.Unmarshal(raw, &definition); err != nil || definition.Fields == nil {
		return nil, fmt.Errorf("native model fields are invalid")
	}
	groups := make([]nativeMetricGroup, 0, len(kinds))
	for _, kind := range kinds {
		group := nativeMetricGroup{DataSourceType: strings.ToLower(kind), Options: []nativeMetricOption{}}
		if kind == "ATTRIBUTE" {
			group.DataSourceType = "attributes"
		}
		for _, field := range definition.Fields {
			if field.Kind != kind {
				continue
			}
			option, err := nativeMetricFromField(field)
			if err != nil {
				return nil, err
			}
			group.Options = append(group.Options, option)
		}
		if len(group.Options) > 0 {
			groups = append(groups, group)
		}
	}
	return groups, nil
}

func nativeMetricFromField(field nativeMetricField) (nativeMetricOption, error) {
	option := nativeMetricOption{Key: field.Identifier, Label: field.Name.Default, Unit: field.Unit}
	if option.Key == "" || option.Label == "" {
		return option, fmt.Errorf("native field identifier and name are required")
	}
	if field.Kind == "COMMAND" || field.Kind == "EVENT" {
		if field.Parameters == nil || field.Parameters.SchemaVersion != 1 {
			return option, fmt.Errorf("native field %s has invalid parameters", field.Identifier)
		}
		parameters := make([]legacyCommandParameter, 0, len(field.Parameters.Items))
		for _, parameter := range field.Parameters.Items {
			mapped, err := mapNativeCommandParameter(parameter, field.Identifier+"."+parameter.Identifier)
			if err != nil {
				return option, err
			}
			parameters = append(parameters, mapped)
		}
		encoded, err := json.Marshal(parameters)
		option.DataType, option.Params = "String", string(encoded)
		return option, err
	}
	if field.ValueType == nil {
		return option, fmt.Errorf("native field %s has no value type", field.Identifier)
	}
	typ := field.ValueType
	switch typ.Kind {
	case "INT", "FLOAT", "NUMBER":
		option.DataType = "Number"
	case "STRING":
		option.DataType = "String"
	case "BOOL":
		option.DataType = "Boolean"
		if typ.Constraint != nil {
			if typ.Constraint.TrueText != "" {
				option.Enum = append(option.Enum, nativeMetricEnum{ValueType: "BOOLEAN", Value: json.RawMessage("true"), Description: typ.Constraint.TrueText})
			}
			if typ.Constraint.FalseText != "" {
				option.Enum = append(option.Enum, nativeMetricEnum{ValueType: "BOOLEAN", Value: json.RawMessage("false"), Description: typ.Constraint.FalseText})
			}
		}
	case "ENUM":
		if len(typ.EnumValues) == 0 {
			return option, fmt.Errorf("native enum %s has no options", field.Identifier)
		}
		option.DataType = "Enum"
		for _, value := range typ.EnumValues {
			if err := validateNativeEnumCode(value.Code, typ.BaseKind, field.Identifier); err != nil {
				return option, err
			}
			option.Enum = append(option.Enum, nativeMetricEnum{ValueType: typ.BaseKind, Value: value.Code, Description: value.Label.Default})
		}
	default:
		return option, fmt.Errorf("unsupported native value type %q", typ.Kind)
	}
	return option, nil
}
