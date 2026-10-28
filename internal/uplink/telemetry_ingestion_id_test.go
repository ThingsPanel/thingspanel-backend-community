package uplink

import (
	"io"
	"testing"

	"github.com/sirupsen/logrus"
	"project/internal/model"
	"project/internal/storage"
)

func TestTrustedTelemetryReceiptIDStablePerPacketAndScopedPerLogicalDevice(t *testing.T) {
	input := make(chan *storage.Message, 4)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	f := NewTelemetryUplink(TelemetryUplinkConfig{StorageInput: input, Logger: logger})
	defer f.Stop()
	newMsg := func(logicalDevice string) {
		msg := trustedTelemetryTestMessage()
		msg.Metadata["ingestion_id"] = "mosq-boot-1-000000000000000a"
		device := &model.Device{ID: logicalDevice, TenantID: msg.TenantID}
		f.processDirectDeviceMessage(device, msg.Payload, msg)
	}
	newMsg("logical-a")
	newMsg("logical-a") // adapter worker retry of the same packet
	newMsg("logical-b") // gateway child has a separate receipt scope
	first, retry, child := <-input, <-input, <-input
	if first.BrokerRoute == nil || retry.BrokerRoute == nil || child.BrokerRoute == nil {
		t.Fatal("trusted telemetry message lost route")
	}
	if first.BrokerRoute.ReceiptID != retry.BrokerRoute.ReceiptID {
		t.Fatalf("same packet produced unstable logical receipt: %q != %q", first.BrokerRoute.ReceiptID, retry.BrokerRoute.ReceiptID)
	}
	if first.BrokerRoute.ReceiptID == child.BrokerRoute.ReceiptID {
		t.Fatal("different logical child shared the same telemetry receipt")
	}
}
