package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A rule that is shared by a device template has to be one body with a few
// holes in it, not one copy per device. This file is where the holes live.
//
// The hole is written as {{name}} inside a JSON string. Substitution happens on
// the serialized body *before* it is decoded, which means a parameter can drive
// anything a definition can express — a threshold, a duration, an identifier —
// without every field needing to learn about parameters. The cost is that the
// substituted body is decoded again, so a parameter that produces something the
// field cannot hold is reported as a decode error naming the parameter.

// ParamType is the declared type of a parameter slot.
//
// The type is not decoration: it is what lets the form, the CLI and the MCP
// tools reject "40 秒" for a threshold before anything is saved. A slot whose
// type is unknown would accept any string and fail later, at run time, in the
// one place nobody is watching.
type ParamType string

const (
	ParamString   ParamType = "string"
	ParamNumber   ParamType = "number"
	ParamDuration ParamType = "duration"
	ParamBool     ParamType = "bool"
)

// Param is one declared slot on a rule body.
type Param struct {
	Key         string    `json:"key"`
	Type        ParamType `json:"type"`
	Default     string    `json:"default"`
	Min         *float64  `json:"min,omitempty"`
	Max         *float64  `json:"max,omitempty"`
	Description string    `json:"description,omitempty"`
	// Unit is shown next to the field in the form ("°C", "分钟"). It has no
	// effect on evaluation; it exists so a value cannot be read as the wrong
	// quantity.
	Unit string `json:"unit,omitempty"`
}

// ParamError is a typed parameter failure. It is separate from Issue because
// the same rules are applied to a binding write, where there is no definition
// field to point at.
type ParamError struct {
	Code    string
	Param   string
	Message string
}

func (e *ParamError) Error() string { return e.Param + ": " + e.Message }

// Defaults returns the declared defaults, in a stable order.
func (d *Definition) Defaults() map[string]string {
	out := make(map[string]string, len(d.Params))
	for _, p := range d.Params {
		out[p.Key] = p.Default
	}
	return out
}

// Values merges declared defaults with caller overrides and checks the result.
//
// An override for an undeclared slot is refused rather than ignored: a typo in
// a binding would otherwise look applied and silently do nothing, which is the
// worst possible outcome for "I set this device to 60 °C".
func (d *Definition) Values(overrides map[string]string) (map[string]string, error) {
	values := d.Defaults()
	declared := make(map[string]Param, len(d.Params))
	for _, p := range d.Params {
		declared[p.Key] = p
	}
	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p, ok := declared[k]
		if !ok {
			return nil, &ParamError{Code: "unknown_param", Param: k,
				Message: "该规则没有声明参数 " + k}
		}
		v := overrides[k]
		if err := checkParamValue(p, v); err != nil {
			return nil, err
		}
		values[k] = v
	}
	// Defaults are checked too: a slot whose default is out of its own range is
	// a broken template, and it would only be noticed on the device that never
	// overrode it.
	for _, p := range d.Params {
		if err := checkParamValue(p, values[p.Key]); err != nil {
			return nil, err
		}
	}
	return values, nil
}

// Materialize returns the body with every parameter filled in.
//
// The returned definition is a normal definition: the evaluator, the compiler
// and the run snapshot all see literal values. Nothing downstream needs to know
// that a parameter existed, which is why a bound rule behaves exactly like a
// hand-written one.
func Materialize(def Definition, overrides map[string]string) (Definition, error) {
	if len(def.Params) == 0 {
		if len(overrides) > 0 {
			return Definition{}, &ParamError{Code: "unknown_param", Param: firstKey(overrides),
				Message: "该规则没有声明任何参数"}
		}
		return def, nil
	}
	values, err := def.Values(overrides)
	if err != nil {
		return Definition{}, err
	}
	raw, err := json.Marshal(def)
	if err != nil {
		return Definition{}, err
	}
	filled, err := Substitute(raw, values)
	if err != nil {
		return Definition{}, err
	}
	var out Definition
	if err := json.Unmarshal(filled, &out); err != nil {
		return Definition{}, &ParamError{Code: "param_type_mismatch", Param: "",
			Message: "参数替换后的规则无法解析: " + err.Error()}
	}
	return out, nil
}

// MaterializeHash reports whether a rule needs per-device materialization.
func (d *Definition) HasParams() bool { return len(d.Params) > 0 }

// Substitute replaces every {{name}} inside JSON string literals.
//
// Only string literals are rewritten, so a parameter can never change the shape
// of the document (no injected fields, no broken quoting): the value is escaped
// as JSON and inserted where a string was already expected.
func Substitute(raw []byte, values map[string]string) ([]byte, error) {
	var out bytes.Buffer
	out.Grow(len(raw) + 64)
	inString := false
	escaped := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if !inString {
			out.WriteByte(c)
			if c == '"' {
				inString = true
			}
			continue
		}
		if escaped {
			out.WriteByte(c)
			escaped = false
			continue
		}
		switch c {
		case '\\':
			out.WriteByte(c)
			escaped = true
			continue
		case '"':
			out.WriteByte(c)
			inString = false
			continue
		case '{':
			if i+1 < len(raw) && raw[i+1] == '{' {
				rel := bytes.Index(raw[i:], []byte("}}"))
				if rel < 0 {
					return nil, &ParamError{Code: "unterminated_param", Param: "",
						Message: "参数占位符缺少结束的 }}"}
				}
				name := string(raw[i+2 : i+rel])
				if name == "" {
					return nil, &ParamError{Code: "empty_param_name", Param: "",
						Message: "参数占位符缺少名称"}
				}
				v, ok := values[name]
				if !ok {
					return nil, &ParamError{Code: "missing_param", Param: name,
						Message: "参数 " + name + " 没有取值"}
				}
				out.WriteString(escapeInnerJSON(v))
				i += rel + 1 // skip "}}" as well
				continue
			}
		}
		out.WriteByte(c)
	}
	return out.Bytes(), nil
}

