package core

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func testModel(fields ...Field) Model {
	return Model{
		ID:       ModelID("model-1"),
		TenantID: TenantID("tenant-1"),
		Name:     "Demo model",
		Fields:   fields,
	}
}

func scalarField(kind FieldKind, identifier string, valueType ValueType) Field {
	return Field{
		Kind: kind, Identifier: identifier,
		Name:      I18nString{Default: identifier},
		Access:    Access{Read: true, Observe: true},
		ValueType: &valueType,
	}
}

func commandField(identifier string, params *Parameters) Field {
	return Field{
		Kind: FieldCommand, Identifier: identifier,
		Name:   I18nString{Default: identifier},
		Access: Access{Write: true}, Parameters: params,
	}
}

func hasIssue(issues []ValidationIssue, code string) bool {
	for _, found := range issues {
		if found.Code == code {
			return true
		}
	}
	return false
}

func TestValidateModelKeepsIdentifierNamespacePerKind(t *testing.T) {
	model := testModel(
		scalarField(FieldTelemetry, "shared", ValueType{Kind: ScalarNumber}),
		scalarField(FieldAttribute, "shared", ValueType{Kind: ScalarString}),
		scalarField(FieldTelemetry, "same-kind", ValueType{Kind: ScalarInt}),
		scalarField(FieldTelemetry, "same-kind", ValueType{Kind: ScalarInt}),
	)
	issues := ValidateModel(model)
	if !hasIssue(issues, "FIELD_IDENTIFIER_DUPLICATE") {
		t.Fatalf("same-kind duplicate should be rejected; issues=%+v", issues)
	}
	for _, found := range issues {
		if found.Code == "FIELD_IDENTIFIER_DUPLICATE" && found.Path != "fields[3].identifier" {
			t.Fatalf("cross-kind same-name pair should be accepted; unexpected duplicate at %q", found.Path)
		}
	}
}

func TestValidateFieldPreservesIdentifierVerbatim(t *testing.T) {
	const identifier = "  Vendor.Temp-Celsius/°C  "
	field := scalarField(FieldTelemetry, identifier, ValueType{Kind: ScalarNumber})
	issues := ValidateField(field)
	if hasIssue(issues, "FIELD_IDENTIFIER_REQUIRED") || hasIssue(issues, "FIELD_IDENTIFIER_INVALID") {
		t.Fatalf("non-empty runtime identifier should be preserved as supplied; issues=%+v", issues)
	}
	if field.Identifier != identifier {
		t.Fatalf("validator changed identifier: got %q, want %q", field.Identifier, identifier)
	}
}

func TestValidateModelAcceptsTypedCommandParameters(t *testing.T) {
	params := &Parameters{
		SchemaVersion: ParametersVersion,
		Items: []Parameter{{
			Identifier: "target",
			Name:       I18nString{Default: "Target"},
			ValueType:  ValueType{Kind: ScalarNumber},
		}, {
			Identifier: "mode",
			Name:       I18nString{Default: "Mode"},
			ValueType: ValueType{Kind: ScalarEnum, EnumBaseKind: ScalarString, EnumValues: []EnumValue{
				{Value: json.RawMessage(`"heat"`), Label: I18nString{Default: "Heat"}},
				{Value: json.RawMessage(`"cool"`), Label: I18nString{Default: "Cool"}},
			}},
		}},
	}
	model := testModel(commandField("set.target", params))
	if issues := ValidateModel(model); len(issues) != 0 {
		t.Fatalf("typed parameters should validate independently of field scalar value_type; issues=%+v", issues)
	}
	if got := model.Fields[0].Parameters.Items[0].ValueType.Kind; got != ScalarNumber {
		t.Fatalf("NUMBER parameter kind changed to %q", got)
	}
}

