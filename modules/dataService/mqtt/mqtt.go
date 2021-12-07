package mqtt

import (
	"fmt"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var _client mqtt.Client

func Listen(broker, username, password, clientid string, msgProc func(m mqtt.Message)) (err error) {
	if _client == nil {
		opts := mqtt.NewClientOptions()
		opts.SetUsername(username)
		opts.SetPassword(password)
		opts.SetClientID(clientid)
		opts.AddBroker(broker)
		opts.SetAutoReconnect(true)
		opts.SetOnConnectHandler(func(c mqtt.Client) {
			fmt.Println("MQTT CONNECT SUCCESS -- ", broker)
		})
		opts.SetDefaultPublishHandler(func(c mqtt.Client, m mqtt.Message) {
			msgProc(m)
		})
		_client = mqtt.NewClient(opts)
		_client.Connect()
	}
	return
}

func Send() (err error) {
	return
}

func Close() {
	if _client != nil {
		_client.Disconnect(3000)
	}
}
