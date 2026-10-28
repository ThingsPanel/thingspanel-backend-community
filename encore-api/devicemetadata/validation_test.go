package devicemetadata

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateIdentifier(t *testing.T) {
	cases := []struct {
		id string
		ok bool
	}{
		{"temperature", true},
		{"_starts_underscore", false},
		{"123start", false},
		{"has-dash", false},
		{"UPPER", false},
		{"emoji😀", false},
		{"id", false},
		{strings.Repeat("a", 65), false},
	}
	for _, tc := range cases {
		err := ValidateIdentifier(tc.id)
		if tc.ok && err != nil {
			t.Fatalf("%q should be valid: %v", tc.id, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("%q should be invalid", tc.id)
		}
	}
}

func TestI18nResolve(t *testing.T) {
	s := &I18nString{
		Default: "Temperature",
		Locales: map[string]string{"zh-CN": "温度", "zh": "温度通"},
	}
	if got := s.Resolve("zh-CN"); got != "温度" {
		t.Fatalf("expected locale hit, got %q", got)
	}
	if got := s.Resolve("zh-TW"); got != "温度通" {
		t.Fatalf("expected language fallback, got %q", got)
	}
	if got := s.Resolve("en-US"); got != "Temperature" {
		t.Fatalf("expected default fallback, got %q", got)
	}
	var nilI18n *I18nString
	if got := nilI18n.Resolve("zh"); got != "" {
		t.Fatalf("expected empty string for nil i18n, got %q", got)
	}
}

func TestValueTypeValidateAndValue(t *testing.T) {
	min, max := 0.0, 100.0
	maxLen := 3
	validCases := []struct {
		name string
		vt   ValueType
		val  string
	}{
		{"int", ValueType{Kind: "INT", Constraint: &ValueConstraint{Min: &min, Max: &max}}, `42`},
		{"float", ValueType{Kind: "FLOAT", Constraint: &ValueConstraint{Min: &min, Max: &max}}, `42.5`},
		{"bool", ValueType{Kind: "BOOL"}, `true`},
		{"string", ValueType{Kind: "STRING", Constraint: &ValueConstraint{Pattern: "^[A-Z]+$", MaxLength: &maxLen}}, `"ABC"`},
		{"enum", ValueType{Kind: "ENUM", EnumValues: []EnumOption{{Code: "ON", Label: I18nString{Default: "On"}}}}, `"ON"`},
	}
	for _, tc := range validCases {
		if err := tc.vt.Validate(); err != nil {
			t.Fatalf("%s validate failed: %v", tc.name, err)
		}
		if err := tc.vt.ValidateValue(json.RawMessage(tc.val)); err != nil {
			t.Fatalf("%s value validation failed: %v", tc.name, err)
		}
	}
}

func TestValidateAccessForType(t *testing.T) {
	if err := ValidateAccessForType("PROPERTY", Access{Read: true}); err != nil {
		t.Fatalf("property access should pass: %v", err)
	}
	if err := ValidateAccessForType("EVENT", Access{Observe: true}); err != nil {
		t.Fatalf("event access should pass: %v", err)
	}
	if err := ValidateAccessForType("COMMAND", Access{Write: true}); err != nil {
		t.Fatalf("command access should pass: %v", err)
	}
	if err := ValidateAccessForType("EVENT", Access{Read: true}); err == nil {
		t.Fatal("event read access should fail")
	}
}

func BenchmarkI18nResolve(b *testing.B) {
	s := &I18nString{Default: "Temperature", Locales: map[string]string{"zh-CN": "温度"}}
	for i := 0; i < b.N; i++ {
		_ = s.Resolve("zh-CN")
	}
}

func BenchmarkValidateValueWithRegex(b *testing.B) {
	maxLen := 8
	vt := ValueType{Kind: "STRING", Constraint: &ValueConstraint{Pattern: "^[A-Z0-9_]+$", MaxLength: &maxLen}}
	raw := json.RawMessage(`"ABC_123"`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := vt.ValidateValue(raw); err != nil {
			b.Fatalf("validate failed: %v", err)
		}
	}
}