// Placeholders lists the parameter names referenced by a body, sorted.
//
// Validation uses it in both directions: a referenced-but-undeclared name is a
// typo that would fail at run time, and a declared-but-unreferenced name is a
// setting the form offers that changes nothing.
func Placeholders(raw []byte) []string {
	seen := map[string]bool{}
	inString := false
	escaped := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if !inString {
			if c == '"' {
				inString = true
			}
			continue
		}
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			inString = false
			continue
		}
		if c == '{' && i+1 < len(raw) && raw[i+1] == '{' {
			rel := bytes.Index(raw[i:], []byte("}}"))
			if rel < 0 {
				return nil
			}
			seen[string(raw[i+2:i+rel])] = true
			i += rel + 1
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// escapeInnerJSON escapes a value for placement inside a JSON string. It is the
// inner part of json.Marshal with the surrounding quotes removed.
func escapeInnerJSON(v string) string {
	b, err := json.Marshal(v)
	if err != nil || len(b) < 2 {
		return v
	}
	return string(b[1 : len(b)-1])
}

func firstKey(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

// checkParamValue enforces the declared type and range.
func checkParamValue(p Param, v string) error {
	switch p.Type {
	case ParamNumber:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return &ParamError{Code: "param_not_a_number", Param: p.Key,
				Message: "参数 " + p.Key + " 需要数字，收到 " + strconv.Quote(v)}
		}
		if p.Min != nil && f < *p.Min {
			return &ParamError{Code: "param_below_min", Param: p.Key,
				Message: fmt.Sprintf("参数 %s 不能小于 %v（收到 %v）", p.Key, *p.Min, f)}
		}
		if p.Max != nil && f > *p.Max {
			return &ParamError{Code: "param_above_max", Param: p.Key,
				Message: fmt.Sprintf("参数 %s 不能大于 %v（收到 %v）", p.Key, *p.Max, f)}
		}
	case ParamDuration:
		d, err := time.ParseDuration(strings.TrimSpace(v))
		if err != nil {
			return &ParamError{Code: "param_not_a_duration", Param: p.Key,
				Message: "参数 " + p.Key + " 需要时长（如 5m、30s），收到 " + strconv.Quote(v)}
		}
		if d < 0 {
			return &ParamError{Code: "param_negative_duration", Param: p.Key,
				Message: "参数 " + p.Key + " 不能为负"}
		}
	case ParamBool:
		if _, err := strconv.ParseBool(strings.TrimSpace(v)); err != nil {
			return &ParamError{Code: "param_not_a_bool", Param: p.Key,
				Message: "参数 " + p.Key + " 需要 true/false，收到 " + strconv.Quote(v)}
		}
	case ParamString:
		if len(v) > 256 {
			return &ParamError{Code: "param_too_long", Param: p.Key,
				Message: "参数 " + p.Key + " 超过 256 字符"}
		}
	default:
		return &ParamError{Code: "param_type_unknown", Param: p.Key,
			Message: "参数 " + p.Key + " 的类型 " + string(p.Type) + " 不受支持"}
	}
	return nil
}

// validateParams is the publish-time gate for the declared slots.
func validateParams(d *Definition, add func(field, code, msg string)) {
	raw, err := json.Marshal(d)
	if err != nil {
		add("params", "invalid_params", "规则无法序列化: "+err.Error())
		return
	}
	used := map[string]bool{}
	for _, name := range Placeholders(raw) {
		used[name] = true
	}
	seen := map[string]bool{}
	for i, p := range d.Params {
		field := fmt.Sprintf("params[%d]", i)
		if !validParamKey(p.Key) {
			add(field+".key", "invalid_param_key",
				"参数名只能包含字母、数字和下划线，且以字母开头: "+p.Key)
		}
		if seen[p.Key] {
			add(field+".key", "duplicate_param", "参数重复声明: "+p.Key)
		}
		seen[p.Key] = true
		switch p.Type {
		case ParamString, ParamNumber, ParamDuration, ParamBool:
		default:
			add(field+".type", "param_type_unknown",
				fmt.Sprintf("参数 %s 的类型 %q 不受支持", p.Key, p.Type))
		}
		if err := checkParamValue(p, p.Default); err != nil {
			add(field+".default", "invalid_param_default", err.Error())
		}
		// A declared slot nobody uses is a form field that changes nothing.
		// Reporting it is friendlier than accepting a setting that silently
		// does not apply.
		if !used[p.Key] {
			add(field, "unused_param", "参数 "+p.Key+" 已声明但正文中没有任何 {{"+p.Key+"}} 引用")
		}
		delete(used, p.Key)
	}
	for name := range used {
		add("params", "undeclared_param", "正文引用了未声明的参数 {{"+name+"}}")
	}
	if len(d.Params) > 32 {
		add("params", "too_many_params", "参数数量不能超过 32 个")
	}
}

func validParamKey(k string) bool {
	if k == "" || len(k) > 64 {
		return false
	}
	for i, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r == '_':
			if i == 0 {
				return false
			}
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
