package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"project/internal/model"
)

func TestConvertLegacyMarketTemplateToNativePreservesV2Shape(t *testing.T) {
	full := legacyNativeFixture()
	converted, err := convertLegacyMarketTemplateToNative(full)
	if err != nil {
		t.Fatalf("convert legacy package: %v", err)
	}

	var nativeModel struct {
		Name            string            `json:"name"`
		DescriptionI18n map[string]string `json:"descriptionI18n"`
		Category        string            `json:"category"`
		Fields          []json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(converted.Model, &nativeModel); err != nil {
		t.Fatal(err)
	}
	if nativeModel.Name != "Legacy fixture" || nativeModel.Category != "sensors" || nativeModel.DescriptionI18n["default"] != "safe fixture" || len(nativeModel.Fields) != 4 {
		t.Fatalf("native ModelDraft metadata/field count mismatch: %#v", nativeModel)
	}
	for index, expectedKind := range []string{"TELEMETRY", "ATTRIBUTE", "EVENT", "COMMAND"} {
		var field map[string]json.RawMessage
		if err := json.Unmarshal(nativeModel.Fields[index], &field); err != nil {
			t.Fatal(err)
		}
		var kind, identifier string
		_ = json.Unmarshal(field["kind"], &kind)
		_ = json.Unmarshal(field["identifier"], &identifier)
		if kind != expectedKind || identifier == "" {
			t.Fatalf("field[%d] mismatch: kind=%q identifier=%q", index, kind, identifier)
		}
		if bytes.Contains(nativeModel.Fields[index], []byte("tenant_id")) || bytes.Contains(nativeModel.Fields[index], []byte("device_template_id")) {
			t.Fatalf("legacy tenant/template identity leaked into native field %d", index)
		}
	}
	if !bytes.Contains(nativeModel.Fields[0], []byte(`"kind":"INT"`)) || !bytes.Contains(nativeModel.Fields[0], []byte(`"precision":2`)) {
		t.Fatalf("native numeric type constraints were not retained: %s", nativeModel.Fields[0])
	}
	if !bytes.Contains(nativeModel.Fields[1], []byte(`"code":2`)) || !bytes.Contains(nativeModel.Fields[1], []byte(`"baseKind":"NUMBER"`)) {
		t.Fatalf("typed enum code/base kind were not retained: %s", nativeModel.Fields[1])
	}
	if !bytes.Contains(nativeModel.Fields[2], []byte(`"schemaVersion":1`)) || !bytes.Contains(nativeModel.Fields[2], []byte(`"identifier":"payload"`)) {
		t.Fatalf("event parameters were not mapped to native Parameters: %s", nativeModel.Fields[2])
	}
	var eventField map[string]json.RawMessage
	_ = json.Unmarshal(nativeModel.Fields[2], &eventField)
	var eventParameters struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(eventField["parameters"], &eventParameters)
	if len(eventParameters.Items) != 1 {
		t.Fatalf("event parameter missing from native document: %s", nativeModel.Fields[2])
	}
	if _, present := eventParameters.Items[0]["descriptionI18n"]; present {
		t.Fatalf("omitted legacy parameter description was invented: %s", eventParameters.Items[0]["descriptionI18n"])
	}
	if !bytes.Contains(nativeModel.Fields[3], []byte(`"identifier":"enabled"`)) {
		t.Fatalf("command parameter was not retained: %s", nativeModel.Fields[3])
	}
	if !bytes.Contains(converted.Resources, []byte(`"modelNumber":"fixture-1"`)) || !bytes.Contains(converted.Resources, []byte(`"tags":[]`)) {
		t.Fatalf("native profile metadata/tags were not retained: %s", converted.Resources)
	}
	if !bytes.Contains(converted.Resources, []byte(`9007199254740001`)) || !bytes.Contains(converted.Resources, []byte(`1.2345678901234567`)) {
		t.Fatalf("chart numeric values changed during conversion: %s", converted.Resources)
	}
}

func TestConvertLegacyMarketTemplateToNativeAcceptsOptionalEnumParameterDescription(t *testing.T) {
	full := legacyNativeFixture()
	var definition map[string]json.RawMessage
	if err := json.Unmarshal(full.TemplateDefinition, &definition); err != nil {
		t.Fatal(err)
	}
	var commands []map[string]json.RawMessage
	if err := json.Unmarshal(definition["commands"], &commands); err != nil || len(commands) != 1 {
		t.Fatalf("decode fixture command: err=%v count=%d", err, len(commands))
	}
	paramsJSON, err := json.Marshal([]map[string]interface{}{{
		"data_identifier": "state", "data_name": "State", "param_type": "Enum", "data_type": "String",
		"enum_config": []map[string]interface{}{{"value": "on", "label": map[string]string{"default": "On"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	commands[0]["params"] = mustJSON(string(paramsJSON))
	definition["commands"] = mustJSON(commands)
	full.TemplateDefinition = mustJSON(definition)

	converted, err := convertLegacyMarketTemplateToNative(full)
	if err != nil {
		t.Fatalf("convert optional enum parameter metadata: %v", err)
	}
	if !bytes.Contains(converted.Model, []byte(`"kind":"ENUM"`)) || !bytes.Contains(converted.Model, []byte(`"default":"On"`)) {
		t.Fatalf("enum parameter type or optional label was not retained: %s", converted.Model)
	}
}

func TestConvertLegacyMarketTemplateToNativeRejectsAmbiguousOrLossyInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *model.MarketTemplateFullData)
		want   string
	}{
		{
			name: "unsupported template key",
			mutate: func(t *testing.T, full *model.MarketTemplateFullData) {
				t.Helper()
				var definition map[string]json.RawMessage
				if err := json.Unmarshal(full.TemplateDefinition, &definition); err != nil {
					t.Fatalf("decode fixture definition: %v", err)
				}
				if _, exists := definition["mystery"]; exists {
					t.Fatal("fixture already contains mystery key")
				}
				definition["mystery"] = json.RawMessage("true")
				encoded, err := json.Marshal(definition)
				if err != nil {
					t.Fatalf("encode mutated definition: %v", err)
				}
				full.TemplateDefinition = encoded
			},
			want: "template_definition.mystery",
		},
		{
			name: "unsafe numeric enum",
			mutate: func(t *testing.T, full *model.MarketTemplateFullData) {
				replaceLegacyDefinitionText(t, full, `\"value\":2`, `\"value\":9007199254740992`)
			},
			want: "exact legacy JavaScript range",
		},
		{
			name: "conflicting cover sources",
			mutate: func(t *testing.T, full *model.MarketTemplateFullData) {
				t.Helper()
				if full.CoverURL == "https://assets.example/outer.png" || full.DeviceConfig.ImageURL == "https://assets.example/device.png" {
					t.Fatal("fixture already has the conflicting cover values")
				}
				full.CoverURL = "https://assets.example/outer.png"
				full.DeviceConfig.ImageURL = "https://assets.example/device.png"
			},
			want: "cover conflicts",
		},
		{
			name: "metadata mismatch",
			mutate: func(t *testing.T, full *model.MarketTemplateFullData) {
				t.Helper()
				if full.Description == "different" {
					t.Fatal("fixture already has the conflicting description")
				}
				full.Description = "different"
			},
			want: "conflicts with package description",
		},
		{
			name: "unknown field property",
			mutate: func(t *testing.T, full *model.MarketTemplateFullData) {
				replaceLegacyDefinitionText(t, full, `"data_identifier":"temperature"`, `"data_identifier":"temperature","vendor_extension":true`)
			},
			want: "vendor_extension",
		},
		{
			name: "native BOOLEAN enum base is invalid",
			mutate: func(t *testing.T, full *model.MarketTemplateFullData) {
				replaceLegacyDefinitionText(t, full, `"data_type":"Enum","unit":""`, `"data_type":"Enum","value_type":{"kind":"ENUM","baseKind":"BOOLEAN","enumValues":[{"code":true,"label":{"default":"On"}}]},"unit":""`)
			},
			want: "baseKind: unsupported enum base kind",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			full := legacyNativeFixture()
			before, err := json.Marshal(full)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(t, full)
			after, err := json.Marshal(full)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(before, after) {
				t.Fatal("test mutator did not change its fixture")
			}
			_, err = convertLegacyMarketTemplateToNative(full)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestConvertLegacyMarketTemplateToNativeUsesCoreBoolEnumBaseEverywhere(t *testing.T) {
	full := legacyNativeFixture()
	var definition map[string]json.RawMessage
	if err := json.Unmarshal(full.TemplateDefinition, &definition); err != nil {
		t.Fatal(err)
	}
	appendRow := func(group string, row map[string]interface{}) {
		t.Helper()
		var rows []json.RawMessage
		if err := json.Unmarshal(definition[group], &rows); err != nil {
			t.Fatalf("decode %s rows: %v", group, err)
		}
		rows = append(rows, mustJSON(row))
		definition[group] = mustJSON(rows)
	}
	appendRow("telemetry", map[string]interface{}{
		"data_name": "Connected", "data_identifier": "connected", "read_write_flag": "R", "data_type": "Boolean", "additional_info": "[]",
	})
	appendRow("attributes", map[string]interface{}{
		"data_name": "Switch", "data_identifier": "switch", "read_write_flag": "RW", "data_type": "Enum",
		"additional_info": `[{"value_type":"Boolean","value":true,"description":"On"},{"value_type":"Boolean","value":false,"description":"Off"}]`,
	})
	setLegacyParameterFixture(t, definition, "events", map[string]interface{}{
		"data_identifier": "enabled", "data_name": "Enabled", "read_write_flag": "Enum", "data_type": "Boolean",
		"enum_config": []map[string]interface{}{{"value": true, "desc": "On"}, {"value": false, "desc": "Off"}},
	})
	setLegacyParameterFixture(t, definition, "commands", map[string]interface{}{
		"data_identifier": "enabled", "data_name": "Enabled", "param_type": "Enum", "data_type": "Boolean",
		"enum_config": []map[string]interface{}{{"value": true, "desc": "On"}, {"value": false, "desc": "Off"}},
	})
	full.TemplateDefinition = mustJSON(definition)

	converted, err := convertLegacyMarketTemplateToNative(full)
	if err != nil {
		t.Fatalf("convert bool scalar and enum fixtures: %v", err)
	}
	var native struct {
		Fields []struct {
			Identifier string          `json:"identifier"`
			ValueType  json.RawMessage `json:"valueType"`
			Parameters json.RawMessage `json:"parameters"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(converted.Model, &native); err != nil {
		t.Fatal(err)
	}
	fieldByID := make(map[string]struct {
		ValueType  json.RawMessage
		Parameters json.RawMessage
	}, len(native.Fields))
	for _, field := range native.Fields {
		fieldByID[field.Identifier] = struct {
			ValueType  json.RawMessage
			Parameters json.RawMessage
		}{field.ValueType, field.Parameters}
	}
	var scalar struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(fieldByID["connected"].ValueType, &scalar); err != nil || scalar.Kind != "BOOL" {
		t.Fatalf("Boolean scalar did not map to native BOOL: %s err=%v", fieldByID["connected"].ValueType, err)
	}
	assertBoolEnum := func(path string, raw json.RawMessage) {
		t.Helper()
		var valueType struct {
			Kind     string            `json:"kind"`
			BaseKind string            `json:"baseKind"`
			Options  []json.RawMessage `json:"enumValues"`
		}
		if err := json.Unmarshal(raw, &valueType); err != nil {
			t.Fatalf("%s: decode value type: %v", path, err)
		}
		if valueType.Kind != "ENUM" || valueType.BaseKind != "BOOL" || len(valueType.Options) != 2 {
			t.Fatalf("%s: expected ENUM/BOOL with two options, got %s", path, raw)
		}
		var codes []bool
		for _, optionRaw := range valueType.Options {
			var option struct {
				Code json.RawMessage `json:"code"`
			}
			if err := json.Unmarshal(optionRaw, &option); err != nil {
				t.Fatalf("%s: decode enum option: %v", path, err)
			}
			var code bool
			if err := json.Unmarshal(option.Code, &code); err != nil {
				t.Fatalf("%s: expected JSON boolean enum code, got %s", path, option.Code)
			}
			codes = append(codes, code)
		}
		if !(codes[0] && !codes[1]) {
			t.Fatalf("%s: boolean enum codes changed: %#v", path, codes)
		}
	}
	assertBoolEnum("switch", fieldByID["switch"].ValueType)
	for _, fieldID := range []string{"report", "set_enabled"} {
		var params struct {
			Items []struct {
				Identifier string          `json:"identifier"`
				ValueType  json.RawMessage `json:"valueType"`
			} `json:"items"`
		}
		if err := json.Unmarshal(fieldByID[fieldID].Parameters, &params); err != nil || len(params.Items) != 1 {
			t.Fatalf("%s: decode enum parameters: err=%v raw=%s", fieldID, err, fieldByID[fieldID].Parameters)
		}
		assertBoolEnum(fieldID+"."+params.Items[0].Identifier, params.Items[0].ValueType)
	}
}

func setLegacyParameterFixture(t *testing.T, definition map[string]json.RawMessage, group string, parameter map[string]interface{}) {
	t.Helper()
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(definition[group], &rows); err != nil || len(rows) != 1 {
		t.Fatalf("decode %s fixture rows: err=%v count=%d", group, err, len(rows))
	}
	paramsJSON, err := json.Marshal([]map[string]interface{}{parameter})
	if err != nil {
		t.Fatal(err)
	}
	rows[0]["params"] = mustJSON(string(paramsJSON))
	definition[group] = mustJSON(rows)
}

func TestConvertLegacyMarketTemplateToNativeFreezesCoverReference(t *testing.T) {
	full := legacyNativeFixture()
	full.CoverURL = "https://market.example/assets/cover.png"
	full.DeviceConfig.ImageURL = full.CoverURL
	replaceLegacyDefinitionText(t, full, `"cover_url":""`, `"cover_url":"https://market.example/assets/cover.png"`)
	converted, err := convertLegacyMarketTemplateToNative(full)
	if err != nil {
		t.Fatalf("convert cover-bearing package: %v", err)
	}
	if len(converted.Assets) != 1 || converted.Assets[0] != (model.MarketNativeThingModelAsset{Key: "native-profile-cover", Source: "market-cover"}) {
		t.Fatalf("unexpected cover asset manifest: %#v", converted.Assets)
	}
	if !bytes.Contains(converted.Resources, []byte(`"cover":"asset://native-profile-cover"`)) {
		t.Fatalf("profile cover was not frozen to the asset manifest: %s", converted.Resources)
	}
}

func replaceLegacyDefinitionText(t *testing.T, full *model.MarketTemplateFullData, before, after string) {
	t.Helper()
	source := string(full.TemplateDefinition)
	if !strings.Contains(source, before) {
		t.Fatalf("fixture text to replace was not present: %q", before)
	}
	updated := strings.Replace(source, before, after, 1)
	if updated == source {
		t.Fatalf("fixture replacement did not change definition: %q", before)
	}
	full.TemplateDefinition = json.RawMessage(updated)
}

func TestLegacyNativePackageOpaqueStringSurvivesMarketMapRoundTrip(t *testing.T) {
	converted, err := convertLegacyMarketTemplateToNative(legacyNativeFixture())
	if err != nil {
		t.Fatalf("convert package: %v", err)
	}
	packageJSON, err := json.Marshal(converted)
	if err != nil {
		t.Fatal(err)
	}
	definition := map[string]interface{}{
		"legacy_native_package":          string(packageJSON),
		"legacy_native_package_encoding": "raw-json-string-v1",
	}
	marketWire, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	var horizonStyleMap map[string]interface{}
	if err := json.Unmarshal(marketWire, &horizonStyleMap); err != nil {
		t.Fatal(err)
	}
	forwarded, err := json.Marshal(horizonStyleMap)
	if err != nil {
		t.Fatal(err)
	}
	var rawDefinition map[string]json.RawMessage
	if err := json.Unmarshal(forwarded, &rawDefinition); err != nil {
		t.Fatal(err)
	}
	var encodedPackage string
	if err := json.Unmarshal(rawDefinition["legacy_native_package"], &encodedPackage); err != nil {
		t.Fatalf("read opaque package after map round trip: %v", err)
	}
	var decoded ConvertedLegacyNativePackage
	if err := json.Unmarshal([]byte(encodedPackage), &decoded); err != nil {
		t.Fatalf("decode opaque package after map round trip: %v", err)
	}
	if !bytes.Contains(decoded.Resources, []byte(`9007199254740001`)) || !bytes.Contains(decoded.Resources, []byte(`1.2345678901234567`)) {
		t.Fatalf("opaque payload numbers changed in Horizon-style map round trip: %s", decoded.Resources)
	}
}

func legacyNativeFixture() *model.MarketTemplateFullData {
	definition := `{
  "name":"Legacy fixture",
  "category":"sensors",
  "description":"safe fixture",
  "web_chart_config":{"charts":[{"series":[{"identifier":"temperature","deviceId":"__template__","weight":9007199254740001}]}]},
	  "app_chart_config":{"precision":1.2345678901234567},
	  "cover_url":"",
	  "template_path":"",
	  "telemetry":[{"id":"legacy-row-1","device_template_id":"old-template","tenant_id":"old-tenant","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","data_name":"Temperature","data_identifier":"temperature","read_write_flag":"R","data_type":"Number","unit":"C","description":"Ambient","additional_info":"[]","value_type":{"kind":"INT","constraint":{"min":-40,"max":125,"precision":2}}}],
	  "attributes":[{"id":"legacy-row-2","device_template_id":"old-template","tenant_id":"old-tenant","data_name":"Mode","data_identifier":"mode","read_write_flag":"RW","data_type":"Enum","unit":"","description":"Selected mode","additional_info":"[{\"value_type\":\"Number\",\"value\":2,\"description\":\"Auto\"},{\"value_type\":\"Number\",\"value\":3,\"description\":\"Manual\"}]"}],
	  "events":[{"id":"legacy-row-3","device_template_id":"old-template","tenant_id":"old-tenant","data_name":"Report","data_identifier":"report","params":"[{\"data_identifier\":\"payload\",\"data_name\":\"Payload\",\"read_write_flag\":\"String\",\"data_type\":\"String\"}]","description":"Reported payload","additional_info":"[]"}],
	  "commands":[{"id":"legacy-row-4","device_template_id":"old-template","tenant_id":"old-tenant","data_name":"Set enabled","data_identifier":"set_enabled","params":"[{\"data_identifier\":\"enabled\",\"data_name\":\"Enabled\",\"param_type\":\"Boolean\",\"data_type\":\"Boolean\"}]","description":"Set state","additional_info":"[]"}
	  ]
	}`
	return &model.MarketTemplateFullData{
		Name: "Legacy fixture", Brand: "Example", ModelNumber: "fixture-1", Category: "sensors", Author: "Fixture author",
		Version: "1.2.3", Description: "safe fixture", TemplateDefinition: json.RawMessage(definition),
		DeviceConfig: &model.DeviceConfigPayload{ImageURL: ""},
	}
}
