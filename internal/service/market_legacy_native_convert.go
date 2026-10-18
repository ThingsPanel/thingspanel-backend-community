package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"

	"project/internal/model"
)

// ConvertedLegacyNativePackage contains only semantic content proven by the
// legacy package; it does not claim native model or snapshot provenance.
type ConvertedLegacyNativePackage struct {
	Model     json.RawMessage                     `json:"model"`
	Resources json.RawMessage                     `json:"resources"`
	Assets    []model.MarketNativeThingModelAsset `json:"assets,omitempty"`
}

// convertLegacyMarketTemplateToNative converts the historical device-template
// package shape into the frozen ThingModel v2 transport shape.
func convertLegacyMarketTemplateToNative(full *model.MarketTemplateFullData) (*ConvertedLegacyNativePackage, error) {
	if full == nil {
		return nil, fmt.Errorf("template: required")
	}
	definition, err := legacyObject(full.TemplateDefinition, "template_definition")
	if err != nil {
		return nil, err
	}
	if err := legacyRejectUnknown(definition, "template_definition", []string{
		"name", "category", "description", "web_chart_config", "app_chart_config", "cover_url", "template_path", "tags",
		"telemetry", "attributes", "commands", "events",
	}); err != nil {
		return nil, err
	}

	name, err := legacyResolvedText("name", full.Name, definition["name"], true)
	if err != nil {
		return nil, err
	}
	category, err := legacyResolvedText("category", full.Category, definition["category"], false)
	if err != nil {
		return nil, err
	}
	description, err := legacyResolvedText("description", full.Description, definition["description"], false)
	if err != nil {
		return nil, err
	}

	fields := make([]json.RawMessage, 0)
	seen := make(map[string]struct{})
	for _, group := range []struct {
		key  string
		kind string
	}{
		{"telemetry", "TELEMETRY"}, {"attributes", "ATTRIBUTE"}, {"events", "EVENT"}, {"commands", "COMMAND"},
	} {
		rows, err := legacyArray(definition[group.key], "template_definition."+group.key)
		if err != nil {
			return nil, err
		}
		for index, row := range rows {
			path := fmt.Sprintf("template_definition.%s[%d]", group.key, index)
			field, identifier, err := legacyField(row, group.kind, path)
			if err != nil {
				return nil, err
			}
			duplicateKey := group.kind + "\x00" + identifier
			if _, exists := seen[duplicateKey]; exists {
				return nil, fmt.Errorf("%s.data_identifier: duplicate identifier %q within %s", path, identifier, group.kind)
			}
			seen[duplicateKey] = struct{}{}
			fields = append(fields, field)
		}
	}

	web, err := legacyChart(definition, "web_chart_config")
	if err != nil {
		return nil, err
	}
	app, err := legacyChart(definition, "app_chart_config")
	if err != nil {
		return nil, err
	}
	cover, err := legacyCover(full, definition)
	if err != nil {
		return nil, err
	}
	tags, err := legacyTags(definition["tags"])
	if err != nil {
		return nil, err
	}

	modelObject := map[string]json.RawMessage{
		"name":   mustJSON(name),
		"fields": mustJSON(fields),
	}
	if strings.TrimSpace(description) != "" {
		modelObject["descriptionI18n"] = mustJSON(map[string]string{"default": description})
	}
	if strings.TrimSpace(category) != "" {
		modelObject["category"] = mustJSON(category)
	}
	modelJSON, err := json.Marshal(modelObject)
	if err != nil {
		return nil, fmt.Errorf("model: encode native ModelDraft: %w", err)
	}
	resourcesObject := map[string]json.RawMessage{
		"profile": mustJSON(map[string]interface{}{
			"author": full.Author, "tags": tags, "cover": "", "version": full.Version,
			"brand": full.Brand, "modelNumber": full.ModelNumber,
		}),
		"webChartConfig": web,
		"appChartConfig": app,
	}
	assets := []model.MarketNativeThingModelAsset(nil)
	if cover != "" {
		assets = []model.MarketNativeThingModelAsset{{Key: "native-profile-cover", Source: "market-cover"}}
		resourcesObject["profile"] = mustJSON(map[string]interface{}{
			"author": full.Author, "tags": tags, "cover": "asset://native-profile-cover", "version": full.Version,
			"brand": full.Brand, "modelNumber": full.ModelNumber,
		})
	}
	resourcesJSON, err := json.Marshal(resourcesObject)
	if err != nil {
		return nil, fmt.Errorf("resources: encode native ResourcesDocument: %w", err)
	}
	modelJSON, err = canonicalJSON(modelJSON)
	if err != nil {
		return nil, fmt.Errorf("model: canonicalize native ModelDraft: %w", err)
	}
	resourcesJSON, err = canonicalJSON(resourcesJSON)
	if err != nil {
		return nil, fmt.Errorf("resources: canonicalize native ResourcesDocument: %w", err)
	}
	// The source URL is intentionally omitted from resources; the asset manifest
	// records the cover dependency for the eventual caller to resolve.
	return &ConvertedLegacyNativePackage{Model: modelJSON, Resources: resourcesJSON, Assets: assets}, nil
}