func TestValidateScalarNumberRetainsGeneralNumericMeaning(t *testing.T) {
	valueType := ValueType{Kind: ScalarNumber}
	for _, raw := range []json.RawMessage{json.RawMessage(`12`), json.RawMessage(`12.75`), json.RawMessage(`1.275e1`)} {
		if err := ValidateScalar(valueType, raw); err != nil {
			t.Errorf("NUMBER should accept numeric scalar %s: %v", raw, err)
		}
	}
	for _, raw := range []json.RawMessage{json.RawMessage(`"12"`), json.RawMessage(`true`), json.RawMessage(`{}`)} {
		if err := ValidateScalar(valueType, raw); err == nil {
			t.Errorf("NUMBER should reject non-numeric scalar %s", raw)
		}
	}
}

func TestValidateScalarRejectsQuotedAndNullValuesForTypedScalars(t *testing.T) {
	cases := []struct {
		name      string
		valueType ValueType
		raw       json.RawMessage
	}{
		{name: "NUMBER quoted number", valueType: ValueType{Kind: ScalarNumber}, raw: json.RawMessage(`"12"`)},
		{name: "NUMBER null", valueType: ValueType{Kind: ScalarNumber}, raw: json.RawMessage(`null`)},
		{name: "BOOL null", valueType: ValueType{Kind: ScalarBool}, raw: json.RawMessage(`null`)},
		{name: "STRING null", valueType: ValueType{Kind: ScalarString}, raw: json.RawMessage(`null`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateScalar(tc.valueType, tc.raw); err == nil {
				t.Fatalf("expected typed scalar %s to reject %s", tc.valueType.Kind, tc.raw)
			}
		})
	}
}

func TestValidateScalarEnumUsesTypedJSONScalars(t *testing.T) {
	stringEnum := ValueType{Kind: ScalarEnum, EnumBaseKind: ScalarString, EnumValues: []EnumValue{
		{Value: json.RawMessage(`"1"`), Label: I18nString{Default: "One"}},
		{Value: json.RawMessage(`"on"`), Label: I18nString{Default: "On"}},
	}}
	if err := ValidateScalar(stringEnum, json.RawMessage(`"1"`)); err != nil {
		t.Fatalf("string enum value should match string scalar: %v", err)
	}
	if err := ValidateScalar(stringEnum, json.RawMessage(`1`)); err == nil {
		t.Fatal("numeric 1 must not be coerced to string enum value \"1\"")
	}
	numberEnum := ValueType{Kind: ScalarEnum, EnumBaseKind: ScalarNumber, EnumValues: []EnumValue{
		{Value: json.RawMessage(`1`), Label: I18nString{Default: "One"}},
		{Value: json.RawMessage(`2.5`), Label: I18nString{Default: "Two and a half"}},
	}}
	if err := ValidateScalar(numberEnum, json.RawMessage(`1`)); err != nil {
		t.Fatalf("numeric enum integer value should match numeric scalar: %v", err)
	}
	if err := ValidateScalar(numberEnum, json.RawMessage(`"1"`)); err == nil {
		t.Fatal("string \"1\" must not be coerced to numeric enum value 1")
	}
}

func TestStringEnumCanonicalizesEscapedJSONValues(t *testing.T) {
	duplicate := ValueType{Kind: ScalarEnum, EnumBaseKind: ScalarString, EnumValues: []EnumValue{
		{Value: json.RawMessage(`"a"`), Label: I18nString{Default: "A"}},
		{Value: json.RawMessage(`"\u0061"`), Label: I18nString{Default: "Letter A"}},
	}}
	issues := ValidateField(scalarField(FieldTelemetry, "mode", duplicate))
	if !hasIssue(issues, "ENUM_VALUE_DUPLICATE") {
		t.Fatalf("JSON string tokens with the same decoded value should conflict; issues=%+v", issues)
	}

	single := ValueType{Kind: ScalarEnum, EnumBaseKind: ScalarString, EnumValues: []EnumValue{
		{Value: json.RawMessage(`"a"`), Label: I18nString{Default: "A"}},
	}}
	if err := ValidateScalar(single, json.RawMessage(`"\u0061"`)); err != nil {
		t.Fatalf("escaped JSON string should match the same decoded enum code: %v", err)
	}
}

