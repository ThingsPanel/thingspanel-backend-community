package mqttadapter

import (
	"context"
	"github.com/eclipse/paho.mqtt.golang/packets"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	bc "project/internal/brokercontract"
	"testing"
	"time"
)

func TestTargetPublisherRealMQTTTransport(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	observed := make(chan error, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			observed <- e
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		packet, e := packets.ReadPacket(conn)
		if e != nil {
			observed <- e
			return
		}
		connect, ok := packet.(*packets.ConnectPacket)
		if !ok {
			observed <- io.ErrUnexpectedEOF
			return
		}
		target, valid := bc.ParseTargetPublisherClientID("platform", connect.ClientIdentifier)
		if !valid || target != "new-target" || !connect.CleanSession || connect.Username != "fixture-service" || string(connect.Password) != "fixture-password" {
			observed <- io.ErrUnexpectedEOF
			return
		}
		ack := packets.NewControlPacket(packets.Connack).(*packets.ConnackPacket)
		if e = ack.Write(conn); e != nil {
			observed <- e
			return
		}
		// Two sends must use the same actual socket and precise target client id.
		for i := 0; i < 2; i++ {
			packet, e = packets.ReadPacket(conn)
			if e != nil {
				observed <- e
				return
			}
			pub, ok := packet.(*packets.PublishPacket)
			if !ok || pub.TopicName != "devices/event/response/new-number/message-1" || pub.Qos != 1 || pub.Retain || string(pub.Payload) != "fixture-response" {
				observed <- io.ErrUnexpectedEOF
				return
			}
			reply := packets.NewControlPacket(packets.Puback).(*packets.PubackPacket)
			reply.MessageID = pub.MessageID
			if e = reply.Write(conn); e != nil {
				observed <- e
				return
			}
		}
		observed <- nil
		// Keep the peer alive until the lifecycle closes it.
		packets.ReadPacket(conn)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	a := &Adapter{logger: logger}
	if err = a.ConfigureTargetPublisher(ctx, MQTTConfig{Broker: "tcp://" + listener.Addr().String(), Username: "fixture-service", Password: "fixture-password", ClientID: "platform"}); err != nil {
		t.Fatal(err)
	}
	defer a.targetPublishers.close()
	for i := 0; i < 2; i++ {
		if err = a.targetPublishers.publish(ctx, "new-target", "devices/event/response/new-number/message-1", []byte("fixture-response")); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err = <-observed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("simulated MQTT peer timed out")
	}
}

func TestTargetPublisherResponseRequiresAuthenticatedID(t *testing.T) {
	a := &Adapter{targetPublishers: &targetPublisherPool{}}
	if err := a.publishResponse("devices/event/response/n/m", nil, nil); err == nil {
		t.Fatal("identity-free response allowed")
	}
	if err := a.publishResponse("devices/event/response/n/m", nil, []string{""}); err == nil {
		t.Fatal("blank response identity allowed")
	}
}