func legacyObject(raw json.RawMessage, path string) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("%s: required object", path)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, fmt.Errorf("%s: must be an object", path)
	}
	return object, nil
}

func legacyRejectUnknown(object map[string]json.RawMessage, path string, allowed []string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for key := range object {
		if _, ok := set[key]; !ok {
			return fmt.Errorf("%s.%s: unsupported field", path, key)
		}
	}
	return nil
}

func legacyResolvedText(path, outer string, inner json.RawMessage, required bool) (string, error) {
	if len(bytes.TrimSpace(inner)) == 0 || bytes.Equal(bytes.TrimSpace(inner), []byte("null")) {
		if required && strings.TrimSpace(outer) == "" {
			return "", fmt.Errorf("%s: required", path)
		}
		return outer, nil
	}
	var value string
	if err := json.Unmarshal(inner, &value); err != nil {
		return "", fmt.Errorf("template_definition.%s: must be a string", path)
	}
	if strings.TrimSpace(outer) != "" && value != outer {
		return "", fmt.Errorf("template_definition.%s: conflicts with package %s", path, path)
	}
	if required && strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("template_definition.%s: required", path)
	}
	return value, nil
}

func legacyArray(raw json.RawMessage, path string) ([]json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("%s: required array", path)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil || rows == nil {
		return nil, fmt.Errorf("%s: must be an array", path)
	}
	return rows, nil
}

func legacyChart(definition map[string]json.RawMessage, key string) (json.RawMessage, error) {
	raw, ok := definition[key]
	if !ok {
		return nil, fmt.Errorf("template_definition.%s: required chart configuration", key)
	}
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage("null"), nil
	}
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return nil, fmt.Errorf("template_definition.%s: must be an object or null", key)
	}
	return append(json.RawMessage(nil), trimmed...), nil
}

func legacyCover(full *model.MarketTemplateFullData, definition map[string]json.RawMessage) (string, error) {
	candidates := []struct{ path, value string }{{"cover_url", full.CoverURL}}
	if full.DeviceConfig != nil {
		candidates = append(candidates, struct{ path, value string }{"device_config.image_url", full.DeviceConfig.ImageURL})
	}
	for _, key := range []string{"cover_url", "template_path"} {
		if raw, ok := definition[key]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return "", fmt.Errorf("template_definition.%s: must be a string", key)
			}
			candidates = append(candidates, struct{ path, value string }{"template_definition." + key, value})
		}
	}
	chosen, chosenPath := "", ""
	for _, candidate := range candidates {
		value := strings.TrimSpace(candidate.value)
		if value == "" {
			continue
		}
		if chosen == "" {
			chosen, chosenPath = value, candidate.path
			continue
		}
		if chosen != value {
			return "", fmt.Errorf("%s: cover conflicts with %s", candidate.path, chosenPath)
		}
	}
	return chosen, nil
}

func legacyTags(raw json.RawMessage) ([]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return []string{}, nil
	}
	var tags []string
	if err := json.Unmarshal(raw, &tags); err != nil || tags == nil {
		return nil, fmt.Errorf("template_definition.tags: must be an array of strings")
	}
	return tags, nil
}

