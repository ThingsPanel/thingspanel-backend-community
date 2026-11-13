package brokersemantic

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

var (
	ErrUplinkInvalid  = errors.New("native uplink does not match its published model")
	ErrUplinkContract = errors.New("native uplink contract is unsupported")
)

// UplinkContract is compiled only from an authenticated immutable publication.
// Its private maps are never returned to callers and are safe for shared reads.
type UplinkContract struct {
	fields    map[string]commandField
	values    map[*commandValueType]*compiledUplinkValue
	cacheCost int
}

type compiledUplinkValue struct {
	kind       string
	constraint *commandConstraint
	pattern    *regexp.Regexp
	enum       map[string]bool
}

func (c *UplinkContract) compileValue(v *commandValueType) error {
	compiled := &compiledUplinkValue{kind: v.Kind, constraint: v.Constraint}
	if v.Kind == "ENUM" {
		compiled.kind = v.BaseKind
		compiled.constraint = nil
		compiled.enum = make(map[string]bool, len(v.EnumValues))
		for _, option := range v.EnumValues {
			value, e := decodeScalar(option.Code)
			key, ok := uplinkScalarKey(v.BaseKind, value)
			if e != nil || !ok {
				return ErrUplinkContract
			}
			compiled.enum[key] = true
			c.cacheCost += len(key)
		}
	} else if v.Constraint != nil && v.Constraint.Pattern != "" {
		pattern, e := regexp.Compile(v.Constraint.Pattern)
		if e != nil {
			return ErrUplinkContract
		}
		compiled.pattern = pattern
		copy := *v.Constraint
		copy.Pattern = ""
		compiled.constraint = &copy
	}
	c.values[v] = compiled
	return nil
}

func (c *UplinkContract) validateValue(v *commandValueType, value any) error {
	compiled := c.values[v]
	if compiled == nil {
		return ErrUplinkContract
	}
	if e := validateUplinkScalar(compiled.kind, value, compiled.constraint); e != nil {
		return e
	}
	if compiled.enum != nil {
		key, ok := uplinkScalarKey(compiled.kind, value)
		if !ok || !compiled.enum[key] {
			return ErrUplinkInvalid
		}
	}
	if compiled.pattern != nil {
		stringValue, ok := value.(string)
		if !ok || !compiled.pattern.MatchString(stringValue) {
			return ErrUplinkInvalid
		}
	}
	return nil
}

// CacheCost is a conservative accounting weight for bounded retention, not an
// exact Go heap measurement. It includes parsed-field/parameter/enum overhead.
func (c *UplinkContract) CacheCost() int {
	if c == nil {
		return 0
	}
	return c.cacheCost
}

func CompileUplinkContract(raw json.RawMessage) (*UplinkContract, error) {
	if len(raw) == 0 || len(raw) > 1<<20 || !validJSONObject(raw) {
		return nil, ErrUplinkContract
	}
	var model commandModel
	if decodeCommandModel(raw, &model) != nil || model.Fields == nil {
		return nil, ErrUplinkContract
	}
	c := &UplinkContract{fields: make(map[string]commandField, len(model.Fields)), values: make(map[*commandValueType]*compiledUplinkValue), cacheCost: len(raw)*4 + len(model.Fields)*512}
	for _, f := range model.Fields {
		if f.Identifier == "" {
			return nil, ErrUplinkContract
		}
		key := f.Kind + "\x00" + f.Identifier
		if _, exists := c.fields[key]; exists {
			return nil, ErrUplinkContract
		}
		switch f.Kind {
		case "TELEMETRY", "ATTRIBUTE":
			if f.ValueType == nil || f.Parameters != nil || normalizeUplinkValueType(f.ValueType) != nil {
				return nil, ErrUplinkContract
			}
		case "EVENT":
			if f.ValueType != nil || f.Parameters == nil || f.Parameters.SchemaVersion != 1 || f.Parameters.Items == nil {
				return nil, ErrUplinkContract
			}
			seen := map[string]bool{}
			for i := range f.Parameters.Items {
				p := &f.Parameters.Items[i]
				if p.Identifier == "" || seen[p.Identifier] || normalizeUplinkValueType(&p.ValueType) != nil {
					return nil, ErrUplinkContract
				}
				seen[p.Identifier] = true
				if c.compileValue(&p.ValueType) != nil {
					return nil, ErrUplinkContract
				}
				c.cacheCost += 512 + len(p.ValueType.EnumValues)*128
			}
		case "COMMAND": // wrong-family input remains rejected below
		default:
			return nil, ErrUplinkContract
		}
		c.fields[key] = f
		if f.ValueType != nil {
			if c.compileValue(f.ValueType) != nil {
				return nil, ErrUplinkContract
			}
			c.cacheCost += len(f.ValueType.EnumValues) * 128
		}
	}
	return c, nil
}

