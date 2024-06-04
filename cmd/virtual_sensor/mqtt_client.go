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
	opts.SetResumeSubs(true)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(20 * time.Second)
	// 消息顺序
	opts.SetOrderMatters(false)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		log.Println("mqtt connect success")
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