func legacyField(raw json.RawMessage, kind, path string) (json.RawMessage, string, error) {
	row, err := legacyObject(raw, path)
	if err != nil {
		return nil, "", err
	}
	if err := legacyRejectUnknown(row, path, []string{
		"id", "fieldID", "sortOrder", "kind", "type", "device_template_id", "created_at", "updated_at", "tenant_id",
		"data_name", "data_identifier", "data_type", "unit", "read_write_flag", "description", "remark", "additional_info",
		"params", "name_i18n", "description_i18n", "value_type", "access",
	}); err != nil {
		return nil, "", err
	}
	for _, key := range []string{"kind", "type"} {
		if rawKind, ok := row[key]; ok {
			var supplied string
			if err := json.Unmarshal(rawKind, &supplied); err != nil || supplied != kind {
				return nil, "", fmt.Errorf("%s.%s: conflicts with legacy array kind %s", path, key, kind)
			}
		}
	}
	for _, key := range []string{"id", "device_template_id", "tenant_id"} {
		if rawIdentity, ok := row[key]; ok && !bytes.Equal(bytes.TrimSpace(rawIdentity), []byte("null")) {
			var ignored string
			if err := json.Unmarshal(rawIdentity, &ignored); err != nil {
				return nil, "", fmt.Errorf("%s.%s: legacy provenance must be a string", path, key)
			}
		}
	}
	for _, key := range []string{"created_at", "updated_at"} {
		if rawTime, ok := row[key]; ok && !bytes.Equal(bytes.TrimSpace(rawTime), []byte("null")) {
			var ignored string
			if err := json.Unmarshal(rawTime, &ignored); err != nil {
				return nil, "", fmt.Errorf("%s.%s: legacy timestamp must be a string", path, key)
			}
		}
	}
	identifier, err := legacyRequiredString(row["data_identifier"], path+".data_identifier")
	if err != nil {
		return nil, "", err
	}
	name, err := legacyName(row, path)
	if err != nil {
		return nil, "", err
	}
	field := map[string]json.RawMessage{
		"kind":       mustJSON(kind),
		"identifier": mustJSON(identifier),
		"nameI18n":   name,
	}
	if fieldID, ok := row["fieldID"]; ok && !bytes.Equal(bytes.TrimSpace(fieldID), []byte("null")) {
		var value string
		if err := json.Unmarshal(fieldID, &value); err != nil {
			return nil, "", fmt.Errorf("%s.fieldID: must be a string", path)
		}
		if value != "" {
			return nil, "", fmt.Errorf("%s.fieldID: native semantic replacement cannot preserve resource field IDs", path)
		}
	}
	if sortOrder, ok := row["sortOrder"]; ok {
		if err := legacyJSONInteger(sortOrder, path+".sortOrder"); err != nil {
			return nil, "", err
		}
		field["sortOrder"] = append(json.RawMessage(nil), sortOrder...)
	}
	if description, err := legacyDescription(row, path); err != nil {
		return nil, "", err
	} else if description != nil {
		field["descriptionI18n"] = description
	}
	if unit, ok := row["unit"]; ok && !bytes.Equal(bytes.TrimSpace(unit), []byte("null")) {
		var value string
		if err := json.Unmarshal(unit, &value); err != nil {
			return nil, "", fmt.Errorf("%s.unit: must be a string", path)
		}
		if value != "" {
			field["unit"] = append(json.RawMessage(nil), unit...)
		}
	}
	if err := legacyRejectSecretText(row["remark"], path+".remark"); err != nil {
		return nil, "", err
	}

	if kind == "TELEMETRY" || kind == "ATTRIBUTE" {
		access, err := legacyScalarAccess(row, path)
		if err != nil {
			return nil, "", err
		}
		field["access"] = access
		valueType, err := legacyFieldValueType(row, path)
		if err != nil {
			return nil, "", err
		}
		field["valueType"] = valueType
	} else {
		if dataType, ok := row["data_type"]; ok && !legacyEmpty(dataType) {
			return nil, "", fmt.Errorf("%s.data_type: scalar types are not supported for %s", path, kind)
		}
		if err := legacyRequireEmptyAdditionalInfo(row["additional_info"], path+".additional_info"); err != nil {
			return nil, "", err
		}
		access := map[string]bool{"read": false, "write": kind == "COMMAND", "observe": kind == "EVENT"}
		if rawAccess, ok := row["access"]; ok {
			var supplied map[string]bool
			if err := json.Unmarshal(rawAccess, &supplied); err != nil || !sameBoolMap(supplied, access) {
				return nil, "", fmt.Errorf("%s.access: conflicts with native %s semantics", path, kind)
			}
		}
		field["access"] = mustJSON(access)
		params, err := legacyParameters(row["params"], kind, path+".params")
		if err != nil {
			return nil, "", err
		}
		field["parameters"] = params
	}
	encoded, err := json.Marshal(field)
	if err != nil {
		return nil, "", fmt.Errorf("%s: encode native field: %w", path, err)
	}
	return encoded, identifier, nil
}

func legacyName(row map[string]json.RawMessage, path string) (json.RawMessage, error) {
	name := ""
	if raw, ok := row["data_name"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if err := json.Unmarshal(raw, &name); err != nil {
			return nil, fmt.Errorf("%s.data_name: must be a string", path)
		}
	}
	if len(bytes.TrimSpace(row["name_i18n"])) != 0 && !bytes.Equal(bytes.TrimSpace(row["name_i18n"]), []byte("null")) {
		i18n, err := legacyI18n(row["name_i18n"], path+".name_i18n")
		if err != nil {
			return nil, err
		}
		var object map[string]json.RawMessage
		_ = json.Unmarshal(i18n, &object)
		var prior string
		_ = json.Unmarshal(object["default"], &prior)
		if name != "" && prior != name {
			return nil, fmt.Errorf("%s.name_i18n.default: conflicts with data_name", path)
		}
		name = prior
		return i18n, nil
	}
	if name == "" {
		return nil, fmt.Errorf("%s.data_name: required non-empty name", path)
	}
	return mustJSON(map[string]string{"default": name}), nil
}

