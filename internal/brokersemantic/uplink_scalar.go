package brokersemantic

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// This mirrors thingmodel/core/validation.go validateScalarJSON and
// validateNumberConstraint, not the command-only rational/precision restrictions.
// The caller has already bounded and parsed the complete JSON message.
func validateUplinkScalar(kind string, value any, c *commandConstraint) error {
	switch kind {
	case "INT", "FLOAT", "NUMBER":
		n, ok := value.(json.Number)
		if !ok {
			return ErrUplinkInvalid
		}
		if kind == "INT" && strings.ContainsAny(string(n), ".eE") {
			return ErrUplinkInvalid
		}
		if kind != "INT" || c != nil {
			v, err := n.Float64()
			if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
				return ErrUplinkInvalid
			}
			if c != nil && (c.Min != nil && v < *c.Min || c.Max != nil && v > *c.Max) {
				return ErrUplinkInvalid
			}
		}
	case "BOOL":
		if _, ok := value.(bool); !ok {
			return ErrUplinkInvalid
		}
	case "STRING":
		v, ok := value.(string)
		if !ok {
			return ErrUplinkInvalid
		}
		if c != nil {
			if c.MaxLength != nil && len(v) > *c.MaxLength {
				return ErrUplinkInvalid
			}
			if c.Pattern != "" {
				matched, err := regexp.MatchString(c.Pattern, v)
				if err != nil || !matched {
					return ErrUplinkInvalid
				}
			}
		}
	default:
		return ErrUplinkContract
	}
	return nil
}

// canonicalUplinkNumber gives exact decimal equality without constructing a
// power of ten or big.Rat. Bounds match Go big.Rat.SetString's decimal exponent
// acceptance used by core enum canonicalScalarKey; allocations stay O(payload).
func canonicalUplinkNumber(n json.Number) (string, bool) {
	s := string(n)
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	exp := int64(0)
	if index := strings.IndexAny(s, "eE"); index >= 0 {
		v, err := strconv.ParseInt(s[index+1:], 10, 64)
		if err != nil {
			return "", false
		}
		exp = v
		s = s[:index]
	}
	fraction := 0
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		fraction = len(s) - dot - 1
		s = s[:dot] + s[dot+1:]
	}
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return "0", true
	}
	if exp < math.MinInt64+int64(fraction) {
		return "", false
	}
	exp -= int64(fraction)
	if exp < -1000000 || exp > 1000000 {
		return "", false
	}
	normalized := strings.TrimRight(s, "0")
	exp += int64(len(s) - len(normalized))
	if negative {
		normalized = "-" + normalized
	}
	return normalized + "e" + strconv.FormatInt(exp, 10), true
}

func uplinkScalarKey(kind string, value any) (string, bool) {
	switch kind {
	case "NUMBER":
		n, ok := value.(json.Number)
		if !ok {
			return "", false
		}
		key, ok := canonicalUplinkNumber(n)
		return "n:" + key, ok
	case "BOOL":
		b, ok := value.(bool)
		return "b:" + strconv.FormatBool(b), ok
	case "STRING":
		s, ok := value.(string)
		return "s:" + s, ok
	}
	return "", false
}
