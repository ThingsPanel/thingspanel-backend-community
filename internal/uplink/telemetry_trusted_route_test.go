package uplink

import (
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"project/internal/model"
	"project/internal/storage"
)

func trustedTelemetryTestMessage() *DeviceMessage {
	return &DeviceMessage{
		Type: "telemetry", DeviceID: "transport-device", TenantID: "tenant-a",
		Timestamp: 1_800_000_000_123, Payload: []byte(`{"temperature":23}`),
		Metadata: map[string]interface{}{
			"transport_profile": "ingress_v1", "device_id": "transport-device",
			"broker_cell": "cell-a", "route_epoch": uint64(9),
		},
	}
}

func TestTrustedTelemetryBuildsCommitRoutedStorageMessage(t *testing.T) {
	input := make(chan *storage.Message, 1)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	f := NewTelemetryUplink(TelemetryUplinkConfig{StorageInput: input, Logger: logger})
	defer f.Stop()
	msg := trustedTelemetryTestMessage()
	device := &model.Device{ID: msg.DeviceID, TenantID: msg.TenantID}
	f.processDirectDeviceMessage(device, msg.Payload, msg)
	select {
	case stored := <-input:
		if stored.BrokerRoute == nil {
			t.Fatal("ingress_v1 storage message has no trusted route")
		}
		route := stored.BrokerRoute
		if _, err := uuid.Parse(route.ReceiptID); err != nil {
			t.Fatalf("receipt id is not a UUID: %q", route.ReceiptID)
		}
		wantObserved := time.UnixMilli(msg.Timestamp).UTC()
		if route.TransportDeviceID != msg.DeviceID || route.BrokerCell != "cell-a" || route.RouteEpoch != 9 || !route.ObservedAt.Equal(wantObserved) {
			t.Fatalf("trusted route=%+v", route)
		}
		if stored.DeviceID != device.ID || stored.TenantID != device.TenantID || stored.Timestamp != msg.Timestamp || stored.AfterCommit == nil {
			t.Fatalf("storage message lost route/commit context: %+v", stored)
		}
	default:
		t.Fatal("telemetry did not reach storage input")
	}
}

func TestTrustedTelemetryRejectsMalformedMetadataBeforeDeviceLookup(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	input := make(chan *storage.Message, 1)
	f := NewTelemetryUplink(TelemetryUplinkConfig{StorageInput: input, Logger: logger})
	defer f.Stop()
	for _, tc := range []struct {
		name   string
		mutate func(*DeviceMessage)
	}{
		{name: "missing device id", mutate: func(m *DeviceMessage) { delete(m.Metadata, "device_id") }},
		{name: "wrong device id type", mutate: func(m *DeviceMessage) { m.Metadata["device_id"] = 12 }},
		{name: "missing broker cell", mutate: func(m *DeviceMessage) { delete(m.Metadata, "broker_cell") }},
		{name: "zero epoch", mutate: func(m *DeviceMessage) { m.Metadata["route_epoch"] = uint64(0) }},
		{name: "fractional epoch", mutate: func(m *DeviceMessage) { m.Metadata["route_epoch"] = float64(1.5) }},
		{name: "string profile", mutate: func(m *DeviceMessage) { m.Metadata["transport_profile"] = []byte("ingress_v1") }},
		{name: "missing timestamp", mutate: func(m *DeviceMessage) { m.Timestamp = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := trustedTelemetryTestMessage()
			tc.mutate(m)
			f.processMessage(m)
			select {
			case got := <-input:
				t.Fatalf("malformed metadata reached storage: %+v", got)
			default:
			}
		})
	}
}

func TestTrustedTelemetryStorageSendStopsWithContext(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	f := NewTelemetryUplink(TelemetryUplinkConfig{StorageInput: make(chan *storage.Message), Logger: logger})
	done := make(chan struct{})
	go func() {
		defer close(done)
		msg := trustedTelemetryTestMessage()
		f.processDirectDeviceMessage(&model.Device{ID: msg.DeviceID, TenantID: msg.TenantID}, msg.Payload, msg)
	}()
	select {
	case <-done:
		t.Fatal("storage send unexpectedly did not block")
	case <-time.After(20 * time.Millisecond):
	}
	f.Stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("storage send ignored canceled context")
	}
}

func TestTrustedTelemetryProfileRecognitionIsExact(t *testing.T) {
	m := trustedTelemetryTestMessage()
	trusted, _, _, _, _, err := trustedTelemetryMetadata(m)
	if err != nil || !trusted {
		t.Fatalf("valid profile trusted=%v err=%v", trusted, err)
	}
	m.Metadata["transport_profile"] = "legacy"
	trusted, _, _, _, _, err = trustedTelemetryMetadata(m)
	if err != nil || trusted {
		t.Fatalf("legacy profile trusted=%v err=%v", trusted, err)
	}
	_, _, _, _, _, err = trustedTelemetryMetadata(nil)
	if err == nil {
		t.Fatal("nil message was not rejected")
	}
}
