package brokersemantic

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestNativeUplinkPublishedCoreParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/uplink-core-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Model json.RawMessage `json:"model"`
		Cases []struct {
			Family  string          `json:"family"`
			Payload json.RawMessage `json:"payload"`
			Valid   bool            `json:"valid"`
		} `json:"cases"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	c, err := CompileUplinkContract(fixture.Model)
	if err != nil {
		t.Fatal(err)
	}
	for i, tc := range fixture.Cases {
		if err = c.Validate(tc.Family, tc.Payload); (err == nil) != tc.Valid {
			t.Fatalf("core parity case%d valid=%v err=%v", i, tc.Valid, err)
		}
	}
}

const uplinkModelFixture = `{"name":"published","fields":[
 {"kind":"TELEMETRY","identifier":"temperature","nameI18n":{"default":"T"},"access":{"read":true},"valueType":{"kind":"NUMBER","constraint":{"min":0,"max":50,"step":0.5,"precision":1}}},
 {"kind":"TELEMETRY","identifier":"switch","nameI18n":{"default":"S"},"access":{"read":true},"valueType":{"kind":"BOOL"}},
 {"kind":"ATTRIBUTE","identifier":"mode","nameI18n":{"default":"M"},"access":{"read":true},"valueType":{"kind":"ENUM","baseKind":"NUMBER","enumValues":[{"code":0,"label":{"default":"Off"}},{"code":1,"label":{"default":"On"}}]}},
 {"kind":"ATTRIBUTE","identifier":"flag","nameI18n":{"default":"Flag"},"access":{"read":true},"valueType":{"kind":"ENUM","baseKind":"BOOLEAN","enumValues":[{"code":false,"label":{"default":"F"}},{"code":true,"label":{"default":"T"}}]}},
 {"kind":"ATTRIBUTE","identifier":"label","nameI18n":{"default":"Label"},"access":{"read":true},"valueType":{"kind":"STRING","constraint":{"maxLength":12}}},
 {"kind":"EVENT","identifier":"overheat","nameI18n":{"default":"E"},"access":{"observe":true},"parameters":{"schemaVersion":1,"items":[{"identifier":"temperature","nameI18n":{"default":"T"},"valueType":{"kind":"NUMBER"}},{"identifier":"active","nameI18n":{"default":"A"},"valueType":{"kind":"BOOL"}}]}},
 {"kind":"COMMAND","identifier":"set","nameI18n":{"default":"Set"},"access":{"write":true},"parameters":{"schemaVersion":1,"items":[]}}
]}`

func TestNativeUplinkWholeMessageTypesAndParameters(t *testing.T) {
	c, err := CompileUplinkContract(json.RawMessage(uplinkModelFixture))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, family, payload string
		valid                 bool
	}{
		{"zero_false", "telemetry", `{"temperature":0,"switch":false}`, true},
		{"partial", "telemetry", `{"temperature":23.5}`, true},
		{"wrong_number", "telemetry", `{"temperature":"23.5"}`, false},
		{"mixed", "telemetry", `{"temperature":24.5,"switch":"false"}`, false},
		{"numeric_boolean", "telemetry", `{"switch":0}`, false},
		{"null", "telemetry", `{"switch":null}`, false},
		{"max", "telemetry", `{"temperature":51}`, false},
		{"step_metadata", "telemetry", `{"temperature":23.1}`, true},
		{"duplicate", "telemetry", `{"switch":false,"switch":true}`, false},
		{"unknown", "telemetry", `{"unknown":23.5}`, false},
		{"wrong_family", "telemetry", `{"mode":1}`, false},
		{"malformed", "telemetry", `{"switch":`, false},
		{"trailing", "telemetry", `{"switch":false}{}`, false},
		{"empty_partial", "telemetry", `{}`, true},
		{"empty_attribute_heartbeat", "attribute", `{}`, true},
		{"enum_types", "attribute", `{"mode":0,"flag":false,"label":""}`, true},
		{"enum_alias", "attribute", `{"flag":true}`, true},
		{"enum_unknown", "attribute", `{"mode":2}`, false},
		{"enum_string", "attribute", `{"mode":"1"}`, false},
		{"enum_bool_string", "attribute", `{"flag":"false"}`, false},
		{"event", "event", `{"method":"overheat","params":{"temperature":38.5,"active":true}}`, true},
		{"event_envelope_metadata", "event", `{"method":"overheat","params":{"temperature":38.5,"active":true},"ts":123,"id":"external","device_id":"untrusted"}`, true},
		{"event_type", "event", `{"method":"overheat","params":{"temperature":"38.5","active":true}}`, false},
		{"event_missing", "event", `{"method":"overheat","params":{"temperature":38.5}}`, false},
		{"event_extra", "event", `{"method":"overheat","params":{"temperature":38.5,"active":true,"x":1}}`, false},
		{"event_unknown", "event", `{"method":"other","params":{}}`, false},
		{"event_nested_duplicate", "event", `{"method":"overheat","params":{"temperature":1,"temperature":2,"active":true}}`, false},
		{"command_as_event", "event", `{"method":"set","params":{}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := c.Validate(tc.family, []byte(tc.payload))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if err != nil && !errors.Is(err, ErrUplinkInvalid) {
				t.Fatal(err)
			}
		})
	}
	// Validation does not mutate a cached contract or retain a caller's payload.
	for i := 0; i < 1000; i++ {
		if c.Validate("telemetry", []byte(`{"switch":false}`)) != nil {
			t.Fatal("shared contract mutated")
		}
	}
}

