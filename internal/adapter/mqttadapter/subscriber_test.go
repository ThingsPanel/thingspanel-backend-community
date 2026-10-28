package mqttadapter

import (
	"encoding/json"
	"testing"
)

func TestDecodeCommandAckPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
		valid   bool
	}{
		{name: "direct success ack", payload: `{"result":0,"message":"success","ts":1}`, want: `{"result":0,"message":"success","ts":1}`, valid: true},
		{name: "direct failed ack", payload: `{"result":1,"message":"failed"}`, want: `{"result":1,"message":"failed"}`, valid: true},
		{name: "missing result", payload: `{"message":"success"}`},
		{name: "invalid result", payload: `{"result":2}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := decodeCommandAckPayload([]byte(tt.payload))
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v, err=%v", tt.valid, err)
			}
			if tt.valid && string(got) != tt.want {
				t.Fatalf("response=%s, want %s", got, tt.want)
			}
		})
	}

	ack := []byte(`{"result":0,"message":"success"}`)
	envelope, err := json.Marshal(map[string]interface{}{"device_id": "device-1", "values": ack})
	if err != nil {
		t.Fatal(err)
	}
	got, deviceID, err := decodeCommandAckPayload(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(ack) || deviceID != "device-1" {
		t.Fatalf("got response=%s device_id=%q", got, deviceID)
	}
}