func normalizeUplinkValueType(v *commandValueType) error {
	// v2 serializes BOOL enum base as BOOLEAN. This explicit schema alias never
	// coerces a payload string/number into a boolean.
	if v.BaseKind == "BOOLEAN" {
		v.BaseKind = "BOOL"
	}
	base := v.Kind
	if base == "ENUM" {
		base = v.BaseKind
	}
	if base != "NUMBER" && base != "FLOAT" && base != "INT" && base != "BOOL" && base != "STRING" {
		return ErrUplinkContract
	}
	// Mirror core.ValidateScalar rather than the stricter command contract.
	// Step/precision/BOOL labels are published metadata for uplink. Derive a
	// private runtime constraint without changing the owner's signed DTO bytes.
	if v.Constraint != nil {
		c := *v.Constraint
		c.Step, c.Precision, c.TrueText, c.FalseText = nil, nil, "", ""
		if c.Min != nil && (!finite(*c.Min) || (c.Max != nil && *c.Min > *c.Max)) || c.Max != nil && !finite(*c.Max) || c.MaxLength != nil && *c.MaxLength < 0 {
			return ErrUplinkContract
		}
		if c.Pattern != "" {
			if _, err := regexp.Compile(c.Pattern); err != nil {
				return ErrUplinkContract
			}
		}
		v.Constraint = &c
	}
	if v.Kind == "ENUM" {
		if (v.BaseKind != "NUMBER" && v.BaseKind != "BOOL" && v.BaseKind != "STRING") || len(v.EnumValues) == 0 {
			return ErrUplinkContract
		}
		seen := make(map[string]bool, len(v.EnumValues))
		for _, option := range v.EnumValues {
			value, err := decodeScalar(option.Code)
			if err != nil || validateUplinkScalar(base, value, nil) != nil {
				return ErrUplinkContract
			}
			key, valid := uplinkScalarKey(base, value)
			if !valid {
				return ErrUplinkContract
			}
			if seen[key] {
				return ErrUplinkContract
			}
			seen[key] = true
		}
	} else if v.BaseKind != "" || len(v.EnumValues) > 0 {
		return ErrUplinkContract
	}
	return nil
}

func uplinkObject(raw []byte) (map[string]any, error) {
	if ValidateUplinkJSON(raw) != nil {
		return nil, ErrUplinkInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if d.Decode(&value) != nil || d.Decode(new(any)) != io.EOF {
		return nil, ErrUplinkInvalid
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, ErrUplinkInvalid
	}
	return object, nil
}

// ValidateUplinkJSON checks the original gateway envelope before RawMessage
// expansion can discard duplicate routing members. It never coerces numbers.
func ValidateUplinkJSON(raw []byte) error {
	if len(raw) == 0 || len(raw) > 1<<20 || !json.Valid(raw) || rejectDuplicateKeys(raw) != nil {
		return ErrUplinkInvalid
	}
	return nil
}

// Validate validates one whole logical message, never just the legal subset.
// Telemetry/attribute reports may be partial; event parameters follow the same
// complete flat v1 parameter contract as the published model editor.
func (c *UplinkContract) Validate(family string, raw []byte) error {
	if c == nil {
		return ErrUplinkContract
	}
	values, err := uplinkObject(raw)
	if err != nil {
		return err
	}
	switch family {
	case "telemetry", "attribute":
		// Empty partial reports retain their previous heartbeat/no-value meaning.
		for identifier, value := range values {
			field, found := c.fields[strings.ToUpper(family)+"\x00"+identifier]
			if !found || field.Kind != strings.ToUpper(family) || field.ValueType == nil {
				return ErrUplinkInvalid
			}
			if err := c.validateValue(field.ValueType, value); err != nil {
				return ErrUplinkInvalid
			}
		}
	case "event":
		// EventInfo historically decodes method/params and ignores additional
		// envelope metadata. Validate only these semantic fields; extra envelope
		// members must not become trusted timestamps, IDs or device identity.
		identifier, ok := values["method"].(string)
		if !ok {
			return ErrUplinkInvalid
		}
		field, found := c.fields["EVENT\x00"+identifier]
		if !found || field.Kind != "EVENT" || field.Parameters == nil {
			return ErrUplinkInvalid
		}
		params, ok := values["params"].(map[string]any)
		if !ok || len(params) != len(field.Parameters.Items) {
			return ErrUplinkInvalid
		}
		for i := range field.Parameters.Items {
			p := &field.Parameters.Items[i]
			value, exists := params[p.Identifier]
			if !exists || c.validateValue(&p.ValueType, value) != nil {
				return ErrUplinkInvalid
			}
		}
	default:
		return ErrUplinkInvalid
	}
	return nil
}