func TestNativeUplinkContractFailsClosed(t *testing.T) {
	for _, raw := range []string{`null`, `{"name":"x","fields":null}`, strings.Replace(uplinkModelFixture, `"baseKind":"NUMBER"`, `"baseKind":"OBJECT"`, 1), strings.Replace(uplinkModelFixture, `"identifier":"switch"`, `"identifier":"temperature"`, 1)} {
		if _, err := CompileUplinkContract(json.RawMessage(raw)); !errors.Is(err, ErrUplinkContract) {
			t.Fatalf("want contract rejection, got %v", err)
		}
	}
}

func TestNativeUplinkSameIdentifierAcrossFamilies(t *testing.T) {
	// Published v2 allows identifiers to be reused across field kinds. A
	// command named temperature must not disable telemetry named temperature.
	raw := strings.Replace(uplinkModelFixture, `"identifier":"set"`, `"identifier":"temperature"`, 1)
	raw = strings.Replace(raw, `"identifier":"label"`, `"identifier":"temperature"`, 1)
	c, err := CompileUplinkContract(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		family, payload string
		valid           bool
	}{
		{"telemetry", `{"temperature":21}`, true},
		{"attribute", `{"temperature":"label"}`, true},
		{"telemetry", `{"temperature":"label"}`, false},
		{"attribute", `{"temperature":21}`, false},
		{"event", `{"method":"temperature","params":{}}`, false},
	} {
		if err := c.Validate(tc.family, []byte(tc.payload)); (err == nil) != tc.valid {
			t.Fatalf("%s: valid=%v err=%v", tc.family, tc.valid, err)
		}
	}
}

func TestNativeUplinkWireIdentifiersAreNotTrimmed(t *testing.T) {
	raw := strings.ReplaceAll(uplinkModelFixture, `"identifier":"temperature"`, `"identifier":"  Vendor.Temp-Celsius/°C  "`)
	c, err := CompileUplinkContract(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Validate("telemetry", []byte(`{"  Vendor.Temp-Celsius/°C  ":20}`)); err != nil {
		t.Fatal(err)
	}
	if err = c.Validate("event", []byte(`{"method":"overheat","params":{"  Vendor.Temp-Celsius/°C  ":20,"active":false}}`)); err != nil {
		t.Fatal(err)
	}
	if err = c.Validate("telemetry", []byte(`{"Vendor.Temp-Celsius/°C":20}`)); !errors.Is(err, ErrUplinkInvalid) {
		t.Fatal("wire identifier was normalized")
	}
}
