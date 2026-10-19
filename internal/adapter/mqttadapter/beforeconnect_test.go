package mqttadapter

import (
	"context"
	"errors"
	"net"
	"testing"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

func TestCreateMQTTClientBeforeConnectFailureStopsBeforeNetworkDial(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan struct{}, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
			accepted <- struct{}{}
		}
	}()
	want := errors.New("route setup failed")
	called := false
	client, err := CreateMQTTClientContext(context.Background(), MQTTConfig{
		Broker: "tcp://" + listener.Addr().String(),
		BeforeConnect: func(_ mqtt.Client) error {
			called = true
			return want
		},
	}, logrus.New())
	if client != nil {
		client.Disconnect(0)
		t.Fatal("client returned despite BeforeConnect failure")
	}
	if !called || !errors.Is(err, want) {
		t.Fatalf("BeforeConnect called=%v error=%v, want setup failure", called, err)
	}
	select {
	case <-accepted:
		t.Fatal("broker network connection was attempted before successful route setup")
	default:
	}
}
