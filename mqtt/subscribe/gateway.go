package subscribe

import (
	"context"
	"os"
	config "project/mqtt"
	"project/mqtt/publish"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/panjf2000/ants"
	"github.com/sirupsen/logrus"
)

var pool *ants.Pool

type SubscribeTopic struct {
	Topic    string
	Qos      byte
	Callback mqtt.MessageHandler
}

func getSubscribeTopics() []SubscribeTopic {
	return []SubscribeTopic{
		{
			Topic:    config.MqttConfig.Telemetry.GatewaySubscribeTopic,
			Qos:      byte(config.MqttConfig.Telemetry.QoS),
			Callback: GatewaySubscribeTelemetryCallback,
		}, {
			Topic:    config.MqttConfig.Attributes.GatewaySubscribeTopic,
			Qos:      byte(config.MqttConfig.Attributes.QoS),
			Callback: GatewaySubscribeAttributesCallback,
		}, {
			Topic:    config.MqttConfig.Attributes.GatewaySubscribeResponseTopic,
			Qos:      byte(config.MqttConfig.Attributes.QoS),
			Callback: GatewaySubscribeSetAttributesResponseCallback,
		}, {
			Topic:    config.MqttConfig.Events.GatewaySubscribeTopic,
			Qos:      byte(config.MqttConfig.Events.QoS),
			Callback: GatewaySubscribeEventCallback,
		}, {
			Topic:    config.MqttConfig.Commands.GatewaySubscribeTopic,
			Qos:      byte(config.MqttConfig.Commands.QoS),
			Callback: GatewaySubscribeCommandResponseCallback,
		},
	}
}

func GatewaySubscribeTelemetryCallback(c mqtt.Client, d mqtt.Message) {
	err := pool.Submit(func() {
		// 处理消息
		GatewayTelemetryMessages(d.Payload(), d.Topic())
	})
	if err != nil {
		logrus.Error(err)
	}
}

func GatewaySubscribeAttributesCallback(c mqtt.Client, d mqtt.Message) {
	messageId, deviceInfo, response, err := GatewayAttributeMessages(d.Payload(), d.Topic())
	logrus.Debug("响应设备属性上报", deviceInfo, err)
	if err != nil {
		logrus.Error(err)
	}
	if deviceInfo != nil && messageId != "" {
		// 响应设备属性上报
		publish.GatewayPublishResponseAttributesMessage(context.Background(), *deviceInfo, messageId, response)
	}
}

func GatewaySubscribeSetAttributesResponseCallback(c mqtt.Client, d mqtt.Message) {
	GatewayDeviceSetAttributesResponse(d.Payload(), d.Topic())
}

func GatewaySubscribeEventCallback(c mqtt.Client, d mqtt.Message) {
	messageId, deviceInfo, response, err := GatewayEventCallback(d.Payload(), d.Topic())
	logrus.Debug("响应设备事件上报", deviceInfo, err)
	if err != nil {
		logrus.Error(err)
	}
	if deviceInfo != nil && messageId != "" {
		publish.GatewayPublishResponseEventMessage(context.Background(), *deviceInfo, messageId, response)
	}
}

func GatewaySubscribeCommandResponseCallback(c mqtt.Client, d mqtt.Message) {
	GatewayDeviceCommandResponse(d.Payload(), d.Topic())
}

// GatewaySubscribeTopic
// @description 网关批量订阅消息
// @return void
func GatewaySubscribeTopic() {
	p, err := ants.NewPool(config.MqttConfig.Telemetry.PoolSize)
	if err != nil {
		logrus.Error("协程池创建失败")
		return
	}
	pool = p
	for _, topic := range getSubscribeTopics() {
		topic.Topic = GenTopic(topic.Topic)
		logrus.Info("subscribe topic:", topic.Topic)
		if token := SubscribeMqttClient.Subscribe(topic.Topic, topic.Qos, topic.Callback); token.Wait() && token.Error() != nil {
			logrus.Error(token.Error())
			os.Exit(1)
		}
	}
}
