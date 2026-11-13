package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"project/internal/brokerledger"
	"project/internal/uplink"
	"project/pkg/global"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

type testIngressStore struct {
	acceptCalls  atomic.Int32
	processCalls atomic.Int32
	entered      chan struct{}
}

func (s *testIngressStore) Accept(context.Context, brokerledger.Message) (string, bool, error) {
	s.acceptCalls.Add(1)
	return "receipt-id", false, nil
}
func (s *testIngressStore) ProcessOne(ctx context.Context, _ func(context.Context, *gorm.DB, brokerledger.Message) error) (bool, error) {
	s.processCalls.Add(1)
	if s.entered != nil {
		select {
		case s.entered <- struct{}{}:
		default:
		}
	}
	<-ctx.Done()
	return false, ctx.Err()
}

func TestBrokerIngressLegacyDoesNotRequireDBOrStartWorker(t *testing.T) {
	previous := viper.GetString("mqtt.input_profile")
	viper.Set("mqtt.input_profile", "legacy")
	t.Cleanup(func() { viper.Set("mqtt.input_profile", previous) })
	service := &MQTTService{app: &Application{Logger: logrus.New()}}
	if err := service.configureBrokerIngress(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if service.brokerWorker != nil {
		t.Fatal("legacy started a durable ingress worker")
	}
}

func TestBrokerIngressIngressV1FailsClosedWithoutDB(t *testing.T) {
	previousProfile := viper.GetString("mqtt.input_profile")
	previousGlobal := global.DB
	viper.Set("mqtt.input_profile", "ingress_v1")
	global.DB = nil
	t.Cleanup(func() { viper.Set("mqtt.input_profile", previousProfile); global.DB = previousGlobal })
	service := &MQTTService{app: &Application{Logger: logrus.New()}}
	if err := service.configureBrokerIngress(context.Background(), nil); err == nil {
		t.Fatal("expected missing DB to fail closed")
	}
	if service.brokerWorker != nil {
		t.Fatal("worker started without a database")
	}
}

func TestBrokerIngressAcceptorRejectsUntrustedRouteEpoch(t *testing.T) {
	store := &testIngressStore{}
	acceptor := &brokerIngressAcceptor{store: store, logger: logrus.New()}
	for _, value := range []any{float64(1.5), float64(-1), "3", uint64(0), float64(1 << 64)} {
		message := &uplink.DeviceMessage{TenantID: "tenant", DeviceID: "device", Type: "event", Metadata: map[string]interface{}{"broker_cell": "cell", "route_epoch": value}}
		if err := acceptor.Accept(context.Background(), message); !errors.Is(err, errBrokerIngressTrust) {
			t.Fatalf("epoch %v: got %v, want trust rejection", value, err)
		}
	}
	if got := store.acceptCalls.Load(); got != 0 {
		t.Fatalf("untrusted messages reached ledger %d times", got)
	}
}

func TestBrokerIngressAcceptorUsesOnlyExplicitSourceEventID(t *testing.T) {
	store := &captureAcceptStore{}
	acceptor := &brokerIngressAcceptor{store: store, logger: logrus.New()}
	msg := &uplink.DeviceMessage{TenantID: "tenant", DeviceID: "device", Type: "event", Timestamp: 1234, Payload: []byte(`{"message_id":"wire-id","source_event_id":"source-id"}`), Metadata: map[string]interface{}{"broker_cell": "cell", "route_epoch": float64(7), "message_id": "mqtt-id"}}
	if err := acceptor.Accept(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if store.message.SourceEventID != "source-id" || store.message.Metadata["message_id"] != "mqtt-id" || store.message.Metadata["timestamp"] != int64(1234) {
		t.Fatalf("unexpected durable message: %+v", store.message)
	}
	msg.Payload = []byte(`{"message_id":"wire-id"}`)
	if err := acceptor.Accept(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if store.message.SourceEventID != "" {
		t.Fatalf("wire message_id became idempotency key: %q", store.message.SourceEventID)
	}
}

func TestBrokerIngressAttributeSourceEventIDRemainsBusinessPayload(t *testing.T) {
	store := &captureAcceptStore{}
	acceptor := &brokerIngressAcceptor{store: store, logger: logrus.New()}
	payload := []byte(`{"source_event_id":"valid-attribute-key","temperature":22.4}`)
	msg := &uplink.DeviceMessage{TenantID: "tenant", DeviceID: "device", Type: "attribute", Timestamp: 1234, Payload: payload, Metadata: map[string]interface{}{"broker_cell": "cell", "route_epoch": uint64(2)}}
	if err := acceptor.Accept(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if store.message.SourceEventID != "" {
		t.Fatalf("ordinary attribute field was treated as deduplication id: %q", store.message.SourceEventID)
	}
	if string(store.message.Payload) != string(payload) {
		t.Fatalf("ordinary attribute payload changed: %s", store.message.Payload)
	}
}

type captureAcceptStore struct{ message brokerledger.Message }

func (s *captureAcceptStore) Accept(_ context.Context, msg brokerledger.Message) (string, bool, error) {
	s.message = msg
	return "receipt-id", false, nil
}

func TestBrokerIngressWorkerStopWaitsAndStartIsIdempotent(t *testing.T) {
	store := &testIngressStore{entered: make(chan struct{}, 4)}
	worker := &brokerIngressWorker{store: store, processor: ingressProcessorFunc(func(context.Context, *gorm.DB, brokerledger.Message) error { return nil }), logger: logrus.New()}
	ctx := context.Background()
	if err := worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	if err := worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := store.processCalls.Load(); got != 1 {
		t.Fatalf("duplicate Start spawned %d process loops", got)
	}
	worker.Stop()
	if worker.done != nil {
		t.Fatal("Stop returned before worker state was cleared")
	}
	if err := worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not restart")
	}
	worker.Stop()
}

type ingressProcessorFunc func(context.Context, *gorm.DB, brokerledger.Message) error

func (f ingressProcessorFunc) Process(ctx context.Context, tx *gorm.DB, msg brokerledger.Message) error {
	return f(ctx, tx, msg)
}
