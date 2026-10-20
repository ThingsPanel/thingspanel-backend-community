package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"project/internal/model"
)

type nativeCommandI18n struct {
	Default string `json:"default"`
}

type nativeCommandModel struct {
	Fields []json.RawMessage `json:"fields"`
}

type nativeCommandField struct {
	Kind            string               `json:"kind"`
	Identifier      string               `json:"identifier"`
	NameI18n        nativeCommandI18n    `json:"nameI18n"`
	DescriptionI18n *nativeCommandI18n   `json:"descriptionI18n"`
	Parameters      *nativeCommandParams `json:"parameters"`
}

type nativeCommandParams struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Items         []nativeCommandParameter `json:"items"`
}

type nativeCommandParameter struct {
	Identifier      string                 `json:"identifier"`
	NameI18n        nativeCommandI18n      `json:"nameI18n"`
	DescriptionI18n *nativeCommandI18n     `json:"descriptionI18n"`
	ValueType       nativeCommandValueType `json:"valueType"`
}

type nativeCommandValueType struct {
	Kind       string `json:"kind"`
	BaseKind   string `json:"baseKind"`
	EnumValues []struct {
		Code  json.RawMessage   `json:"code"`
		Label nativeCommandI18n `json:"label"`
	} `json:"enumValues"`
}

type legacyCommandParameter struct {
	ID             string             `json:"id"`
	DataName       string             `json:"data_name"`
	DataIdentifier string             `json:"data_identifier"`
	ParamType      string             `json:"param_type"`
	DataType       string             `json:"data_type"`
	Description    string             `json:"description"`
	EnumConfig     []legacyEnumOption `json:"enum_config"`
}

type legacyEnumOption struct {
	Value json.RawMessage `json:"value"`
	Desc  string          `json:"desc"`
}

// Native binding identity is the three reference columns. Binding revision is
// a CAS counter and remains non-zero after an intentional unbind.
func hasNativeDeviceConfigBinding(config *model.DeviceConfig) bool {
	return config != nil && (config.NativeThingModelID != nil || config.NativeThingModelSnapshotID != nil ||
		config.NativeThingModelRevision != nil)
}

func nativeDeviceCommands(ctx context.Context, communityAuthorization string, config *model.DeviceConfig) ([]model.GetCommandListRes, error) {
	if config == nil || config.NativeThingModelID == nil || config.NativeThingModelSnapshotID == nil ||
		config.NativeThingModelRevision == nil || config.NativeBindingRevision <= 0 {
		return nil, fmt.Errorf("native device-config binding is incomplete")
	}
	if config.DeviceTemplateID != nil {
		return nil, fmt.Errorf("native device-config binding cannot also reference a legacy template")
	}
	modelID := strings.TrimSpace(*config.NativeThingModelID)
	snapshotID := strings.TrimSpace(*config.NativeThingModelSnapshotID)
	revision := *config.NativeThingModelRevision
	if modelID == "" || snapshotID == "" || revision <= 0 {
		return nil, fmt.Errorf("native device-config binding is incomplete")
	}
	if strings.TrimSpace(communityAuthorization) == "" {
		return nil, fmt.Errorf("authenticated Community session is required to read native commands")
	}

	client, err := newNativeMarketImportClient()
	if err != nil {
		return nil, err
	}
	snapshot, err := client.readSnapshot(ctx, communityAuthorization, modelID, revision)
	if err != nil {
		return nil, fmt.Errorf("read bound native command snapshot: %w", err)
	}
	if snapshot.SnapshotID != snapshotID {
		return nil, fmt.Errorf("native command snapshot does not match the device-config binding")
	}
	return nativeCommandListFromModel(snapshot.Model)
}

// nativeCommandListFromModel adapts the native v2 COMMAND definition to the
// existing GetCommandListRes.params JSON-string contract used by Community UI.
func nativeCommandListFromModel(rawModel json.RawMessage) ([]model.GetCommandListRes, error) {
	var nativeModel nativeCommandModel
	if err := json.Unmarshal(rawModel, &nativeModel); err != nil || nativeModel.Fields == nil {
		return nil, fmt.Errorf("native command model fields are invalid")
	}
	result := make([]model.GetCommandListRes, 0)
	for index, rawField := range nativeModel.Fields {
		var field nativeCommandField
		if err := json.Unmarshal(rawField, &field); err != nil {
			return nil, fmt.Errorf("fields[%d]: invalid native field", index)
		}
		if field.Kind != "TELEMETRY" && field.Kind != "ATTRIBUTE" && field.Kind != "EVENT" && field.Kind != "COMMAND" {
			return nil, fmt.Errorf("fields[%d].kind: unsupported native field kind %q", index, field.Kind)
		}
		if field.Kind != "COMMAND" {
			continue
		}
		path := fmt.Sprintf("fields[%d]", index)
		if strings.TrimSpace(field.Identifier) == "" {
			return nil, fmt.Errorf("%s.identifier: required", path)
		}
		if field.Parameters == nil {
			return nil, fmt.Errorf("%s.parameters: required for COMMAND", path)
		}
		if field.Parameters.SchemaVersion != 1 {
			return nil, fmt.Errorf("%s.parameters.schemaVersion: unsupported version", path)
		}
		if field.Parameters.Items == nil {
			return nil, fmt.Errorf("%s.parameters.items: required array", path)
		}

		params := make([]legacyCommandParameter, 0, len(field.Parameters.Items))
		for paramIndex, parameter := range field.Parameters.Items {
			paramPath := fmt.Sprintf("%s.parameters.items[%d]", path, paramIndex)
			if strings.TrimSpace(parameter.Identifier) == "" {
				return nil, fmt.Errorf("%s.identifier: required", paramPath)
			}
			mapped, err := mapNativeCommandParameter(parameter, paramPath)
			if err != nil {
				return nil, err
			}
			params = append(params, mapped)
		}
		encodedParams, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("%s.parameters: encode legacy command parameters: %w", path, err)
		}
		name := field.NameI18n.Default
		if name == "" {
			name = field.Identifier
		}
		description := ""
		if field.DescriptionI18n != nil {
			description = field.DescriptionI18n.Default
		}
		result = append(result, model.GetCommandListRes{
			Name: name, Identifier: field.Identifier, Params: string(encodedParams), Description: description,
		})
	}
	return result, nil
}

