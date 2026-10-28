package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strings"
)

func ValidateModel(model Model) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	if model.ID == "" {
		issues = append(issues, issue("id", "MODEL_ID_REQUIRED", "model id is required"))
	}
	if model.TenantID == "" {
		issues = append(issues, issue("tenant_id", "TENANT_ID_REQUIRED", "tenant id is required"))
	}
	if strings.TrimSpace(model.Name) == "" {
		issues = append(issues, issue("name", "MODEL_NAME_REQUIRED", "model name is required"))
	}
	if model.Description != nil {
		issues = append(issues, appendI18nIssues("description", *model.Description, false)...)
	}
	seen := make(map[string]struct{}, len(model.Fields))
	for i, field := range model.Fields {
		path := fmt.Sprintf("fields[%d]", i)
		for _, found := range ValidateField(field) {
			found.Path = path + "." + found.Path
			issues = append(issues, found)
		}
		key := string(field.Kind) + "\x00" + field.Identifier
		if _, ok := seen[key]; ok {
			issues = append(issues, issue(path+".identifier", "FIELD_IDENTIFIER_DUPLICATE", "identifier is already used by this field kind"))
		} else {
			seen[key] = struct{}{}
		}
	}
	return issues
}

func ValidateField(field Field) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	if !validFieldKind(field.Kind) {
		issues = append(issues, issue("kind", "FIELD_KIND_INVALID", "kind must be TELEMETRY, ATTRIBUTE, EVENT, or COMMAND"))
	}
	if field.Identifier == "" {
		issues = append(issues, issue("identifier", "FIELD_IDENTIFIER_REQUIRED", "identifier is required"))
	}
	issues = append(issues, appendI18nIssues("name", field.Name, true)...)
	if field.Description != nil {
		issues = append(issues, appendI18nIssues("description", *field.Description, false)...)
	}
	switch field.Kind {
	case FieldTelemetry, FieldAttribute:
		if field.ValueType == nil {
			issues = append(issues, issue("value_type", "FIELD_VALUE_TYPE_REQUIRED", "scalar field requires value_type"))
		} else {
			issues = append(issues, appendValueTypeIssues("value_type", *field.ValueType)...)
		}
		if field.Parameters != nil {
			issues = append(issues, issue("parameters", "FIELD_PARAMETERS_UNEXPECTED", "TELEMETRY and ATTRIBUTE fields do not have parameters"))
		}
	case FieldEvent, FieldCommand:
		if field.ValueType != nil {
			issues = append(issues, issue("value_type", "FIELD_VALUE_TYPE_UNEXPECTED", "EVENT and COMMAND fields use typed parameters"))
		}
		if field.Parameters == nil {
			issues = append(issues, issue("parameters", "FIELD_PARAMETERS_REQUIRED", "EVENT and COMMAND fields require parameters"))
			break
		}
		if field.Parameters.SchemaVersion != ParametersVersion {
			issues = append(issues, issue("parameters.schema_version", "PARAMETERS_VERSION_UNSUPPORTED", "unsupported parameter schema version"))
		}
		seen := make(map[string]struct{}, len(field.Parameters.Items))
		for i, parameter := range field.Parameters.Items {
			path := fmt.Sprintf("parameters[%d]", i)
			if parameter.Identifier == "" {
				issues = append(issues, issue(path+".identifier", "PARAMETER_IDENTIFIER_REQUIRED", "parameter identifier is required"))
			}
			if _, ok := seen[parameter.Identifier]; ok {
				issues = append(issues, issue(path+".identifier", "PARAMETER_IDENTIFIER_DUPLICATE", "parameter identifier is duplicated"))
			}
			seen[parameter.Identifier] = struct{}{}
			issues = append(issues, appendI18nIssues(path+".name", parameter.Name, true)...)
			if parameter.Description != nil {
				issues = append(issues, appendI18nIssues(path+".description", *parameter.Description, false)...)
			}
			for _, found := range appendValueTypeIssues(path+".value_type", parameter.ValueType) {
				issues = append(issues, found)
			}
		}
	}
	return issues
}

func appendI18nIssues(path string, value I18nString, required bool) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	if required && strings.TrimSpace(value.Default) == "" {
		issues = append(issues, issue(path+".default", "I18N_DEFAULT_REQUIRED", "default text is required"))
	}
	if len(value.Default) > 1024 {
		issues = append(issues, issue(path+".default", "I18N_TEXT_TOO_LONG", "default text exceeds 1024 bytes"))
	}
	for locale, text := range value.Locales {
		if len(locale) == 0 || strings.TrimSpace(text) == "" {
			issues = append(issues, issue(path+".locales", "I18N_LOCALE_INVALID", "locale and text are required"))
		}
		if len(text) > 1024 {
			issues = append(issues, issue(path+".locales."+locale, "I18N_TEXT_TOO_LONG", "localized text exceeds 1024 bytes"))
		}
	}
	return issues
}

