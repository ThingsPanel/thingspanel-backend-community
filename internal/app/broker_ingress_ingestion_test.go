package app

import (
	"context"
	"testing"

	"project/internal/brokerledger"
	"project/internal/uplink"
)

type ingestionIDCaptureStore struct{ messages []brokerledger.Message }

func (s *ingestionIDCaptureStore) Accept(_ context.Context, message brokerledger.Message) (string, bool, error) {
	s.messages = append(s.messages, message)
	return message.ID, false, nil
}

func TestBrokerIngressAcceptorUsesStableScopedIngestionReceiptID(t *testing.T) {
	store := &ingestionIDCaptureStore{}
	acceptor := &brokerIngressAcceptor{store: store}
	accept := func(device string, stream string, ingestionID string) {
		t.Helper()
		message := &uplink.DeviceMessage{
			TenantID: "tenant", DeviceID: device, Type: stream, Payload: []byte(`{"v":1}`),
			Metadata: map[string]any{"broker_cell": "cell", "route_epoch": uint64(1), "ingestion_id": ingestionID},
		}
		if err := acceptor.Accept(context.Background(), message); err != nil {
			t.Fatal(err)
		}
	}
	accept("device-a", "attribute", "mosq-boot-1-1")
	accept("device-a", "attribute", "mosq-boot-1-1") // broker redelivery
	accept("device-a", "attribute", "mosq-boot-1-2") // new PUBLISH, same bytes
	accept("device-b", "attribute", "mosq-boot-1-1") // same ID in another scope
	accept("device-a", "event", "mosq-boot-1-1")

	if len(store.messages) != 5 {
		t.Fatalf("captured %d messages, want 5", len(store.messages))
	}
	ids := make([]string, len(store.messages))
	for i, message := range store.messages {
		ids[i] = message.ID
		if got := message.Metadata["ingestion_id"]; got != "mosq-boot-1-1" && i != 2 {
			t.Fatalf("message %d lost ingestion_id: %v", i, got)
		}
	}
	if ids[0] != ids[1] {
		t.Fatalf("same packet produced different receipt IDs: %q != %q", ids[0], ids[1])
	}
	for _, i := range []int{2, 3, 4} {
		if ids[i] == ids[0] {
			t.Fatalf("different publish/device/stream reused receipt ID at index %d", i)
		}
	}
}
