package service

import (
	"encoding/json"
	"testing"
	"time"

	"project/internal/brokersemantic"
)

func TestTelemetryEvidenceRequestBounds(t *testing.T) {
	now := time.Now().UTC()
	valid := TelemetryEvidenceRequest{Start: now.Add(-time.Hour), End: now, Keys: []string{"temperature"}, Limit: 100}
	if err := ValidateTelemetryEvidenceRequest(valid); err != nil {
		t.Fatalf("valid bounded query rejected: %v", err)
	}
	for name, req := range map[string]TelemetryEvidenceRequest{
		"missing start":   {End: now, Keys: []string{"temperature"}, Limit: 100},
		"reversed":        {Start: now, End: now.Add(-time.Second), Keys: []string{"temperature"}, Limit: 100},
		"over 24h":        {Start: now.Add(-24*time.Hour - time.Nanosecond), End: now, Keys: []string{"temperature"}, Limit: 100},
		"too many keys":   {Start: now.Add(-time.Hour), End: now, Keys: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}, Limit: 100},
		"duplicate key":   {Start: now.Add(-time.Hour), End: now, Keys: []string{"a", "a"}, Limit: 100},
		"zero limit":      {Start: now.Add(-time.Hour), End: now, Keys: []string{"a"}, Limit: 0},
		"oversized limit": {Start: now.Add(-time.Hour), End: now, Keys: []string{"a"}, Limit: 201},
		"whitespace key":  {Start: now.Add(-time.Hour), End: now, Keys: []string{" a"}, Limit: 100},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateTelemetryEvidenceRequest(req); err == nil {
				t.Fatal("invalid bounded query accepted")
			}
		})
	}
}

func TestTelemetryMetricDefinitionUsesPublishedEncoreShape(t *testing.T) {
	description := brokersemantic.Description{Model: json.RawMessage(`{"fields":[{"kind":"TELEMETRY","identifier":"temperature","unit":"°C","access":{"read":true,"write":false,"observe":true},"valueType":{"kind":"FLOAT"}},{"kind":"TELEMETRY","identifier":"unsupported_enum","access":{"read":true,"write":false,"observe":true},"valueType":{"kind":"ENUM"}},{"kind":"ATTRIBUTE","identifier":"label","access":{"read":true,"write":false,"observe":true},"valueType":{"kind":"STRING"}}]}`)}
	defs, err := telemetryMetricDefinitions(description, []string{"temperature"})
	if err != nil {
		t.Fatalf("published metric model rejected: %v", err)
	}
	if got := defs["temperature"]; got.Kind != "FLOAT" || got.Unit != "°C" {
		t.Fatalf("metric definition=%+v", got)
	}
	unitless := brokersemantic.Description{Model: json.RawMessage(`{"fields":[{"kind":"TELEMETRY","identifier":"online","access":{"read":true,"observe":true},"valueType":{"kind":"BOOL"}}]}`)}
	unitlessDefs, err := telemetryMetricDefinitions(unitless, []string{"online"})
	if err != nil || unitlessDefs["online"].UnitKnown || unitlessDefs["online"].Unit != "" {
		t.Fatalf("unitless metric was assigned a unit: defs=%+v err=%v", unitlessDefs, err)
	}
	if _, ok := defs["label"]; ok {
		t.Fatal("attribute was treated as telemetry")
	}
	if _, err := telemetryMetricDefinitions(description, []string{"unsupported_enum"}); err == nil {
		t.Fatal("unsupported enum was presented as a supported metric")
	}
}

func TestTelemetryMetricDefinitionFailsClosed(t *testing.T) {
	for name, raw := range map[string]string{
		"duplicate JSON key": `{"fields":[],"fields":[]}`,
		"missing fields":     `{"name":"model"}`,
		"untyped telemetry":  `{"fields":[{"kind":"TELEMETRY","identifier":"x","access":{"read":true,"observe":true}}]}`,
		"unreadable":         `{"fields":[{"kind":"TELEMETRY","identifier":"x","access":{"read":false,"observe":true},"valueType":{"kind":"FLOAT"}}]}`,
		"unsupported enum":   `{"fields":[{"kind":"TELEMETRY","identifier":"x","access":{"read":true,"observe":true},"valueType":{"kind":"ENUM","baseKind":"STRING"}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := telemetryMetricDefinitions(brokersemantic.Description{Model: json.RawMessage(raw)}, []string{"x"}); err == nil {
				t.Fatal("unsupported telemetry model accepted")
			}
		})
	}
}

func TestTelemetryValuesMatchPublishedScalarKind(t *testing.T) {
	for _, tc := range []struct {
		kind string
		raw  string
		want bool
	}{
		{"INT", `42`, true}, {"INT", `42.0`, false}, {"INT", `"42"`, false},
		{"FLOAT", `1.25`, true}, {"NUMBER", `-2e3`, true}, {"FLOAT", `"1.25"`, false},
		{"BOOL", `true`, true}, {"BOOL", `1`, false},
		{"STRING", `"ok"`, true}, {"STRING", `null`, false}, {"STRING", `[]`, false},
	} {
		t.Run(tc.kind+"/"+tc.raw, func(t *testing.T) {
			if got := telemetryValueMatches(tc.kind, json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("telemetryValueMatches(%s,%s)=%v, want %v", tc.kind, tc.raw, got, tc.want)
			}
		})
	}
}
