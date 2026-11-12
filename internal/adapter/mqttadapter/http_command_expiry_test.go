package mqttadapter

import (
	"encoding/json"
	"project/internal/downlink"
	"testing"
	"time"
)

func TestHTTPCommandWireUsesAcceptedDeadlineOnly(t *testing.T) {
	msg := &downlink.Message{Type: downlink.MessageTypeCommand, TopicPrefix: "plugin/HTTP/"}
	deadline := time.UnixMilli(1234567890123)
	raw := []byte(`{"method":"set","params":{"enabled":true,"expires_at":777},"expires_at":999}`)
	wire, err := httpCommandDeadlinePayload(msg, raw, deadline)
	if err != nil {
		t.Fatal(err)
	}
	var object struct {
		Method    string
		Params    map[string]interface{}
		ExpiresAt int64 `json:"expires_at"`
	}
	if err = json.Unmarshal(wire, &object); err != nil {
		t.Fatal(err)
	}
	if object.ExpiresAt != deadline.UnixMilli() || object.Method != "set" || object.Params["expires_at"] != float64(777) || object.Params["enabled"] != true {
		t.Fatal(string(wire))
	}
	for _, bad := range []string{"null", "[]", "plain"} {
		if _, err := httpCommandDeadlinePayload(msg, []byte(bad), deadline); err == nil {
			t.Fatalf("accepted non-object %s", bad)
		}
	}
	for _, prefix := range []string{"", "plugin/ModbusTCP/", "plugin/HomeAssistant/"} {
		msg.TopicPrefix = prefix
		got, err := httpCommandDeadlinePayload(msg, raw, deadline)
		if err != nil || string(got) != string(raw) {
			t.Fatalf("changed %s payload", prefix)
		}
	}
	msg.TopicPrefix = "plugin/HTTP/"
	msg.Type = downlink.MessageTypeTelemetry
	got, err := httpCommandDeadlinePayload(msg, raw, deadline)
	if err != nil || string(got) != string(raw) {
		t.Fatal("changed telemetry contract")
	}
}