// ValidateScalar executes min/max, pattern, and max_length constraints. Step,
// precision, and BOOL display texts remain typed metadata until runtime value
// enforcement for those properties is defined.
func ValidateScalar(valueType ValueType, raw json.RawMessage) error {
	if found := appendValueTypeIssues("value_type", valueType); len(found) > 0 {
		return fmt.Errorf("%s: %s", found[0].Code, found[0].Message)
	}
	base := valueType.Kind
	if base == ScalarEnum {
		base = valueType.EnumBaseKind
	}
	if valueType.Kind == ScalarEnum {
		for _, option := range valueType.EnumValues {
			if scalarEqual(option.Value, raw, base) {
				return nil
			}
		}
		return fmt.Errorf("VALUE_NOT_IN_ENUM: value is not an enum option")
	}
	if err := validateScalarJSON(raw, base, valueType.Constraint); err != nil {
		return err
	}
	return nil
}

func appendValueTypeIssues(path string, valueType ValueType) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	if valueType.Kind != ScalarInt && valueType.Kind != ScalarFloat && valueType.Kind != ScalarNumber && valueType.Kind != ScalarBool && valueType.Kind != ScalarString && valueType.Kind != ScalarEnum {
		return []ValidationIssue{issue(path, "VALUE_TYPE_INVALID", "unsupported scalar kind")}
	}
	if valueType.Kind == ScalarEnum {
		if !validEnumBase(valueType.EnumBaseKind) {
			issues = append(issues, issue(path+".enum_base_kind", "ENUM_BASE_KIND_INVALID", "enum base kind must be NUMBER, BOOL, or STRING"))
		}
		if len(valueType.EnumValues) == 0 {
			issues = append(issues, issue(path+".enum_values", "ENUM_VALUES_REQUIRED", "enum requires at least one value"))
		}
		seen := make(map[string]struct{}, len(valueType.EnumValues))
		for i, option := range valueType.EnumValues {
			if !validEnumBase(valueType.EnumBaseKind) {
				continue
			}
			issues = append(issues, appendI18nIssues(fmt.Sprintf("%s.enum_values[%d].label", path, i), option.Label, true)...)
			if err := validateScalarJSON(option.Value, valueType.EnumBaseKind, nil); err != nil {
				issues = append(issues, issue(fmt.Sprintf("%s.enum_values[%d]", path, i), "ENUM_VALUE_INVALID", err.Error()))
				continue
			}
			key, _ := canonicalScalarKey(option.Value, valueType.EnumBaseKind)
			if _, ok := seen[key]; ok {
				issues = append(issues, issue(path+".enum_values", "ENUM_VALUE_DUPLICATE", "enum contains a duplicate value"))
			}
			seen[key] = struct{}{}
		}
	}
	if c := valueType.Constraint; c != nil {
		if c.Min != nil && c.Max != nil && *c.Min > *c.Max {
			issues = append(issues, issue(path+".constraint", "VALUE_CONSTRAINT_INVALID", "min exceeds max"))
		}
		if c.Step != nil && (*c.Step <= 0 || math.IsNaN(*c.Step) || math.IsInf(*c.Step, 0)) {
			issues = append(issues, issue(path+".constraint.step", "VALUE_CONSTRAINT_INVALID", "step must be finite and positive"))
		}
		if c.Min != nil && (math.IsNaN(*c.Min) || math.IsInf(*c.Min, 0)) || c.Max != nil && (math.IsNaN(*c.Max) || math.IsInf(*c.Max, 0)) {
			issues = append(issues, issue(path+".constraint", "VALUE_CONSTRAINT_INVALID", "numeric limits must be finite"))
		}
		if c.Pattern != "" {
			if _, err := regexp.Compile(c.Pattern); err != nil {
				issues = append(issues, issue(path+".constraint.pattern", "VALUE_PATTERN_INVALID", "pattern is not valid: "+err.Error()))
			}
		}
		if c.MaxLength != nil && *c.MaxLength < 0 {
			issues = append(issues, issue(path+".constraint.max_length", "VALUE_CONSTRAINT_INVALID", "max length cannot be negative"))
		}
		if c.Precision != nil && *c.Precision < 0 {
			issues = append(issues, issue(path+".constraint.precision", "VALUE_CONSTRAINT_INVALID", "precision cannot be negative"))
		}
		if (c.TrueText != "" || c.FalseText != "") && valueType.Kind != ScalarBool {
			issues = append(issues, issue(path+".constraint", "VALUE_CONSTRAINT_INVALID", "true_text and false_text only apply to BOOL"))
		}
		constraintKind := valueType.Kind
		if constraintKind == ScalarEnum {
			constraintKind = valueType.EnumBaseKind
		}
		if c.KindDoesNotSupport(constraintKind) {
			issues = append(issues, issue(path+".constraint", "VALUE_CONSTRAINT_INVALID", "constraint does not apply to this value kind"))
		}
	}
	return issues
}