func legacyDescription(row map[string]json.RawMessage, path string) (json.RawMessage, error) {
	text := ""
	if raw, ok := row["description"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, fmt.Errorf("%s.description: must be a string", path)
		}
	}
	if raw, ok := row["description_i18n"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		i18n, err := legacyI18n(raw, path+".description_i18n")
		if err != nil {
			return nil, err
		}
		var object map[string]json.RawMessage
		_ = json.Unmarshal(i18n, &object)
		var prior string
		_ = json.Unmarshal(object["default"], &prior)
		if text != "" && text != prior {
			return nil, fmt.Errorf("%s.description_i18n.default: conflicts with description", path)
		}
		return i18n, nil
	}
	if text == "" {
		return nil, nil
	}
	return mustJSON(map[string]string{"default": text}), nil
}

func legacyI18n(raw json.RawMessage, path string) (json.RawMessage, error) {
	object, err := legacyObject(raw, path)
	if err != nil {
		return nil, err
	}
	if err := legacyRejectUnknown(object, path, []string{"default", "locales"}); err != nil {
		return nil, err
	}
	if _, err := legacyRequiredString(object["default"], path+".default"); err != nil {
		return nil, err
	}
	if rawLocales, ok := object["locales"]; ok && !bytes.Equal(bytes.TrimSpace(rawLocales), []byte("null")) {
		var locales map[string]string
		if err := json.Unmarshal(rawLocales, &locales); err != nil || locales == nil {
			return nil, fmt.Errorf("%s.locales: must be a string map", path)
		}
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid i18n value", path)
	}
	return canonical, nil
}

func legacyRequiredString(raw json.RawMessage, path string) (string, error) {
	var value string
	if len(bytes.TrimSpace(raw)) == 0 || json.Unmarshal(raw, &value) != nil || value == "" {
		return "", fmt.Errorf("%s: required non-empty string", path)
	}
	return value, nil
}

func legacyScalarAccess(row map[string]json.RawMessage, path string) (json.RawMessage, error) {
	raw, ok := row["read_write_flag"]
	if !ok {
		return nil, fmt.Errorf("%s.read_write_flag: required (R or RW)", path)
	}
	var mode string
	if err := json.Unmarshal(raw, &mode); err != nil || (mode != "R" && mode != "RW") {
		return nil, fmt.Errorf("%s.read_write_flag: unsupported access mode", path)
	}
	access := map[string]bool{"read": true, "write": mode == "RW", "observe": false}
	if rawAccess, ok := row["access"]; ok {
		var supplied map[string]bool
		if err := json.Unmarshal(rawAccess, &supplied); err != nil || !sameBoolMap(supplied, access) {
			return nil, fmt.Errorf("%s.access: conflicts with read_write_flag", path)
		}
	}
	return mustJSON(access), nil
}

func legacyFieldValueType(row map[string]json.RawMessage, path string) (json.RawMessage, error) {
	dataType := ""
	if raw, ok := row["data_type"]; ok {
		if err := json.Unmarshal(raw, &dataType); err != nil {
			return nil, fmt.Errorf("%s.data_type: must be a string", path)
		}
	}
	var prior json.RawMessage
	if raw, ok := row["value_type"]; ok && !legacyEmpty(raw) {
		var err error
		prior, err = legacyValueType(raw, path+".value_type")
		if err != nil {
			return nil, err
		}
	}
	if dataType == "Enum" {
		rows, err := legacyStringOrArray(row["additional_info"], path+".additional_info")
		if err != nil {
			return nil, err
		}
		converted, err := legacyEnumType(rows, path+".additional_info")
		if err != nil {
			return nil, err
		}
		if prior != nil && !legacyEnumTypeMatches(prior, converted) {
			return nil, fmt.Errorf("%s.value_type: conflicts with legacy enum rows", path)
		}
		return converted, nil
	}
	if err := legacyRequireEmptyAdditionalInfo(row["additional_info"], path+".additional_info"); err != nil {
		return nil, err
	}
	kind := map[string]string{"String": "STRING", "Number": "NUMBER", "Boolean": "BOOL"}[dataType]
	if kind == "" {
		return nil, fmt.Errorf("%s.data_type: unsupported or missing scalar type %q", path, dataType)
	}
	if prior != nil {
		var candidate map[string]json.RawMessage
		_ = json.Unmarshal(prior, &candidate)
		var storedKind string
		_ = json.Unmarshal(candidate["kind"], &storedKind)
		valid := storedKind == kind || (dataType == "Number" && (storedKind == "INT" || storedKind == "FLOAT"))
		if !valid {
			return nil, fmt.Errorf("%s.value_type.kind: conflicts with data_type %q", path, dataType)
		}
		return prior, nil
	}
	return mustJSON(map[string]string{"kind": kind}), nil
}

