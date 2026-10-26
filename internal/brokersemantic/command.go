package brokersemantic

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

var (
	// ErrCommandInvalid means caller parameters do not satisfy the published schema.
	ErrCommandInvalid = errors.New("brokersemantic: command parameters are invalid")
	// ErrCommandUnsupported means the published command/schema is outside this
	// validator's explicitly supported v1 subset and cannot be safely authorized.
	ErrCommandUnsupported = errors.New("brokersemantic: command semantics are unsupported")
)

const (
	maxCommandParamsBytes = 64 << 10
	maxCommandParameters  = 32
)

type commandModel struct {
	Name            string         `json:"name"`
	DescriptionI18n *commandI18n   `json:"descriptionI18n,omitempty"`
	Category        *string        `json:"category,omitempty"`
	Fields          []commandField `json:"fields"`
}

type commandField struct {
	FieldID         string            `json:"fieldID,omitempty"`
	Kind            string            `json:"kind"`
	Identifier      string            `json:"identifier"`
	NameI18n        commandI18n       `json:"nameI18n"`
	DescriptionI18n *commandI18n      `json:"descriptionI18n,omitempty"`
	ValueType       *commandValueType `json:"valueType,omitempty"`
	Access          *commandAccess    `json:"access"`
	Unit            string            `json:"unit,omitempty"`
	Parameters      *commandParams    `json:"parameters,omitempty"`
	MetaItems       json.RawMessage   `json:"metaItems,omitempty"`
	SortOrder       int               `json:"sortOrder,omitempty"`
}

type commandAccess struct {
	Read    *bool `json:"read"`
	Write   *bool `json:"write"`
	Observe *bool `json:"observe"`
}

type commandParams struct {
	SchemaVersion int                `json:"schemaVersion"`
	Items         []commandParameter `json:"items"`
}

type commandParameter struct {
	Identifier      string           `json:"identifier"`
	NameI18n        commandI18n      `json:"nameI18n"`
	DescriptionI18n *commandI18n     `json:"descriptionI18n,omitempty"`
	ValueType       commandValueType `json:"valueType"`
}

type commandI18n struct {
	Default string            `json:"default"`
	Locales map[string]string `json:"locales,omitempty"`
}

type commandValueType struct {
	Kind       string              `json:"kind"`
	BaseKind   string              `json:"baseKind,omitempty"`
	Constraint *commandConstraint  `json:"constraint,omitempty"`
	EnumValues []commandEnumOption `json:"enumValues,omitempty"`
}