func mapNativeCommandParameter(parameter nativeCommandParameter, path string) (legacyCommandParameter, error) {
	valueType := parameter.ValueType
	paramType, dataType := "", ""
	enumConfig := make([]legacyEnumOption, 0)
	switch valueType.Kind {
	case "ENUM":
		if valueType.BaseKind != "STRING" && valueType.BaseKind != "NUMBER" && valueType.BaseKind != "BOOLEAN" {
			return legacyCommandParameter{}, fmt.Errorf("%s.valueType.baseKind: unsupported enum base kind %q", path, valueType.BaseKind)
		}
		paramType, dataType = "Enum", valueType.BaseKind
		if len(valueType.EnumValues) == 0 {
			return legacyCommandParameter{}, fmt.Errorf("%s.valueType.enumValues: required array", path)
		}
		enumConfig = make([]legacyEnumOption, 0, len(valueType.EnumValues))
		for enumIndex, option := range valueType.EnumValues {
			enumPath := fmt.Sprintf("%s.valueType.enumValues[%d].code", path, enumIndex)
			if err := validateNativeEnumCode(option.Code, valueType.BaseKind, enumPath); err != nil {
				return legacyCommandParameter{}, err
			}
			enumConfig = append(enumConfig, legacyEnumOption{Value: option.Code, Desc: option.Label.Default})
		}
	case "BOOL":
		paramType, dataType = "Boolean", "Boolean"
	case "INT", "FLOAT", "NUMBER":
		paramType, dataType = "Number", "Number"
	case "STRING":
		paramType, dataType = "string", "String"
	default:
		return legacyCommandParameter{}, fmt.Errorf("%s.valueType.kind: unsupported native command parameter type %q", path, valueType.Kind)
	}
	name := parameter.NameI18n.Default
	if name == "" {
		name = parameter.Identifier
	}
	description := ""
	if parameter.DescriptionI18n != nil {
		description = parameter.DescriptionI18n.Default
	}
	return legacyCommandParameter{
		ID: parameter.Identifier, DataName: name, DataIdentifier: parameter.Identifier,
		ParamType: paramType, DataType: dataType, Description: description, EnumConfig: enumConfig,
	}, nil
}

func validateNativeEnumCode(raw json.RawMessage, baseKind, path string) error {
	if len(raw) == 0 || !json.Valid(raw) {
		return fmt.Errorf("%s: enum code is missing or invalid", path)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("%s: enum code cannot be null", path)
	}
	switch baseKind {
	case "STRING":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("%s: expected string enum code", path)
		}
	case "BOOLEAN":
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("%s: expected boolean enum code", path)
		}
	case "NUMBER":
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("%s: expected numeric enum code", path)
		}
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("%s: expected numeric enum code", path)
		}
		if err := validateJavaScriptNumber(number); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	default:
		return fmt.Errorf("%s: unsupported enum base kind %q", path, baseKind)
	}
	return nil
}

func validateJavaScriptNumber(number json.Number) error {
	value, err := strconv.ParseFloat(number.String(), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("numeric enum code is outside the JavaScript number range")
	}
	original, ok := new(big.Rat).SetString(number.String())
	if !ok {
		return fmt.Errorf("numeric enum code is not a supported JSON number")
	}
	if original.IsInt() {
		maxSafeInteger := big.NewInt(1<<53 - 1)
		integer := new(big.Int).Quo(original.Num(), original.Denom())
		if new(big.Int).Abs(integer).Cmp(maxSafeInteger) > 0 {
			return fmt.Errorf("integer enum code exceeds JavaScript's safe integer range")
		}
	}
	javascriptRoundTrip, ok := new(big.Rat).SetString(strconv.FormatFloat(value, 'g', -1, 64))
	if !ok || original.Cmp(javascriptRoundTrip) != 0 {
		return fmt.Errorf("numeric enum code would change in the JavaScript UI")
	}
	return nil
}
