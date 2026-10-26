package mqttadapter

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestCreateMQTTClientContextReturnsOnCanceledStartup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	client, err := CreateMQTTClientContext(ctx, MQTTConfig{Broker: "tcp://127.0.0.1:1"}, logrus.New())
	if client != nil {
		t.Fatal("client must be nil when startup context is already canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("canceled startup took %s, want under 1s", elapsed)
	}
}

func TestMQTTClientManualAckFollowsConfig(t *testing.T) {
	if opts := newMQTTClientOptions(MQTTConfig{}, logrus.New()); opts.AutoAckDisabled {
		t.Fatal("legacy/default MQTT client unexpectedly disabled automatic ACK")
	}
	if opts := newMQTTClientOptions(MQTTConfig{ManualAck: true}, logrus.New()); !opts.AutoAckDisabled {
		t.Fatal("ingress_v1 MQTT client did not disable automatic ACK")
	}
}

func TestCreateMQTTClientContextBoundsUnavailableBrokerRetry(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	broker := "tcp://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	client, err := CreateMQTTClientContext(ctx, MQTTConfig{Broker: broker, ClientID: "lifecycle-test"}, logrus.New())
	if client != nil {
		t.Fatal("client must be nil when broker is unavailable")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("unavailable broker startup took %s, want under 1s", elapsed)
	}
}