type commandConstraint struct {
	Min       *float64 `json:"min,omitempty"`
	Max       *float64 `json:"max,omitempty"`
	Step      *float64 `json:"step,omitempty"`
	Precision *int     `json:"precision,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
	MaxLength *int     `json:"maxLength,omitempty"`
	TrueText  string   `json:"trueText,omitempty"`
	FalseText string   `json:"falseText,omitempty"`
}

type commandEnumOption struct {
	Code  json.RawMessage `json:"code"`
	Label commandI18n     `json:"label"`
}

// ValidateCommand checks raw JSON parameters against one COMMAND field in the
// exact published ModelDraft carried by Description.Model. No model state is
// inferred from display labels, device payloads, or community-side metadata.
func ValidateCommand(description Description, identifier string, params *string) error {
	if strings.TrimSpace(identifier) == "" || identifier != strings.TrimSpace(identifier) || params == nil || len(*params) > maxCommandParamsBytes {
		return ErrCommandInvalid
	}
	var model commandModel
	if len(description.Model) == 0 || !validJSONObject(description.Model) || decodeCommandModel(description.Model, &model) != nil || model.Fields == nil {
		return ErrCommandUnsupported
	}
	var selected *commandField
	for i := range model.Fields {
		field := &model.Fields[i]
		if field.Identifier != identifier {
			continue
		}
		if selected != nil {
			return ErrCommandUnsupported
		}
		selected = field
	}
	if selected == nil || selected.Kind != "COMMAND" || selected.Access == nil || selected.Access.Write == nil || !*selected.Access.Write || selected.Parameters == nil {
		return ErrCommandUnsupported
	}
	if selected.Parameters.SchemaVersion != 1 || selected.Parameters.Items == nil || len(selected.Parameters.Items) > maxCommandParameters {
		return ErrCommandUnsupported
	}
	paramsMap, err := decodeCommandParams([]byte(*params))
	if err != nil {
		return ErrCommandInvalid
	}
	if len(paramsMap) != len(selected.Parameters.Items) {
		return ErrCommandInvalid
	}
	seen := make(map[string]struct{}, len(selected.Parameters.Items))
	for _, parameter := range selected.Parameters.Items {
		if parameter.Identifier == "" || strings.TrimSpace(parameter.Identifier) != parameter.Identifier {
			return ErrCommandUnsupported
		}
		if _, duplicate := seen[parameter.Identifier]; duplicate {
			return ErrCommandUnsupported
		}
		seen[parameter.Identifier] = struct{}{}
		value, exists := paramsMap[parameter.Identifier]
		if !exists {
			return ErrCommandInvalid
		}
		if err := validateCommandValue(parameter.ValueType, value); err != nil {
			if errors.Is(err, ErrCommandUnsupported) {
				return err
			}
			return ErrCommandInvalid
		}
	}
	return nil
}

func decodeCommandParams(data []byte) (map[string]any, error) {
	if len(data) == 0 || len(data) > maxCommandParamsBytes || !json.Valid(data) || rejectDuplicateKeys(data) != nil {
		return nil, ErrCommandInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, ErrCommandInvalid
	}
	object, ok := value.(map[string]any)
	if !ok { // Includes null and arrays; v1 parameters are a JSON object.
		return nil, ErrCommandInvalid
	}
	return object, nil
}

func validateCommandValue(v commandValueType, value any) error {
	base := v.Kind
	if v.Kind == "ENUM" {
		if v.BaseKind != "NUMBER" && v.BaseKind != "BOOL" && v.BaseKind != "STRING" || len(v.EnumValues) == 0 {
			return ErrCommandUnsupported
		}
		if err := validateConstraintSchema(v, v.BaseKind); err != nil {
			return err
		}
		matched := false
		for _, option := range v.EnumValues {
			candidate, err := decodeScalar(option.Code)
			if err != nil || validateScalarValue(v.BaseKind, candidate, nil) != nil {
				return ErrCommandUnsupported
			}
			if scalarEqual(v.BaseKind, candidate, value) {
				if matched { // Malformed duplicate enum encodings fail closed.
					return ErrCommandUnsupported
				}
				matched = true
			}
		}
		if !matched {
			return ErrCommandInvalid
		}
		return validateScalarValue(v.BaseKind, value, v.Constraint)
	}
	if v.BaseKind != "" || len(v.EnumValues) > 0 {
		return ErrCommandUnsupported
	}
	if err := validateConstraintSchema(v, base); err != nil {
		return err
	}
	return validateScalarValue(base, value, v.Constraint)
}

func validateConstraintSchema(v commandValueType, base string) error {
	c := v.Constraint
	if c == nil {
		return nil
	}
	if c.TrueText != "" || c.FalseText != "" { // Display metadata is not runtime semantics.
		return ErrCommandUnsupported
	}
	if c.Min != nil || c.Max != nil || c.Step != nil || c.Precision != nil {
		if base != "INT" && base != "FLOAT" && base != "NUMBER" {
			return ErrCommandUnsupported
		}
	}
	if (c.Pattern != "" || c.MaxLength != nil) && base != "STRING" {
		return ErrCommandUnsupported
	}
	if c.Min != nil && c.Max != nil && *c.Min > *c.Max {
		return ErrCommandUnsupported
	}
	if c.Min != nil && !finite(*c.Min) || c.Max != nil && !finite(*c.Max) || c.Step != nil && (*c.Step <= 0 || !finite(*c.Step)) || c.Precision != nil && (*c.Precision < 0 || *c.Precision > 64) || c.MaxLength != nil && (*c.MaxLength < 0 || *c.MaxLength > maxCommandParamsBytes) {
		return ErrCommandUnsupported
	}
	if c.Pattern != "" {
		if _, err := regexp.Compile(c.Pattern); err != nil {
			return ErrCommandUnsupported
		}
	}
	if base != "INT" && base != "FLOAT" && base != "NUMBER" && base != "BOOL" && base != "STRING" {
		return ErrCommandUnsupported
	}
	return nil
}

func validateScalarValue(kind string, value any, c *commandConstraint) error {
	switch kind {
	case "INT", "FLOAT", "NUMBER":
		number, ok := value.(json.Number)
		if !ok {
			return ErrCommandInvalid
		}
		if kind == "INT" && strings.ContainsAny(string(number), ".eE") {
			return ErrCommandInvalid
		}
		r, ok := exactJSONNumber(number)
		if !ok {
			return ErrCommandInvalid
		}
		if c != nil {
			if c.Min != nil {
				min, ok := rationalFromFloat(*c.Min)
				if !ok {
					return ErrCommandUnsupported
				}
				if r.Cmp(min) < 0 {
					return ErrCommandInvalid
				}
			}
			if c.Max != nil {
				max, ok := rationalFromFloat(*c.Max)
				if !ok {
					return ErrCommandUnsupported
				}
				if r.Cmp(max) > 0 {
					return ErrCommandInvalid
				}
			}
			if c.Step != nil {
				step, ok := rationalFromFloat(*c.Step)
				if !ok {
					return ErrCommandUnsupported
				}
				origin := new(big.Rat)
				if c.Min != nil {
					origin, ok = rationalFromFloat(*c.Min)
					if !ok {
						return ErrCommandUnsupported
					}
				}
				quotient := new(big.Rat).Quo(new(big.Rat).Sub(r, origin), step)
				if quotient.Denom().Cmp(big.NewInt(1)) != 0 {
					return ErrCommandInvalid
				}
			}
			if c.Precision != nil && decimalPlaces(r) > *c.Precision {
				return ErrCommandInvalid
			}
		}
		return nil
	case "BOOL":
		if _, ok := value.(bool); !ok {
			return ErrCommandInvalid
		}
		return nil
	case "STRING":
		text, ok := value.(string)
		if !ok {
			return ErrCommandInvalid
		}
		if c != nil {
			if c.MaxLength != nil && len(text) > *c.MaxLength {
				return ErrCommandInvalid
			}
			if c.Pattern != "" {
				re, err := regexp.Compile(c.Pattern)
				if err != nil {
					return ErrCommandUnsupported
				}
				if !re.MatchString(text) {
					return ErrCommandInvalid
				}
			}
		}
		return nil
	default:
		return ErrCommandUnsupported
	}
}

func scalarEqual(kind string, a, b any) bool {
	if kind == "NUMBER" {
		an, aok := a.(json.Number)
		bn, bok := b.(json.Number)
		if !aok || !bok {
			return false
		}
		ar, aok := exactJSONNumber(an)
		br, bok := exactJSONNumber(bn)
		return aok && bok && ar.Cmp(br) == 0
	}
	return kind == "BOOL" && a == b || kind == "STRING" && a == b
}

func decodeScalar(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || !json.Valid(raw) || rejectDuplicateKeys(raw) != nil {
		return nil, ErrCommandUnsupported
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, ErrCommandUnsupported
	}
	switch value.(type) {
	case json.Number, bool, string:
		return value, nil
	default:
		return nil, ErrCommandUnsupported
	}
}

func exactJSONNumber(n json.Number) (*big.Rat, bool) {
	s := string(n)
	if s == "" || len(s) > 128 {
		return nil, false
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exponentText := s[i+1:]
		if len(exponentText) > 4 {
			return nil, false
		}
		exponent, err := strconv.Atoi(exponentText)
		if err != nil || exponent < -128 || exponent > 128 {
			return nil, false
		}
	}
	// big.Rat.SetString preserves bounded decimal and exponent input without float64 loss.
	r, ok := new(big.Rat).SetString(s)
	return r, ok
}

func rationalFromFloat(value float64) (*big.Rat, bool) {
	if !finite(value) {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(strconv.FormatFloat(value, 'g', -1, 64))
	return r, ok
}

func decimalPlaces(r *big.Rat) int {
	d := new(big.Int).Set(r.Denom())
	if d.Sign() <= 0 {
		return 0
	}
	two, five := big.NewInt(2), big.NewInt(5)
	countTwo, countFive := 0, 0
	rem := new(big.Int)
	for {
		q := new(big.Int)
		q.QuoRem(d, two, rem)
		if rem.Sign() != 0 {
			break
		}
		d = q
		countTwo++
	}
	for {
		q := new(big.Int)
		q.QuoRem(d, five, rem)
		if rem.Sign() != 0 {
			break
		}
		d = q
		countFive++
	}
	if d.Cmp(big.NewInt(1)) != 0 {
		return maxCommandParamsBytes
	}
	if countTwo > countFive {
		return countTwo
	}
	return countFive
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func decodeCommandModel(data []byte, dst any) error {
	if !json.Valid(data) || rejectDuplicateKeys(data) != nil {
		return ErrCommandUnsupported
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return ErrCommandUnsupported
	}
	return nil
}
