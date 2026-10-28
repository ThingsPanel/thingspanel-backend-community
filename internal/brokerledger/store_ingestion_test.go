package brokerledger

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestPostgresIngestionIDReceiptRetryScopeAndSourceDedup(t *testing.T) {
	store, db := postgresStore(t, Limits{})
	tenant, device := uuid.NewString(), uuid.NewString()
	otherTenant, otherDevice := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenant, device, "cell-a", 1, "active")
	insertBinding(t, db, otherTenant, otherDevice, "cell-a", 1, "active")

	makeMessage := func(ingestionID, payload string) Message {
		return Message{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("receipt:"+ingestionID)).String(), TenantID: tenant, DeviceID: device, Stream: "attribute", BrokerCell: "cell-a", RouteEpoch: 1, Payload: []byte(payload), Metadata: map[string]any{"topic": "devices/attributes", "ingestion_id": ingestionID, "observed_at": "volatile"}}
	}

	first := makeMessage("same-packet", `{"humidity":72}`)
	id, duplicate, err := store.Accept(context.Background(), first)
	if err != nil || duplicate || id != first.ID {
		t.Fatalf("first accept id=%q duplicate=%v err=%v", id, duplicate, err)
	}
	// The broker may re-deliver the same durable internal envelope after the
	// device PUBACK but before adapter ACK; this is a retry of one sample.
	retry := first
	retry.Metadata = map[string]any{"topic": "devices/attributes", "ingestion_id": "same-packet", "observed_at": "retry-time"}
	id2, duplicate, err := store.Accept(context.Background(), retry)
	if err != nil || !duplicate || id2 != id {
		t.Fatalf("same packet retry id=%q duplicate=%v err=%v, want %q/true", id2, duplicate, err, id)
	}
	changedBody := first
	changedBody.Payload = []byte(`{"humidity":73}`)
	if _, _, err := store.Accept(context.Background(), changedBody); !errors.Is(err, ErrConflict) {
		t.Fatalf("same ID with changed body err=%v, want ErrConflict", err)
	}
	changedScope := first
	changedScope.TenantID, changedScope.DeviceID = otherTenant, otherDevice
	if _, _, err := store.Accept(context.Background(), changedScope); !errors.Is(err, ErrConflict) {
		t.Fatalf("same ID with different scope err=%v, want ErrConflict", err)
	}

	// Identical bytes from a new MQTT PUBLISH are a distinct legitimate sample.
	newPublish := makeMessage("new-publish", `{"humidity":72}`)
	id3, duplicate, err := store.Accept(context.Background(), newPublish)
	if err != nil || duplicate || id3 == id {
		t.Fatalf("new publish id=%q duplicate=%v err=%v, want distinct receipt", id3, duplicate, err)
	}

	// Explicit source_event_id remains the stronger business idempotency key
	// across a retransmission whose trusted transport ingestion_id changed.
	event := Message{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("event-ingress-1")).String(), TenantID: tenant, DeviceID: device, Stream: "event", SourceEventID: "source-event-1", BrokerCell: "cell-a", RouteEpoch: 1, Payload: []byte(`{"state":"on"}`), Metadata: map[string]any{"topic": "devices/event/1", "ingestion_id": "event-ingress-1"}}
	eventID, duplicate, err := store.Accept(context.Background(), event)
	if err != nil || duplicate {
		t.Fatalf("first event id=%q duplicate=%v err=%v", eventID, duplicate, err)
	}
	eventRetry := event
	eventRetry.ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte("event-ingress-2")).String()
	eventRetry.Metadata = map[string]any{"topic": "devices/event/1", "ingestion_id": "event-ingress-2"}
	eventRetryID, duplicate, err := store.Accept(context.Background(), eventRetry)
	if err != nil || !duplicate || eventRetryID != eventID {
		t.Fatalf("source event retry id=%q duplicate=%v err=%v, want %q/true", eventRetryID, duplicate, err, eventID)
	}
	eventRetry.Payload = []byte(`{"state":"off"}`)
	if _, _, err := store.Accept(context.Background(), eventRetry); !errors.Is(err, ErrConflict) {
		t.Fatalf("same source event with changed payload err=%v, want ErrConflict", err)
	}

	stats, err := store.Stats(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	if stats.PendingCount != 3 {
		t.Fatalf("pending receipt count=%d, want attribute retry 1 + new sample 1 + source event 1", stats.PendingCount)
	}
}