func TestBuildSnapshotNeedsNoTemplateAndCreatesNextRevision(t *testing.T) {
	model := testModel()
	model.PublishedRevision = 4
	snapshot, err := BuildSnapshot(model, SnapshotID("snapshot-5"))
	if err != nil {
		t.Fatalf("standalone model should snapshot without a template reference: %v", err)
	}
	if snapshot.Revision != 5 {
		t.Fatalf("snapshot revision=%d, want next published revision 5", snapshot.Revision)
	}
	if snapshot.ModelID != model.ID || snapshot.TenantID != model.TenantID || snapshot.Name != model.Name {
		t.Fatalf("snapshot lost model identity or name: %+v", snapshot)
	}
}

func TestBuildSnapshotForEmptyModelIsActuallyEmpty(t *testing.T) {
	model := testModel()
	model.Fields = make([]Field, 0)
	snapshot, err := BuildSnapshot(model, SnapshotID("snapshot-empty"))
	if err != nil {
		t.Fatalf("empty compatibility model should produce a snapshot: %v", err)
	}
	if len(snapshot.Fields) != 0 {
		t.Fatalf("empty model gained placeholder fields: %+v", snapshot.Fields)
	}
	var payload struct {
		Fields []json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(snapshot.Content, &payload); err != nil {
		t.Fatalf("snapshot content is not valid JSON: %v", err)
	}
	if payload.Fields == nil || len(payload.Fields) != 0 {
		t.Fatalf("snapshot content should encode a real empty field list, got %s", snapshot.Content)
	}
}

func TestBuildSnapshotDetachesNestedDraftData(t *testing.T) {
	min := 0.0
	localeMap := map[string]string{"zh-CN": "温度"}
	paramLocales := map[string]string{"zh-CN": "目标"}
	paramDescription := I18nString{Default: "设定的目标值", Locales: map[string]string{"en": "Requested target"}}
	enumLocales := map[string]string{"zh-CN": "制热"}
	enumRaw := json.RawMessage(`"heat"`)
	model := testModel(commandField("set.mode", &Parameters{
		SchemaVersion: ParametersVersion,
		Items: []Parameter{{
			Identifier: "mode", Name: I18nString{Default: "Mode", Locales: paramLocales},
			Description: &paramDescription,
			ValueType: ValueType{Kind: ScalarEnum, EnumBaseKind: ScalarString, EnumValues: []EnumValue{{
				Value: enumRaw, Label: I18nString{Default: "Heating", Locales: enumLocales},
			}}},
		}},
	}))
	model.Fields = append(model.Fields, scalarField(FieldTelemetry, "temperature", ValueType{
		Kind: ScalarNumber, Constraint: &ValueConstraint{Min: &min},
	}))
	model.Fields[1].Name.Locales = localeMap
	model.Fields[1].SortOrder = 17
	description := I18nString{Default: "Initial", Locales: map[string]string{"en": "Initial"}}
	model.Description = &description
	category := "hvac"
	model.Category = &category

	snapshot, err := BuildSnapshot(model, SnapshotID("snapshot-detached"))
	if err != nil {
		t.Fatalf("BuildSnapshot failed: %v", err)
	}
	contentBefore := append([]byte(nil), snapshot.Content...)

	model.Description.Locales["en"] = "changed description"
	model.Fields[0].Parameters.Items[0].Name.Locales["zh-CN"] = "changed parameter"
	model.Fields[0].Parameters.Items[0].Description.Locales["en"] = "changed parameter description"
	model.Fields[0].Parameters.Items[0].ValueType.EnumValues[0].Value[1] = 'C'
	*model.Fields[1].ValueType.Constraint.Min = 99
	model.Category = ptrString("changed category")
	model.Fields[0].Parameters.Items[0].ValueType.EnumValues[0].Label.Locales["zh-CN"] = "changed enum label"
	model.Fields = append(model.Fields, scalarField(FieldAttribute, "later", ValueType{Kind: ScalarBool}))

	if !bytes.Equal(snapshot.Content, contentBefore) {
		t.Fatal("snapshot serialized content changed after draft data mutation")
	}
	if snapshot.Name != "Demo model" || snapshot.Description.Locales["en"] != "Initial" || snapshot.Category == nil || *snapshot.Category != "hvac" {
		t.Fatal("snapshot model maps still alias the mutable draft")
	}
	command := snapshot.Fields[0]
	if command.Kind != FieldCommand {
		t.Fatalf("expected snapshot fields sorted by kind, got %+v", snapshot.Fields)
	}
	parameter := command.Parameters.Items[0]
	if parameter.Name.Locales["zh-CN"] != "目标" {
		t.Fatal("snapshot parameter locale map still aliases the mutable draft")
	}
	if parameter.Description.Locales["en"] != "Requested target" {
		t.Fatal("snapshot parameter description still aliases the mutable draft")
	}
	if string(parameter.ValueType.EnumValues[0].Value) != `"heat"` {
		t.Fatal("snapshot enum raw value still aliases the mutable draft")
	}
	if parameter.ValueType.EnumValues[0].Label.Locales["zh-CN"] != "制热" {
		t.Fatal("snapshot enum label map still aliases the mutable draft")
	}
	telemetry := snapshot.Fields[1]
	if telemetry.ValueType.Constraint.Min == nil || *telemetry.ValueType.Constraint.Min != 0 {
		t.Fatal("snapshot constraint pointer still aliases the mutable draft")
	}
	if telemetry.Name.Locales["zh-CN"] != "温度" || telemetry.SortOrder != 17 {
		t.Fatal("snapshot field locale or sort order was not preserved")
	}
	if len(snapshot.Fields) != 2 {
		t.Fatalf("snapshot field slice changed with draft append: got %d fields", len(snapshot.Fields))
	}
}

func ptrString(value string) *string { return &value }

func TestAuthorizeRejectsMissingIdentityScopeOrCapability(t *testing.T) {
	valid := Principal{SubjectID: "user-1", TenantID: TenantID("tenant-1"), Capabilities: []string{string(CapabilityPublish)}}
	cases := []struct {
		name       string
		principal  Principal
		target     TenantID
		capability Capability
		wantErr    bool
	}{
		{name: "valid scope and capability", principal: valid, target: TenantID("tenant-1"), capability: CapabilityPublish},
		{name: "tenant mismatch", principal: valid, target: TenantID("tenant-2"), capability: CapabilityPublish, wantErr: true},
		{name: "empty target tenant", principal: valid, target: "", capability: CapabilityPublish, wantErr: true},
		{name: "missing capability", principal: valid, target: TenantID("tenant-1"), capability: CapabilityWrite, wantErr: true},
		{name: "missing subject", principal: Principal{TenantID: valid.TenantID, Capabilities: valid.Capabilities}, target: valid.TenantID, capability: CapabilityPublish, wantErr: true},
		{name: "missing principal tenant", principal: Principal{SubjectID: valid.SubjectID, Capabilities: valid.Capabilities}, target: valid.TenantID, capability: CapabilityPublish, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Authorize(tc.principal, tc.target, tc.capability)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Authorize error=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestSnapshotChecksumChangesWithSemanticContent(t *testing.T) {
	firstModel := testModel(scalarField(FieldTelemetry, "temperature", ValueType{Kind: ScalarNumber}))
	first, err := BuildSnapshot(firstModel, SnapshotID("snapshot-1"))
	if err != nil {
		t.Fatalf("BuildSnapshot first: %v", err)
	}
	changedMin := 1.0
	secondModel := testModel(scalarField(FieldTelemetry, "temperature", ValueType{Kind: ScalarNumber, Constraint: &ValueConstraint{Min: &changedMin}}))
	second, err := BuildSnapshot(secondModel, SnapshotID("snapshot-2"))
	if err != nil {
		t.Fatalf("BuildSnapshot second: %v", err)
	}
	if first.Checksum == "" || first.Checksum == second.Checksum {
		t.Fatalf("checksum should be non-empty and reflect semantic field changes: first=%q second=%q", first.Checksum, second.Checksum)
	}
	if strings.Contains(string(first.Content), "snapshot-1") || strings.Contains(string(second.Content), "snapshot-2") {
		t.Fatal("snapshot ID must not leak into semantic content/checksum input")
	}
}
