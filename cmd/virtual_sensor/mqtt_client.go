package main

import (
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-basic/uuid"
)

type MqttConfig struct {
	Broker string
	User   string
	Pass   string
}

// 创建mqtt客户端
func CreateMqttClient(config MqttConfig) *mqtt.Client {
	// 初始化配置
	opts := mqtt.NewClientOptions()
	opts.AddBroker(config.Broker)
	opts.SetUsername(config.User)
	if config.Pass != "" {
		opts.SetPassword(config.Pass)
	}
	opts.SetClientID(uuid.New())
	// 干净会话
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)
	// 恢复客户端订阅，需要broker支持
	opts.SetResumeSubs(false)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(20 * time.Second)
	// 消息顺序
	opts.SetOrderMatters(false)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		log.Println("mqtt connect success")
	})
	// 断线重连
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Println("mqtt connect  lost: ", err)
		// 等待连接成功，失败重新连接
		for {
			if token := client.Connect(); token.Wait() && token.Error() == nil {
				log.Println("Reconnected to MQTT broker")
				break
			} else {
				log.Printf("Reconnect failed: %v\n", token.Error())
			}
			time.Sleep(5 * time.Second)
		}
	})
	mqttClient := mqtt.NewClient(opts)
	for {
		if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
			log.Println(token.Error())
			time.Sleep(15 * time.Second)
		} else {
			break
		}
	}
	return &mqttClient
}