func legacyValueType(raw json.RawMessage, path string) (json.RawMessage, error) {
	object, err := legacyObject(raw, path)
	if err != nil {
		return nil, err
	}
	if err := legacyRejectUnknown(object, path, []string{"kind", "baseKind", "constraint", "enumValues"}); err != nil {
		return nil, err
	}
	var kind string
	if err := json.Unmarshal(object["kind"], &kind); err != nil {
		return nil, fmt.Errorf("%s.kind: required string", path)
	}
	switch kind {
	case "STRING", "BOOL", "INT", "FLOAT", "NUMBER":
		if len(object["baseKind"]) > 0 || len(object["enumValues"]) > 0 {
			return nil, fmt.Errorf("%s: scalar value type cannot contain enum properties", path)
		}
		if rawConstraint, ok := object["constraint"]; ok && !bytes.Equal(bytes.TrimSpace(rawConstraint), []byte("null")) {
			constraint, err := legacyObject(rawConstraint, path+".constraint")
			if err != nil {
				return nil, err
			}
			allowed := []string{"trueText", "falseText"}
			if kind == "STRING" {
				allowed = []string{"pattern", "maxLength"}
			} else if kind == "INT" || kind == "FLOAT" || kind == "NUMBER" {
				allowed = []string{"min", "max", "step", "precision"}
			}
			if err := legacyRejectUnknown(constraint, path+".constraint", allowed); err != nil {
				return nil, err
			}
			for key, value := range constraint {
				if kind == "STRING" || kind == "BOOL" {
					if key == "maxLength" {
						if err := legacyJSONInteger(value, path+".constraint."+key); err != nil {
							return nil, err
						}
					} else {
						var text string
						if err := json.Unmarshal(value, &text); err != nil {
							return nil, fmt.Errorf("%s.constraint.%s: must be a string", path, key)
						}
					}
				} else if key == "precision" {
					if err := legacyJSONInteger(value, path+".constraint."+key); err != nil {
						return nil, err
					}
				} else if err := legacyJSONNumber(value, path+".constraint."+key); err != nil {
					return nil, err
				}
			}
		}
	case "ENUM":
		var base string
		if err := json.Unmarshal(object["baseKind"], &base); err != nil || (base != "STRING" && base != "NUMBER" && base != "BOOLEAN") {
			return nil, fmt.Errorf("%s.baseKind: unsupported enum base kind", path)
		}
		if len(object["constraint"]) > 0 && !bytes.Equal(bytes.TrimSpace(object["constraint"]), []byte("null")) {
			return nil, fmt.Errorf("%s.constraint: not supported for ENUM", path)
		}
		var options []json.RawMessage
		if err := json.Unmarshal(object["enumValues"], &options); err != nil || len(options) == 0 {
			return nil, fmt.Errorf("%s.enumValues: non-empty array required", path)
		}
		for index, optionRaw := range options {
			optionPath := fmt.Sprintf("%s.enumValues[%d]", path, index)
			option, err := legacyObject(optionRaw, optionPath)
			if err != nil {
				return nil, err
			}
			if err := legacyRejectUnknown(option, optionPath, []string{"code", "label"}); err != nil {
				return nil, err
			}
			if err := legacyEnumCode(option["code"], base, optionPath+".code"); err != nil {
				return nil, err
			}
			if _, err := legacyI18n(option["label"], optionPath+".label"); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("%s.kind: unsupported native scalar kind %q", path, kind)
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid value type JSON", path)
	}
	return canonical, nil
}

func legacyEnumType(rows []json.RawMessage, path string) (json.RawMessage, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s: enum requires at least one row", path)
	}
	base := ""
	options := make([]map[string]json.RawMessage, 0, len(rows))
	for index, rowRaw := range rows {
		rowPath := fmt.Sprintf("%s[%d]", path, index)
		row, err := legacyObject(rowRaw, rowPath)
		if err != nil {
			return nil, err
		}
		if err := legacyRejectUnknown(row, rowPath, []string{"value_type", "value", "description", "label"}); err != nil {
			return nil, err
		}
		var label string
		if err := json.Unmarshal(row["value_type"], &label); err != nil {
			return nil, fmt.Errorf("%s.value_type: required enum scalar type", rowPath)
		}
		rowBase := map[string]string{"String": "STRING", "Number": "NUMBER", "Boolean": "BOOLEAN"}[label]
		if rowBase == "" {
			return nil, fmt.Errorf("%s.value_type: unsupported enum scalar type %q", rowPath, label)
		}
		if base == "" {
			base = rowBase
		} else if base != rowBase {
			return nil, fmt.Errorf("%s.value_type: enum rows must use one scalar type", rowPath)
		}
		if err := legacyEnumCode(row["value"], rowBase, rowPath+".value"); err != nil {
			return nil, err
		}
		description := ""
		if rawDescription, ok := row["description"]; ok && !bytes.Equal(bytes.TrimSpace(rawDescription), []byte("null")) {
			if err := json.Unmarshal(rawDescription, &description); err != nil {
				return nil, fmt.Errorf("%s.description: must be a string", rowPath)
			}
		}
		labelI18n := mustJSON(map[string]string{"default": description})
		if rawLabel, ok := row["label"]; ok && !bytes.Equal(bytes.TrimSpace(rawLabel), []byte("null")) {
			labelI18n, err = legacyI18n(rawLabel, rowPath+".label")
			if err != nil {
				return nil, err
			}
			var object map[string]json.RawMessage
			_ = json.Unmarshal(labelI18n, &object)
			var prior string
			_ = json.Unmarshal(object["default"], &prior)
			if description != "" && prior != description {
				return nil, fmt.Errorf("%s.label.default: conflicts with description", rowPath)
			}
		} else if description == "" {
			return nil, fmt.Errorf("%s.description: required because native enum labels are required", rowPath)
		}
		options = append(options, map[string]json.RawMessage{"code": append(json.RawMessage(nil), row["value"]...), "label": labelI18n})
	}
	return json.Marshal(map[string]interface{}{"kind": "ENUM", "baseKind": base, "enumValues": options})
}

