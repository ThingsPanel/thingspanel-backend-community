package mqtt

import (
	"fmt"
	"os"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var running bool
var _client mqtt.Client

func Listen(broker, username, password, clientid string, msgProc func(m mqtt.Message)) (err error) {
	running = false
	if _client == nil {
		opts := mqtt.NewClientOptions()
		opts.SetUsername(username)
		opts.SetPassword(password)
		opts.SetClientID(clientid)
		opts.AddBroker(broker)
		opts.SetAutoReconnect(true)
		opts.SetOnConnectHandler(func(c mqtt.Client) {
			if !running {
				fmt.Println("MQTT CONNECT SUCCESS -- ", broker)
			}
			running = true
		})
		opts.SetDefaultPublishHandler(func(c mqtt.Client, m mqtt.Message) {
			msgProc(m)
		})
		_client = mqtt.NewClient(opts)
		if token := _client.Connect(); token.Wait() && token.Error() != nil {
			panic(token.Error())
		}
		if token := _client.Subscribe("ThingsPanel", 0, nil); token.Wait() &&
			token.Error() != nil {
			fmt.Println(token.Error())
			os.Exit(1)
		}
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
