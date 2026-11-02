package brokercontract

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseProtocolTelemetryStrictlyDecodesLegacyBytesOnce(t *testing.T) {
	wire, _ := json.Marshal(struct {
		DeviceID string `json:"device_id"`
		Values   []byte `json:"values"`
	}{"device-a", []byte(`{"temperature":265}`)})
	got, err := ParseProtocolTelemetry(wire)
	if err != nil || got.DeviceID != "device-a" || string(got.Values) != `{"temperature":265}` {
		t.Fatalf("legacy wrapper decode: %#v %v", got, err)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"device_id":"device-a","values":"%%%%"}`),
		[]byte(`{"device_id":"device-a","values":"e30=","device_id":"device-a"}`),
		[]byte(`{"device_id":"device-a","values":"e30=","extra":1}`),
		[]byte(`{"device_id":"device-a","values":""}`),
	} {
		if _, err := ParseProtocolTelemetry(invalid); err == nil {
			t.Errorf("invalid wrapper accepted: %s", invalid)
		}
	}
	if _, err := ParseProtocolTelemetry([]byte(`{"device_id":"device-a","values":"` + base64.StdEncoding.EncodeToString([]byte(`{"x":1}`)) + `"} {}`)); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}

func TestParseProtocolTelemetryBoundsTargetAndPayload(t *testing.T) {
	for _, test := range []struct {
		id     string
		values []byte
	}{
		{strings.Repeat("x", MaxIdentityBytes+1), []byte("{}")},
		{"device-a", make([]byte, MaxPayloadBytes+1)},
	} {
		wire, err := json.Marshal(struct {
			DeviceID string `json:"device_id"`
			Values   []byte `json:"values"`
		}{test.id, test.values})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = ParseProtocolTelemetry(wire); err == nil {
			t.Fatal("accepted oversized target or payload")
		}
	}
}
