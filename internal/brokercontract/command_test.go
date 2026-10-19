package brokercontract

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func validCommandFrame(now time.Time) CommandFrame {
	return CommandFrame{
		CommandID: "命令-α", TenantID: "租户-一", TargetDeviceID: "device-测-1",
		RouteEpoch: 9, MappingVersion: "mapping-v1", WireTopic: "devices/command/device-测-1/命令-α",
		ExpiresAt: now.Add(time.Minute).UnixMilli(), Payload: []byte{0, 0xff, '{', '}', 0x80},
	}
}

func TestCommandFrameRoundTripsBinaryAndBase64EncodesOnce(t *testing.T) {
	now := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	f := validCommandFrame(now)
	encoded, err := EncodeCommandFrame(f, now)
	if err != nil {
		t.Fatal(err)
	}
	if !IsCommandFrame(encoded) {
		t.Fatal("encoded payload is missing private storage marker")
	}
	body := bytes.TrimPrefix(encoded, []byte(commandMarker))
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := json.Unmarshal(obj["payload"], &payload); err != nil {
		t.Fatal(err)
	}
	if payload != base64.StdEncoding.EncodeToString(f.Payload) {
		t.Fatalf("frame payload must be base64 exactly once: %q", payload)
	}
	got, err := ParseCommandFrame(encoded, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.CommandID != f.CommandID || got.TenantID != f.TenantID || got.TargetDeviceID != f.TargetDeviceID || got.RouteEpoch != f.RouteEpoch || got.MappingVersion != f.MappingVersion || got.WireTopic != f.WireTopic || !bytes.Equal(got.Payload, f.Payload) {
		t.Fatalf("frame round trip mismatch: %#v", got)
	}
}

func TestCommandFrameRejectsExpiryBoundsSizeAndMalformedStorage(t *testing.T) {
	now := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*CommandFrame)
	}{
		{"expired", func(f *CommandFrame) { f.ExpiresAt = now.UnixMilli() }},
		{"over 24 hours", func(f *CommandFrame) { f.ExpiresAt = now.Add(24*time.Hour + time.Millisecond).UnixMilli() }},
		{"oversized payload", func(f *CommandFrame) { f.Payload = bytes.Repeat([]byte{'x'}, MaxCommandPayload+1) }},
		{"empty payload", func(f *CommandFrame) { f.Payload = nil }},
		{"empty target", func(f *CommandFrame) { f.TargetDeviceID = "" }},
		{"wildcard target topic", func(f *CommandFrame) { f.WireTopic = "devices/command/+/命令-α" }},
		{"wrong target topic family", func(f *CommandFrame) { f.WireTopic = "devices/event/device-测-1/命令-α" }},
		{"topic command id mismatch", func(f *CommandFrame) { f.WireTopic = "devices/command/device-测-1/other" }},
		{"empty topic level", func(f *CommandFrame) { f.WireTopic = "devices/command//命令-α" }},
		{"invalid UTF-8 topic", func(f *CommandFrame) {
			f.WireTopic = string([]byte{'d', 'e', 'v', 'i', 'c', 'e', 's', '/', 'c', 'o', 'm', 'm', 'a', 'n', 'd', '/', 0xff, '/', 'x'})
		}},
		{"oversized topic", func(f *CommandFrame) {
			f.WireTopic = "devices/command/device/" + strings.Repeat("x", MaxWireTopicBytes)
		}},
		{"control character topic", func(f *CommandFrame) { f.WireTopic = "devices/command/device/cmd\x00" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := validCommandFrame(now)
			tt.mutate(&f)
			if _, err := EncodeCommandFrame(f, now); err == nil {
				t.Fatal("invalid frame encoded")
			}
		})
	}

	valid, err := EncodeCommandFrame(validCommandFrame(now), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ParseCommandFrame(append(bytes.Clone(valid), []byte(` {}`)...), now); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	unknown := bytes.Replace(bytes.Clone(valid), []byte(`{"command_id":`), []byte(`{"unknown":true,"command_id":`), 1)
	if _, err = ParseCommandFrame(unknown, now); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err = ParseCommandFrame(append([]byte(commandMarker), bytes.Repeat([]byte{'x'}, (1<<20)+1)...), now); err == nil {
		t.Fatal("oversized frame accepted")
	}
	if _, err = ParseCommandFrame(append([]byte(commandMarker), []byte(strings.Repeat("{", 3))...), now); err == nil {
		t.Fatal("malformed JSON accepted")
	}
}
