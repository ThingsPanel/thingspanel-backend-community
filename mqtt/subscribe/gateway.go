package subscribe

import (
	"context"
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
		},
		// 网关属性设置响应已迁移到 MQTTAdapter.SubscribeResponseTopics
		// {
		// 	Topic:    config.MqttConfig.Attributes.GatewaySubscribeResponseTopic,
		// 	Qos:      byte(config.MqttConfig.Attributes.QoS),
		// 	Callback: GatewaySubscribeSetAttributesResponseCallback,
		// },
		{
			Topic:    config.MqttConfig.Events.GatewaySubscribeTopic,
			Qos:      byte(config.MqttConfig.Events.QoS),
			Callback: GatewaySubscribeEventCallback,
		},
		// 网关命令响应已迁移到 MQTTAdapter.SubscribeResponseTopics
		// {
		// 	Topic:    config.MqttConfig.Commands.GatewaySubscribeTopic,
		// 	Qos:      byte(config.MqttConfig.Commands.QoS),
		// 	Callback: GatewaySubscribeCommandResponseCallback,
		// },
	}
}

func GatewaySubscribeTelemetryCallback(_ mqtt.Client, d mqtt.Message) {
	// 如果启用了Flow层且Adapter已注册，使用新的Flow处理流程
	if mqttAdapter != nil {
		logrus.WithFields(logrus.Fields{
			"topic": d.Topic(),
		}).Debug("Gateway telemetry routing to Flow layer")

		if err := mqttAdapter.HandleTelemetryMessage(d.Payload(), d.Topic()); err != nil {
			logrus.WithError(err).Error("Flow layer gateway telemetry processing failed")
		}
		return
	}

	// 否则使用原有的处理流程（兼容性保留）
	logrus.Debug("Gateway telemetry using legacy processing")
	err := pool.Submit(func() {
		// 处理消息
		GatewayTelemetryMessages(d.Payload(), d.Topic())
	})
	if err != nil {
		logrus.Error(err)
	}
}

func GatewaySubscribeAttributesCallback(_ mqtt.Client, d mqtt.Message) {
	// 如果启用了Flow层且Adapter已注册，使用新的Flow处理流程
	if mqttAdapter != nil {
		if err := mqttAdapter.HandleAttributeMessage(d.Payload(), d.Topic()); err != nil {
			logrus.WithError(err).Error("Flow layer gateway attribute processing failed")
		}
		return
	}

	// 否则使用原有的处理流程（兼容性保留）
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

func GatewaySubscribeSetAttributesResponseCallback(_ mqtt.Client, d mqtt.Message) {
	GatewayDeviceSetAttributesResponse(d.Payload(), d.Topic())
}

func GatewaySubscribeEventCallback(_ mqtt.Client, d mqtt.Message) {
	// 如果启用了Flow层且Adapter已注册，使用新的Flow处理流程
	if mqttAdapter != nil {
		if err := mqttAdapter.HandleEventMessage(d.Payload(), d.Topic()); err != nil {
			logrus.WithError(err).Error("Flow layer gateway event processing failed")
		}
		return
	}

	// 否则使用原有的处理流程（兼容性保留）
	messageId, deviceInfo, response, err := GatewayEventCallback(d.Payload(), d.Topic())
	logrus.Debug("响应设备事件上报", deviceInfo, err)
	if err != nil {
		logrus.Error(err)
	}
	if deviceInfo != nil && messageId != "" {
		publish.GatewayPublishResponseEventMessage(context.Background(), *deviceInfo, messageId, response)
	}
}

func GatewaySubscribeCommandResponseCallback(_ mqtt.Client, d mqtt.Message) {
	GatewayDeviceCommandResponse(d.Payload(), d.Topic())
}

// GatewaySubscribeTopic
// @description 网关批量订阅消息
// @return void
func GatewaySubscribeTopic() error {
	p, err := ants.NewPool(config.MqttConfig.Telemetry.PoolSize)
	if err != nil {
		logrus.Error("协程池创建失败")
		return err
	}
	pool = p
	for _, topic := range getSubscribeTopics() {
		topic.Topic = GenTopic(topic.Topic)
		logrus.Info("subscribe topic:", topic.Topic)
		if token := SubscribeMqttClient.Subscribe(topic.Topic, topic.Qos, topic.Callback); token.Wait() && token.Error() != nil {
			logrus.Error(token.Error())
			return token.Error()
		}
	}
	return nil
}
