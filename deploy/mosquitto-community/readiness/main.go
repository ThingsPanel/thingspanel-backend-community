package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	bc "project/internal/brokercontract"
	"project/internal/brokercontrol"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func main() {
	if len(os.Args) != 2 {
		fail()
	}
	var err error
	switch os.Args[1] {
	case "owner-liveness":
		var conn net.Conn
		conn, err = net.DialTimeout("tcp", "127.0.0.1:9801", time.Second)
		if conn != nil {
			_ = conn.Close()
		}
	case "broker":
		err = brokerReady()
	default:
		err = errors.New("invalid readiness mode")
	}
	if err != nil {
		fail()
	}
}

func brokerReady() error {
	data, err := os.ReadFile("/run/secrets/broker-services.json")
	if err != nil || len(data) > 1<<20 {
		return errors.New("service policy unavailable")
	}
	var services []brokercontrol.ServiceCredential
	if err := json.Unmarshal(data, &services); err != nil {
		return errors.New("service policy invalid")
	}
	clientID := os.Getenv("TP_BROKER_READINESS_CLIENT_ID")
	if clientID == "" {
		clientID = "tp-community-readiness"
	}
	var selected *brokercontrol.ServiceCredential
	for i := range services {
		service := &services[i]
		allowedID := false
		for _, id := range service.AllowedClientIDs {
			if id == clientID {
				allowedID = true
				break
			}
		}
		allowedFilter := false
		for _, filter := range service.Policy.Subscribe {
			if filter == bc.StatePrefix+"/#" {
				allowedFilter = true
				break
			}
		}
		if allowedID && allowedFilter {
			selected = service
			break
		}
	}
	if selected == nil || selected.Username == "" || selected.Password == "" {
		return errors.New("readiness identity is not allowlisted")
	}
	opts := mqtt.NewClientOptions().
		AddBroker("tcp://127.0.0.1:1883").
		SetClientID(clientID).
		SetUsername(selected.Username).
		SetPassword(selected.Password).
		SetConnectTimeout(2 * time.Second).
		SetKeepAlive(5 * time.Second).
		SetCleanSession(true).
		SetAutoReconnect(false).
		SetConnectRetry(false)
	client := mqtt.NewClient(opts)
	if token := client.Connect(); !token.WaitTimeout(3*time.Second) || token.Error() != nil {
		client.Disconnect(0)
		return errors.New("authenticated broker connect failed")
	}
	defer client.Disconnect(100)
	sub := client.Subscribe(bc.StatePrefix+"/#", 0, func(mqtt.Client, mqtt.Message) {})
	if !sub.WaitTimeout(3*time.Second) || sub.Error() != nil {
		return errors.New("authorized readiness subscription failed")
	}
	return nil
}

func fail() {
	_, _ = fmt.Fprintln(os.Stderr, "community broker readiness failed")
	os.Exit(1)
}
