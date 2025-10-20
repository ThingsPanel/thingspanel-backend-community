package mqttadapter

import (
	"fmt"

	"project/internal/downlink"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

// MQTTPublisher MQTT 发布适配器（实现 downlink.MessagePublisher 接口）
// 负责下行消息发送：command、attribute set
type MQTTPublisher struct {
	mqttClient mqtt.Client
	logger     *logrus.Logger
}

// 确保实现了接口
var _ downlink.MessagePublisher = (*MQTTPublisher)(nil)

// NewMQTTPublisher 创建 MQTT 发布器（使用 Adapter 的客户端）
func NewMQTTPublisher(mqttClient mqtt.Client, logger *logrus.Logger) *MQTTPublisher {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	return &MQTTPublisher{
		mqttClient: mqttClient,
		logger:     logger,
	}
}

// Publish 发布消息到 MQTT（实现 MessagePublisher 接口）
func (p *MQTTPublisher) Publish(topic string, qos byte, payload []byte) error {
	if p.mqttClient == nil {
		return fmt.Errorf("mqtt client is nil")
	}

	if !p.mqttClient.IsConnected() {
		return fmt.Errorf("mqtt client not connected")
	}

	p.logger.WithFields(logrus.Fields{
		"topic":   topic,
		"qos":     qos,
		"payload": string(payload),
	}).Debug("Publishing to MQTT")

	token := p.mqttClient.Publish(topic, qos, false, payload)
	token.Wait()

	if err := token.Error(); err != nil {
		p.logger.WithFields(logrus.Fields{
			"topic":   topic,
			"qos":     qos,
			"payload": string(payload),
			"error":   err,
		}).Error("MQTT publish failed")
		return err
	}

	p.logger.WithField("topic", topic).Debug("MQTT message published successfully")
	return nil
}
