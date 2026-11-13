package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"
)

var ErrCommandParameters = errors.New("command parameters do not match published model")

// ValidateCommandParameters validates the flat v1 command contract against the
// immutable snapshot owned by this service. Labels never coerce scalar values.
func ValidateCommandParameters(snapshot Snapshot, identifier string, raw json.RawMessage) error {
	if len(raw) == 0 || len(raw) > 64<<10 || !json.Valid(raw) {
		return ErrCommandParameters
	}
	var selected *Field
	for i := range snapshot.Fields {
		if snapshot.Fields[i].Identifier == identifier {
			if selected != nil {
				return ErrCommandParameters
			}
			selected = &snapshot.Fields[i]
		}
	}
	if selected == nil || selected.Kind != FieldCommand || !selected.Access.Write || selected.Parameters == nil || selected.Parameters.SchemaVersion != 1 || selected.Parameters.Items == nil || len(selected.Parameters.Items) > 32 {
		return ErrCommandParameters
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return ErrCommandParameters
	}
	params := map[string]json.RawMessage{}
	for d.More() {
		k, err := d.Token()
		if err != nil {
			return ErrCommandParameters
		}
		key, ok := k.(string)
		if !ok {
			return ErrCommandParameters
		}
		if _, found := params[key]; found {
			return ErrCommandParameters
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return ErrCommandParameters
		}
		params[key] = value
	}
	if len(params) != len(selected.Parameters.Items) {
		return ErrCommandParameters
	}
	seen := map[string]bool{}
	for _, p := range selected.Parameters.Items {
		value, ok := params[p.Identifier]
		if !ok || seen[p.Identifier] {
			return ErrCommandParameters
		}
		seen[p.Identifier] = true
		kind := p.ValueType.Kind
		if kind == ScalarEnum {
			kind = p.ValueType.EnumBaseKind
		}
		if kind == ScalarNumber || kind == ScalarFloat || kind == ScalarInt {
			if !boundedCommandNumber(value) {
				return ErrCommandParameters
			}
			for _, option := range p.ValueType.EnumValues {
				if !boundedCommandNumber(option.Value) {
					return ErrCommandParameters
				}
			}
		}
		if err := ValidateScalar(p.ValueType, value); err != nil {
			return ErrCommandParameters
		}
		// ENUM membership and all applicable constraints both apply.
		if validateScalarJSON(value, kind, p.ValueType.Constraint) != nil {
			return ErrCommandParameters
		}
		if kind == ScalarNumber || kind == ScalarFloat || kind == ScalarInt {
			if !validCommandNumber(value, p.ValueType.Constraint) {
				return ErrCommandParameters
			}
		}
	}
	return nil
}

// Bound exact rational parsing before both enum comparison and constraints.
// Keep the existing Community command contract's 128-character/exponent limit.
func boundedCommandNumber(raw json.RawMessage) bool {
	n, ok := decodeJSONNumber(raw)
	if !ok {
		return false
	}
	s := n.String()
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exponent := s[i+1:]
		if len(exponent) > 4 {
			return false
		}
		v, err := strconv.Atoi(exponent)
		if err != nil || v < -128 || v > 128 {
			return false
		}
	}
	return true
}

func validCommandNumber(raw json.RawMessage, c *ValueConstraint) bool {
	n, ok := decodeJSONNumber(raw)
	if !ok {
		return false
	}
	if _, err := n.Float64(); err != nil {
		return false
	}
	r, ok := new(big.Rat).SetString(n.String())
	if !ok {
		return false
	}
	if c == nil {
		return true
	}
	fromFloat := func(v float64) *big.Rat {
		r, _ := new(big.Rat).SetString(strconv.FormatFloat(v, 'g', -1, 64))
		return r
	}
	if c.Min != nil && r.Cmp(fromFloat(*c.Min)) < 0 || c.Max != nil && r.Cmp(fromFloat(*c.Max)) > 0 {
		return false
	}
	if c.Step != nil {
		base := new(big.Rat)
		if c.Min != nil {
			base = fromFloat(*c.Min)
		}
		q := new(big.Rat).Quo(new(big.Rat).Sub(r, base), fromFloat(*c.Step))
		if !q.IsInt() {
			return false
		}
	}
	if c.Precision != nil {
		if *c.Precision < 0 || *c.Precision > 64 {
			return false
		}
		scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(*c.Precision)), nil)
		if !new(big.Rat).Mul(r, new(big.Rat).SetInt(scale)).IsInt() {
			return false
		}
	}
	return true
}
