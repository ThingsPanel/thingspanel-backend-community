package brokersemantic

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Mirrors the model accepted through Create/Publish/Version by
// app.TestPublishedCommandKindNamespaceAndVerbatimIdentifiers. All four kinds
// share the same exact identifier, but only COMMAND is eligible for dispatch.
func TestValidateCommandKindNamespaceAndVerbatimIdentifiers(t *testing.T) {
	for _, identifier := range []string{"Mode-Wire", "  Vendor.Mode/°C  ", " "} {
		t.Run(identifier, func(t *testing.T) {
			write, no := true, false
			model := commandModel{Name: "Namespace contract", Fields: []commandField{
				{Kind: "TELEMETRY", Identifier: identifier, NameI18n: commandI18n{Default: "Reported mode"}, Access: &commandAccess{Read: &write, Write: &no, Observe: &write}, ValueType: &commandValueType{Kind: "NUMBER"}},
				{Kind: "ATTRIBUTE", Identifier: identifier, NameI18n: commandI18n{Default: "Mode attribute"}, Access: &commandAccess{Read: &write, Write: &no, Observe: &no}, ValueType: &commandValueType{Kind: "STRING"}},
			}}
			parameters := &commandParams{SchemaVersion: 1, Items: []commandParameter{{Identifier: "  target/value  ", NameI18n: commandI18n{Default: "Target"}, ValueType: commandValueType{Kind: "BOOL"}}}}
			model.Fields = append(model.Fields,
				commandField{Kind: "EVENT", Identifier: identifier, NameI18n: commandI18n{Default: "Mode event"}, Access: &commandAccess{Read: &no, Write: &no, Observe: &write}, Parameters: parameters},
				commandField{Kind: "COMMAND", Identifier: identifier, NameI18n: commandI18n{Default: "Set mode"}, Access: &commandAccess{Read: &no, Write: &write, Observe: &no}, Parameters: parameters})
			encode := func() Description {
				raw, err := json.Marshal(model)
				if err != nil {
					t.Fatal(err)
				}
				return Description{Model: raw}
			}
			description := encode()
			for _, raw := range []string{`{"  target/value  ":true}`, `{"  target/value  ":false}`} {
				if err := ValidateCommand(description, identifier, params(raw)); err != nil {
					t.Errorf("published command rejected: %v", err)
				}
			}
			for _, raw := range []string{`{}`, `{"target/value":true}`, `{"  target/value  ":"true"}`, `{"  target/value  ":1}`, `{"  target/value  ":true,"extra":1}`} {
				if err := ValidateCommand(description, identifier, params(raw)); err == nil {
					t.Errorf("invalid parameters accepted: %s", raw)
				}
			}
			if ValidateCommand(description, identifier+"missing", params(`{"  target/value  ":true}`)) == nil {
				t.Fatal("unknown command accepted")
			}
			// Malformed duplicate commands still fail closed even though such a
			// model cannot be published; do not confuse this with cross-kind reuse.
			model.Fields = append(model.Fields, model.Fields[3])
			if ValidateCommand(encode(), identifier, params(`{"  target/value  ":true}`)) == nil {
				t.Fatal("duplicate command accepted")
			}
			model.Fields = model.Fields[:3]
			if ValidateCommand(encode(), identifier, params(`{"  target/value  ":true}`)) == nil {
				t.Fatal("non-command namespace used")
			}
		})
	}
}

func commandDescription(t *testing.T, kind string, constraint *commandConstraint, enums []commandEnumOption) Description {
	t.Helper()
	write := true
	model := commandModel{
		Name: "test",
		Fields: []commandField{{
			Kind: "COMMAND", Identifier: "set", Access: &commandAccess{Write: &write},
			Parameters: &commandParams{SchemaVersion: 1, Items: []commandParameter{{
				Identifier: "value", ValueType: commandValueType{Kind: kind, BaseKind: enumBase(kind), Constraint: constraint, EnumValues: enums},
			}}},
		}},
	}
	data, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	return Description{Model: data}
}

func enumBase(kind string) string {
	if kind == "ENUM" {
		return "STRING"
	}
	return ""
}

func params(raw string) *string { return &raw }

func TestValidateCommandEnumStillAppliesScalarConstraints(t *testing.T) {
	description := commandDescription(t, "ENUM", &commandConstraint{Pattern: "^[A-Z]+$", MaxLength: intPtr(8)}, []commandEnumOption{
		{Code: json.RawMessage(`"GOOD"`)},
		{Code: json.RawMessage(`"bad"`)}, // Published enum values do not override constraints.
	})
	if err := ValidateCommand(description, "set", params(`{"value":"GOOD"}`)); err != nil {
		t.Fatalf("valid constrained enum rejected: %v", err)
	}
	if err := ValidateCommand(description, "set", params(`{"value":"bad"}`)); !errors.Is(err, ErrCommandInvalid) {
		t.Fatalf("enum bypassed scalar pattern: got %v", err)
	}
}

func TestValidateCommandBoundsNumberParsing(t *testing.T) {
	description := commandDescription(t, "NUMBER", nil, nil)
	cases := []struct {
		name  string
		value string
	}{
		{"huge positive exponent", `1e999999999`},
		{"huge negative exponent", `1e-999999999`},
		{"long exponent", `1e+9999`},
		{"oversized digits", strings.Repeat("9", 129)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateCommand(description, "set", params(`{"value":`+tc.value+`}`)); !errors.Is(err, ErrCommandInvalid) {
				t.Fatalf("got %v; want ErrCommandInvalid", err)
			}
		})
	}
}

func TestValidateCommandUsesCoreUTF8ByteMaxLength(t *testing.T) {
	description := commandDescription(t, "STRING", &commandConstraint{MaxLength: intPtr(3)}, nil)
	if err := ValidateCommand(description, "set", params(`{"value":"中"}`)); err != nil {
		t.Fatalf("3-byte UTF-8 character should fit max_length=3: %v", err)
	}
	description = commandDescription(t, "STRING", &commandConstraint{MaxLength: intPtr(2)}, nil)
	if err := ValidateCommand(description, "set", params(`{"value":"中"}`)); !errors.Is(err, ErrCommandInvalid) {
		t.Fatalf("3-byte UTF-8 character should exceed max_length=2: %v", err)
	}
}

func TestValidateCommandRejectsNonFiniteConstraint(t *testing.T) {
	value := 0.0
	value = 1.0 / value
	v := commandValueType{Kind: "NUMBER", Constraint: &commandConstraint{Min: &value}}
	if err := validateConstraintSchema(v, "NUMBER"); !errors.Is(err, ErrCommandUnsupported) {
		t.Fatalf("non-finite schema constraint should fail closed: %v", err)
	}
}

func intPtr(value int) *int { return &value }