func legacyEnumTypeMatches(left, right json.RawMessage) bool {
	leftCanonical, leftErr := canonicalJSON(left)
	rightCanonical, rightErr := canonicalJSON(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftCanonical, rightCanonical)
}

func legacyEnumCode(raw json.RawMessage, base, path string) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("%s: required enum scalar", path)
	}
	switch base {
	case "STRING":
		var value string
		if json.Unmarshal(trimmed, &value) != nil {
			return fmt.Errorf("%s: expected string enum code", path)
		}
	case "BOOLEAN":
		if !bytes.Equal(trimmed, []byte("true")) && !bytes.Equal(trimmed, []byte("false")) {
			return fmt.Errorf("%s: expected boolean enum code", path)
		}
	case "NUMBER":
		var number json.Number
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.UseNumber()
		if decoder.Decode(&number) != nil {
			return fmt.Errorf("%s: expected numeric enum code", path)
		}
		if err := legacyRejectUnsafeNumber(number, path); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s: unsupported enum base kind", path)
	}
	return nil
}

func legacyRejectUnsafeNumber(number json.Number, path string) error {
	text := number.String()
	if strings.ContainsAny(text, ".eE") {
		// Fractional/exponent enum codes remain exact in raw JSON, but their
		// compatibility with the historic JavaScript market path is unprovable.
		return fmt.Errorf("%s: fractional or exponent enum codes cannot be proven lossless", path)
	}
	integer, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return fmt.Errorf("%s: invalid numeric enum code", path)
	}
	limit := big.NewInt(9007199254740991)
	if new(big.Int).Abs(integer).Cmp(limit) > 0 {
		return fmt.Errorf("%s: numeric enum code exceeds the exact legacy JavaScript range", path)
	}
	return nil
}

func legacyJSONNumber(raw json.RawMessage, path string) error {
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&number) != nil {
		return fmt.Errorf("%s: must be a JSON number", path)
	}
	value, err := number.Float64()
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return fmt.Errorf("%s: numeric value is outside native constraint range", path)
	}
	text := number.String()
	if !strings.ContainsAny(text, ".eE") {
		integer, ok := new(big.Int).SetString(text, 10)
		if !ok || new(big.Int).Abs(integer).Cmp(big.NewInt(9007199254740991)) > 0 {
			return fmt.Errorf("%s: integer exceeds the exact legacy JavaScript range", path)
		}
	}
	return nil
}

func legacyJSONInteger(raw json.RawMessage, path string) error {
	if err := legacyJSONNumber(raw, path); err != nil {
		return err
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&number) != nil || strings.ContainsAny(number.String(), ".eE") {
		return fmt.Errorf("%s: must be an integer", path)
	}
	if _, err := number.Int64(); err != nil {
		return fmt.Errorf("%s: integer is outside supported range", path)
	}
	return nil
}

func legacyStringOrArray(raw json.RawMessage, path string) ([]json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("%s: required enum rows", path)
	}
	if bytes.TrimSpace(raw)[0] == '"' {
		var encoded string
		if err := json.Unmarshal(raw, &encoded); err != nil {
			return nil, fmt.Errorf("%s: invalid JSON string", path)
		}
		raw = json.RawMessage(encoded)
	}
	return legacyArray(raw, path)
}

