package core

import (
	"encoding/json"
	"testing"
)

func commandSnapshotFixture() Snapshot {
	min, max, step, precision := 0.0, 10.0, 0.5, 1
	return Snapshot{Fields: []Field{{Kind: FieldCommand, Identifier: "set", Access: Access{Write: true}, Parameters: &Parameters{SchemaVersion: 1, Items: []Parameter{
		{Identifier: "enabled", ValueType: ValueType{Kind: ScalarBool, Constraint: &ValueConstraint{TrueText: "On", FalseText: "Off"}}},
		{Identifier: "mode", ValueType: ValueType{Kind: ScalarEnum, EnumBaseKind: ScalarString, EnumValues: []EnumValue{{Value: json.RawMessage(`"auto"`), Label: I18nString{Default: "Auto"}}, {Value: json.RawMessage(`"manual"`), Label: I18nString{Default: "Manual"}}}}},
		{Identifier: "level", ValueType: ValueType{Kind: ScalarNumber, Constraint: &ValueConstraint{Min: &min, Max: &max, Step: &step, Precision: &precision}}},
	}}}}}
}

func TestPublishedCommandParameterContract(t *testing.T) {
	snapshot := commandSnapshotFixture()
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{`{"enabled":true,"mode":"auto","level":0}`, true},
		{`{"enabled":false,"mode":"manual","level":10}`, true},
		{`{"enabled":true,"mode":"auto","level":0.5}`, true},
		{`{"enabled":"true","mode":"auto","level":1}`, false},
		{`{"enabled":1,"mode":"auto","level":1}`, false},
		{`{"enabled":null,"mode":"auto","level":1}`, false},
		{`{"enabled":true,"mode":"missing","level":1}`, false},
		{`{"enabled":true,"mode":"auto","level":"1"}`, false},
		{`{"enabled":true,"mode":"auto","level":11}`, false},
		{`{"enabled":true,"mode":"auto","level":-1}`, false},
		{`{"enabled":true,"mode":"auto","level":0.1}`, false},
		{`{"enabled":true,"mode":"auto","level":1e999}`, false},
		{`{"enabled":true,"mode":"auto","level":1e-999999}`, false},
		{`{"enabled":true,"mode":"auto","level":1,"extra":2}`, false},
		{`{"enabled":true,"mode":"auto","level":1,"level":2}`, false},
		{`{"enabled":true,"mode":"auto"}`, false},
		{`[]`, false}, {`null`, false}, {`{} {}`, false},
	} {
		if got := ValidateCommandParameters(snapshot, "set", json.RawMessage(tc.value)); (got == nil) != tc.valid {
			t.Errorf("params=%s valid=%t error=%v", tc.value, tc.valid, got)
		}
	}
	if ValidateCommandParameters(snapshot, "unknown", json.RawMessage(`{}`)) == nil {
		t.Fatal("unknown command accepted")
	}
	snapshot.Fields[0].Access.Write = false
	if ValidateCommandParameters(snapshot, "set", json.RawMessage(`{"enabled":true,"mode":"auto","level":1}`)) == nil {
		t.Fatal("non-writable command accepted")
	}
}

func TestPublishedCommandTypedEnumsAndExactNumber(t *testing.T) {
	s := Snapshot{Fields: []Field{{Kind: FieldCommand, Identifier: "set", Access: Access{Write: true}, Parameters: &Parameters{SchemaVersion: 1, Items: []Parameter{{Identifier: "value", ValueType: ValueType{Kind: ScalarEnum, EnumBaseKind: ScalarNumber, EnumValues: []EnumValue{{Value: json.RawMessage(`9007199254740993`), Label: I18nString{Default: "Large"}}}}}}}}}}
	for _, tc := range []struct {
		v     string
		valid bool
	}{{`9007199254740993`, true}, {`9007199254740992`, false}, {`"9007199254740993"`, false}, {`true`, false}} {
		raw := json.RawMessage(`{"value":` + tc.v + `}`)
		if (ValidateCommandParameters(s, "set", raw) == nil) != tc.valid {
			t.Fatal(tc.v)
		}
	}
	s.Fields[0].Parameters.Items[0].ValueType = ValueType{Kind: ScalarEnum, EnumBaseKind: ScalarBool, EnumValues: []EnumValue{{Value: json.RawMessage(`true`), Label: I18nString{Default: "On"}}}}
	if ValidateCommandParameters(s, "set", json.RawMessage(`{"value":true}`)) != nil || ValidateCommandParameters(s, "set", json.RawMessage(`{"value":1}`)) == nil {
		t.Fatal("boolean enum coercion")
	}
}
