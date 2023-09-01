package wsmqtt

import (
	"ThingsPanel-Go/utils"
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/spf13/viper"
)

var WsMqttClient mqtt.Client

func CreateWsMqttClient() (err error) {
	var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
		fmt.Printf("Mqtt Connect lost: %v", err)
		i := 0
		for {
			time.Sleep(5 * time.Second)
			if !WsMqttClient.IsConnected() {
				fmt.Println("Mqtt reconnecting...")
				if token := WsMqttClient.Connect(); token.Wait() && token.Error() != nil {
					fmt.Println(token.Error())
					i++
				} else {
					fmt.Println("Mqtt reconnect success")
					break
				}
			} else {
				break
			}
		}
	}
	opts := mqtt.NewClientOptions()
	opts.SetUsername(viper.GetString("mqtt.user"))
	opts.SetPassword(viper.GetString("mqtt.pass"))
	opts.SetClientID(utils.GetUuid())
	opts.AddBroker(viper.GetString("mqtt.broker"))
	// 自动重连
	opts.SetAutoReconnect(true)
	// 重连间隔时间
	opts.SetResumeSubs(true)
	opts.SetCleanSession(false)
	opts.SetConnectRetryInterval(time.Duration(5) * time.Second)
	opts.SetOrderMatters(false)
	opts.OnConnectionLost = connectLostHandler
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		fmt.Println("wsmqtt客户端已连接")
	})
	WsMqttClient = mqtt.NewClient(opts)
	for {
		if token := WsMqttClient.Connect(); token.Wait() && token.Error() != nil {
			fmt.Println(token.Error())
			time.Sleep(5 * time.Second)
		} else {
			break
		}
	}
	return nil
}
