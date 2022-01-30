package mqtt

import (
	"errors"
	"fmt"
	"os"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/spf13/viper"
)

var running bool
var _client mqtt.Client

func Listen(broker, username, password, clientid string, msgProc func(m mqtt.Message)) (err error) {
	running = false
	if _client == nil {
		var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
			fmt.Printf("Connect lost: %v", err)
		}
		opts := mqtt.NewClientOptions()
		fmt.Println(broker + username + password + clientid)
		opts.SetUsername(username)
		opts.SetPassword(password)
		opts.SetClientID(clientid)
		opts.AddBroker(broker)
		opts.SetAutoReconnect(true)
		opts.OnConnectionLost = connectLostHandler
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
		if token := _client.Subscribe(viper.GetString("mqtt.topicToSubscribe"), 0, nil); token.Wait() &&
			token.Error() != nil {
			fmt.Println(token.Error())
			os.Exit(1)
		}
	}
	return
}

//发送消息
func Send(payload []byte) (err error) {
	var clientErr = errors.New("_client is error")
	if _client == nil {
		return clientErr
	}
	token := _client.Publish(viper.GetString("mqtt.topicToPublish"), 1, false, string(payload))
	if token.Error() != nil {
		fmt.Println(token.Error())
	}
	return token.Error()
}

func Close() {
	if _client != nil {
		_client.Disconnect(3000)
	}
}
