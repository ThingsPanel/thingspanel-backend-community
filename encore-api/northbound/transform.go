package northbound

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Filter decides which source events a pipeline accepts. An empty list means
// "no restriction on this dimension"; it never means "match nothing".
type Filter struct {
	DeviceIDs    []string `json:"deviceIds,omitempty"`
	EventTypes   []string `json:"eventTypes,omitempty"`
	Keys         []string `json:"keys,omitempty"`
	SubjectMatch string   `json:"subjectMatch,omitempty"`
}

// Transform is deliberately a fixed vocabulary, not a scripting language:
// select, rename, add constants, convert units, mask. Anything more complex
// belongs in the existing rule engine.
type Transform struct {
	Include     []string                   `json:"include,omitempty"`
	Exclude     []string                   `json:"exclude,omitempty"`
	Rename      map[string]string          `json:"rename,omitempty"`
	Constants   map[string]json.RawMessage `json:"constants,omitempty"`
	UnitConvert map[string]UnitConversion  `json:"unitConvert,omitempty"`
	Mask        []string                   `json:"mask,omitempty"`
	// WrapAs, when set, nests the produced values under this key instead of
	// sending them flat.
	WrapAs string `json:"wrapAs,omitempty"`
}

type UnitConversion struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Serialization struct {
	// Format is a hint for the plugin: json | line | text.
	Format string `json:"format,omitempty"`
	// Topic is used by the MQTT connector. {deviceId} and {eventType} are
	// substituted; the value is not interpreted by the platform.
	Topic string `json:"topic,omitempty"`
}

// matchEvent applies the filter. It returns a human-readable reason either way,
// because the preview must explain why something did or did not match.
func matchEvent(f Filter, env SourceEnvelope) (bool, string) {
	if len(f.DeviceIDs) > 0 && !contains(f.DeviceIDs, env.DeviceID) {
		return false, "device not selected"
	}
	if len(f.EventTypes) > 0 && !contains(f.EventTypes, env.EventType) {
		return false, "event type not selected"
	}
	if f.SubjectMatch != "" && !strings.HasPrefix(env.Subject, f.SubjectMatch) {
		return false, "subject does not match"
	}
	if len(f.Keys) > 0 {
		keys := topLevelKeys(env.Data)
		if !intersects(f.Keys, keys) {
			return false, "none of the selected fields are present"
		}
	}
	return true, "matched"
}

// applyTransform produces the outgoing document. Unknown operations are ignored
// rather than failing a delivery: a stale transform must not block data.
func applyTransform(t Transform, data json.RawMessage, warnings *[]string) (json.RawMessage, error) {
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		// Non-object payloads (arrays, scalars) pass through untouched.
		*warnings = append(*warnings, "payload is not a JSON object; sent as-is")
		return data, nil
	}
	out := make(map[string]any, len(values))
	for k, v := range values {
		out[k] = v
	}
	if len(t.Include) > 0 {
		kept := make(map[string]any, len(t.Include))
		for _, k := range t.Include {
			if v, ok := out[k]; ok {
				kept[k] = v
			}
		}
		out = kept
	}
	for _, k := range t.Exclude {
		delete(out, k)
	}
	for from, to := range t.Rename {
		if v, ok := out[from]; ok {
			delete(out, from)
			out[to] = v
		}
	}
	for _, k := range t.Mask {
		if _, ok := out[k]; ok {
			out[k] = "***"
		}
	}
	for k, conv := range t.UnitConvert {
		v, ok := out[k]
		if !ok {
			continue
		}
		num, ok := toFloat(v)
		if !ok {
			*warnings = append(*warnings, fmt.Sprintf("field %q is not numeric; unit conversion skipped", k))
			continue
		}
		converted, err := convertUnit(num, conv.From, conv.To)
		if err != nil {
			*warnings = append(*warnings, fmt.Sprintf("field %q: %v", k, err))
			continue
		}
		out[k] = converted
	}
	for k, raw := range t.Constants {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		out[k] = v
	}
	if t.WrapAs != "" {
		return json.Marshal(map[string]any{t.WrapAs: out})
	}
	return json.Marshal(out)
}

func convertUnit(v float64, from, to string) (float64, error) {
	if from == to {
		return v, nil
	}
	key := strings.ToLower(from + "->" + to)
	switch key {
	case "cel->k", "c->k", "celsius->kelvin":
		return v + 273.15, nil
	case "k->cel", "k->c", "kelvin->celsius":
		return v - 273.15, nil
	case "cel->f", "c->f":
		return v*9/5 + 32, nil
	case "f->cel", "f->c":
		return (v - 32) * 5 / 9, nil
	case "kpa->pa":
		return v * 1000, nil
	case "pa->kpa":
		return v / 1000, nil
	case "bar->kpa":
		return v * 100, nil
	case "kpa->bar":
		return v / 100, nil
	case "kwh->wh":
		return v * 1000, nil
	case "wh->kwh":
		return v / 1000, nil
	case "v->mv":
		return v * 1000, nil
	case "mv->v":
		return v / 1000, nil
	case "a->ma":
		return v * 1000, nil
	case "ma->a":
		return v / 1000, nil
	case "%->ratio":
		return v / 100, nil
	case "ratio->%":
		return v * 100, nil
	}
	return 0, fmt.Errorf("unsupported unit conversion %s -> %s", from, to)
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func topLevelKeys(raw json.RawMessage) []string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func intersects(a, b []string) bool {
	for _, v := range a {
		if contains(b, v) {
			return true
		}
	}
	return false
}

func roundTo(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}