func legacyParameters(raw json.RawMessage, kind, path string) (json.RawMessage, error) {
	rows := []json.RawMessage{}
	if len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		var err error
		rows, err = legacyStringOrArray(raw, path)
		if err != nil {
			return nil, err
		}
	}
	items := make([]map[string]json.RawMessage, 0, len(rows))
	seen := make(map[string]struct{})
	for index, itemRaw := range rows {
		itemPath := fmt.Sprintf("%s[%d]", path, index)
		item, err := legacyObject(itemRaw, itemPath)
		if err != nil {
			return nil, err
		}
		if err := legacyRejectUnknown(item, itemPath, []string{
			"id", "data_identifier", "data_name", "data_type", "param_type", "read_write_flag", "description", "description_i18n",
			"name", "value_type", "unit", "enum_config", "label",
		}); err != nil {
			return nil, err
		}
		if rawID, ok := item["id"]; ok && !bytes.Equal(bytes.TrimSpace(rawID), []byte("null")) {
			var ignored string
			if err := json.Unmarshal(rawID, &ignored); err != nil {
				return nil, fmt.Errorf("%s.id: legacy parameter identity must be a string", itemPath)
			}
		}
		identifier, err := legacyRequiredString(item["data_identifier"], itemPath+".data_identifier")
		if err != nil {
			return nil, err
		}
		if _, ok := seen[identifier]; ok {
			return nil, fmt.Errorf("%s.data_identifier: duplicate parameter identifier %q", itemPath, identifier)
		}
		seen[identifier] = struct{}{}
		name, err := legacyRequiredString(item["data_name"], itemPath+".data_name")
		if err != nil {
			return nil, err
		}
		nameI18n := mustJSON(map[string]string{"default": name})
		if rawName, ok := item["name"]; ok && !bytes.Equal(bytes.TrimSpace(rawName), []byte("null")) {
			nameI18n, err = legacyI18n(rawName, itemPath+".name")
			if err != nil {
				return nil, err
			}
			var object map[string]json.RawMessage
			_ = json.Unmarshal(nameI18n, &object)
			var prior string
			_ = json.Unmarshal(object["default"], &prior)
			if prior != name {
				return nil, fmt.Errorf("%s.name.default: conflicts with data_name", itemPath)
			}
		}
		descriptionI18n, err := legacyDescription(item, itemPath)
		if err != nil {
			return nil, err
		}
		if unit, ok := item["unit"]; ok && !legacyEmpty(unit) {
			return nil, fmt.Errorf("%s.unit: native Parameters have no unit field", itemPath)
		}
		valueType, err := legacyParameterValueType(item, kind, itemPath)
		if err != nil {
			return nil, err
		}
		if rawLabel, ok := item["label"]; ok && !legacyEmpty(rawLabel) {
			return nil, fmt.Errorf("%s.label: cannot be represented independently from data_name", itemPath)
		}
		if legacyType, ok := item[map[bool]string{true: "param_type", false: "read_write_flag"}[kind == "COMMAND"]]; ok {
			var selected string
			if err := json.Unmarshal(legacyType, &selected); err != nil {
				return nil, fmt.Errorf("%s: parameter type must be a string", itemPath)
			}
			otherKey := map[bool]string{true: "read_write_flag", false: "param_type"}[kind == "COMMAND"]
			if other, ok := item[otherKey]; ok && !legacyEmpty(other) {
				var alternate string
				if err := json.Unmarshal(other, &alternate); err != nil || alternate != selected {
					return nil, fmt.Errorf("%s.%s: conflicts with %s", itemPath, otherKey, map[bool]string{true: "param_type", false: "read_write_flag"}[kind == "COMMAND"])
				}
			}
			if selected != "Enum" {
				if rawDataType, ok := item["data_type"]; ok && !legacyEmpty(rawDataType) {
					var alternate string
					if json.Unmarshal(rawDataType, &alternate) != nil || alternate != selected {
						return nil, fmt.Errorf("%s.data_type: conflicts with parameter type %q", itemPath, selected)
					}
				}
				if rawEnum, ok := item["enum_config"]; ok && !legacyEmpty(rawEnum) {
					return nil, fmt.Errorf("%s.enum_config: only supported for Enum parameters", itemPath)
				}
			}
		}
		parameter := map[string]json.RawMessage{"identifier": mustJSON(identifier), "nameI18n": nameI18n, "valueType": valueType}
		if descriptionI18n != nil {
			parameter["descriptionI18n"] = descriptionI18n
		}
		items = append(items, parameter)
	}
	return json.Marshal(map[string]interface{}{"schemaVersion": 1, "items": items})
}