func (c *ValueConstraint) KindDoesNotSupport(kind ScalarKind) bool {
	if c == nil {
		return false
	}
	if (c.Min != nil || c.Max != nil || c.Step != nil || c.Precision != nil) && kind != ScalarInt && kind != ScalarFloat && kind != ScalarNumber {
		return true
	}
	if (c.Pattern != "" || c.MaxLength != nil) && kind != ScalarString {
		return true
	}
	return false
}

func validateScalarJSON(raw json.RawMessage, kind ScalarKind, constraint *ValueConstraint) error {
	if len(raw) == 0 || !json.Valid(raw) {
		return fmt.Errorf("VALUE_INVALID: value must be one valid JSON scalar")
	}
	switch kind {
	case ScalarInt:
		n, ok := decodeJSONNumber(raw)
		if !ok || strings.ContainsAny(n.String(), ".eE") {
			return fmt.Errorf("VALUE_INVALID: expected integer")
		}
		if _, ok := new(big.Int).SetString(n.String(), 10); !ok {
			return fmt.Errorf("VALUE_INVALID: expected integer")
		}
		return validateNumberConstraint(n, constraint)
	case ScalarFloat, ScalarNumber:
		n, ok := decodeJSONNumber(raw)
		if !ok {
			return fmt.Errorf("VALUE_INVALID: expected number")
		}
		if _, err := n.Float64(); err != nil {
			return fmt.Errorf("VALUE_INVALID: expected finite number")
		}
		return validateNumberConstraint(n, constraint)
	case ScalarBool:
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("VALUE_INVALID: expected boolean")
		}
		return nil
	case ScalarString:
		var value string
		if err := json.Unmarshal(raw, &value); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("VALUE_INVALID: expected string")
		}
		if constraint != nil {
			if constraint.MaxLength != nil && len(value) > *constraint.MaxLength {
				return fmt.Errorf("VALUE_INVALID: max_length exceeded")
			}
			if constraint.Pattern != "" {
				re, err := regexp.Compile(constraint.Pattern)
				if err != nil || !re.MatchString(value) {
					return fmt.Errorf("VALUE_INVALID: pattern mismatch")
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("VALUE_TYPE_INVALID: unsupported base kind")
	}
}

func validateNumberConstraint(value json.Number, constraint *ValueConstraint) error {
	if constraint == nil {
		return nil
	}
	v, err := value.Float64()
	if err != nil {
		return fmt.Errorf("VALUE_INVALID: expected finite number")
	}
	if constraint.Min != nil && v < *constraint.Min {
		return fmt.Errorf("VALUE_INVALID: below min")
	}
	if constraint.Max != nil && v > *constraint.Max {
		return fmt.Errorf("VALUE_INVALID: above max")
	}
	return nil
}

func decodeJSONNumber(raw json.RawMessage) (json.Number, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", false
	}
	number, ok := value.(json.Number)
	return number, ok
}

func scalarEqual(a, b json.RawMessage, kind ScalarKind) bool {
	if err := validateScalarJSON(b, kind, nil); err != nil {
		return false
	}
	keyA, okA := canonicalScalarKey(a, kind)
	keyB, okB := canonicalScalarKey(b, kind)
	return okA && okB && keyA == keyB
}

func canonicalScalarKey(raw json.RawMessage, kind ScalarKind) (string, bool) {
	if kind == ScalarInt || kind == ScalarFloat || kind == ScalarNumber {
		number, ok := decodeJSONNumber(raw)
		if !ok {
			return "", false
		}
		rational, ok := new(big.Rat).SetString(number.String())
		if !ok {
			return "", false
		}
		return "n:" + rational.RatString(), true
	}
	switch kind {
	case ScalarString:
		var value string
		if err := json.Unmarshal(raw, &value); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return "", false
		}
		return "s:" + value, true
	case ScalarBool:
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return "", false
		}
		return fmt.Sprintf("b:%t", value), true
	default:
		return "", false
	}
}

func validFieldKind(kind FieldKind) bool {
	return kind == FieldTelemetry || kind == FieldAttribute || kind == FieldEvent || kind == FieldCommand
}

func validEnumBase(kind ScalarKind) bool {
	return kind == ScalarNumber || kind == ScalarBool || kind == ScalarString
}

func issue(path, code, message string) ValidationIssue {
	return ValidationIssue{Path: path, Code: code, Message: message}
}