func legacyParameterValueType(item map[string]json.RawMessage, kind, path string) (json.RawMessage, error) {
	if prior, ok := item["value_type"]; ok && !legacyEmpty(prior) {
		validated, err := legacyValueType(prior, path+".value_type")
		if err != nil {
			return nil, err
		}
		var object map[string]json.RawMessage
		_ = json.Unmarshal(validated, &object)
		var priorKind string
		_ = json.Unmarshal(object["kind"], &priorKind)
		legacyType := ""
		if kind == "COMMAND" {
			_ = json.Unmarshal(item["param_type"], &legacyType)
		} else {
			_ = json.Unmarshal(item["read_write_flag"], &legacyType)
		}
		dataType := ""
		_ = json.Unmarshal(item["data_type"], &dataType)
		if priorKind == "ENUM" {
			if legacyType != "Enum" || dataType == "" {
				return nil, fmt.Errorf("%s.value_type: enum conflicts with legacy parameter type", path)
			}
			var base string
			_ = json.Unmarshal(object["baseKind"], &base)
			if map[string]string{"String": "STRING", "Number": "NUMBER", "Boolean": "BOOLEAN"}[dataType] != base {
				return nil, fmt.Errorf("%s.data_type: conflicts with value_type.baseKind", path)
			}
			converted, err := legacyParameterEnumType(item, path)
			if err != nil {
				return nil, err
			}
			if !legacyEnumTypeMatches(validated, converted) {
				return nil, fmt.Errorf("%s.value_type: conflicts with legacy enum_config", path)
			}
			return converted, nil
		}
		want := map[string]string{"String": "STRING", "Number": "NUMBER", "Boolean": "BOOL"}[legacyType]
		if want == "" || (want != priorKind && !(legacyType == "Number" && (priorKind == "INT" || priorKind == "FLOAT"))) {
			return nil, fmt.Errorf("%s.value_type.kind: conflicts with parameter type %q", path, legacyType)
		}
		return validated, nil
	}
	legacyType := ""
	if kind == "COMMAND" {
		_ = json.Unmarshal(item["param_type"], &legacyType)
	} else {
		_ = json.Unmarshal(item["read_write_flag"], &legacyType)
	}
	if legacyType == "Enum" {
		return legacyParameterEnumType(item, path)
	}
	if legacyType == "" {
		return nil, fmt.Errorf("%s.%s: required parameter scalar type", path, map[bool]string{true: "param_type", false: "read_write_flag"}[kind == "COMMAND"])
	}
	legacy := map[string]string{"String": "String", "Number": "Number", "Boolean": "Boolean"}[legacyType]
	if legacy == "" {
		return nil, fmt.Errorf("%s: unsupported parameter type %q", path, legacyType)
	}
	row := map[string]json.RawMessage{"data_type": mustJSON(legacy)}
	return legacyFieldValueType(row, path)
}

func legacyParameterEnumType(item map[string]json.RawMessage, path string) (json.RawMessage, error) {
	configs, err := legacyStringOrArray(item["enum_config"], path+".enum_config")
	if err != nil {
		return nil, err
	}
	dataType := ""
	if err := json.Unmarshal(item["data_type"], &dataType); err != nil {
		return nil, fmt.Errorf("%s.data_type: required for enum parameter", path)
	}
	base := map[string]string{"String": "STRING", "Number": "NUMBER", "Boolean": "BOOLEAN"}[dataType]
	if base == "" {
		return nil, fmt.Errorf("%s.data_type: unsupported enum scalar type %q", path, dataType)
	}
	rows := make([]json.RawMessage, 0, len(configs))
	for index, configRaw := range configs {
		config, err := legacyObject(configRaw, fmt.Sprintf("%s.enum_config[%d]", path, index))
		if err != nil {
			return nil, err
		}
		configPath := fmt.Sprintf("%s.enum_config[%d]", path, index)
		if err := legacyRejectUnknown(config, configPath, []string{"value", "desc", "label"}); err != nil {
			return nil, err
		}
		row := map[string]json.RawMessage{
			"value_type": mustJSON(dataType),
			"value":      config["value"],
		}
		if description, ok := config["desc"]; ok {
			row["description"] = description
		}
		if label, ok := config["label"]; ok {
			row["label"] = label
		}
		rows = append(rows, mustJSON(row))
	}
	return legacyEnumType(rows, path+".enum_config")
}

func legacyRequireEmptyAdditionalInfo(raw json.RawMessage, path string) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || legacyEmpty(raw) {
		return nil
	}
	rows, err := legacyStringOrArray(raw, path)
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		return fmt.Errorf("%s: non-empty scalar additional_info is unsupported for events and commands", path)
	}
	return nil
}

func legacyRejectSecretText(raw json.RawMessage, path string) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return fmt.Errorf("%s: must be a string", path)
	}
	if strings.TrimSpace(value) != "" {
		return fmt.Errorf("%s: non-empty legacy remark cannot be represented", path)
	}
	return nil
}

func legacyEmpty(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("\"\"")) || bytes.Equal(trimmed, []byte("[]")) {
		return true
	}
	var value string
	if json.Unmarshal(trimmed, &value) == nil {
		return strings.TrimSpace(value) == "" || value == "[]"
	}
	return false
}

func sameBoolMap(left, right map[string]bool) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range right {
		if left[key] != value {
			return false
		}
	}
	return true
}

func mustJSON(value interface{}) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
